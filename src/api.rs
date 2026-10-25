//! HTTP API：路由装配与请求处理依赖；一个端点一个文件（§2 vertical slice）。

pub mod auth;
pub mod healthz;

use std::fmt;
use std::sync::Arc;

use axum::Router;
use axum::routing::{get, post};
use chrono::TimeDelta;

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
pub fn router(state: AppState) -> Router {
    Router::new()
        .route("/healthz", get(healthz::healthz))
        .route("/api/auth/register", post(auth::register::register))
        .route("/api/auth/login", post(auth::login::login))
        .route("/api/auth/refresh", post(auth::refresh::refresh))
        .route("/api/auth/logout", post(auth::logout::logout))
        .with_state(state)
}
