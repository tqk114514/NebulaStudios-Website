//! POST /api/user/totp/setup：生成 pending 密钥与 otpauth URI。
//! Pending 态重试即换新密钥（旧码作废）；已 Enabled 则 409。

use axum::Json;
use axum::extract::State;
use axum::response::{IntoResponse, Response};
use serde::Serialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::FirstPartyUser;
use crate::model::totp::{generate_secret, otpauth_uri};
use crate::storage::{TotpRepo, UserRepo};

/// setup 响应；密钥与 URI 由用户导入验证器 App。
#[derive(Debug, Serialize)]
pub struct SetupResponse {
    /// base32 无填充密钥。
    pub secret: String,
    /// `otpauth://` URI，验证器扫码或手动录入。
    pub otpauth_uri: String,
}

/// 处理 setup：Disabled/Pending 生成新 pending 密钥；Enabled 拒绝。
///
/// # Errors
/// 未认证 401；已启用 409 `totp_not_enabled`；内部故障 500。
pub async fn setup(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Response, ApiError> {
    // setup 响应的 otpauth URI 用真实邮箱作 label
    let credentials =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?.ok_or_else(
            || ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)),
        )?;
    let Some((totp_state, _existing)) =
        state.totp_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)));
    };

    totp_state.on_setup().map_err(|_| ApiError::TotpNotEnabled)?;

    let secret = generate_secret();
    state
        .totp_repo
        .set_pending(&user.user_id, &secret, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    let response =
        SetupResponse { otpauth_uri: otpauth_uri(&credentials.user.email, &secret), secret };
    Ok(Json(response).into_response())
}
