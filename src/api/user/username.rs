//! `PATCH /api/user/username`：修改用户名（当前口令再认证）。

use axum::extract::State;
use axum::http::StatusCode;
use serde::Deserialize;

use crate::api::{AppState, verify_current_password};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::{FirstPartyUser, JsonBody};
use crate::model::audit::AuditAction;
use crate::model::user::username_problem;
use crate::storage::UserRepoError;

/// 用户名修改请求。
// 含明文口令，故意不实现 Debug，避免误日志。
#[allow(missing_debug_implementations)]
#[derive(Deserialize)]
pub struct UpdateUsernameRequest {
    /// 新用户名；大小写保留并精确匹配。
    pub username: String,
    /// 当前口令，用于再认证。
    pub current_password: String,
}

/// 修改用户名；UID、邮箱与会话均不变。
///
/// # Errors
/// 未认证 401；口令错误 401；CSRF 失败 403；用户名非法 422；用户名被占用 409；
/// 存储故障 500。
pub async fn update_username(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: JsonBody<UpdateUsernameRequest>,
) -> Result<StatusCode, ApiError> {
    let request = body.0;
    let username = request.username.trim().to_owned();
    if let Some(code) = username_problem(&username) {
        return Err(invalid("username", code));
    }
    let current = verify_current_password(&state, &user.user_id, &request.current_password).await?;
    if current.username == username {
        return Ok(StatusCode::NO_CONTENT);
    }

    match state.user_repo.update_username(&user.user_id, &username).await {
        Ok(true) => {}
        Ok(false) => return Err(ApiError::Unauthorized),
        Err(UserRepoError::Conflict) => return Err(ApiError::UsernameTaken),
        Err(err) => return Err(ApiError::Internal(anyhow::Error::new(err))),
    }
    crate::api::record_audit(
        &state,
        user.user_id,
        AuditAction::UserUsernameChanged,
        Some(user.user_id),
        Some(serde_json::json!({ "old": current.username, "username": username })),
    )
    .await;
    Ok(StatusCode::NO_CONTENT)
}

fn invalid(field: &'static str, code: &'static str) -> ApiError {
    ApiError::Validation(vec![FieldInvalid { field, code: code.to_owned() }])
}
