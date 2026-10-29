//! Google 身份客户端：授权页直连 `accounts.google.com`，取数经 Cloudflare Worker
//! 代理（ADR-0012）。身份取自 Worker 已背书的 `id_token`，不读未签名的 userinfo。

use std::fmt;
use std::sync::Arc;

use async_trait::async_trait;
use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use serde::Deserialize;

use super::worker::{WorkerEnvelope, verify_envelope};
use super::{ProviderIdentity, SocialError, SocialProviderClient};
use crate::context::Clock;
use crate::model::social::SocialProvider;

/// Google 授权端点；浏览器直达，不经代理。
const AUTHORIZE_ENDPOINT: &str = "https://accounts.google.com/o/oauth2/v2/auth";
/// 请求范围：`openid` 取身份 subject，`profile` 取昵称。
const SCOPE: &str = "openid profile";

/// Google 客户端配置（装配层从 [`crate::config::SocialConfig`] 映射）。
#[derive(Debug, Clone)]
pub struct GoogleConfig {
    /// OAuth 客户端 id。
    pub client_id: String,
    /// OAuth 客户端密钥。
    pub client_secret: String,
    /// 回跳地址。
    pub redirect_uri: String,
    /// Worker 代理基地址。
    pub proxy_url: String,
    /// 访问 Worker 的 Cloudflare Access service token client id。
    pub proxy_access_client_id: String,
    /// 访问 Worker 的 Cloudflare Access service token secret。
    pub proxy_access_client_secret: String,
    /// Worker 背书公钥（PKIX PEM）。
    pub worker_signing_public_key: String,
}

/// Google 身份客户端。
pub struct GoogleClient {
    config: GoogleConfig,
    http: reqwest::Client,
    clock: Arc<dyn Clock>,
}

#[derive(Debug, Deserialize)]
struct TokenResponse {
    id_token: String,
}

#[derive(Debug, Deserialize)]
struct IdTokenClaims {
    sub: String,
    #[serde(default)]
    name: Option<String>,
}

impl GoogleClient {
    /// 构建客户端；`clock` 用于校验 Worker 背书时间戳。
    ///
    /// # Errors
    /// reqwest 客户端构建失败（TLS 后端初始化失败）时返回 [`SocialError::Transport`]。
    pub fn new(config: GoogleConfig, clock: Arc<dyn Clock>) -> Result<Self, SocialError> {
        let http = reqwest::Client::builder().build().map_err(SocialError::Transport)?;
        Ok(Self { config, http, clock })
    }
}

// 配置含密钥，Debug 只列结构名
impl fmt::Debug for GoogleClient {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("GoogleClient").finish_non_exhaustive()
    }
}

#[async_trait]
impl SocialProviderClient for GoogleClient {
    fn provider(&self) -> SocialProvider {
        SocialProvider::Google
    }

    fn authorize_url(&self, state: &str, code_challenge: &str) -> String {
        let mut url = reqwest::Url::parse(AUTHORIZE_ENDPOINT)
            .unwrap_or_else(|_| unreachable!("google authorize 端点为常量 URL"));
        url.query_pairs_mut()
            .append_pair("client_id", &self.config.client_id)
            .append_pair("redirect_uri", &self.config.redirect_uri)
            .append_pair("response_type", "code")
            .append_pair("scope", SCOPE)
            .append_pair("response_mode", "query")
            .append_pair("state", state)
            .append_pair("code_challenge", code_challenge)
            .append_pair("code_challenge_method", "S256")
            .append_pair("prompt", "select_account");
        url.to_string()
    }

    async fn fetch_identity(
        &self,
        code: &str,
        code_verifier: &str,
    ) -> Result<ProviderIdentity, SocialError> {
        let token_url = format!("{}/token", self.config.proxy_url.trim_end_matches('/'));
        let response = self
            .http
            .post(&token_url)
            .header("CF-Access-Client-Id", &self.config.proxy_access_client_id)
            .header("CF-Access-Client-Secret", &self.config.proxy_access_client_secret)
            .form(&[
                ("client_id", self.config.client_id.as_str()),
                ("client_secret", self.config.client_secret.as_str()),
                ("code", code),
                ("redirect_uri", self.config.redirect_uri.as_str()),
                ("grant_type", "authorization_code"),
                ("code_verifier", code_verifier),
            ])
            .send()
            .await
            .map_err(SocialError::Transport)?;

        if !response.status().is_success() {
            return Err(SocialError::Provider(format!("worker token 返回 {}", response.status())));
        }
        let envelope: WorkerEnvelope = response.json().await.map_err(SocialError::Transport)?;
        let data =
            verify_envelope(&envelope, &self.config.worker_signing_public_key, self.clock.now())?;
        let token: TokenResponse = serde_json::from_str(&data)
            .map_err(|_| SocialError::Malformed("Google token 响应不是合法 JSON"))?;
        let claims = decode_id_token(&token.id_token)?;
        if claims.sub.is_empty() {
            return Err(SocialError::Malformed("Google id_token 缺少 sub"));
        }
        Ok(ProviderIdentity { provider_user_id: claims.sub, display_name: claims.name })
    }
}

/// 解出 `id_token` 载荷。签名由 Worker 背书承担（ADR-0012），此处只读 claims。
fn decode_id_token(token: &str) -> Result<IdTokenClaims, SocialError> {
    let payload =
        token.split('.').nth(1).ok_or(SocialError::Malformed("Google id_token 不是三段式 JWT"))?;
    let bytes = URL_SAFE_NO_PAD
        .decode(payload)
        .map_err(|_| SocialError::Malformed("Google id_token 载荷不是合法 base64url"))?;
    serde_json::from_slice(&bytes)
        .map_err(|_| SocialError::Malformed("Google id_token 载荷不是合法 JSON"))
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use base64::Engine as _;
    use base64::engine::general_purpose::URL_SAFE_NO_PAD;

    use super::decode_id_token;

    /// 构造仅含载荷的假 JWT（签名段留空，本函数不校验签名）。
    fn fake_id_token(claims: &serde_json::Value) -> String {
        let payload = URL_SAFE_NO_PAD.encode(serde_json::to_vec(claims).expect("序列化必须成功"));
        format!("header.{payload}.")
    }

    #[test]
    fn decodes_sub_and_optional_name() {
        let claims = decode_id_token(&fake_id_token(
            &serde_json::json!({ "sub": "google-sub-1", "name": "Nova" }),
        ))
        .expect("合法载荷必须可解析");
        assert_eq!(claims.sub, "google-sub-1");
        assert_eq!(claims.name.as_deref(), Some("Nova"));
    }

    #[test]
    fn missing_name_is_none() {
        let payload = URL_SAFE_NO_PAD.encode(br#"{"sub":"s"}"#);
        let claims = decode_id_token(&format!("h.{payload}.sig")).expect("合法载荷必须可解析");
        assert_eq!(claims.sub, "s");
        assert_eq!(claims.name, None);
    }

    #[test]
    fn malformed_tokens_are_rejected() {
        assert!(decode_id_token("not-a-jwt").is_err());
        assert!(decode_id_token("h.%%%.sig").is_err());
        assert!(decode_id_token("h..sig").is_err());
    }
}
