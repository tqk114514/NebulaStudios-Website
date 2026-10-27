//! POST /api/user/totp/setup：生成 pending 密钥与 otpauth URI。
//! Pending 态重试即换新密钥（旧码作废）；已 Enabled 则 409；须当前口令再认证。

use axum::Json;
use axum::body::Bytes;
use axum::extract::State;
use axum::response::{IntoResponse, Response};
use serde::{Deserialize, Serialize};

use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;
use crate::model::totp::{generate_secret, otpauth_uri};

/// setup 请求；写操作要求当前口令，防止被盗会话直接绑定攻击者验证器。
#[derive(Debug, Deserialize)]
pub struct SetupRequest {
    /// 当前登录口令。
    pub password: String,
}

/// setup 响应；密钥与 URI 由用户导入验证器 App。
#[derive(Debug, Serialize)]
pub struct SetupResponse {
    /// base32 无填充密钥。
    pub secret: String,
    /// `otpauth://` URI，验证器扫码或手动录入。
    pub otpauth_uri: String,
}

/// 处理 setup：口令再认证 → Disabled/Pending 生成新 pending 密钥；Enabled 拒绝。
///
/// # Errors
/// 未认证 401；口令错 401 `invalid_credentials`；已启用 409 `totp_already_enabled`；
/// 内部故障 500。
pub async fn setup(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: SetupRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    if req.password.is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "password",
            code: "password_missing".to_owned(),
        }]));
    }
    let account = crate::api::verify_current_password(&state, &user.user_id, &req.password).await?;

    let Some((totp_state, _existing)) =
        state.totp_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)));
    };
    totp_state.on_setup().map_err(|_| ApiError::TotpAlreadyEnabled)?;

    let secret = generate_secret();
    let encrypted = state
        .secret_cipher
        .encrypt(&secret)
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    state
        .totp_repo
        .set_pending(&user.user_id, &encrypted, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    let response = SetupResponse { otpauth_uri: otpauth_uri(&account.email, &secret), secret };
    Ok(Json(response).into_response())
}
