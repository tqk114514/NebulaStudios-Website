//! POST /api/auth/login：口令登录（ADR-0002 语义；TOTP 二段随 TOTP 切片接入，
//! 接入后此处改为签发中转 token 而非直接建会话）。

use std::sync::OnceLock;

use axum::Json;
use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::{
    SessionTokens, cookie_max_age, issue_session, set_access_cookie, set_csrf_cookie,
    set_refresh_cookie,
};
use crate::api::AppState;
use crate::error::{ApiError, FieldInvalid};
use crate::model::password::PasswordHash;
use crate::model::user::User;
use crate::storage::UserRepo;

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
        let _ = verify_in_background(dummy, req.password.clone()).await?;
        return Err(ApiError::InvalidCredentials);
    };

    let ok = verify_in_background(credentials.password_hash, req.password.clone()).await?;
    if !ok {
        return Err(ApiError::InvalidCredentials);
    }
    let user = credentials.user;
    let tokens = issue_session(&state, user.id).await?;
    Ok(session_response(StatusCode::OK, &state, &user, &tokens))
}

/// 哑哈希只用于对齐「用户不存在」路径的耗时；生成失败（OS 随机源故障）按 500，
/// 此故障下签发真会话同样不可能。
static DUMMY_HASH: OnceLock<Result<PasswordHash, String>> = OnceLock::new();

fn dummy_hash() -> Result<PasswordHash, ApiError> {
    let stored = DUMMY_HASH.get_or_init(|| {
        PasswordHash::hash("nebula-timing-equalizer").map_err(|err| err.to_string())
    });
    match stored {
        Ok(hash) => Ok(hash.clone()),
        Err(msg) => Err(ApiError::Internal(anyhow::anyhow!("哑哈希不可用: {msg}"))),
    }
}

/// 在阻塞线程池上校验口令；存储哈希损坏（非口令错）按 500 处理。
async fn verify_in_background(hash: PasswordHash, plain: String) -> Result<bool, ApiError> {
    tokio::task::spawn_blocking(move || hash.verify(&plain))
        .await
        .map_err(anyhow::Error::from)?
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))
}

/// 组装带会话 cookie 的响应；登录返回用户本体（ADR-0004 裸数据）。
fn session_response(
    status: StatusCode,
    state: &AppState,
    user: &User,
    tokens: &SessionTokens,
) -> Response {
    let mut headers = HeaderMap::new();
    set_access_cookie(&mut headers, &tokens.access, cookie_max_age(state.session.access_ttl));
    set_refresh_cookie(&mut headers, &tokens.refresh, cookie_max_age(state.session.refresh_ttl));
    set_csrf_cookie(&mut headers, &tokens.csrf, cookie_max_age(state.session.refresh_ttl));
    (status, headers, Json(user)).into_response()
}
