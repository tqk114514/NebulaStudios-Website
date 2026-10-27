//! POST /api/auth/password/change：已登录改密。验证当前口令 → 更新哈希 →
//! 整族撤销全部会话、作废未消费 TOTP 挑战并清 cookie（红线 #1：与登出、注销
//! 同一条撤销路径）。CSRF 由 [`FirstPartyUser`] 内建门禁承担（cookie 来源的写方法）。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::{hash_password_in_background, password_problem};
use crate::api::{AppState, clear_session_cookies, verify_current_password};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;

/// 改密请求。
#[derive(Debug, Deserialize)]
pub struct ChangePasswordRequest {
    /// 当前口令。
    pub current_password: String,
    /// 新口令；强度规则与注册一致。
    pub new_password: String,
}

/// 处理改密。
///
/// # Errors
/// 未认证 401；当前口令错误 401 `invalid_credentials`；新口令不合规 422；
/// 内部故障 500。成功 204 并清除会话。
pub async fn change_password(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: ChangePasswordRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;

    let mut issues = Vec::new();
    if req.current_password.is_empty() {
        issues
            .push(FieldInvalid { field: "current_password", code: "password_missing".to_owned() });
    }
    if let Some(code) = password_problem(&req.new_password) {
        issues.push(FieldInvalid { field: "new_password", code: code.to_owned() });
    }
    if !issues.is_empty() {
        return Err(ApiError::Validation(issues));
    }

    // 当前口令校验：与 TOTP 启用/关闭共用同一再认证助手
    verify_current_password(&state, &user.user_id, &req.current_password).await?;

    let new_hash = hash_password_in_background(req.new_password.clone()).await?;
    state
        .user_repo
        .update_password_hash(&user.user_id, &new_hash)
        .await
        .map_err(anyhow::Error::from)?;

    // 改密即全端下线（红线 #1）：撤销全部家族、作废未消费的 TOTP 挑战，再清 cookie
    let now = state.clock.now();
    state
        .refresh_repo
        .revoke_all_sessions(&user.user_id, now)
        .await
        .map_err(anyhow::Error::from)?;
    state.totp_repo.invalidate_challenges(&user.user_id, now).await.map_err(anyhow::Error::from)?;

    let mut headers = HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
