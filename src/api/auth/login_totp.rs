//! POST /api/auth/login/totp：二段验证。以密码步骤签发的单次挑战换取会话；
//! 六位纯数字走 TOTP 码，其余按恢复码（单次消费）。校验失败计次，打满作废。

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::Response;
use serde::Deserialize;

use super::{issue_session, session_response};
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::JsonBody;
use crate::model::audit::AuditAction;
use crate::model::tokens::RefreshToken;
use crate::model::totp::{CHALLENGE_MAX_ATTEMPTS, verify_code_step};

/// 二段请求。
// 含挑战令牌与验证码，故意不实现 Debug，避免误日志。
#[allow(missing_debug_implementations)]
#[derive(Deserialize, utoipa::ToSchema)]
pub struct LoginTotpRequest {
    /// 密码步骤响应 `details.challenge_token`。
    pub challenge_token: String,
    /// TOTP 码或恢复码。
    pub code: String,
}

/// 处理二段：挑战有效 → 码校验 → 原子消费挑战 → 建会话。
///
/// # Errors
/// 字段缺失 422；挑战缺失/过期/耗尽/码错 401 `totp_invalid`；内部故障 500。
#[utoipa::path(
    post,
    path = "/api/auth/login/totp",
    tag = "auth",
    request_body = LoginTotpRequest,
    responses(
        (status = 200, description = "二段通过，返回用户并下发会话 cookie", body = crate::model::user::User),
        (status = 400, description = "请求体非法", body = crate::error::ErrorBody),
        (status = 401, description = "挑战或验证码无效", body = crate::error::ErrorBody),
        (status = 403, description = "账户已被封禁", body = crate::error::ErrorBody),
        (status = 415, description = "Content-Type 必须为 application/json", body = crate::error::ErrorBody),
        (status = 422, description = "字段校验失败", body = crate::error::ErrorBody),
        (status = 429, description = "请求过于频繁", body = crate::error::ErrorBody),
        (status = 500, description = "内部错误", body = crate::error::ErrorBody),
    )
)]
pub async fn login_totp(
    State(state): State<AppState>,
    body: JsonBody<LoginTotpRequest>,
) -> Result<Response, ApiError> {
    let req = body.0;
    let mut issues = Vec::new();
    if req.challenge_token.trim().is_empty() {
        issues
            .push(FieldInvalid { field: "challenge_token", code: "challenge_missing".to_owned() });
    }
    if req.code.trim().is_empty() {
        issues.push(FieldInvalid { field: "code", code: "code_missing".to_owned() });
    }
    if !issues.is_empty() {
        return Err(ApiError::Validation(issues));
    }

    let invalid = ApiError::TotpInvalid;
    let token_hash = RefreshToken::hash_of(req.challenge_token.trim());
    let Some(challenge) =
        state.totp_repo.find_challenge(&token_hash).await.map_err(anyhow::Error::from)?
    else {
        return Err(invalid);
    };
    let now = state.clock.now();
    if challenge.expires_at <= now {
        return Err(invalid);
    }

    // 挑战签发后可能被封禁；二段完成前再查一次实时状态
    let Some(access) = state
        .user_repo
        .find_access_state(&challenge.user_id, now)
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?
    else {
        return Err(invalid);
    };
    if access.ban.is_some() {
        return Err(ApiError::Banned);
    }

    // 用户须仍处于 Enabled（disable 后挑战已全部作废，此处是纵深防御）
    let Some((crate::model::totp::TotpState::Enabled, Some(encrypted))) =
        state.totp_repo.find(&challenge.user_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(invalid);
    };

    // 在验证前原子预占一次尝试：并发猜码不能各自读到旧计数而突破 5 次上限
    let reserved = state
        .totp_repo
        .reserve_challenge_attempt(&challenge.id, CHALLENGE_MAX_ATTEMPTS)
        .await
        .map_err(anyhow::Error::from)?;
    if !reserved {
        return Err(invalid);
    }

    // 码校验：六位数字走 TOTP，其余按恢复码（字母表不含 0/1，无格式冲突）
    let code = req.code.trim();
    let verified = if code.len() == 6 && code.bytes().all(|b| b.is_ascii_digit()) {
        let secret = state
            .secret_cipher
            .decrypt(&encrypted)
            .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
        match verify_code_step(&secret, code, now) {
            // 同一时间片只能成功一次，跨挑战重放被封死
            Some(step) => state
                .totp_repo
                .consume_step(&challenge.user_id, step)
                .await
                .map_err(anyhow::Error::from)?,
            None => false,
        }
    } else {
        state
            .totp_repo
            .consume_recovery_code(&challenge.user_id, &state.code_hasher.hash(code), now)
            .await
            .map_err(anyhow::Error::from)?
    };
    if !verified {
        return Err(invalid);
    }

    let consumed =
        state.totp_repo.consume_challenge(&challenge.id, now).await.map_err(anyhow::Error::from)?;
    if !consumed {
        // 并发下已被他者消费
        return Err(invalid);
    }

    let tokens = issue_session(&state, challenge.user_id, access.token_version).await?;
    let user = state
        .user_repo
        .find_by_id(&challenge.user_id)
        .await
        .map_err(anyhow::Error::from)?
        .ok_or_else(|| {
        ApiError::Internal(anyhow::anyhow!("挑战指向的用户不存在: {}", challenge.user_id))
    })?;
    crate::api::record_audit(
        &state,
        challenge.user_id,
        AuditAction::AuthLogin,
        Some(challenge.user_id),
        Some(serde_json::json!({ "method": "totp" })),
    )
    .await;
    Ok(session_response(StatusCode::OK, &state, &user.user, &tokens))
}
