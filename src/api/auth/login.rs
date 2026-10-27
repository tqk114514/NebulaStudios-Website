//! POST /api/auth/login：口令登录（ADR-0002 语义）。启用 TOTP 的账户不直接建会话，
//! 而是签发单次登录挑战（`totp_required` + `details.challenge_token`），由
//! `/api/auth/login/totp` 完成二段验证。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::Response;
use serde::Deserialize;

use super::{issue_session, session_response, verify_password_in_background};
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::model::password::PasswordHash;
use crate::model::totp::{CHALLENGE_TTL, TotpState};

/// 登录请求；`identifier` 为邮箱或用户名（§4：同一意图一个键名）。
#[derive(Debug, Deserialize)]
pub struct LoginRequest {
    /// 登录标识。
    pub identifier: String,
    /// 明文口令。
    pub password: String,
}

/// 上限只为挡异常长度输入进入 argon2；口令强度规则只约束注册，不约束登录。
const PASSWORD_INPUT_LIMIT: usize = 128;

/// 处理登录：取凭据 → 验证（时序对齐）→ 签发会话。
///
/// # Errors
/// JSON 非法 400、字段缺失 422、凭据错误 401、内部故障 500（ADR-0004）。
pub async fn login(State(state): State<AppState>, body: Bytes) -> Result<Response, ApiError> {
    let req: LoginRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let identifier = req.identifier.trim().to_lowercase();

    let mut issues = Vec::new();
    if identifier.is_empty() {
        issues.push(FieldInvalid { field: "identifier", code: "identifier_missing".to_owned() });
    }
    if req.password.is_empty() {
        issues.push(FieldInvalid { field: "password", code: "password_missing".to_owned() });
    } else if req.password.len() > PASSWORD_INPUT_LIMIT {
        issues.push(FieldInvalid { field: "password", code: "password_invalid".to_owned() });
    }
    if !issues.is_empty() {
        return Err(ApiError::Validation(issues));
    }

    let credentials =
        state.user_repo.find_login_credentials(&identifier).await.map_err(anyhow::Error::from)?;

    let Some(credentials) = credentials else {
        // 时序对齐（v3 同款）：用户不存在也跑一次等价 argon2，耗时与密码错误不可区分
        let dummy = dummy_hash()?;
        let _ = verify_password_in_background(dummy, req.password.clone()).await?;
        return Err(ApiError::InvalidCredentials);
    };

    let ok = verify_password_in_background(credentials.password_hash, req.password.clone()).await?;
    if !ok {
        return Err(ApiError::InvalidCredentials);
    }
    let user = credentials.user;

    // 封禁必须在签发会话前拦截；已签发 access token 由 FirstPartyUser 同一判定拦截
    let Some(access) = state
        .user_repo
        .find_access_state(&user.id, state.clock.now())
        .await
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?
    else {
        return Err(ApiError::InvalidCredentials);
    };
    if access.ban.is_some() {
        return Err(ApiError::Banned);
    }

    // 已启用 TOTP：不建会话，签发单次挑战走二段（v3 pendingToken 语义，入库哈希）
    if let Some((TotpState::Enabled, _)) =
        state.totp_repo.find(&user.id).await.map_err(anyhow::Error::from)?
    {
        let challenge = crate::model::tokens::RefreshToken::generate();
        let now = state.clock.now();
        state
            .totp_repo
            .issue_challenge(&user.id, &challenge.to_hash_hex(), now + CHALLENGE_TTL, now)
            .await
            .map_err(anyhow::Error::from)?;
        return Err(ApiError::TotpRequired(challenge.as_str().to_owned()));
    }

    let tokens = issue_session(&state, user.id).await?;
    Ok(session_response(StatusCode::OK, &state, &user, &tokens))
}

/// 哑哈希只用于对齐「用户不存在」路径的耗时；生成失败（OS 随机源故障）按 500，
/// 此故障下签发真会话同样不可能。
static DUMMY_HASH: std::sync::OnceLock<Result<PasswordHash, String>> = std::sync::OnceLock::new();

fn dummy_hash() -> Result<PasswordHash, ApiError> {
    let stored = DUMMY_HASH.get_or_init(|| {
        PasswordHash::hash("nebula-timing-equalizer").map_err(|err| err.to_string())
    });
    match stored {
        Ok(hash) => Ok(hash.clone()),
        Err(msg) => Err(ApiError::Internal(anyhow::anyhow!("哑哈希不可用: {msg}"))),
    }
}
