//! `POST /api/admin/users/{user_id}/ban`：临时/永久封禁并在同一事务内撤销会话。

use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde_json::json;

use super::{invalid, target_id};
use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::AdminUser;
use crate::model::audit::NewAuditEntry;
use crate::model::user::Ban;

/// 封禁请求。
#[derive(Debug, Deserialize)]
pub struct BanRequest {
    /// `temporary` 或 `permanent`。
    pub kind: String,
    /// 临时封禁的到期时间；永久封禁必须省略。
    pub expires_at: Option<DateTime<Utc>>,
    /// 封禁理由，写入审计。
    pub reason: String,
}

/// 封禁用户；仅管理员可访问。
///
/// # Errors
/// 未认证 401；非管理员 403；目标不存在 404；封禁自己/其他管理员 409；
/// 请求字段非法 422；存储故障 500。
pub async fn ban(
    State(state): State<AppState>,
    admin: AdminUser,
    Path(raw_id): Path<String>,
    body: Bytes,
) -> Result<Response, ApiError> {
    let target = target_id(&raw_id)?;
    if target == admin.user.user_id {
        return Err(ApiError::CannotBanSelf);
    }
    let req: BanRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let now = state.clock.now();
    let reason = req.reason.trim();
    if reason.is_empty() {
        return Err(invalid("reason", "reason_missing"));
    }
    if reason.chars().count() > 500 {
        return Err(invalid("reason", "reason_too_long"));
    }
    let ban = match req.kind.as_str() {
        "permanent" => {
            if req.expires_at.is_some() {
                return Err(invalid("expires_at", "unexpected_expiry"));
            }
            Ban::Permanent
        }
        "temporary" => {
            let Some(expires_at) = req.expires_at else {
                return Err(invalid("expires_at", "expiry_required"));
            };
            if expires_at <= now {
                return Err(invalid("expires_at", "expiry_in_past"));
            }
            Ban::Temporary(expires_at)
        }
        _ => return Err(invalid("kind", "invalid_ban_kind")),
    };

    let Some(access) = state
        .user_repo
        .find_access_state(&target, now)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?
    else {
        return Err(ApiError::UserNotFound);
    };
    if state.admin_emails.contains(&access.email) {
        return Err(ApiError::CannotBanAdmin);
    }

    let banned = state
        .admin_repo
        .ban_user(&target, &ban, reason, &admin.user.user_id, now)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    if !banned {
        return Err(ApiError::UserNotFound);
    }

    let (kind, expires_at) = match &ban {
        Ban::Permanent => ("permanent", None),
        Ban::Temporary(expires_at) => ("temporary", Some(expires_at)),
    };
    let details = json!({ "kind": kind, "expires_at": expires_at, "reason": reason });
    state
        .admin_repo
        .record_audit(
            &NewAuditEntry {
                actor_id: admin.user.user_id,
                action: "admin.user.ban",
                target_id: Some(target),
                details: Some(details),
            },
            now,
        )
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;

    Ok(StatusCode::NO_CONTENT.into_response())
}
