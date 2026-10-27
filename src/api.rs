//! HTTP API：路由装配与请求处理依赖；一个端点一个文件（§2 vertical slice）。

pub mod auth;
pub mod healthz;
pub mod metrics;
pub mod oauth;
pub mod user;

use std::fmt;
use std::sync::Arc;

use axum::Json;
use axum::Router;
use axum::extract::DefaultBodyLimit;
use axum::http::header::SET_COOKIE;
use axum::http::{HeaderMap, HeaderName, HeaderValue, Method, StatusCode, header};
use axum::middleware;
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use chrono::TimeDelta;
use tower_governor::GovernorLayer;
use tower_governor::errors::GovernorError;
use tower_governor::governor::GovernorConfigBuilder;
use tower_http::cors::CorsLayer;
use tower_http::request_id::{
    MakeRequestId as TowerMakeRequestId, PropagateRequestIdLayer, RequestId as TowerRequestId,
    SetRequestIdLayer,
};
use tower_http::set_header::SetResponseHeaderLayer;

use crate::context::{Clock, RequestId};
use crate::mailer::Mailer;
use crate::storage::{
    AccountRepo, EmailVerificationRepo, MaintenanceRepo, OAuthRepo, RefreshTokenRepo, TotpRepo,
    UserRepo,
};

/// access 会话 cookie 名；全站路径。
pub const ACCESS_COOKIE: &str = "nebula_access";
/// refresh 会话 cookie 名；Path 限定在 `/api/auth`，其余路径不随请求携带。
pub const REFRESH_COOKIE: &str = "nebula_refresh";
/// CSRF 双提交 cookie 名；非 HttpOnly（SPA 读取后经 `X-CSRF-Token` 回显，ADR-0005）。
pub const CSRF_COOKIE: &str = "nebula_csrf";
/// 请求 ID 头；入站透传与出站回显共用。
pub const REQUEST_ID_HEADER: &str = "x-request-id";
/// 全局请求体上限：当前认证端点均为小 JSON，上传路由将来单独放宽。
pub const MAX_BODY_BYTES: usize = 64 * 1024;

/// 生成 uuid v7 请求 ID；v7 时间有序，日志按字面排序即按时间排序。
#[derive(Debug, Clone, Copy, Default)]
struct MakeV7RequestId;

impl TowerMakeRequestId for MakeV7RequestId {
    fn make_request_id<B>(&mut self, _request: &axum::http::Request<B>) -> Option<TowerRequestId> {
        let value = RequestId::new().to_string().parse::<HeaderValue>().ok()?;
        Some(TowerRequestId::new(value))
    }
}

/// 从请求 Cookie 头解析指定名字的值；提取器与端点共用同一解析规则。
#[must_use]
pub fn cookie_value(headers: &axum::http::HeaderMap, name: &str) -> Option<String> {
    let prefix = format!("{name}=");
    headers.get(axum::http::header::COOKIE).and_then(|value| value.to_str().ok()).and_then(
        |cookies| {
            cookies
                .split(';')
                .map(str::trim)
                .find_map(|pair| pair.strip_prefix(&prefix).map(ToOwned::to_owned))
        },
    )
}

/// cookie 的 Max-Age 取整秒；负值（异常配置）钳到 0。
pub(crate) fn cookie_max_age(ttl: TimeDelta) -> u64 {
    ttl.num_seconds().max(0).cast_unsigned()
}

/// 追加 access cookie 到响应头。
pub(crate) fn set_access_cookie(headers: &mut HeaderMap, token: &str, max_age_secs: u64) {
    append_cookie(
        headers,
        &format!(
            "{ACCESS_COOKIE}={token}; Path=/; Max-Age={max_age_secs}; HttpOnly; Secure; SameSite=Lax"
        ),
    );
}

/// 追加 csrf cookie 到响应头；非 HttpOnly——SPA 要读取它经头回显（ADR-0005）。
pub(crate) fn set_csrf_cookie(headers: &mut HeaderMap, token: &str, max_age_secs: u64) {
    append_cookie(
        headers,
        &format!("{CSRF_COOKIE}={token}; Path=/; Max-Age={max_age_secs}; Secure; SameSite=Lax"),
    );
}

