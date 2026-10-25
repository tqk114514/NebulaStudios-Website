//! 认证域端点与会话 cookie 约定（§10 保留 v3 姿态：HttpOnly + Secure +
//! `SameSite` 的 access/refresh 分离 cookie）。

pub mod login;
pub mod logout;
pub mod refresh;
pub mod register;

use axum::http::header::SET_COOKIE;
use axum::http::{HeaderMap, HeaderValue};

use super::{ACCESS_COOKIE, REFRESH_COOKIE};
use crate::api::AppState;
use crate::error::ApiError;
use crate::model::tokens::{AccessClaims, RefreshToken, sign_access};
use crate::model::user::UserId;
use crate::storage::RefreshTokenRepo;
use uuid::Uuid;

/// 只签 access JWT；登录与刷新共用。
pub(crate) fn sign_access_for(state: &AppState, user_id: UserId) -> Result<String, ApiError> {
    let claims = AccessClaims::new(user_id, state.clock.now(), state.session.access_ttl);
    sign_access(&claims, state.jwt.encode())
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))
}

/// 登录成功后签发全新会话（access JWT + 新 refresh 家族），返回两个明文 token。
/// 刷新不得走这里——轮转须留在原家族内，否则重放撤销伤不到新代（见 refresh.rs）。
pub(crate) async fn issue_session(
    state: &AppState,
    user_id: UserId,
) -> Result<(String, String), ApiError> {
    let access = sign_access_for(state, user_id)?;

    let refresh = RefreshToken::generate();
    let entry = crate::model::tokens::NewRefreshToken {
        family_id: Uuid::now_v7(),
        user_id,
        token_hash: refresh.to_hash_hex(),
        expires_at: state.clock.now() + state.session.refresh_ttl,
    };
    state
        .refresh_repo
        .create_family(&entry, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;
    Ok((access, refresh.as_str().to_owned()))
}

/// cookie 的 Max-Age 取整秒；负值（异常配置）钳到 0。
pub(crate) fn cookie_max_age(ttl: chrono::TimeDelta) -> u64 {
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

/// 追加 refresh cookie 到响应头（路径收窄到 /api/auth）。
pub(crate) fn set_refresh_cookie(headers: &mut HeaderMap, token: &str, max_age_secs: u64) {
    append_cookie(
        headers,
        &format!(
            "{REFRESH_COOKIE}={token}; Path=/api/auth; Max-Age={max_age_secs}; HttpOnly; Secure; SameSite=Lax"
        ),
    );
}

/// 追加两条失效 cookie（Max-Age=0）清除会话。
pub(crate) fn clear_session_cookies(headers: &mut HeaderMap) {
    append_cookie(
        headers,
        &format!("{ACCESS_COOKIE}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax"),
    );
    append_cookie(
        headers,
        &format!("{REFRESH_COOKIE}=; Path=/api/auth; Max-Age=0; HttpOnly; Secure; SameSite=Lax"),
    );
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
