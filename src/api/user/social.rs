//! 社交身份绑定管理端点（ADR-0012）：发起绑定、列出已绑、解绑。
//! 绑定以当前口令再认证后发起（与 TOTP setup/enable 同款，ADR-0002）；
//! 实际绑定在 provider 回调中完成（见 `api/auth/social.rs`）。

use axum::Json;
use axum::body::Bytes;
use axum::extract::{Path, State};
use axum::http::StatusCode;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::api::{AppState, parse_social_provider};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;
use crate::model::oauth2::{code_challenge_s256, generate_code_verifier};
use crate::model::social::{SOCIAL_STATE_TTL, SocialLoginAction, SocialLoginState};
use crate::model::tokens::random_hex_32;

/// 绑定发起请求；写操作要求当前口令，防止被盗会话绑定攻击者身份。
#[derive(Debug, Deserialize)]
pub struct LinkRequest {
    /// 当前登录口令。
    pub password: String,
}

/// 绑定发起响应；客户端应导航到该地址完成绑定。
#[derive(Debug, Serialize)]
pub struct LinkResponse {
    /// provider 授权页地址。
    pub authorize_url: String,
}

/// 已完成绑定的视图；不回传 provider 侧用户 id。
#[derive(Debug, Serialize)]
pub struct BindingView {
    /// provider 标识。
    pub provider: &'static str,
    /// 展示昵称。
    pub display_name: Option<String>,
    /// 绑定时刻。
    pub created_at: DateTime<Utc>,
}

/// 已绑身份列表响应（ADR-0004 裸数据）。
#[derive(Debug, Serialize)]
pub struct ListResponse {
    /// 已绑定的身份。
    pub identities: Vec<BindingView>,
}

/// 发起绑定：口令再认证 → 生成 state 与 PKCE 落库 → 返回授权页地址。
///
/// # Errors
/// 未认证 401；口令错 401 `invalid_credentials`；字段缺失 422；provider 未启用 404；
/// 内部故障 500。
pub async fn link(
    State(state): State<AppState>,
    user: FirstPartyUser,
    Path(provider): Path<String>,
    body: Bytes,
) -> Result<Json<LinkResponse>, ApiError> {
    let provider = parse_social_provider(&provider)?;
    let client = state.social_clients.get(provider).ok_or(ApiError::SocialProviderDisabled)?;
    let req: LinkRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    if req.password.is_empty() {
        return Err(ApiError::Validation(vec![FieldInvalid {
            field: "password",
            code: "password_missing".to_owned(),
        }]));
    }
    crate::api::verify_current_password(&state, &user.user_id, &req.password).await?;

    let now = state.clock.now();
    let nonce = random_hex_32();
    let verifier = generate_code_verifier();
    let challenge = code_challenge_s256(&verifier);
    state
        .social_repo
        .put_state(
            &SocialLoginState {
                state: nonce.clone(),
                provider,
                code_verifier: verifier,
                action: SocialLoginAction::Link,
                user_id: Some(user.user_id),
                expires_at: now + SOCIAL_STATE_TTL,
            },
            now,
        )
        .await
        .map_err(anyhow::Error::from)?;

    Ok(Json(LinkResponse { authorize_url: client.authorize_url(&nonce, &challenge) }))
}

/// 列出当前用户已绑定的社交身份。
///
/// # Errors
/// 未认证 401；内部故障 500。
pub async fn list(
    State(state): State<AppState>,
    user: FirstPartyUser,
) -> Result<Json<ListResponse>, ApiError> {
    let identities =
        state.social_repo.list_for_user(&user.user_id).await.map_err(anyhow::Error::from)?;
    let identities = identities
        .into_iter()
        .map(|identity| BindingView {
            provider: identity.provider.as_str(),
            display_name: identity.display_name,
            created_at: identity.created_at,
        })
        .collect();
    Ok(Json(ListResponse { identities }))
}

/// 解绑指定 provider。
///
/// # Errors
/// 未认证 401；未绑定该 provider 409 `social_not_linked`；内部故障 500。
pub async fn unlink(
    State(state): State<AppState>,
    user: FirstPartyUser,
    Path(provider): Path<String>,
) -> Result<StatusCode, ApiError> {
    let provider = parse_social_provider(&provider)?;
    let removed =
        state.social_repo.unlink(&user.user_id, provider).await.map_err(anyhow::Error::from)?;
    if !removed {
        return Err(ApiError::SocialNotLinked);
    }
    Ok(StatusCode::NO_CONTENT)
}
