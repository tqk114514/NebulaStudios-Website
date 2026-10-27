//! 请求提取器（§2）：第一方用户会话与 CSRF 门禁（ADR-0005、ADR-0006）。

use std::future::Future;

use axum::extract::FromRequestParts;
use axum::http::Method;
use axum::http::request::Parts;

use crate::api::{ACCESS_COOKIE, AppState, CSRF_COOKIE, cookie_value};
use crate::error::ApiError;
use crate::model::tokens::{VerifiedAccessToken, verify_access};
use crate::model::user::UserId;

/// 回显 CSRF 值的请求头。
pub const CSRF_HEADER: &str = "x-csrf-token";

/// 第一方用户会话；由 access token 解析而来——cookie 优先，其次
/// `Authorization: Bearer`。只接受 `aud = nebula:user-session` 的令牌，
/// OAuth 客户端授权令牌在此不可表达（ADR-0006）。
///
/// 只证明 JWT 有效，不查库；需要用户实时状态（封禁等）的端点必须自行查库。
///
/// CSRF 门禁内建于此（ADR-0005）：cookie 来源的认证遇写方法必须通过
/// `verify_csrf`，Bearer 来源与读方法豁免——经本提取器的未来写端点自动合规。
#[derive(Debug, Clone, Copy)]
pub struct FirstPartyUser {
    /// token 声明的用户标识。
    pub user_id: UserId,
}

// 解析全程同步，用 ready future 满足 trait 的 RPITIT 签名，不引入 async fn
impl FromRequestParts<AppState> for FirstPartyUser {
    type Rejection = ApiError;

    fn from_request_parts(
        parts: &mut Parts,
        state: &AppState,
    ) -> impl Future<Output = Result<Self, Self::Rejection>> + Send {
        std::future::ready(first_party_user_of(parts, state))
    }
}

/// 独立 CSRF 门禁：不经 [`FirstPartyUser`] 的 cookie 写端点（如 refresh）显式挂载。
#[derive(Debug)]
pub struct CsrfGuard;

impl FromRequestParts<AppState> for CsrfGuard {
    type Rejection = ApiError;

    fn from_request_parts(
        parts: &mut Parts,
        _state: &AppState,
    ) -> impl Future<Output = Result<Self, Self::Rejection>> + Send {
        std::future::ready(if is_write_method(&parts.method) {
            verify_csrf(parts).map(|()| CsrfGuard)
        } else {
            Ok(CsrfGuard)
        })
    }
}

fn first_party_user_of(parts: &Parts, state: &AppState) -> Result<FirstPartyUser, ApiError> {
    let (token, from_cookie) = access_token_of(parts).ok_or(ApiError::Unauthorized)?;
    // 跨站页面无法携带自定义头，Bearer 认证豁免 CSRF（ADR-0005）
    if from_cookie && is_write_method(&parts.method) {
        verify_csrf(parts)?;
    }
    match verify_access(&token, state.jwt.decode(), state.clock.now()) {
        Ok(VerifiedAccessToken::UserSession { user_id }) => Ok(FirstPartyUser { user_id }),
        // 第三方客户端令牌不是第一方会话；与无效令牌同体返回，避免泄露令牌类型
        Ok(VerifiedAccessToken::ClientGrant { .. }) | Err(_) => Err(ApiError::Unauthorized),
    }
}

/// cookie 优先、Bearer 次之；返回 (token, 是否来自 cookie)。
fn access_token_of(parts: &Parts) -> Option<(String, bool)> {
    if let Some(value) = cookie_value(&parts.headers, ACCESS_COOKIE) {
        return Some((value, true));
    }
    parts
        .headers
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|value| value.to_str().ok())
        .and_then(|header| header.strip_prefix("Bearer "))
        .map(|token| (token.to_owned(), false))
}

/// 写方法判定只看方法，不看有无 body（v3 曾按 body 判定放过写请求，§10）。
fn is_write_method(method: &Method) -> bool {
    matches!(*method, Method::POST | Method::PUT | Method::PATCH | Method::DELETE)
}

/// 校验 `X-CSRF-Token` 头与 `nebula_csrf` cookie 恒定时间相等。
fn verify_csrf(parts: &Parts) -> Result<(), ApiError> {
    let cookie = cookie_value(&parts.headers, CSRF_COOKIE).ok_or(ApiError::CsrfInvalid)?;
    let header = parts
        .headers
        .get(CSRF_HEADER)
        .and_then(|value| value.to_str().ok())
        .ok_or(ApiError::CsrfInvalid)?;
    if constant_time_eq(cookie.as_bytes(), header.as_bytes()) {
        Ok(())
    } else {
        Err(ApiError::CsrfInvalid)
    }
}

/// 异或折叠的恒定时间比较；长度不等直接拒绝——长度不是秘密（定长摘要）。
pub(crate) fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
    if a.len() != b.len() {
        return false;
    }
    a.iter().zip(b).fold(0, |diff, (x, y)| diff | (x ^ y)) == 0
}

#[cfg(test)]
mod tests {
    use super::constant_time_eq;

    #[test]
    fn constant_time_eq_matches_and_rejects() {
        assert!(constant_time_eq(b"same-bytes", b"same-bytes"));
        assert!(!constant_time_eq(b"same-bytes", b"samX-bytes"));
        assert!(!constant_time_eq(b"short", b"longer-string"));
        // 全等长的单字节差异也必须被发现
        assert!(!constant_time_eq(b"aaaaaaaa", b"aaaaaaab"));
    }
}
