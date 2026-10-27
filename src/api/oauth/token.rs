//! POST /api/oauth/token：授权码兑换与 refresh 轮转（RFC 6749 §3.2）。
//! 表单编码请求、RFC 错误形状（ADR-0004 §6 例外）；`plain` PKCE 不存在（红线 #9）。

use std::sync::OnceLock;

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::rfc_error;
use crate::api::AppState;
use crate::model::oauth2::{TokenError, verify_pkce_s256};
use crate::model::password::PasswordHash;
use crate::model::tokens::{AccessClaims, NewRefreshFamily, RefreshToken, sign_access};
use crate::model::user::UserId;
use crate::storage::{ClientBinding, RotateRefreshError};
use uuid::Uuid;

/// 兑换请求（`application/x-www-form-urlencoded`）。
#[derive(Debug, Deserialize)]
pub struct TokenRequest {
    /// 授权类型；`authorization_code` 或 `refresh_token`。
    pub grant_type: String,
    /// 授权码（`authorization_code` 流）。
    #[serde(default)]
    pub code: String,
    /// 回跳地址；须与 authorize 时一致。
    #[serde(default)]
    pub redirect_uri: String,
    /// PKCE verifier。
    #[serde(default)]
    pub code_verifier: String,
    /// refresh token（`refresh` 流）。
    #[serde(default)]
    pub refresh_token: String,
    /// 客户端标识。
    pub client_id: String,
    /// 客户端密钥（明文只在请求中出现，库中 Argon2id）。
    #[serde(default)]
    pub client_secret: String,
}

/// RFC 6749 成功响应。
#[derive(Debug, serde::Serialize)]
pub struct TokenResponse {
    /// 签发的 access token（ES256 JWT）。
    pub access_token: String,
    /// 恒为 `Bearer`。
    pub token_type: &'static str,
    /// access 有效秒数。
    pub expires_in: i64,
    /// 轮转出的 refresh token。
    pub refresh_token: String,
    /// 授权范围。
    pub scope: String,
}

/// 处理 token 端点：分发 `grant_type`。
///
/// # Errors
/// 本端点错误以 RFC 6749 形状经 `rfc_error` 返回，不经统一信封。
pub async fn token(State(state): State<AppState>, body: Bytes) -> Response {
    let req: TokenRequest = match serde_urlencoded::from_bytes(&body) {
        Ok(req) => req,
        Err(_) => return rfc_error(StatusCode::BAD_REQUEST, TokenError::InvalidRequest.as_str()),
    };
    match req.grant_type.as_str() {
        "authorization_code" => authorization_code_grant(state, req).await,
        "refresh_token" => refresh_grant(state, req).await,
        _ => rfc_error(StatusCode::BAD_REQUEST, TokenError::UnsupportedGrantType.as_str()),
    }
}

/// 授权码兑换：client 认证 → 码校验（归属/时效/redirect/PKCE）→ 原子消费 → 签发对。
async fn authorization_code_grant(state: AppState, req: TokenRequest) -> Response {
    let client = match authenticated_client(&state, &req).await {
        Ok(Some(client)) => client,
        Ok(None) => {
            return rfc_error(StatusCode::UNAUTHORIZED, TokenError::InvalidClient.as_str());
        }
        Err(err) => return internal(err),
    };

    let invalid = || rfc_error(StatusCode::BAD_REQUEST, TokenError::InvalidGrant.as_str());
    let code_hash = RefreshToken::hash_of(req.code.trim());
    let record = match state.oauth_repo.find_code(&code_hash).await {
        Ok(Some(record)) => record,
        Ok(None) => return invalid(),
        Err(err) => return internal(anyhow::Error::new(err)),
    };
    let now = state.clock.now();
    if record.expires_at <= now {
        return invalid();
    }
    if record.client_id != client.id || record.redirect_uri != req.redirect_uri {
        return invalid();
    }
    if !verify_pkce_s256(req.code_verifier.trim(), &record.code_challenge) {
        return invalid();
    }
    // 原子消费：并发重放只有一个赢家
    let Ok(consumed) = state.oauth_repo.consume_code(&record.id, now).await else {
        return internal(anyhow::anyhow!("消费授权码失败"));
    };
    if !consumed {
        return invalid();
    }

    match issue_client_grant(&state, record.user_id, &client.id, &record.scope).await {
        Ok(response) => response,
        Err(err) => internal(err),
    }
}

