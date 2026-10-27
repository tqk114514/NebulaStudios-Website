//! POST /api/user/totp/enable：以 pending 密钥下的验证码启用 TOTP，签发
//! 恢复码批次（明文仅在本次响应出现，库中只存 SHA-256）。

use axum::Json;
use axum::body::Bytes;
use axum::extract::State;
use axum::response::{IntoResponse, Response};
use serde::{Deserialize, Serialize};

use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;
use crate::model::totp::{RECOVERY_CODE_COUNT, RecoveryCode, verify_code};
use crate::storage::TotpRepo;

/// enable 请求。
#[derive(Debug, Deserialize)]
pub struct EnableRequest {
    /// 验证器 App 当前展示的六位码。
    pub code: String,
}

/// enable 响应；恢复码明文仅此一次。
#[derive(Debug, Serialize)]
pub struct EnableResponse {
    /// 一次性恢复码（v3 同款 8 个）。
    pub recovery_codes: Vec<String>,
}

/// 处理 enable：Pending 态 + pending 密钥下的码验证通过 → Enabled。
///
/// # Errors
/// 未认证 401；码错 401 `totp_invalid`；非 Pending 态 409 `totp_not_enabled`；
/// 内部故障 500。
pub async fn enable(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: EnableRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    if req.code.trim().is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "code",
            code: "code_missing".to_owned(),
        }]));
    }

    let Some((totp_state, Some(secret))) =
        state.totp_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::TotpNotEnabled);
    };
    // Pending 且密钥在位才可启用；Enabled 再 enable 是非法转移
    if totp_state.on_enable().is_err() {
        return Err(ApiError::TotpNotEnabled);
    }
    if !verify_code(&secret, req.code.trim(), state.clock.now()) {
        return Err(ApiError::TotpInvalid);
    }

    state.totp_repo.enable(&user.user_id, state.clock.now()).await.map_err(anyhow::Error::from)?;

    let codes: Vec<RecoveryCode> =
        (0..RECOVERY_CODE_COUNT).map(|_| RecoveryCode::generate()).collect();
    let hashes = codes.iter().map(RecoveryCode::to_hash_hex).collect();
    state
        .totp_repo
        .add_recovery_codes(&user.user_id, hashes, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    Ok(Json(EnableResponse {
        recovery_codes: codes.iter().map(|c| c.as_str().to_owned()).collect(),
    })
    .into_response())
}
