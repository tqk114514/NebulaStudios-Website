//! `GET /api/oauth/authorize`（consent 页取数）与
//! `POST /api/oauth/authorize/consent`（登录用户同意 → 302 回携码）。
//! 参数校验失败一律 400 统一信封：`redirect_uri` 未经验证前不得向其跳转，
//! 第三方错误页回传（RFC 6749 §4.1.2.1）由后续切片按已验证前提交接。

use axum::body::Bytes;
use axum::extract::State;
use axum::http::{HeaderMap, StatusCode, header};
use axum::response::{IntoResponse, Response};
use serde::Deserialize;

use crate::api::AppState;
use crate::error::ApiError;
use crate::extractors::AuthUser;
use crate::model::oauth2::{AUTH_CODE_TTL, NewAuthorizationCode, valid_challenge, valid_scope};
use crate::model::tokens::RefreshToken;
use crate::storage::OAuthRepo;

/// authorize 查询参数（RFC 6749 §4.1.1）。
#[derive(Debug, Deserialize)]
pub struct AuthorizeQuery {
    /// 客户端标识。
    pub client_id: String,
    /// 回跳地址（精确匹配）。
    pub redirect_uri: String,
    /// 响应类型；仅接受 `code`。
    pub response_type: String,
    /// `scope`（空格分隔，可为空）。
    #[serde(default)]
    pub scope: String,
    /// PKCE challenge（S256）。
    pub code_challenge: String,
    /// PKCE 方法；仅接受 `S256`（红线 #9）。
    pub code_challenge_method: String,
    /// 防CSRF/重放的透传值。
    #[serde(default)]
    pub state: String,
}

/// consent 请求体。
#[derive(Debug, Deserialize)]
pub struct ConsentRequest {
    /// 客户端标识。
    pub client_id: String,
    /// 回跳地址。
    pub redirect_uri: String,
    /// `scope`。
    #[serde(default)]
    pub scope: String,
    /// PKCE challenge。
    pub code_challenge: String,
    /// 透传值。
    #[serde(default)]
    pub state: String,
}

/// 校验授权请求的全部参数；通过返回客户端快照。
/// 这是 GET 与 POST 两条路径共用的校验面。
pub(crate) async fn validate_authorize(
    state: &AppState,
    query: &AuthorizeQuery,
) -> Result<crate::model::oauth2::OAuthClient, ApiError> {
    let Some(client) =
        state.oauth_repo.find_client(&query.client_id).await.map_err(anyhow::Error::from)?
    else {
        return Err(bad("invalid_client"));
    };
    if !client.enabled {
        return Err(bad("invalid_client"));
    }
    // 精确匹配（v3 语义；不识别通配）
    if client.redirect_uri != query.redirect_uri {
        return Err(bad("invalid_redirect_uri"));
    }
    if query.response_type != "code" {
        return Err(bad("unsupported_response_type"));
    }
    if !valid_scope(&query.scope) {
        return Err(bad("invalid_scope"));
    }
    // PKCE 强制 S256：缺 method、method 非 S256、challenge 非法全部拒绝（红线 #9）
    if query.code_challenge_method != "S256" {
        return Err(bad("invalid_request"));
    }
    if !valid_challenge(&query.code_challenge) {
        return Err(bad("invalid_request"));
    }
    Ok(client)
}

fn bad(code: &'static str) -> ApiError {
    ApiError::OAuthAuthorize(code)
}

/// 处理 GET：返回 consent 页渲染所需的客户端信息（ADR-0004 裸数据）。
///
/// # Errors
/// 参数非法 400；内部故障 500。
pub async fn authorize(
    State(state): State<AppState>,
    axum::extract::Query(query): axum::extract::Query<AuthorizeQuery>,
) -> Result<Response, ApiError> {
    let client = validate_authorize(&state, &query).await?;
    Ok(axum::Json(serde_json::json!({
        "client_name": client.name,
        "scope": query.scope,
        "state": query.state,
    }))
    .into_response())
}

/// 处理 consent：重复全量校验（防 TOCTOU）→ 签发授权码 → 302 回携码。
/// CSRF 由 [`AuthUser`] 内建门禁承担。
///
/// # Panics
/// 构造的 `Location` 值含非法头字符时 panic——组成部分均经校验或编码，触发即实现缺陷。
///
/// # Errors
/// 未认证 401；参数非法 400；内部故障 500。
pub async fn consent(
    State(state): State<AppState>,
    user: AuthUser,
    body: Bytes,
) -> Result<Response, ApiError> {
    let req: ConsentRequest =
        serde_json::from_slice(&body).map_err(|err| ApiError::MalformedJson(err.to_string()))?;
    let query = AuthorizeQuery {
        client_id: req.client_id,
        redirect_uri: req.redirect_uri,
        response_type: "code".to_owned(),
        scope: req.scope,
        code_challenge: req.code_challenge,
        code_challenge_method: "S256".to_owned(),
        state: req.state,
    };
    let client = validate_authorize(&state, &query).await?;

    // 码明文只在 302 Location 出现一次，库中存哈希
    let code = RefreshToken::generate();
    let entry = NewAuthorizationCode {
        client_id: client.id,
        user_id: user.user_id,
        redirect_uri: query.redirect_uri.clone(),
        scope: query.scope.clone(),
        code_challenge: query.code_challenge.clone(),
        expires_at: state.clock.now() + AUTH_CODE_TTL,
    };
    state
        .oauth_repo
        .create_code(&code.to_hash_hex(), &entry, state.clock.now())
        .await
        .map_err(anyhow::Error::from)?;

    let mut location = query.redirect_uri.clone();
    location.push_str(if query.redirect_uri.contains('?') { "&" } else { "?" });
    location.push_str("code=");
    location.push_str(code.as_str());
    if !query.state.is_empty() {
        location.push_str("&state=");
        location.push_str(&crate::api::auth::form_urlencode(&query.state));
    }

    let mut headers = HeaderMap::new();
    // redirect_uri 与 state 均经校验/编码，非法头字符只会是缺陷
    if let Ok(value) = location.parse::<header::HeaderValue>() {
        headers.insert(header::LOCATION, value);
    } else {
        unreachable!("Location 值含非法头字符: {location}");
    }
    Ok((StatusCode::FOUND, headers).into_response())
}
