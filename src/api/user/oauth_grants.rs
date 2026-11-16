//! `GET /api/user/oauth/grants` 与 `DELETE /api/user/oauth/grants/{client_id}`：
//! 查看并撤销已授权的第三方应用。

use axum::Json;
use axum::extract::{Path, State};
use axum::http::StatusCode;
use serde::Serialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::FirstPartyUser;
use crate::model::audit::AuditAction;
use crate::model::oauth2::OAuthGrant;

/// 已授权第三方应用列表。
#[derive(Debug, Serialize)]
pub struct GrantsResponse {
    /// 每个客户端一条；多次授权已合并 scope 与最早授权时间。
    pub grants: Vec<OAuthGrant>,
}

/// 返回当前用户仍有效的第三方应用授权。
///
/// # Errors
/// 未认证 401；存储故障 500。
pub async fn list(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Json<GrantsResponse>, ApiError> {
    let grants = state
        .oauth_repo
        .list_grants(&user.user_id, state.clock.now())
        .await
        .map_err(anyhow::Error::new)?;
    Ok(Json(GrantsResponse { grants }))
}

/// 撤销对指定客户端的全部有效授权；没有可撤销授权返回 404。
///
/// 只撤销 refresh 家族，已签发的第三方 access token 在到期前仍然有效
/// （≤ access TTL），不为此引入逐客户端 denylist。
///
/// # Errors
/// 未认证 401；CSRF 失败 403；该客户端无有效授权 404；存储故障 500。
pub async fn revoke(
    State(state): State<AppState>,
    user: FirstPartyUser,
    Path(client_id): Path<String>,
) -> Result<StatusCode, ApiError> {
    if client_id.is_empty() {
        return Err(ApiError::OAuthGrantNotFound);
    }
    let now = state.clock.now();
    let revoked = state
        .oauth_repo
        .revoke_client(&user.user_id, &client_id, now)
        .await
        .map_err(anyhow::Error::new)?;
    if !revoked {
        return Err(ApiError::OAuthGrantNotFound);
    }
    crate::api::record_audit(
        &state,
        user.user_id,
        AuditAction::OAuthGrantRevoked,
        Some(user.user_id),
        Some(serde_json::json!({ "client_id": client_id })),
    )
    .await;
    Ok(StatusCode::NO_CONTENT)
}