/// `refresh` 流：在原家族内轮转，家族客户端与 scope 由存储层校验并返回。
async fn refresh_grant(state: AppState, req: TokenRequest) -> Response {
    let client = match authenticated_client(&state, &req).await {
        Ok(Some(client)) => client,
        Ok(None) => {
            return rfc_error(StatusCode::UNAUTHORIZED, TokenError::InvalidClient.as_str());
        }
        Err(err) => return internal(err),
    };

    let invalid = || rfc_error(StatusCode::BAD_REQUEST, TokenError::InvalidGrant.as_str());
    let presented = RefreshToken::hash_of(req.refresh_token.trim());
    let replacement = RefreshToken::generate();
    let now = state.clock.now();
    let rotated = state
        .refresh_repo
        .rotate(
            &presented,
            &replacement.to_hash_hex(),
            now + state.session.refresh_ttl,
            now,
            ClientBinding::Client(&client.id),
        )
        .await;
    match rotated {
        Ok(session) => {
            match sign_client_access(&state, session.user_id, &client.id, &session.scope, now) {
                Ok(access) => token_response(
                    access,
                    replacement.as_str(),
                    &session.scope,
                    state.session.access_ttl.num_seconds(),
                ),
                Err(err) => internal(err),
            }
        }
        // 重放、无效与跨客户端呈递都按 invalid_grant 返回，不向调用方泄露令牌归属
        Err(
            RotateRefreshError::Replay
            | RotateRefreshError::Invalid
            | RotateRefreshError::ClientMismatch,
        ) => invalid(),
        Err(RotateRefreshError::Storage(err)) => internal(anyhow::Error::new(err)),
    }
}

/// client 认证：存在且启用 + 密钥 Argon2id 校验；哑哈希对齐无密钥路径的耗时。
/// `Ok(None)` 表示认证失败；存储故障与哈希数据损坏走 `Err`，不得伪装成客户端错误。
async fn authenticated_client(
    state: &AppState,
    req: &TokenRequest,
) -> Result<Option<crate::model::oauth2::OAuthClient>, anyhow::Error> {
    let Some(client) =
        state.oauth_repo.find_client(&req.client_id).await.map_err(anyhow::Error::new)?
    else {
        return Ok(None);
    };
    if !client.enabled {
        return Ok(None);
    }

    let stored =
        state.oauth_repo.find_client_secret(&client.id).await.map_err(anyhow::Error::new)?;
    let plain = req.client_secret.clone();
    let ok = if let Some(encoded) = stored {
        let hash = PasswordHash::from_encoded(encoded).map_err(anyhow::Error::new)?;
        tokio::task::spawn_blocking(move || hash.verify(&plain))
            .await
            .map_err(anyhow::Error::from)?
            .map_err(anyhow::Error::new)?
    } else {
        // 公共客户端（未设密钥）拒绝；哑哈希对齐耗时，`client_id` 枚举不可测
        let _ = dummy_verify(req.client_secret.clone()).await;
        false
    };
    Ok(ok.then_some(client))
}

/// 哑哈希只用于对齐「客户端无密钥」路径的耗时（与 login 同策略）。
static DUMMY_HASH: OnceLock<Result<PasswordHash, String>> = OnceLock::new();

async fn dummy_verify(plain: String) -> bool {
    let stored = DUMMY_HASH.get_or_init(|| {
        PasswordHash::hash("nebula-oauth-timing-equalizer").map_err(|err| err.to_string())
    });
    match stored {
        Ok(hash) => {
            let hash = hash.clone();
            tokio::task::spawn_blocking(move || hash.verify(&plain))
                .await
                .ok()
                .and_then(Result::ok)
                .unwrap_or(false)
        }
        Err(_) => false,
    }
}

/// 授权码首次兑换：创建以 client + scope 固定的家族并返回令牌对。
async fn issue_client_grant(
    state: &AppState,
    user_id: UserId,
    client_id: &str,
    scope: &str,
) -> Result<Response, anyhow::Error> {
    let now = state.clock.now();
    // 先签 access：非法 scope 在落库前失败，不会留下没有对应响应的孤儿家族
    let access = sign_client_access(state, user_id, client_id, scope, now)?;
    let refresh = RefreshToken::generate();
    let family = NewRefreshFamily {
        family_id: Uuid::now_v7(),
        user_id,
        client_id: Some(client_id.to_owned()),
        scope: scope.to_owned(),
        token_hash: refresh.to_hash_hex(),
        expires_at: now + state.session.refresh_ttl,
    };
    state.refresh_repo.create_family(&family, now).await?;
    Ok(token_response(access, refresh.as_str(), scope, state.session.access_ttl.num_seconds()))
}

/// 签发第三方 access JWT；`audience`、`client_id` 与 `scope` 由 claims 构造函数固定。
fn sign_client_access(
    state: &AppState,
    user_id: UserId,
    client_id: &str,
    scope: &str,
    now: chrono::DateTime<chrono::Utc>,
) -> Result<String, anyhow::Error> {
    let claims =
        AccessClaims::client_grant(user_id, client_id, scope, now, state.session.access_ttl)?;
    Ok(sign_access(&claims, state.jwt.encode())?)
}

/// RFC 6749 成功响应；refresh 明文由调用方持有，本函数不触碰存储。
fn token_response(access: String, refresh: &str, scope: &str, expires_in: i64) -> Response {
    (
        StatusCode::OK,
        [(header::CACHE_CONTROL, "no-store"), (header::PRAGMA, "no-cache")],
        axum::Json(TokenResponse {
            access_token: access,
            token_type: "Bearer",
            expires_in,
            refresh_token: refresh.to_owned(),
            scope: scope.to_owned(),
        }),
    )
        .into_response()
}

fn internal(err: impl Into<anyhow::Error>) -> Response {
    tracing::error!(error = ?err.into(), "oauth token internal error");
    super::rfc_error(StatusCode::INTERNAL_SERVER_ERROR, "server_error")
}
