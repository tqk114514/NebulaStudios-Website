//! 请求提取器（§2）：AuthUser。

use std::future::Future;

use axum::extract::FromRequestParts;
use axum::http::request::Parts;

use crate::api::{ACCESS_COOKIE, AppState, cookie_value};
use crate::error::ApiError;
use crate::model::tokens::verify_access;
use crate::model::user::UserId;

/// 已认证用户；由 access token 解析而来——cookie 优先，其次 `Authorization: Bearer`。
/// 只证明 JWT 有效，不查库；需要用户实时状态（封禁等）的端点必须自行查库。
#[derive(Debug, Clone, Copy)]
pub struct AuthUser {
    /// token 声明的用户标识。
    pub user_id: UserId,
}

// 解析全程同步，用 ready future 满足 trait 的 RPITIT 签名，不引入 async fn
impl FromRequestParts<AppState> for AuthUser {
    type Rejection = ApiError;

    fn from_request_parts(
        parts: &mut Parts,
        state: &AppState,
    ) -> impl Future<Output = Result<Self, Self::Rejection>> + Send {
        std::future::ready(auth_user_of(parts, state))
    }
}

fn auth_user_of(parts: &Parts, state: &AppState) -> Result<AuthUser, ApiError> {
    let token = access_token_of(parts).ok_or(ApiError::Unauthorized)?;
    let claims = verify_access(&token, state.jwt.decode()).map_err(|_| ApiError::Unauthorized)?;
    let id = uuid::Uuid::parse_str(&claims.sub).map_err(|_| ApiError::Unauthorized)?;
    Ok(AuthUser { user_id: UserId::from_uuid(id) })
}

fn access_token_of(parts: &Parts) -> Option<String> {
    if let Some(value) = cookie_value(&parts.headers, ACCESS_COOKIE) {
        return Some(value);
    }
    parts
        .headers
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|value| value.to_str().ok())
        .and_then(|header| header.strip_prefix("Bearer "))
        .map(ToOwned::to_owned)
}
