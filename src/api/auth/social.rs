//! 社交登录端点（ADR-0012）：`GET /api/auth/{provider}` 起授权，
//! `GET /api/auth/{provider}/callback` 完成登录或绑定。
//!
//! 回调是浏览器导航，故失败也以 302 回前端并带 `social_error`，不返回 JSON 信封。
//! 未绑定的身份一律拒绝：不比对邮箱、不自动建号（ADR-0002）。

use axum::extract::{Path, RawQuery, State};
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use super::{SessionTokens, issue_session};
use crate::api::{
    AppState, cookie_max_age, set_access_cookie, set_csrf_cookie, set_refresh_cookie,
};
use crate::error::ApiError;
use crate::model::oauth2::{code_challenge_s256, generate_code_verifier};
use crate::model::social::{
    NewSocialIdentity, SOCIAL_STATE_TTL, SocialLoginAction, SocialLoginState, SocialProvider,
};
use crate::model::tokens::random_hex_32;
use crate::storage::LinkIdentityError;

/// 登录流程的前端落点。
const LOGIN_PATH: &str = "/account/login";
/// 绑定流程的前端落点。
const SETTINGS_PATH: &str = "/account/settings";

/// 回调查询参数。
#[derive(Debug, Deserialize)]
pub struct CallbackQuery {
    /// 授权码。
    #[serde(default)]
    code: String,
    /// 发起时写入的中转态。
    #[serde(default)]
    state: String,
}

/// 回调失败：向前端回跳的路径与状态标识。
struct Failure {
    /// 前端路径。
    path: &'static str,
    /// 状态标识，随 `social_error` 回传。
    slug: &'static str,
}

impl Failure {
    /// 登录流程的失败，落回登录页。
    fn login(slug: &'static str) -> Self {
        Self { path: LOGIN_PATH, slug }
    }

    /// 绑定流程的失败，落回设置页。
    fn settings(slug: &'static str) -> Self {
        Self { path: SETTINGS_PATH, slug }
    }
}

/// 处理起授权：生成 state 与 PKCE、落库中转态，302 到 provider 授权页。
///
/// # Errors
/// provider 未启用或不存在返回 [`ApiError::SocialProviderDisabled`]；落库失败或
/// 回跳地址非法返回 [`ApiError::Internal`]。
pub async fn start(
    State(state): State<AppState>,
    Path(provider): Path<String>,
) -> Result<Response, ApiError> {
    let provider = crate::api::parse_social_provider(&provider)?;
    let client = state.social_clients.get(provider).ok_or(ApiError::SocialProviderDisabled)?;

    let now = state.clock.now();
    let state_nonce = random_hex_32();
    let verifier = generate_code_verifier();
    let challenge = code_challenge_s256(&verifier);
    let entry = SocialLoginState {
        state: state_nonce.clone(),
        provider,
        code_verifier: verifier,
        action: SocialLoginAction::Login,
        user_id: None,
        expires_at: now + SOCIAL_STATE_TTL,
    };
    state.social_repo.put_state(&entry, now).await.map_err(anyhow::Error::from)?;

    location_response(&client.authorize_url(&state_nonce, &challenge))
}

/// 处理回调：取出中转态 → 换身份 → 登录（按绑定）或绑定。
///
/// 一切结果都以 302 回前端：成功带 Cookie 与 `social=ok`，失败带 `social_error`。
pub async fn callback(
    State(state): State<AppState>,
    Path(provider): Path<String>,
    RawQuery(raw): RawQuery,
) -> Response {
    match complete(&state, &provider, raw.as_deref()).await {
        Ok(response) => response,
        Err(failure) => frontend_redirect(&state, failure.path, "social_error", failure.slug),
    }
}

/// 回调主体；失败以 [`Failure`] 表达跳转意图。
async fn complete(
    state: &AppState,
    provider_raw: &str,
    raw: Option<&str>,
) -> Result<Response, Failure> {
    let provider = SocialProvider::from_str_raw(provider_raw)
        .ok_or_else(|| Failure::login("unknown_provider"))?;
    let client =
        state.social_clients.get(provider).ok_or_else(|| Failure::login("provider_disabled"))?;
    let Some(raw) = raw else { return Err(Failure::login("invalid_request")) };
    let query: CallbackQuery =
        serde_urlencoded::from_str(raw).map_err(|_| Failure::login("invalid_request"))?;
    if query.code.is_empty() || query.state.is_empty() {
        return Err(Failure::login("invalid_request"));
    }

    let now = state.clock.now();
    let pending = match state.social_repo.take_state(&query.state).await {
        Ok(Some(pending)) => pending,
        Ok(None) => return Err(Failure::login("invalid_state")),
        Err(err) => {
            tracing::error!(error = ?err, "读取社交中转态失败");
            return Err(Failure::login("server_error"));
        }
    };
    // state 与路径 provider 必须一致：跨 provider 呈递视为无效
    if pending.provider != provider {
        return Err(Failure::login("invalid_state"));
    }
    if pending.expires_at <= now {
        return Err(Failure::login("expired"));
    }

    let identity =
        client.fetch_identity(&query.code, &pending.code_verifier).await.map_err(|err| {
            tracing::warn!(error = %err, "社交取数失败");
            Failure::login("provider_error")
        })?;

    match pending.action {
        SocialLoginAction::Login => complete_login(state, provider, identity, now).await,
        SocialLoginAction::Link => {
            complete_link(state, provider, identity, pending.user_id, now).await
        }
    }
}

