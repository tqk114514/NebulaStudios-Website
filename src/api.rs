//! HTTP API：路由装配与请求处理依赖；一个端点一个文件（§2 vertical slice）。

pub mod auth;
pub mod healthz;

use std::fmt;
use std::sync::Arc;

use axum::Router;
use axum::middleware::Next;
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use chrono::TimeDelta;
use tower_governor::GovernorLayer;
use tower_governor::governor::GovernorConfigBuilder;

use crate::context::Clock;
use crate::storage::sqlite::{SqliteRefreshRepo, SqliteUserRepo};

/// access 会话 cookie 名；全站路径。
pub const ACCESS_COOKIE: &str = "nebula_access";
/// refresh 会话 cookie 名；Path 限定在 `/api/auth`，其余路径不随请求携带。
pub const REFRESH_COOKIE: &str = "nebula_refresh";
/// CSRF 双提交 cookie 名；非 HttpOnly（SPA 读取后经 `X-CSRF-Token` 回显，ADR-0005）。
pub const CSRF_COOKIE: &str = "nebula_csrf";

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
    pub user_repo: SqliteUserRepo,
    /// refresh 家族仓储；与用户仓储共享连接池。
    pub refresh_repo: SqliteRefreshRepo,
    /// 时钟；所有时间取值经此（§2 时钟注入），handler 不得直接 `Utc::now()`。
    pub clock: Arc<dyn Clock>,
    /// JWT 密钥对。
    pub jwt: JwtKeys,
    /// 会话时长。
    pub session: SessionPolicy,
}

impl fmt::Debug for AppState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("AppState")
            .field("user_repo", &self.user_repo)
            .field("refresh_repo", &self.refresh_repo)
            .field("session", &self.session)
            .finish_non_exhaustive()
    }
}

/// 组装全部路由；新增端点在此登记。
/// 限额路由按端点分组：governor 在内层按 IP 拦断，信封化中间件在外层把
/// 其 429 纯文本体改写为 ADR-0004 统一信封。
pub fn router(state: AppState) -> Router {
    let open = Router::new()
        .route("/healthz", get(healthz::healthz))
        .route("/api/auth/refresh", post(auth::refresh::refresh))
        .route("/api/auth/logout", post(auth::logout::logout));

    // 限额参数沿用 v3：注册突发 3、每 20 秒补 1；登录突发 5、每 12 秒补 1。
    // per_second 语义是「补 1 个令牌的间隔秒数」（tower_governor 命名反直觉）。
    // finish() 对常量参数必然 Some；None 只可能是实现笔误，启动即暴露优于带病运行。
    let register_limited = Router::new()
        .route("/api/auth/register", post(auth::register::register))
        .layer(GovernorLayer::new(
            GovernorConfigBuilder::default()
                .per_second(20)
                .burst_size(3)
                .finish()
                .unwrap_or_else(|| unreachable!("注册限额常量非法")),
        ))
        .layer(axum::middleware::from_fn(envelope_rate_limited));

    let login_limited = Router::new()
        .route("/api/auth/login", post(auth::login::login))
        .layer(GovernorLayer::new(
            GovernorConfigBuilder::default()
                .per_second(12)
                .burst_size(5)
                .finish()
                .unwrap_or_else(|| unreachable!("登录限额常量非法")),
        ))
        .layer(axum::middleware::from_fn(envelope_rate_limited));

    open.merge(register_limited).merge(login_limited).with_state(state)
}

/// `tower_governor` 的 429 是纯文本体，不符合 ADR-0004；改写为统一信封，
/// `retry_after` 取自其 `retry-after` 头（该头默认启用）。命中同时打 warn——
/// 指标体系（§9.8）接入前的可观测替代。
async fn envelope_rate_limited(request: axum::extract::Request, next: Next) -> Response {
    let response = next.run(request).await;
    if response.status() != axum::http::StatusCode::TOO_MANY_REQUESTS {
        return response;
    }
    let retry_after = response
        .headers()
        .get("retry-after")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.parse::<u64>().ok());
    tracing::warn!(?retry_after, "rate limited");

    let (mut parts, _) = response.into_parts();
    // 原 text 体的长度头对新 body 不再适用，移除后由 Json 重建
    parts.headers.remove(axum::http::header::CONTENT_LENGTH);
    let body = crate::error::ErrorBody {
        code: "rate_limited",
        message: "请求过于频繁".to_owned(),
        details: retry_after.map(|secs| serde_json::json!({ "retry_after": secs })),
    };
    (parts.status, parts.headers, axum::Json(body)).into_response()
}
