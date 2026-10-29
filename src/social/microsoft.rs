//! Microsoft 身份客户端：直连 Microsoft identity platform 与 Graph（ADR-0012）。
//! 身份取自 Graph `/me`，不读 email、不取头像。

use std::fmt;

use async_trait::async_trait;
use serde::Deserialize;

use super::{ProviderIdentity, SocialError, SocialProviderClient};
use crate::model::social::SocialProvider;

/// 授权端点（`common` 租户，覆盖个人与组织账户）。
const AUTHORIZE_ENDPOINT: &str = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize";
/// 令牌端点。
const TOKEN_ENDPOINT: &str = "https://login.microsoftonline.com/common/oauth2/v2.0/token";
/// Graph 当前用户端点。
const GRAPH_ME_ENDPOINT: &str = "https://graph.microsoft.com/v1.0/me";
/// 请求范围：`User.Read` 供 Graph 读取当前用户。
const SCOPE: &str = "openid profile User.Read";

/// Microsoft 客户端配置。
#[derive(Debug, Clone)]
pub struct MicrosoftConfig {
    /// OAuth 客户端 id。
    pub client_id: String,
    /// OAuth 客户端密钥。
    pub client_secret: String,
    /// 回跳地址。
    pub redirect_uri: String,
}

/// Microsoft 身份客户端。
pub struct MicrosoftClient {
    config: MicrosoftConfig,
    http: reqwest::Client,
}

#[derive(Debug, Deserialize)]
struct TokenResponse {
    access_token: String,
}

#[derive(Debug, Deserialize)]
struct GraphUser {
    id: String,
    #[serde(default, rename = "displayName")]
    display_name: Option<String>,
}

impl MicrosoftClient {
    /// 构建客户端。
    ///
    /// # Errors
    /// reqwest 客户端构建失败（TLS 后端初始化失败）时返回 [`SocialError::Transport`]。
    pub fn new(config: MicrosoftConfig) -> Result<Self, SocialError> {
        let http = reqwest::Client::builder().build().map_err(SocialError::Transport)?;
        Ok(Self { config, http })
    }
}

// 配置含密钥，Debug 只列结构名
impl fmt::Debug for MicrosoftClient {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("MicrosoftClient").finish_non_exhaustive()
    }
}

#[async_trait]
impl SocialProviderClient for MicrosoftClient {
    fn provider(&self) -> SocialProvider {
        SocialProvider::Microsoft
    }

    fn authorize_url(&self, state: &str, code_challenge: &str) -> String {
        let mut url = reqwest::Url::parse(AUTHORIZE_ENDPOINT)
            .unwrap_or_else(|_| unreachable!("microsoft authorize 端点为常量 URL"));
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
        let token: TokenResponse = self
            .http
            .post(TOKEN_ENDPOINT)
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
            .map_err(SocialError::Transport)?
            .error_for_status()
            .map_err(|err| SocialError::Provider(format!("microsoft token 失败: {err}")))?
            .json()
            .await
            .map_err(SocialError::Transport)?;

        let user: GraphUser = self
            .http
            .get(GRAPH_ME_ENDPOINT)
            .bearer_auth(&token.access_token)
            .send()
            .await
            .map_err(SocialError::Transport)?
            .error_for_status()
            .map_err(|err| SocialError::Provider(format!("graph /me 失败: {err}")))?
            .json()
            .await
            .map_err(SocialError::Transport)?;

        if user.id.is_empty() {
            return Err(SocialError::Malformed("Microsoft /me 缺少 id"));
        }
        Ok(ProviderIdentity { provider_user_id: user.id, display_name: user.display_name })
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use super::GraphUser;

    #[test]
    fn graph_user_maps_display_name() {
        let user: GraphUser =
            serde_json::from_str(r#"{"id":"abc","displayName":"Nova"}"#).expect("必须可解析");
        assert_eq!(user.id, "abc");
        assert_eq!(user.display_name.as_deref(), Some("Nova"));
    }

    #[test]
    fn graph_user_without_display_name_is_none() {
        let user: GraphUser = serde_json::from_str(r#"{"id":"abc"}"#).expect("必须可解析");
        assert_eq!(user.display_name, None);
    }
}
