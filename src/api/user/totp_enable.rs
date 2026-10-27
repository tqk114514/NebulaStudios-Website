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

/// enable 请求。
#[derive(Debug, Deserialize)]
pub struct EnableRequest {
    /// 当前登录口令；启用前再认证。
    pub password: String,
    /// 验证器 App 当前展示的六位码。
    pub code: String,
}

/// enable 响应；恢复码明文仅此一次。
#[derive(Debug, Serialize)]
pub struct EnableResponse {
    /// 一次性恢复码（v3 同款 8 个）。
    pub recovery_codes: Vec<String>,
}

/// 处理 enable：口令再认证 → Pending 态 + pending 密钥下的码验证通过 → Enabled。
///
/// # Errors
/// 未认证 401；口令错 401 `invalid_credentials`；码错 401 `totp_invalid`；
/// 非 Pending 态 409 `totp_not_pending`；内部故障 500。
pub async fn enable(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: EnableRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let mut issues = Vec::new();
    if req.password.is_empty() {
        issues.push(FieldInvalid { field: "password", code: "password_missing".to_owned() });
    }
    if req.code.trim().is_empty() {
        issues.push(FieldInvalid { field: "code", code: "code_missing".to_owned() });
    }
    if !issues.is_empty() {
        return Err(ApiError::Validation(issues));
    }
    crate::api::verify_current_password(&state, &user.user_id, &req.password).await?;

    let Some((totp_state, secret)) =
        state.totp_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)));
    };
    // 仅 Pending 可启用；Enabled 再 enable、Disabled 直接 enable 都是非法转移
    totp_state.on_enable().map_err(|_| ApiError::TotpNotPending)?;
    // Pending 态密钥必须存在；缺失属数据损坏，不能当作客户端错误
    let Some(secret) = secret else {
        return Err(ApiError::Internal(anyhow::anyhow!(
            "pending 状态缺少 TOTP 密钥: {}",
            user.user_id
        )));
    };
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