/// 追加 refresh cookie 到响应头（路径收窄到 /api/auth）。
pub(crate) fn set_refresh_cookie(headers: &mut HeaderMap, token: &str, max_age_secs: u64) {
    append_cookie(
        headers,
        &format!(
            "{REFRESH_COOKIE}={token}; Path=/api/auth; Max-Age={max_age_secs}; HttpOnly; Secure; SameSite=Lax"
        ),
    );
}

/// 追加三条失效 cookie（Max-Age=0）清除会话。
pub(crate) fn clear_session_cookies(headers: &mut HeaderMap) {
    append_cookie(
        headers,
        &format!("{ACCESS_COOKIE}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax"),
    );
    append_cookie(
        headers,
        &format!("{REFRESH_COOKIE}=; Path=/api/auth; Max-Age=0; HttpOnly; Secure; SameSite=Lax"),
    );
    append_cookie(headers, &format!("{CSRF_COOKIE}=; Path=/; Max-Age=0; Secure; SameSite=Lax"));
}

fn append_cookie(headers: &mut HeaderMap, cookie: &str) {
    // cookie 各组成部分均为 token 安全字符，parse 失败只可能是实现 bug
    match cookie.parse::<HeaderValue>() {
        Ok(value) => {
            headers.append(SET_COOKIE, value);
        }
        Err(_) => unreachable!("cookie 值含非法头字符: {cookie}"),
    }
}

/// JWT 密钥对（ES256）；从配置指定的 PEM 文件加载。
#[derive(Clone)]
pub struct JwtKeys {
    encode: jsonwebtoken::EncodingKey,
    decode: jsonwebtoken::DecodingKey,
}

/// 密钥加载失败（PEM 非法或算法不符）。
#[derive(Debug, thiserror::Error)]
#[error("JWT 密钥加载失败: {0}")]
pub struct LoadJwtKeysError(#[from] jsonwebtoken::errors::Error);

impl JwtKeys {
    /// 从 PEM 字节构建；私钥须为 PKCS#8（§1 API 约束），公钥为普通 EC 公钥。
    ///
    /// # Errors
    /// 任一 PEM 解析失败时返回错误。
    pub fn from_pem(private: &[u8], public: &[u8]) -> Result<Self, LoadJwtKeysError> {
        Ok(Self {
            encode: jsonwebtoken::EncodingKey::from_ec_pem(private)?,
            decode: jsonwebtoken::DecodingKey::from_ec_pem(public)?,
        })
    }

    /// 签发密钥。
    #[must_use]
    pub fn encode(&self) -> &jsonwebtoken::EncodingKey {
        &self.encode
    }

    /// 校验密钥。
    #[must_use]
    pub fn decode(&self) -> &jsonwebtoken::DecodingKey {
        &self.decode
    }
}

impl fmt::Debug for JwtKeys {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("JwtKeys").finish_non_exhaustive()
    }
}

/// 会话时长策略；来自配置，启动时定死。
#[derive(Debug, Clone, Copy)]
pub struct SessionPolicy {
    /// access token 有效期。
    pub access_ttl: TimeDelta,
    /// refresh token 有效期。
    pub refresh_ttl: TimeDelta,
}

