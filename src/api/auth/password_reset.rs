//! 未登录态密码重置（ADR-0002 认证内核；v3 语义）。
//! request：向邮箱发六位验证码——未知邮箱同样 204 不发信（防枚举）。
//! confirm：验码（用途 `password_reset`，与验证/注销互不相通）→ 换哈希 →
//! 整族撤销全部会话并作废 TOTP 挑战（红线 #1）。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::{hash_password_in_background, issue_and_send_code, password_problem};
use crate::api::{AppState, clear_session_cookies};
use crate::error::{ApiError, FieldInvalid};
use crate::model::verification::{RESEND_COOLDOWN, VerificationPurpose};

/// 重置请求（request 端点）。
#[derive(Debug, Deserialize)]
pub struct ResetRequestPayload {
    /// 注册邮箱。
    pub email: String,
}

/// 重置完成（confirm 端点）。
#[derive(Debug, Deserialize)]
pub struct ResetConfirmPayload {
    /// 注册邮箱。
    pub email: String,
    /// 邮件中的六位验证码。
    pub code: String,
    /// 新口令；强度规则与注册一致。
    pub new_password: String,
}

/// POST /api/auth/password/reset/request：发重置验证码。
///
/// # Errors
/// 字段缺失 422；冷却中 429 `resend_cooldown`；内部故障 500。未知邮箱 204。
pub async fn reset_request(
    State(state): State<AppState>,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: ResetRequestPayload =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let email = req.email.trim().to_lowercase();
    if email.is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "email",
            code: "email_missing".to_owned(),
        }]));
    }

    if let Some(user) = state.user_repo.find_by_email(&email).await.map_err(anyhow::Error::from)? {
        if let Some(latest) = state
            .verification_repo
            .latest_unconsumed(&user.id, VerificationPurpose::PasswordReset)
            .await
            .map_err(anyhow::Error::from)?
        {
            let elapsed = state.clock.now() - latest.created_at;
            if elapsed < RESEND_COOLDOWN {
                let remaining = (RESEND_COOLDOWN - elapsed).num_seconds().max(1);
                return Err(ApiError::ResendCooldown(remaining.cast_unsigned()));
            }
        }
        issue_and_send_code(&state, &user, VerificationPurpose::PasswordReset).await?;
    }
    Ok(StatusCode::NO_CONTENT.into_response())
}

/// POST /api/auth/password/reset/confirm：验码并完成重置。
/// 「用户不存在」与「码错」同体 401（防枚举）。
///
/// # Errors
/// 字段缺失/新口令不合规 422；码错/过期/耗尽 401 `invalid_code`；内部故障 500。
pub async fn reset_confirm(
    State(state): State<AppState>,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: ResetConfirmPayload =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let email = req.email.trim().to_lowercase();
    let mut issues = Vec::new();
    if email.is_empty() {
        issues.push(FieldInvalid { field: "email", code: "email_missing".to_owned() });
    }
    if req.code.trim().is_empty() {
        issues.push(FieldInvalid { field: "code", code: "code_missing".to_owned() });
    }
    if let Some(code) = password_problem(&req.new_password) {
        issues.push(FieldInvalid { field: "new_password", code: code.to_owned() });
    }
    if !issues.is_empty() {
        return Err(ApiError::Validation(issues));
    }

    let invalid = ApiError::InvalidVerificationCode;
    let Some(user) = state.user_repo.find_by_email(&email).await.map_err(anyhow::Error::from)?
    else {
        return Err(invalid);
    };

    // v3 已知边界：重置只依赖邮箱所有权，不要求 TOTP（未登录态无法走二段）
    let verification_id = super::check_verification_code(
        &state,
        &user.id,
        req.code.trim(),
        VerificationPurpose::PasswordReset,
    )
    .await?;

    let consumed = state
        .verification_repo
        .consume(&verification_id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;
    if !consumed {
        return Err(invalid);
    }

    let new_hash = hash_password_in_background(req.new_password.clone()).await?;
    state.user_repo.update_password_hash(&user.id, &new_hash).await.map_err(anyhow::Error::from)?;

    // 红线 #1：密码变更即全端下线；未消费的登录挑战一并作废
    state
        .refresh_repo
        .revoke_all_for_user(&user.id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;
    state
        .totp_repo
        .invalidate_challenges(&user.id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    let mut headers = axum::http::HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
