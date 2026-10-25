//! POST /api/auth/register/resend：为未验证邮箱重发验证码。
//! 未注册与已验证同样返回 204，三者对外同形——注册状态不由本端点泄露
//! （注册端点的 409 已公开邮箱占用，不构成新增泄露面）。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::issue_and_send_verification;
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::model::verification::RESEND_COOLDOWN;
use crate::storage::{EmailVerificationRepo, UserRepo};

/// 重发请求。
#[derive(Debug, Deserialize)]
pub struct ResendRequest {
    /// 需要重发的邮箱。
    pub email: String,
}

/// 处理重发：冷却检查 → 签发新码并异步发送。
///
/// # Errors
/// 字段缺失 422；冷却中 429 `resend_cooldown`（`details.retry_after` 为剩余秒数）；
/// 内部故障 500。
pub async fn resend(State(state): State<AppState>, body: Bytes) -> Result<Response, ApiError> {
    let req: ResendRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let email = req.email.trim().to_lowercase();
    if email.is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "email",
            code: "email_missing".to_owned(),
        }]));
    }

    let user = state.user_repo.find_by_email(&email).await.map_err(anyhow::Error::from)?;

    if let Some(user) = user
        && !user.email_verified
    {
        if let Some(latest) = state
            .verification_repo
            .latest_unconsumed(&user.id)
            .await
            .map_err(anyhow::Error::from)?
        {
            let elapsed = state.clock.now() - latest.created_at;
            if elapsed < RESEND_COOLDOWN {
                let remaining = (RESEND_COOLDOWN - elapsed).num_seconds().max(1);
                return Err(ApiError::ResendCooldown(remaining.cast_unsigned()));
            }
        }
        issue_and_send_verification(&state, &user).await?;
    }
    Ok(StatusCode::NO_CONTENT.into_response())
}