/// 请求处理依赖集合；克隆共享连接池、时钟与密钥。
#[derive(Clone)]
pub struct AppState {
    /// 用户仓储。
    pub user_repo: Arc<dyn UserRepo>,
    /// refresh 家族仓储；与用户仓储共享连接池。
    pub refresh_repo: Arc<dyn RefreshTokenRepo>,
    /// 时钟；所有时间取值经此（§2 时钟注入），handler 不得直接 `Utc::now()`。
    pub clock: Arc<dyn Clock>,
    /// JWT 密钥对。
    pub jwt: JwtKeys,
    /// 会话时长。
    pub session: SessionPolicy,
    /// 邮箱验证码仓储；与用户仓储共享连接池。
    pub verification_repo: Arc<dyn EmailVerificationRepo>,
    /// 账户删除仓储；与用户仓储共享连接池。
    pub account_repo: Arc<dyn AccountRepo>,
    /// TOTP 仓储；与用户仓储共享连接池。
    pub totp_repo: Arc<dyn TotpRepo>,
    /// `OAuth2` 仓储；与用户仓储共享连接池。
    pub oauth_repo: Arc<dyn OAuthRepo>,
    /// 过期数据清理仓储；与用户仓储共享连接池。
    pub maintenance_repo: Arc<dyn MaintenanceRepo>,
    /// 邮件端口。
    pub mailer: Arc<dyn Mailer>,
    /// Prometheus 指标句柄；测试装配为 `None`，`/metrics` 返回 404。
    pub metrics: Option<Arc<axum_prometheus::metrics_exporter_prometheus::PrometheusHandle>>,
    /// CORS 允许的来源；`None` 表示同源部署、不启用 CORS。
    pub cors_allowed_origin: Option<HeaderValue>,
}

impl fmt::Debug for AppState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("AppState").field("session", &self.session).finish_non_exhaustive()
    }
}

/// 组装全部路由；新增端点在此登记。限额层以对端 IP 为键，429 经
/// `governor_error_response` 直接产出 ADR-0004 信封。
pub fn router(state: AppState) -> Router {
    let cors_allowed_origin = state.cors_allowed_origin.clone();
    let open = Router::new()
        .route("/healthz", get(healthz::healthz))
        .route("/metrics", get(metrics::metrics))
        .route("/api/auth/refresh", post(auth::refresh::refresh))
        .route("/api/auth/logout", post(auth::logout::logout))
        .route("/api/auth/password/change", post(auth::change_password::change_password))
        .route("/api/auth/account/delete/request", post(auth::delete_request::delete_request))
        .route("/api/auth/login/totp", post(auth::login_totp::login_totp))
        .route("/api/user/totp/setup", post(user::totp_setup::setup))
        .route("/api/user/totp/enable", post(user::totp_enable::enable))
        .route("/api/user/totp/disable", post(user::totp_disable::disable))
        .route("/api/oauth/authorize", get(oauth::authorize::authorize))
        .route("/api/oauth/authorize/consent", post(oauth::authorize::consent))
        .route("/api/oauth/token", post(oauth::token::token));

    // v3 参数：密码重置发码突发 3、每 20 秒补 1
    let reset_limited = Router::new()
        .route("/api/auth/password/reset/request", post(auth::password_reset::reset_request))
        .route("/api/auth/password/reset/confirm", post(auth::password_reset::reset_confirm))
        .layer(
            GovernorLayer::new(
                GovernorConfigBuilder::default()
                    .per_second(20)
                    .burst_size(3)
                    .finish()
                    .unwrap_or_else(|| unreachable!("重置限额常量非法")),
            )
            .error_handler(|error| governor_error_response(&error)),
        );

    // 限额参数沿用 v3：注册突发 3、每 20 秒补 1（per_second 语义是「补 1 个
    // 令牌的间隔秒数」，tower_governor 命名反直觉）；finish() 对常量参数必然
    // Some，None 只可能是实现笔误，启动即暴露优于带病运行。
    let register_limited = Router::new()
        .route("/api/auth/register", post(auth::register::register))
        .route("/api/auth/register/resend", post(auth::resend::resend))
        .layer(
            GovernorLayer::new(
                GovernorConfigBuilder::default()
                    .per_second(20)
                    .burst_size(3)
                    .finish()
                    .unwrap_or_else(|| unreachable!("注册限额常量非法")),
            )
            .error_handler(|error| governor_error_response(&error)),
        );

    // v3 参数：登录突发 5、每 12 秒补 1
    let login_limited = Router::new().route("/api/auth/login", post(auth::login::login)).layer(
        GovernorLayer::new(
            GovernorConfigBuilder::default()
                .per_second(12)
                .burst_size(5)
                .finish()
                .unwrap_or_else(|| unreachable!("登录限额常量非法")),
        )
        .error_handler(|error| governor_error_response(&error)),
    );

    // v3 参数：验证码校验突发 5、每 10 秒补 1
    let verify_limited = Router::new()
        .route("/api/auth/register/verify", post(auth::verify::verify))
        .route("/api/auth/account/delete/confirm", post(auth::delete_confirm::delete_confirm))
        .layer(
            GovernorLayer::new(
                GovernorConfigBuilder::default()
                    .per_second(10)
                    .burst_size(5)
                    .finish()
                    .unwrap_or_else(|| unreachable!("验证限额常量非法")),
            )
            .error_handler(|error| governor_error_response(&error)),
        );

    let mut router = open
        .merge(register_limited)
        .merge(login_limited)
        .merge(verify_limited)
        .merge(reset_limited)
        .with_state(state)
        .layer(DefaultBodyLimit::max(MAX_BODY_BYTES))
        .layer(middleware::map_response(envelope_payload_too_large));

    if let Some(origin) = cors_allowed_origin {
        router = router.layer(cors_layer(origin));
    }

    router = security_headers(router);

    let request_id_header = HeaderName::from_static(REQUEST_ID_HEADER);
    router
        .layer(PropagateRequestIdLayer::new(request_id_header.clone()))
        .layer(SetRequestIdLayer::new(request_id_header, MakeV7RequestId))
}

