//! HTTP API：路由装配与请求处理依赖；一个端点一个文件（§2 vertical slice）。

pub mod admin;
pub mod auth;
pub mod healthz;
pub mod metrics;
pub mod oauth;
pub mod policy;
pub mod user;

use std::collections::HashSet;
use std::fmt;
use std::sync::Arc;

use axum::Json;
use axum::Router;
use axum::extract::DefaultBodyLimit;
use axum::http::header::SET_COOKIE;
use axum::http::{HeaderMap, HeaderName, HeaderValue, Method, StatusCode, header};
use axum::middleware;
use axum::response::{IntoResponse, Response};
use axum::routing::{delete, get, patch, post};
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

use crate::avatar::{AvatarStore, MAX_REQUEST_BYTES};
use crate::context::{Clock, RequestId};
use crate::crypto::{CodeHasher, SecretCipher};
use crate::error::{ApiError, FieldInvalid};
use crate::mailer::{
    EmailMessage, Locale, Mailer, SecurityNotice, security_notice, verification_code,
};
use crate::model::audit::{AuditAction, NewAuditEntry};
use crate::model::policy::PolicyCatalog;
use crate::model::user::{User, UserId};
use crate::model::verification::{VERIFICATION_TTL, VerificationCode, VerificationPurpose};
use crate::social::SocialClients;
use crate::storage::{
    AccountRepo, AdminRepo, ConsentRepo, EmailChangeRepo, EmailVerificationRepo, ExportRepo,
    MaintenanceRepo, OAuthRepo, RefreshTokenRepo, SocialRepo, TotpRepo, UserRepo,
};
use crate::turnstile::TurnstileVerifier;

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
/// 口令输入上限；只为挡住异常长度进入 Argon2，不代替强度规则。
pub(crate) const PASSWORD_INPUT_LIMIT: usize = 128;

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
    /// 管理后台仓储：统计、用户列表、封禁/解封与审计。
    pub admin_repo: Arc<dyn AdminRepo>,
    /// 社交身份与 OAuth 中转态仓储（ADR-0012）。
    pub social_repo: Arc<dyn SocialRepo>,
    /// 数据导出一次性下载令牌仓储。
    pub export_repo: Arc<dyn ExportRepo>,
    /// 政策同意记录仓储。
    pub consent_repo: Arc<dyn ConsentRepo>,
    /// 邮箱变更待确认请求仓储。
    pub email_change_repo: Arc<dyn EmailChangeRepo>,
    /// 启动时加载并校验的政策目录；空目录表示政策中心未配置。
    pub policy_catalog: Arc<PolicyCatalog>,
    /// Turnstile 校验端口；`None` 表示未启用。
    pub turnstile: Option<Arc<dyn TurnstileVerifier>>,
    /// 前端渲染 Turnstile 组件用的公开 site key；未启用时 `None`。
    pub turnstile_site_key: Option<String>,
    /// 头像文件存储；上传、公开读取与注销清理共用。
    pub avatar_store: Arc<AvatarStore>,
    /// 已启用的社交 provider 客户端；未启用的 provider 不在表内。
    pub social_clients: Arc<SocialClients>,
    /// 社交回跳后跳转的前端基地址（`social.base_url`）；社交未启用时为空串。
    pub social_base_url: String,
    /// 配置化管理员邮箱（已 trim + 小写）；`AdminUser` 提取器据此判定。
    pub admin_emails: Arc<HashSet<String>>,
    /// 允许注册的邮箱域名（小写、精确匹配）；空集合表示不限制。
    pub allowed_email_domains: Arc<HashSet<String>>,
    /// TOTP 密钥静态加密端口。
    pub secret_cipher: Arc<dyn SecretCipher>,
    /// 低熵验证码的 HMAC pepper 摘要端口。
    pub code_hasher: Arc<dyn CodeHasher>,
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

/// 发送安全变更通知到账户邮箱。异步发送、失败只记日志，不影响已经完成的状态
/// 变更——状态已是事实，通知只是告知，处置入口在其它端点。
pub(crate) fn send_security_notice(
    state: &AppState,
    to: &str,
    notice: SecurityNotice,
    locale: Locale,
) {
    let content = security_notice(locale, notice);
    let mailer = state.mailer.clone();
    let message = EmailMessage { to: to.to_owned(), subject: content.subject, body: content.body };
    tokio::spawn(async move {
        if let Err(err) = mailer.send(message).await {
            tracing::warn!(error = %err, "安全通知邮件发送失败");
        }
    });
}

