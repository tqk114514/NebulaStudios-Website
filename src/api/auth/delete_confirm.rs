//! POST /api/auth/account/delete/confirm：以注册邮箱收到的注销确认码完成
//! 账户删除（硬删：用户及其全部从属数据，单事务）。删除即最终撤销。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::check_verification_code;
use crate::api::{AppState, clear_session_cookies};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;
use crate::model::audit::AuditAction;
use crate::model::verification::VerificationPurpose;

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
    // 头像文件不在数据库事务内；删除失败时文件成为孤儿，但用户记录已不存在，
    // 资料端点不会再给出它的地址
    if let Err(err) = state.avatar_store.remove(&user.user_id).await {
        tracing::warn!(error = ?err, user_id = %user.user_id, "头像文件清理失败");
    }
    crate::api::record_audit(
        &state,
        user.user_id,
        AuditAction::AuthAccountDelete,
        Some(user.user_id),
        None,
    )
    .await;

    let mut headers = HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
