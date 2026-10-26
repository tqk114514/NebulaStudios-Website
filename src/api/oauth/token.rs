//! POST /api/oauth/token：授权码兑换与 refresh 轮转（RFC 6749 §3.2）。
//! 表单编码请求、RFC 错误形状（ADR-0004 §6 例外）；`plain` PKCE 不存在（红线 #9）。

use std::sync::OnceLock;

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use crate::api::AppState;
use crate::model::oauth2::{TokenError, verify_pkce_s256};
use crate::model::password::PasswordHash;
use crate::model::tokens::{AccessClaims, NewRefreshToken, RefreshToken, sign_access};
use crate::model::user::UserId;
use crate::storage::{OAuthRepo, RefreshTokenRepo, RotateRefreshError};
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
/// 本端点错误以 RFC 6749 形状经 [`rfc_error`] 返回，不经统一信封。
pub async fn token(State(state): State<AppState>, body: Bytes) -> Response {
    let req: TokenRequest = match serde_urlencoded::from_bytes(&body) {
        Ok(req) => req,
        Err(_) => return rfc_error(StatusCode::BAD_REQUEST, TokenError::InvalidRequest),
    };
    match req.grant_type.as_str() {
        "authorization_code" => authorization_code_grant(state, req).await,
        "refresh_token" => refresh_grant(state, req).await,
        _ => rfc_error(StatusCode::BAD_REQUEST, TokenError::UnsupportedGrantType),
    }
}

/// 授权码兑换：client 认证 → 码校验（归属/时效/redirect/PKCE）→ 原子消费 → 签发对。
async fn authorization_code_grant(state: AppState, req: TokenRequest) -> Response {
    let Some(client) = authenticated_client(&state, &req).await else {
        return rfc_error(StatusCode::UNAUTHORIZED, TokenError::InvalidClient);
    };

    let invalid = || rfc_error(StatusCode::BAD_REQUEST, TokenError::InvalidGrant);
    let code_hash = RefreshToken::hash_of(req.code.trim());
    let Ok(Some(record)) = state.oauth_repo.find_code(&code_hash).await else {
        return invalid();
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

    match issue_pair(&state, record.user_id, Some(client.id.clone()), record.scope).await {
        Ok(response) => response,
        Err(err) => internal(err),
    }
}

/// `refresh` 流：与第一方同一轮转/重放机制（家族带 `client_id` 归属）。
async fn refresh_grant(state: AppState, req: TokenRequest) -> Response {
    let Some(client) = authenticated_client(&state, &req).await else {
        return rfc_error(StatusCode::UNAUTHORIZED, TokenError::InvalidClient);
    };

    let invalid = || rfc_error(StatusCode::BAD_REQUEST, TokenError::InvalidGrant);
    let presented = RefreshToken::hash_of(req.refresh_token.trim());
    let replacement = RefreshToken::generate();
    let now = state.clock.now();
    let rotated = state
        .refresh_repo
        .rotate(&presented, &replacement.to_hash_hex(), now + state.session.refresh_ttl, now)
        .await;
    match rotated {
        Ok(user_id) => {
            if let Ok(response) =
                issue_pair(&state, user_id, Some(client.id.clone()), "profile".to_owned()).await
            {
                response
            } else {
                internal(anyhow::anyhow!("签发第三方令牌对失败"))
            }
        }
        // 第三方重放同样整族撤销（该 client 名下的全部会话）
        Err(RotateRefreshError::Replay | RotateRefreshError::Invalid) => invalid(),
        Err(RotateRefreshError::Storage(err)) => internal(anyhow::Error::new(err)),
    }
}

/// client 认证：存在且启用 + 密钥 Argon2id 校验；哑哈希对齐无密钥路径的耗时。
async fn authenticated_client(
    state: &AppState,
    req: &TokenRequest,
) -> Option<crate::model::oauth2::OAuthClient> {
    let client = state
        .oauth_repo
        .find_client(&req.client_id)
        .await
        .ok()
        .flatten()
        .filter(|client| client.enabled)?;
    let stored = state.oauth_repo.find_client_secret(&client.id).await.ok().flatten();
    let plain = req.client_secret.clone();
    let ok = if let Some(encoded) = stored {
        let hash = PasswordHash::from_encoded(encoded).ok()?;
        tokio::task::spawn_blocking(move || hash.verify(&plain)).await.ok()?.ok()?
    } else {
        // 公共客户端（未设密钥）拒绝；哑哈希对齐耗时，`client_id` 枚举不可测
        let _ = dummy_verify(req.client_secret.clone()).await;
        false
    };
    ok.then_some(client)
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

/// 签发第三方对：access JWT + 挂 `client_id` 的 refresh 家族。
async fn issue_pair(
    state: &AppState,
    user_id: UserId,
    client_id: Option<String>,
    scope: String,
) -> Result<Response, anyhow::Error> {
    let now = state.clock.now();
    let claims = AccessClaims::new(user_id, now, state.session.access_ttl);
    let access = sign_access(&claims, state.jwt.encode())?;
    let refresh = RefreshToken::generate();
    let entry = NewRefreshToken {
        family_id: Uuid::now_v7(),
        user_id,
        token_hash: refresh.to_hash_hex(),
        expires_at: now + state.session.refresh_ttl,
        client_id,
    };
    state.refresh_repo.create_family(&entry, now).await?;
    Ok((
        StatusCode::OK,
        [(header::CACHE_CONTROL, "no-store"), (header::PRAGMA, "no-cache")],
        axum::Json(TokenResponse {
            access_token: access,
            token_type: "Bearer",
            expires_in: state.session.access_ttl.num_seconds(),
            refresh_token: refresh.as_str().to_owned(),
            scope,
        }),
    )
        .into_response())
}

/// RFC 6749 错误响应（`{"error": "..."}`，no-store）。
fn rfc_error(status: StatusCode, error: TokenError) -> Response {
    (
        status,
        [(header::CACHE_CONTROL, "no-store"), (header::PRAGMA, "no-cache")],
        axum::Json(serde_json::json!({ "error": error.as_str() })),
    )
        .into_response()
}

fn internal(err: impl Into<anyhow::Error>) -> Response {
    tracing::error!(error = ?err.into(), "oauth token internal error");
    (StatusCode::INTERNAL_SERVER_ERROR, axum::Json(serde_json::json!({ "error": "server_error" })))
        .into_response()
}
