//! `DELETE /api/admin/users/{user_id}/ban`：解除封禁并写审计。

use axum::extract::{Path, State};
use axum::http::StatusCode;

use super::target_id;
use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::AdminUser;
use crate::model::audit::NewAuditEntry;

/// 解除封禁；仅管理员可访问。
///
/// # Errors
/// 未认证 401；非管理员 403；没有封禁记录 404；存储故障 500。
pub async fn unban(
    State(state): State<AppState>,
    admin: AdminUser,
    Path(raw_id): Path<String>,
) -> Result<StatusCode, ApiError> {
    let target = target_id(&raw_id)?;
    let removed = state
        .admin_repo
        .unban_user(&target)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    if !removed {
        return Err(ApiError::BanNotFound);
    }

    state
        .admin_repo
        .record_audit(
            &NewAuditEntry {
                actor_id: admin.user.user_id,
                action: "admin.user.unban",
                target_id: Some(target),
                details: None,
            },
            state.clock.now(),
        )
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;

    Ok(StatusCode::NO_CONTENT)
}
