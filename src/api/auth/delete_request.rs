//! POST /api/auth/account/delete/request：已登录发起注销，向注册邮箱发送
//! 注销确认码（用途 `account_delete`，与邮箱验证码互不相通）。

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};

use super::issue_and_send_code;
use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::AuthUser;
use crate::model::verification::{RESEND_COOLDOWN, VerificationPurpose};
use crate::storage::{EmailVerificationRepo, UserRepo};

/// 处理注销发起：对未验证邮箱拒绝（注销确认码发往邮箱，邮箱不可达则注销
/// 不可完成）；随后与重发同款 60 秒冷却。
///
/// # Errors
/// 未认证 401；邮箱未验证 409 `email_unverified`；冷却中 429
/// `resend_cooldown`；内部故障 500。
pub async fn delete_request(
    State(state): State<AppState>,
    user: AuthUser,
) -> Result<Response, ApiError> {
    let Some(credentials) =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)));
    };
    if !credentials.user.email_verified {
        return Err(ApiError::EmailUnverified);
    }

    if let Some(latest) = state
        .verification_repo
        .latest_unconsumed(&user.user_id, VerificationPurpose::DeleteAccount)
        .await
        .map_err(anyhow::Error::from)?
    {
        let elapsed = state.clock.now() - latest.created_at;
        if elapsed < RESEND_COOLDOWN {
            let remaining = (RESEND_COOLDOWN - elapsed).num_seconds().max(1);
            return Err(ApiError::ResendCooldown(remaining.cast_unsigned()));
        }
    }

    issue_and_send_code(&state, &credentials.user, VerificationPurpose::DeleteAccount).await?;
    Ok(StatusCode::NO_CONTENT.into_response())
}