/// 发送验证码邮件到指定地址；发送失败只记日志，不阻塞已完成的状态变更。
pub(crate) fn send_code_email(
    state: &AppState,
    to: &str,
    purpose: VerificationPurpose,
    code: &VerificationCode,
    locale: Locale,
) {
    let content = verification_code(locale, purpose, code.as_str(), VERIFICATION_TTL.num_minutes());
    let mailer = state.mailer.clone();
    let message = EmailMessage { to: to.to_owned(), subject: content.subject, body: content.body };
    tokio::spawn(async move {
        if let Err(err) = mailer.send(message).await {
            tracing::warn!(error = %err, "验证邮件发送失败");
        }
    });
}

/// 头像公开读取地址；版本为 0 表示从未上传。
///
/// 路径与版本参数属于 HTTP 契约，故放在 `api/` 父模块供资料与用户列表共用
/// （§2 依赖方向 3）。
pub(crate) fn avatar_url(user_id: &UserId, version: i64) -> Option<String> {
    (version > 0).then(|| format!("/api/users/{user_id}/avatar?v={version}"))
}

/// 启用 Turnstile 时校验请求携带的 token；未启用直接通过。
///
/// Token 缺失与校验未通过同体返回 422，避免把 Cloudflare 的判定细节外泄；
/// 校验请求本身失败返回 503，由前端提示稍后再试。
///
/// # Errors
/// 启用时缺 token 或校验未通过返回 422；校验服务不可用返回 503。
pub(crate) async fn verify_turnstile(
    state: &AppState,
    token: Option<&str>,
) -> Result<(), ApiError> {
    let Some(verifier) = &state.turnstile else {
        return Ok(());
    };
    let Some(token) = token.map(str::trim).filter(|token| !token.is_empty()) else {
        return Err(turnstile_invalid());
    };
    let outcome = verifier.verify(token).await.map_err(|err| {
        tracing::error!(error = %err, "Turnstile 校验请求失败");
        ApiError::TurnstileUnavailable
    })?;
    if outcome.success {
        Ok(())
    } else {
        tracing::warn!(error_codes = ?outcome.error_codes, "Turnstile 校验未通过");
        Err(turnstile_invalid())
    }
}

fn turnstile_invalid() -> ApiError {
    ApiError::Validation(vec![FieldInvalid {
        field: "turnstile_token",
        code: "turnstile_invalid".to_owned(),
    }])
}

/// 追加一条审计记录；失败只记日志，不回滚已经完成的业务动作。
///
/// 用户事件与管理事件共用 `audit_logs`（AGENTS §0）；审计失败意味着该事件
/// 没有留痕，调用方不得把它当作业务失败，排查依赖日志中的 `action` 与 `user_id`。
pub(crate) async fn record_audit(
    state: &AppState,
    actor_id: UserId,
    action: AuditAction,
    target_id: Option<UserId>,
    details: Option<serde_json::Value>,
) {
    let entry = NewAuditEntry { actor_id, action, target_id, details };
    if let Err(err) = state.admin_repo.record_audit(&entry, state.clock.now()).await {
        tracing::warn!(
            error = ?err,
            action = action.as_str(),
            user_id = %actor_id,
            "审计写入失败，事件未留痕"
        );
    }
}

/// 解析路径中的社交 provider；未知值按「未启用」同体返回，避免泄露配置细节。
pub(crate) fn parse_social_provider(
    raw: &str,
) -> Result<crate::model::social::SocialProvider, ApiError> {
    crate::model::social::SocialProvider::from_str_raw(raw).ok_or(ApiError::SocialProviderDisabled)
}

