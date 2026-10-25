//! POST /api/auth/password/change：已登录改密。验证当前口令 → 更新哈希 →
//! 整族撤销全部会话并清 cookie（红线 #1：与登出、注销同一条撤销路径）。
//! CSRF 由 [`AuthUser`] 内建门禁承担（cookie 来源的写方法）。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::{
    clear_session_cookies, hash_password_in_background, password_problem,
    verify_password_in_background,
};
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::AuthUser;
use crate::storage::{RefreshTokenRepo, UserRepo};

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
    user: AuthUser,
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

    // 当前口令校验：按 id 取哈希 → spawn_blocking 验证
    let credentials =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?;
    // JWT 保证用户存在；无凭据（哈希缺失）在此路径不可能出现，按口令错处理
    let verified = match credentials {
        Some(credentials) => {
            verify_password_in_background(credentials.password_hash, req.current_password.clone())
                .await?
        }
        None => false,
    };
    if !verified {
        return Err(ApiError::InvalidCredentials);
    }

    let new_hash = hash_password_in_background(req.new_password.clone()).await?;
    state
        .user_repo
        .update_password_hash(&user.user_id, &new_hash)
        .await
        .map_err(anyhow::Error::from)?;

    // 改密即全端下线（红线 #1）：撤销全部家族再清 cookie
    state
        .refresh_repo
        .revoke_all_for_user(&user.user_id, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    let mut headers = HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
