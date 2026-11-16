//! `POST /api/user/email/change/request` 与 `.../confirm`：两步修改账户邮箱。
//!
//! request：当前口令 + 新邮箱 → 向新邮箱发验证码；confirm：验码后改邮箱并置已验证。
//! 现有会话保留（ADR-0015）；旧邮箱收到安全通知。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use serde::Deserialize;
use validator::Validate;

use crate::api::{
    AppState, SecurityNotice, send_code_email, send_security_notice, verify_current_password,
};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::{FirstPartyUser, PreferredLocale, constant_time_eq};
use crate::model::audit::AuditAction;
use crate::model::verification::{
    MAX_ATTEMPTS, RESEND_COOLDOWN, VERIFICATION_TTL, VerificationCode, VerificationPurpose,
};
use crate::storage::EmailChangeError;

/// 邮箱变更申请。
#[derive(Debug, Deserialize, Validate)]
pub struct RequestEmailChangeRequest {
    /// 新邮箱；服务端 trim 并小写化。
    #[validate(length(min = 1, max = 254))]
    #[validate(email)]
    pub new_email: String,
    /// 当前口令，用于再认证。
    pub current_password: String,
}

/// 邮箱变更确认。
#[derive(Debug, Deserialize)]
pub struct ConfirmEmailChangeRequest {
    /// 发送到新邮箱的六位验证码。
    pub code: String,
}

/// 申请邮箱变更：校验新邮箱、当前口令与唯一性后发送验证码。
///
/// # Errors
/// 未认证 401；口令错误 401；CSRF 失败 403；邮箱非法/未变化/域名不允许 422；
/// 邮箱被占用 409；冷却中 429；存储故障 500。
pub async fn request_email_change(
    State(state): State<AppState>,
    user: FirstPartyUser,
    locale: PreferredLocale,
    body: Bytes,
) -> Result<StatusCode, ApiError> {
    let mut request: RequestEmailChangeRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    request.new_email = request.new_email.trim().to_lowercase();
    if request.validate().is_err() {
        return Err(invalid("new_email", "invalid_email"));
    }
    let current = verify_current_password(&state, &user.user_id, &request.current_password).await?;
    if current.email == request.new_email {
        return Err(invalid("new_email", "email_unchanged"));
    }
    if !state.allowed_email_domains.is_empty() {
        let domain = request.new_email.rsplit_once('@').map_or("", |(_, domain)| domain);
        if !state.allowed_email_domains.contains(domain) {
            return Err(invalid("new_email", "email_domain_not_allowed"));
        }
    }
    if let Some(existing) =
        state.user_repo.find_by_email(&request.new_email).await.map_err(anyhow::Error::from)?
        && existing.id != user.user_id
    {
        return Err(ApiError::EmailTaken);
    }

    let now = state.clock.now();
    if let Some(pending) =
        state.email_change_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    {
        let elapsed = now - pending.created_at;
        if elapsed < RESEND_COOLDOWN {
            let remaining = (RESEND_COOLDOWN - elapsed).num_seconds().max(1);
            return Err(ApiError::ResendCooldown(remaining.cast_unsigned()));
        }
    }

    let code = VerificationCode::generate();
    state
        .email_change_repo
        .upsert(
            &user.user_id,
            &request.new_email,
            &state.code_hasher.hash(code.as_str()),
            now + VERIFICATION_TTL,
            now,
        )
        .await
        .map_err(anyhow::Error::from)?;
    send_code_email(&state, &request.new_email, VerificationPurpose::EmailChange, &code, locale.0);
    Ok(StatusCode::NO_CONTENT)
}

/// 确认邮箱变更：验码 → 更新邮箱并置已验证 → 审计 → 通知旧邮箱。
///
/// # Errors
/// 未认证 401；码错/过期/耗尽 401 `invalid_code`；CSRF 失败 403；邮箱冲突 409；
/// 存储故障 500。
pub async fn confirm_email_change(
    State(state): State<AppState>,
    user: FirstPartyUser,
    locale: PreferredLocale,
    body: Bytes,
) -> Result<StatusCode, ApiError> {
    let request: ConfirmEmailChangeRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let code = request.code.trim();
    if code.is_empty() {
        return Err(invalid("code", "code_missing"));
    }

    let now = state.clock.now();
    let Some(pending) =
        state.email_change_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::InvalidVerificationCode);
    };
    if pending.expires_at <= now || pending.attempts >= MAX_ATTEMPTS {
        return Err(ApiError::InvalidVerificationCode);
    }
    let candidate = state.code_hasher.hash(code);
    if !constant_time_eq(candidate.as_bytes(), pending.code_hash.as_bytes()) {
        state.email_change_repo.record_attempt(&user.user_id).await.map_err(anyhow::Error::from)?;
        return Err(ApiError::InvalidVerificationCode);
    }

    let Some(current) =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::Unauthorized);
    };
    match state.email_change_repo.apply(&user.user_id, &pending.new_email).await {
        Ok(()) => {}
        Err(EmailChangeError::Conflict) => return Err(ApiError::EmailTaken),
        Err(EmailChangeError::NotFound) => return Err(ApiError::InvalidVerificationCode),
        Err(EmailChangeError::Storage(err)) => {
            return Err(ApiError::Internal(anyhow::Error::new(err)));
        }
    }

    crate::api::record_audit(
        &state,
        user.user_id,
        AuditAction::UserEmailChanged,
        Some(user.user_id),
        Some(serde_json::json!({ "email": pending.new_email })),
    )
    .await;
    send_security_notice(&state, &current.user.email, SecurityNotice::EmailChanged, locale.0);
    Ok(StatusCode::NO_CONTENT)
}

fn invalid(field: &'static str, code: &'static str) -> ApiError {
    ApiError::Validation(vec![FieldInvalid { field, code: code.to_owned() }])
}
