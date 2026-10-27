//! POST /api/auth/account/delete/confirm：以注册邮箱收到的注销确认码完成
//! 账户删除（硬删：用户及其全部从属数据，单事务）。删除即最终撤销。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::{check_verification_code, clear_session_cookies};
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;
use crate::model::verification::VerificationPurpose;
use crate::storage::{AccountRepo, EmailVerificationRepo};

/// 注销确认请求。
#[derive(Debug, Deserialize)]
pub struct DeleteConfirmRequest {
    /// 邮件中的六位确认码。
    pub code: String,
}

/// 处理注销确认：验码（用途隔离，邮箱验证码不可用）→ 原子消费 → 硬删账户。
///
/// # Errors
/// 未认证 401；码错/过期/耗尽 401 `invalid_code`；内部故障 500。
pub async fn delete_confirm(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: DeleteConfirmRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    if req.code.trim().is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "code",
            code: "code_missing".to_owned(),
        }]));
    }

    let verification_id = check_verification_code(
        &state,
        &user.user_id,
        req.code.trim(),
        VerificationPurpose::DeleteAccount,
    )
    .await?;

    let consumed = state
        .verification_repo
        .consume(&verification_id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;
    if !consumed {
        return Err(ApiError::InvalidVerificationCode);
    }

    // 删除即撤销：账户连同全部 refresh 家族与验证码一并消失
    state.account_repo.delete_user(&user.user_id).await.map_err(anyhow::Error::from)?;

    let mut headers = HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
