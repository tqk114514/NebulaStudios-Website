//! POST /api/auth/register/verify：以邮件中的验证码完成邮箱验证。
//! 「用户不存在」与「验证码错误」同体 401，防枚举；错误尝试限 5 次后作废。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::model::verification::VerificationPurpose;
use crate::storage::{EmailVerificationRepo, UserRepo};

/// 验证请求。
#[derive(Debug, Deserialize)]
pub struct VerifyRequest {
    /// 邮箱。
    pub email: String,
    /// 邮件中的六位验证码。
    pub code: String,
}

/// 处理验证：取最新未消费验证码 → 时效/限次检查 → 恒时哈希比较 → 原子消费并置已验证。
///
/// # Errors
/// 字段缺失 422；验证码错误/过期/耗尽/用户不存在 401 `invalid_code`；内部故障 500。
pub async fn verify(State(state): State<AppState>, body: Bytes) -> Result<Response, ApiError> {
    let req: VerifyRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let email = req.email.trim().to_lowercase();
    let mut issues = Vec::new();
    if email.is_empty() {
        issues.push(FieldInvalid { field: "email", code: "email_missing".to_owned() });
    }
    if req.code.trim().is_empty() {
        issues.push(FieldInvalid { field: "code", code: "code_missing".to_owned() });
    }
    if !issues.is_empty() {
        return Err(ApiError::Validation(issues));
    }

    // 用户不存在与验证码错误同形同体
    let invalid = ApiError::InvalidVerificationCode;
    let Some(user) = state.user_repo.find_by_email(&email).await.map_err(anyhow::Error::from)?
    else {
        return Err(invalid);
    };

    let verification_id = super::check_verification_code(
        &state,
        &user.id,
        req.code.trim(),
        VerificationPurpose::VerifyEmail,
    )
    .await?;

    let consumed = state
        .verification_repo
        .consume_and_mark_verified(&verification_id, &user.id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;
    if !consumed {
        // 并发下已被他者消费
        return Err(invalid);
    }
    Ok(StatusCode::NO_CONTENT.into_response())
}