/// 登录：按身份查绑定，命中即签发会话；未命中一律拒绝（不建号、不比对邮箱）。
async fn complete_login(
    state: &AppState,
    provider: SocialProvider,
    identity: crate::social::ProviderIdentity,
    now: chrono::DateTime<chrono::Utc>,
) -> Result<Response, Failure> {
    let binding = match state.social_repo.find_identity(provider, &identity.provider_user_id).await
    {
        Ok(Some(binding)) => binding,
        Ok(None) => return Err(Failure::login("not_linked")),
        Err(err) => {
            tracing::error!(error = ?err, "查询社交绑定失败");
            return Err(Failure::login("server_error"));
        }
    };
    // 账户可能已被删除或封禁；与第一方提取器同一判定
    let access = match state.user_repo.find_access_state(&binding.user_id, now).await {
        Ok(Some(access)) => access,
        Ok(None) => return Err(Failure::login("account_unavailable")),
        Err(err) => {
            tracing::error!(error = ?err, "查询账户状态失败");
            return Err(Failure::login("server_error"));
        }
    };
    if access.ban.is_some() {
        return Err(Failure::login("account_unavailable"));
    }

    let tokens =
        issue_session(state, binding.user_id, access.token_version).await.map_err(|err| {
            tracing::error!(error = ?err, "签发社交会话失败");
            Failure::login("server_error")
        })?;
    Ok(session_redirect(state, &tokens, "ok"))
}

/// 绑定：把身份绑到中转态里记录的用户。
async fn complete_link(
    state: &AppState,
    provider: SocialProvider,
    identity: crate::social::ProviderIdentity,
    user_id: Option<crate::model::user::UserId>,
    now: chrono::DateTime<chrono::Utc>,
) -> Result<Response, Failure> {
    let Some(user_id) = user_id else { return Err(Failure::settings("invalid_state")) };
    let new = NewSocialIdentity {
        user_id,
        provider,
        provider_user_id: identity.provider_user_id,
        display_name: identity.display_name,
    };
    match state.social_repo.link_identity(&new, now).await {
        Ok(()) => Ok(frontend_redirect(state, SETTINGS_PATH, "social", "linked")),
        Err(LinkIdentityError::Conflict) => Err(Failure::settings("already_linked")),
        Err(LinkIdentityError::Storage(err)) => {
            tracing::error!(error = ?err, "绑定社交身份失败");
            Err(Failure::settings("server_error"))
        }
    }
}

/// 302 到给定的绝对地址；地址非法按内部错误处理。
fn location_response(location: &str) -> Result<Response, ApiError> {
    let value = location
        .parse::<header::HeaderValue>()
        .map_err(|err| ApiError::Internal(anyhow::Error::new(err)))?;
    let mut headers = HeaderMap::new();
    headers.insert(header::LOCATION, value);
    Ok((StatusCode::FOUND, headers).into_response())
}

/// 回前端并附带状态查询参数；回跳地址非法时记录日志并回 500。
fn frontend_redirect(state: &AppState, path: &str, key: &str, value: &str) -> Response {
    let base = state.social_base_url.trim_end_matches('/');
    let location = format!("{base}{path}?{key}={value}");
    if let Ok(response) = location_response(&location) {
        return response;
    }
    tracing::error!(location, "社交回跳地址非法");
    ApiError::Internal(anyhow::anyhow!("社交回跳地址非法")).into_response()
}

/// 登录成功：下发会话三 cookie 并 302 回登录页。
fn session_redirect(state: &AppState, tokens: &SessionTokens, status: &str) -> Response {
    let mut headers = HeaderMap::new();
    set_access_cookie(&mut headers, &tokens.access, cookie_max_age(state.session.access_ttl));
    set_refresh_cookie(&mut headers, &tokens.refresh, cookie_max_age(state.session.refresh_ttl));
    set_csrf_cookie(&mut headers, &tokens.csrf, cookie_max_age(state.session.refresh_ttl));

    let base = state.social_base_url.trim_end_matches('/');
    let location = format!("{base}{LOGIN_PATH}?social={status}");
    if let Ok(mut response) = location_response(&location) {
        for (name, value) in &headers {
            response.headers_mut().append(name, value.clone());
        }
        return response;
    }
    tracing::error!(location, "社交回跳地址非法");
    ApiError::Internal(anyhow::anyhow!("社交回跳地址非法")).into_response()
}