fn cors_layer(origin: HeaderValue) -> CorsLayer {
    CorsLayer::new()
        .allow_origin(origin)
        .allow_credentials(true)
        .allow_methods([
            Method::GET,
            Method::POST,
            Method::PUT,
            Method::PATCH,
            Method::DELETE,
            Method::OPTIONS,
        ])
        .allow_headers([
            header::AUTHORIZATION,
            header::CONTENT_TYPE,
            HeaderName::from_static("x-csrf-token"),
            HeaderName::from_static(REQUEST_ID_HEADER),
        ])
        .expose_headers([HeaderName::from_static(REQUEST_ID_HEADER)])
}

fn security_headers(mut router: Router) -> Router {
    for (name, value) in [
        ("x-content-type-options", "nosniff"),
        ("referrer-policy", "no-referrer"),
        ("permissions-policy", "camera=(), microphone=(), geolocation=()"),
        ("x-frame-options", "DENY"),
        ("content-security-policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'"),
    ] {
        router = router.layer(SetResponseHeaderLayer::if_not_present(
            HeaderName::from_static(name),
            HeaderValue::from_static(value),
        ));
    }
    router
}

/// 把 axum 的裸 413 换成 ADR-0004 信封；外层安全头/请求 ID 层仍会附加。
async fn envelope_payload_too_large(response: Response) -> Response {
    if response.status() == StatusCode::PAYLOAD_TOO_LARGE {
        crate::error::ApiError::PayloadTooLarge.into_response()
    } else {
        response
    }
}

/// 限额 429 的统一信封；`wait_time` 直接进 `details.retry_after`（ADR-0004）。
/// 命中同时打 warn 并计数；键提取失败（缺连接信息）按 500 信封处理：
/// 属装配错误而非客户端问题。
fn governor_error_response(error: &GovernorError) -> Response {
    if let GovernorError::TooManyRequests { wait_time, .. } = error {
        ::metrics::counter!("nebula_rate_limit_hits_total").increment(1);
        tracing::warn!(retry_after = wait_time, "rate limited");
        let body = crate::error::ErrorBody {
            code: "rate_limited",
            message: "请求过于频繁".to_owned(),
            details: Some(serde_json::json!({ "retry_after": wait_time })),
        };
        return (StatusCode::TOO_MANY_REQUESTS, Json(body)).into_response();
    }
    tracing::error!(%error, "限额键提取失败");
    let body = crate::error::ErrorBody {
        code: "internal_error",
        message: "internal server error".to_owned(),
        details: None,
    };
    (StatusCode::INTERNAL_SERVER_ERROR, Json(body)).into_response()
}
