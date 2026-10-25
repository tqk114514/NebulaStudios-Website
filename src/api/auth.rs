//! 认证域端点与会话 cookie 约定（§10 保留 v3 姿态：HttpOnly + Secure +
//! `SameSite` 的 access/refresh 分离 cookie）。

pub mod login;
pub mod logout;
pub mod refresh;
pub mod register;
pub mod resend;
pub mod verify;

use axum::http::header::SET_COOKIE;
use axum::http::{HeaderMap, HeaderValue};

use super::{ACCESS_COOKIE, CSRF_COOKIE, REFRESH_COOKIE};
use crate::api::AppState;
use crate::error::ApiError;
use crate::mailer::EmailMessage;
use crate::model::tokens::{AccessClaims, RefreshToken, random_hex_32, sign_access};
use crate::model::user::{User, UserId};
use crate::model::verification::{NewEmailVerification, VERIFICATION_TTL, VerificationCode};
use crate::storage::{EmailVerificationRepo, RefreshTokenRepo};
use uuid::Uuid;

/// 一次会话签发的三个明文凭据；csrf 与 refresh 同步轮转（ADR-0005）。
pub(crate) struct SessionTokens {
    /// access JWT。
    pub access: String,
    /// refresh 明文。
    pub refresh: String,
    /// csrf 双提交令牌，随 cookie 下发、随头回显。
    pub csrf: String,
}

/// 只签 access JWT；登录与刷新共用。
pub(crate) fn sign_access_for(state: &AppState, user_id: UserId) -> Result<String, ApiError> {
    let claims = AccessClaims::new(user_id, state.clock.now(), state.session.access_ttl);
    sign_access(&claims, state.jwt.encode())
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))
}

/// 登录成功后签发全新会话（access JWT + 新 refresh 家族 + csrf 令牌）。
/// 刷新不得走这里——轮转须留在原家族内，否则重放撤销伤不到新代（见 refresh.rs）。
pub(crate) async fn issue_session(
    state: &AppState,
    user_id: UserId,
) -> Result<SessionTokens, ApiError> {
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
    Ok(SessionTokens { access, refresh: refresh.as_str().to_owned(), csrf: random_hex_32() })
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

/// 签发验证码并异步发送邮件（发送不阻塞调用方响应）；落库失败才是请求失败，
/// 发送失败只记日志——用户可经重发端点补发。
pub(crate) async fn issue_and_send_verification(
    state: &AppState,
    user: &User,
) -> Result<(), ApiError> {
    let code = VerificationCode::generate();
    let now = state.clock.now();
    let entry = NewEmailVerification {
        user_id: user.id,
        code_hash: code.to_hash_hex(),
        expires_at: now + VERIFICATION_TTL,
    };
    state.verification_repo.issue(&entry, now).await.map_err(anyhow::Error::from)?;

    let mailer = state.mailer.clone();
    let message = EmailMessage {
        to: user.email.clone(),
        subject: "Nebula 邮箱验证码".to_owned(),
        body: format!("验证码：{}，{} 分钟内有效。", code.as_str(), VERIFICATION_TTL.num_minutes()),
    };
    tokio::spawn(async move {
        if let Err(err) = mailer.send(message).await {
            tracing::warn!(error = %err, "验证邮件发送失败");
        }
    });
    Ok(())
}