/// 校验当前口令并返回用户快照；口令错误 401，用户缺失/存储故障 500。
pub(crate) async fn verify_current_password(
    state: &AppState,
    user_id: &UserId,
    password: &str,
) -> Result<User, ApiError> {
    // 与登录共用同一输入上限；超长直接按凭据错误拒绝，不进入 Argon2
    if password.len() > PASSWORD_INPUT_LIMIT {
        return Err(ApiError::InvalidCredentials);
    }
    let Some(credentials) = state
        .user_repo
        .find_by_id(user_id)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?
    else {
        return Err(ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {user_id}")));
    };
    let plain = password.to_owned();
    let ok = tokio::task::spawn_blocking(move || credentials.password_hash.verify(&plain))
        .await
        .map_err(anyhow::Error::from)?
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    if ok { Ok(credentials.user) } else { Err(ApiError::InvalidCredentials) }
}

/// 组装全部路由；新增端点在此登记。限额层以对端 IP 为键，429 经
/// `governor_error_response` 直接产出 ADR-0004 信封。
pub fn router(state: AppState) -> Router {
    let cors_allowed_origin = state.cors_allowed_origin.clone();
    let open = Router::new()
        .route("/healthz", get(healthz::healthz))
        .route("/metrics", get(metrics::metrics))
        .route(
            "/api/auth/logout",
            post(auth::logout::logout)
                .layer(middleware::map_response(clear_cookies_on_auth_failure)),
        )
        .route("/api/user", get(user::profile::profile))
        .route("/api/admin/stats", get(admin::stats::stats))
        .route("/api/admin/users", get(admin::users::users))
        .route("/api/admin/users/{user_id}/ban", post(admin::ban::ban).delete(admin::unban::unban))
        .route("/api/admin/audit", get(admin::audit::audit))
        .route("/api/auth/{provider}", get(auth::social::start))
        .route("/api/auth/{provider}/callback", get(auth::social::callback))
        .route("/api/auth/turnstile", get(auth::turnstile::settings))
        .route("/api/policies", get(policy::policies))
        .route("/api/users/{user_id}/avatar", get(user::avatar_serve::avatar))
        .route("/api/user/export/{token}", get(user::export_download::download_export))
        .route("/api/user/consents", get(user::consents::list).post(user::consents::record))
        .route("/api/user/oauth/grants", get(user::oauth_grants::list))
        .route("/api/user/oauth/grants/{client_id}", delete(user::oauth_grants::revoke))
        .route("/api/user/social", get(user::social::list))
        .route("/api/user/social/{provider}", post(user::social::link).delete(user::social::unlink))
        .route("/api/oauth/authorize", get(oauth::authorize::authorize))
        .route("/api/oauth/authorize/consent", post(oauth::authorize::consent))
        .route("/api/oauth/token", post(oauth::token::token));

    let mut router = open
        .merge(limited_routes())
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

/// 按端点风险装配 GCRA 限额；参数沿用 v3（`per_second` 是补 1 个令牌的
/// 间隔秒数，`tower_governor` 命名反直觉）。
fn limited_routes() -> Router<AppState> {
    // 注册：突发 3、每 20 秒补 1
    let register_limited = limited(
        Router::new()
            .route("/api/auth/register", post(auth::register::register))
            .route("/api/auth/register/resend", post(auth::resend::resend)),
        20,
        3,
    );

    // 登录：突发 5、每 12 秒补 1
    let login_limited =
        limited(Router::new().route("/api/auth/login", post(auth::login::login)), 12, 5);

    // 改密与用户名修改：验证当前口令并可能重算哈希；突发 5、每 10 秒补 1
    let password_change_limited = limited(
        Router::new()
            .route("/api/auth/password/change", post(auth::change_password::change_password))
            .route("/api/user/username", patch(user::username::update_username)),
        10,
        5,
    );

    // 验证码校验：突发 5、每 10 秒补 1
    let verify_limited = limited(
        Router::new()
            .route("/api/auth/register/verify", post(auth::verify::verify))
            .route("/api/auth/account/delete/confirm", post(auth::delete_confirm::delete_confirm))
            .route(
                "/api/user/email/change/confirm",
                post(user::email_change::confirm_email_change),
            ),
        10,
        5,
    );

    // 密码重置发码：突发 3、每 20 秒补 1
    let reset_limited = limited(
        Router::new()
            .route("/api/auth/password/reset/request", post(auth::password_reset::reset_request))
            .route("/api/auth/password/reset/confirm", post(auth::password_reset::reset_confirm)),
        20,
        3,
    );

    // TOTP 二段与账户 TOTP 管理：突发 10、每 2 秒补 1；挑战本身另有 5 次上限
    let totp_limited = limited(
        Router::new()
            .route("/api/auth/login/totp", post(auth::login_totp::login_totp))
            .route("/api/user/totp/setup", post(user::totp_setup::setup))
            .route("/api/user/totp/enable", post(user::totp_enable::enable))
            .route("/api/user/totp/disable", post(user::totp_disable::disable)),
        2,
        10,
    );

    // 注销发码与邮箱变更申请会触发外发邮件，限额与注册/重置发码同档（突发 3、每 20 秒补 1）
    let delete_request_limited = limited(
        Router::new()
            .route("/api/auth/account/delete/request", post(auth::delete_request::delete_request))
            .route(
                "/api/user/email/change/request",
                post(user::email_change::request_email_change),
            ),
        20,
        3,
    );

    // 头像上传：解码与重编码是 CPU 工作，单独放宽请求体上限；突发 5、每 10 秒补 1
    let avatar_limited = limited(
        Router::new()
            .route("/api/user/avatar", post(user::avatar_upload::upload))
            .layer(DefaultBodyLimit::max(MAX_REQUEST_BYTES)),
        10,
        5,
    );

    // 数据导出申请写库并返回一次性链接；突发 3、每 20 秒补 1
    let export_limited = limited(
        Router::new().route("/api/user/export", post(user::export_request::request_export)),
        20,
        3,
    );

    // 刷新可被匿名触发并放大一次 DB 查询与潜在的整族撤销，故限额挡放大：
    // 突发 30、每 1 秒补 1——正常客户端刷新频率远低于此（access TTL 默认 1 小时）
    let refresh_limited = limited(
        Router::new()
            .route("/api/auth/refresh", post(auth::refresh::refresh))
            .layer(middleware::map_response(clear_cookies_on_forbidden)),
        1,
        30,
    );

    register_limited
        .merge(login_limited)
        .merge(password_change_limited)
        .merge(verify_limited)
        .merge(reset_limited)
        .merge(totp_limited)
        .merge(delete_request_limited)
        .merge(avatar_limited)
        .merge(export_limited)
        .merge(refresh_limited)
}

/// 给子路由挂 GCRA 限额层；`per_second` 是补 1 个令牌的间隔秒数，
/// 429 经 `governor_error_response` 产出统一信封（ADR-0004）。
///
/// `finish()` 对合法参数必然 `Some`，`None` 只可能是调用侧写错常量，
/// 启动即暴露优于带病运行。
fn limited(routes: Router<AppState>, per_second: u64, burst_size: u32) -> Router<AppState> {
    routes.layer(
        GovernorLayer::new(
            GovernorConfigBuilder::default()
                .per_second(per_second)
                .burst_size(burst_size)
                .finish()
                .unwrap_or_else(|| {
                    unreachable!("限额参数非法: per_second={per_second} burst_size={burst_size}")
                }),
        )
        .error_handler(|error| governor_error_response(&error)),
    )
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

/// 登出与刷新在提取器阶段被 CSRF 拒绝时 handler 不会执行；按 ADR-0005 决策 5
/// 补发清会话 cookie 头。这两个路由的 403 只可能来自认证/CSRF 门禁。
#[allow(clippy::unused_async)] // axum map_response 只接受 async fn
async fn clear_cookies_on_forbidden(response: Response) -> Response {
    clear_cookies_if(response, |status| status == StatusCode::FORBIDDEN)
}

/// 登出的 401（缺失/过期 access）与 403（CSRF/封禁）都发生在 handler 之前，
/// 同样按 ADR-0005 决策 5 清掉死 cookie；刷新 401 已由 handler 自行清理。
#[allow(clippy::unused_async)] // axum map_response 只接受 async fn
async fn clear_cookies_on_auth_failure(response: Response) -> Response {
    clear_cookies_if(response, |status| {
        status == StatusCode::UNAUTHORIZED || status == StatusCode::FORBIDDEN
    })
}

fn clear_cookies_if(response: Response, should_clear: impl FnOnce(StatusCode) -> bool) -> Response {
    if !should_clear(response.status()) {
        return response;
    }
    let (mut parts, body) = response.into_parts();
    clear_session_cookies(&mut parts.headers);
    Response::from_parts(parts, body)
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
