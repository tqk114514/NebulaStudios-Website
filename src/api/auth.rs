//! 认证域端点与会话 cookie 约定（§10 保留 v3 姿态：HttpOnly + Secure +
//! `SameSite` 的 access/refresh 分离 cookie）。

pub mod change_password;
pub mod delete_confirm;
pub mod delete_request;
pub mod login;
pub mod login_totp;
pub mod logout;
pub mod password_reset;
pub mod refresh;
pub mod register;
pub mod resend;
pub mod verify;

use axum::http::header::SET_COOKIE;
use axum::http::{HeaderMap, HeaderValue};
use axum::response::IntoResponse;

use super::{ACCESS_COOKIE, CSRF_COOKIE, REFRESH_COOKIE};
use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::constant_time_eq;
use crate::mailer::EmailMessage;
use crate::model::password::PasswordHash;
use crate::model::tokens::{AccessClaims, RefreshToken, random_hex_32, sha256_hex, sign_access};
use crate::model::user::{User, UserId};
use crate::model::verification::{
    MAX_ATTEMPTS, NewEmailVerification, VERIFICATION_TTL, VerificationCode, VerificationPurpose,
};
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

/// 只签第一方 access JWT；登录与刷新共用。
pub(crate) fn sign_access_for(state: &AppState, user_id: UserId) -> Result<String, ApiError> {
    let claims = AccessClaims::user_session(user_id, state.clock.now(), state.session.access_ttl);
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
    let family = crate::model::tokens::NewRefreshFamily {
        family_id: Uuid::now_v7(),
        user_id,
        client_id: None,
        scope: String::new(),
        token_hash: refresh.to_hash_hex(),
        expires_at: state.clock.now() + state.session.refresh_ttl,
    };
    state
        .refresh_repo
        .create_family(&family, state.clock.now())
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

/// 组装带会话 cookie 的响应；登录与二段共用（ADR-0004 裸数据）。
pub(crate) fn session_response(
    status: axum::http::StatusCode,
    state: &AppState,
    user: &User,
    tokens: &SessionTokens,
) -> axum::response::Response {
    use axum::Json;

    let mut headers = HeaderMap::new();
    set_access_cookie(&mut headers, &tokens.access, cookie_max_age(state.session.access_ttl));
    set_refresh_cookie(&mut headers, &tokens.refresh, cookie_max_age(state.session.refresh_ttl));
    set_csrf_cookie(&mut headers, &tokens.csrf, cookie_max_age(state.session.refresh_ttl));
    (status, headers, Json(user)).into_response()
}

/// 签发验证码并异步发送邮件（发送不阻塞调用方响应）；落库失败才是请求失败，
/// 发送失败只记日志——用户可经对应端点补发。邮件主题随用途区分。
pub(crate) async fn issue_and_send_code(
    state: &AppState,
    user: &User,
    purpose: VerificationPurpose,
) -> Result<(), ApiError> {
    let code = VerificationCode::generate();
    let now = state.clock.now();
    let entry = NewEmailVerification {
        user_id: user.id,
        code_hash: code.to_hash_hex(),
        expires_at: now + VERIFICATION_TTL,
        purpose,
    };
    state.verification_repo.issue(&entry, now).await.map_err(anyhow::Error::from)?;

    let subject = match purpose {
        VerificationPurpose::VerifyEmail => "Nebula 邮箱验证码",
        VerificationPurpose::DeleteAccount => "Nebula 账户注销确认码",
        VerificationPurpose::PasswordReset => "Nebula 密码重置验证码",
    };
    let mailer = state.mailer.clone();
    let message = EmailMessage {
        to: user.email.clone(),
        subject: subject.to_owned(),
        body: format!("验证码：{}，{} 分钟内有效。", code.as_str(), VERIFICATION_TTL.num_minutes()),
    };
    tokio::spawn(async move {
        if let Err(err) = mailer.send(message).await {
            tracing::warn!(error = %err, "验证邮件发送失败");
        }
    });
    Ok(())
}

/// 校验某用途下的验证码：时效、限次、恒时比较，错码自动计数。
/// 成功返回验证码记录 id，由调用方决定消费后的附带动作。
///
/// # Errors
/// 无未消费验证码、过期、打满限次或比对不符时返回
/// [`ApiError::InvalidVerificationCode`]。
pub(crate) async fn check_verification_code(
    state: &AppState,
    user_id: &UserId,
    code: &str,
    purpose: VerificationPurpose,
) -> Result<uuid::Uuid, ApiError> {
    let invalid = ApiError::InvalidVerificationCode;
    let Some(verification) = state
        .verification_repo
        .latest_unconsumed(user_id, purpose)
        .await
        .map_err(anyhow::Error::from)?
    else {
        return Err(invalid);
    };
    if verification.expires_at <= state.clock.now() || verification.attempts >= MAX_ATTEMPTS {
        return Err(invalid);
    }
    let candidate = sha256_hex(code);
    if !constant_time_eq(candidate.as_bytes(), verification.code_hash.as_bytes()) {
        state
            .verification_repo
            .record_attempt(&verification.id)
            .await
            .map_err(anyhow::Error::from)?;
        return Err(invalid);
    }
    Ok(verification.id)
}

/// 口令强度规则（v3 语义，注册与改密共用）：16–64 位且含大小写、数字、标点。
/// 返回第一个不满足项的 code；合规返回 `None`。
#[must_use]
pub(crate) fn password_problem(password: &str) -> Option<&'static str> {
    let len = password.chars().count();
    if len < 16 {
        return Some("password_too_short");
    }
    if len > 64 {
        return Some("password_too_long");
    }
    let has_digit = password.chars().any(|c| c.is_ascii_digit());
    let has_lower = password.chars().any(|c| c.is_ascii_lowercase());
    let has_upper = password.chars().any(|c| c.is_ascii_uppercase());
    let has_special = password.chars().any(|c| c.is_ascii_punctuation());
    if !has_digit {
        return Some("password_missing_digit");
    }
    if !has_lower {
        return Some("password_missing_lowercase");
    }
    if !has_upper {
        return Some("password_missing_uppercase");
    }
    if !has_special {
        return Some("password_missing_special");
    }
    None
}

/// 在阻塞线程池上校验口令；存储哈希损坏（非口令错）按 500 处理。
pub(crate) async fn verify_password_in_background(
    hash: PasswordHash,
    plain: String,
) -> Result<bool, ApiError> {
    tokio::task::spawn_blocking(move || hash.verify(&plain))
        .await
        .map_err(anyhow::Error::from)?
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))
}

/// 在阻塞线程池上计算口令哈希。
pub(crate) async fn hash_password_in_background(plain: String) -> Result<PasswordHash, ApiError> {
    tokio::task::spawn_blocking(move || PasswordHash::hash(&plain))
        .await
        .map_err(anyhow::Error::from)?
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))
}

/// form/查询参数值的最小百分号编码（RFC 3986 保留字符；state 回传用）。
pub(crate) fn form_urlencode(value: &str) -> String {
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut out = String::with_capacity(value.len());
    for byte in value.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(byte as char);
            }
            _ => {
                out.push('%');
                out.push(HEX[usize::from(byte >> 4)] as char);
                out.push(HEX[usize::from(byte & 0x0f)] as char);
            }
        }
    }
    out
}
