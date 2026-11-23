//! POST /api/auth/login：口令登录（ADR-0002 语义）。启用 TOTP 的账户不直接建会话，
//! 而是签发单次登录挑战（`totp_required` + `details.challenge_token`），由
//! `/api/auth/login/totp` 完成二段验证。

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::Response;
use serde::Deserialize;

use super::{
    issue_session, session_response, verify_login_password, verify_password_in_background,
};
use crate::api::{AppState, PASSWORD_INPUT_LIMIT};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::JsonBody;
use crate::model::audit::AuditAction;
use crate::model::password::{PasswordCheck, PasswordHash};
use crate::model::totp::{CHALLENGE_TTL, TotpState};
use crate::model::user::canonicalize_email;

/// 登录请求；`identifier` 为邮箱或用户名（§4：同一意图一个键名）。
// 含明文口令，故意不实现 Debug，避免误日志。
#[allow(missing_debug_implementations)]
#[derive(Deserialize)]
pub struct LoginRequest {
    /// 登录标识。
    pub identifier: String,
    /// 明文口令。
    pub password: String,
}

/// 处理登录：取凭据 → 验证（时序对齐）→ 参数落后时升级哈希 → 签发会话。
///
/// # Errors
/// JSON 非法 400、字段缺失 422、凭据错误 401、内部故障 500（ADR-0004）。
pub async fn login(
    State(state): State<AppState>,
    body: JsonBody<LoginRequest>,
) -> Result<Response, ApiError> {
    let req = body.0;
    // 邮箱大小写不敏感，用户名大小写敏感（ADR-0015）：只有含 @ 的标识才小写化
    let raw_identifier = req.identifier.trim();
    let identifier = if raw_identifier.contains('@') {
        canonicalize_email(raw_identifier).unwrap_or_else(|| raw_identifier.to_lowercase())
    } else {
        raw_identifier.to_owned()
    };

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

    let user_id = credentials.user.id;
    let verified_hash = credentials.password_hash.clone();
    match verify_login_password(credentials.password_hash, req.password.clone()).await? {
        PasswordCheck::Mismatch => return Err(ApiError::InvalidCredentials),
        PasswordCheck::Current => {}
        PasswordCheck::Outdated(upgraded) => {
            // 口令已确认正确；升级写回失败不阻断登录，旧参数仍可校验，记录后果便于排查。
            // 比较并交换防止并发改密写入的新哈希被旧口令的重算结果覆盖。
            match state.user_repo.upgrade_password_hash(&user_id, &verified_hash, &upgraded).await {
                Ok(true) => {}
                Ok(false) => {
                    tracing::warn!(user_id = %user_id, "口令哈希已被并发改写，跳过升级写回");
                }
                Err(err) => {
                    tracing::warn!(
                        error = %err,
                        user_id = %user_id,
                        "口令哈希参数升级写回失败，旧参数继续生效"
                    );
                }
            }
        }
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

    let tokens = issue_session(&state, user.id, access.token_version).await?;
    crate::api::record_audit(
        &state,
        user.id,
        AuditAction::AuthLogin,
        Some(user.id),
        Some(serde_json::json!({ "method": "password" })),
    )
    .await;
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
