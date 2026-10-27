//! POST /api/user/totp/disable：关闭两步验证。须当前口令 + 有效验证码
//! （TOTP 码或恢复码）双因素；关闭即撤销全部会话（红线 #1：认证强度降级
//! 必须下线）并作废全部恢复码与未消费挑战。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use crate::api::{AppState, clear_session_cookies};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;
use crate::model::tokens::sha256_hex;
use crate::model::totp::verify_code;

/// disable 请求。
#[derive(Debug, Deserialize)]
pub struct DisableRequest {
    /// 当前口令。
    pub password: String,
    /// TOTP 码或恢复码。
    pub code: String,
}

/// 处理 disable：双因素验证 → Disabled + 撤销。
///
/// # Errors
/// 未认证 401；口令错 401 `invalid_credentials`；码错 401 `totp_invalid`；
/// 未启用 409 `totp_not_enabled`；内部故障 500。
pub async fn disable(
    State(state): State<AppState>,
    user: FirstPartyUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: DisableRequest =
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

    let Some((totp_state, secret)) =
        state.totp_repo.find(&user.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)));
    };
    // Enabled 才可关闭；非法转移显式拒绝
    if totp_state.on_disable().is_err() {
        return Err(ApiError::TotpNotEnabled);
    }

    // 因素一：当前口令
    let credentials =
        state.user_repo.find_by_id(&user.user_id).await.map_err(anyhow::Error::from)?.ok_or_else(
            || ApiError::Internal(anyhow::anyhow!("JWT 指向的用户不存在: {}", user.user_id)),
        )?;
    let password_ok =
        tokio::task::spawn_blocking(move || credentials.password_hash.verify(&req.password))
            .await
            .map_err(anyhow::Error::from)?
            .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    if !password_ok {
        return Err(ApiError::InvalidCredentials);
    }

    // 因素二：TOTP 码或恢复码（六位数字走 TOTP，其余按恢复码）
    let code = req.code.trim();
    let now = state.clock.now();
    let second_factor_ok =
        if secret.is_some() && code.bytes().all(|b| b.is_ascii_digit()) && code.len() == 6 {
            secret.is_some_and(|s| verify_code(&s, code, now))
        } else {
            state
                .totp_repo
                .consume_recovery_code(&user.user_id, &sha256_hex(code), now)
                .await
                .map_err(anyhow::Error::from)?
        };
    if !second_factor_ok {
        return Err(ApiError::TotpInvalid);
    }

    // 关闭即下线（红线 #1）
    state.totp_repo.disable(&user.user_id, now).await.map_err(anyhow::Error::from)?;
    state
        .refresh_repo
        .revoke_all_for_user(&user.user_id, now)
        .await
        .map_err(anyhow::Error::from)?;

    let mut headers = HeaderMap::new();
    clear_session_cookies(&mut headers);
    Ok((StatusCode::NO_CONTENT, headers).into_response())
}
