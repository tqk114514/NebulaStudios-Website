//! 社交登录出站取数：provider 身份客户端与 Worker 背书校验（ADR-0012）。
//!
//! 与 `api/` 不同，本模块只做出站调用；不依赖 axum 或 sqlx，供装配层构建后注入。

pub mod google;
pub mod microsoft;
pub mod worker;

use std::collections::HashMap;
use std::fmt;
use std::sync::Arc;

use async_trait::async_trait;

use crate::model::social::SocialProvider;

/// 从 provider 取得的最小身份；只含绑定与展示所需字段（不含头像、邮箱）。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProviderIdentity {
    /// provider 侧用户 id；绑定的唯一凭据。
    pub provider_user_id: String,
    /// 展示昵称；provider 未提供时为 `None`。
    pub display_name: Option<String>,
}

/// 出站取数错误。
#[derive(Debug, thiserror::Error)]
pub enum SocialError {
    /// 网络故障或客户端构建失败。
    #[error("社交 provider 请求失败: {0}")]
    Transport(#[source] reqwest::Error),
    /// provider 或代理返回非 2xx。
    #[error("社交 provider 返回错误: {0}")]
    Provider(String),
    /// 响应结构不符合预期（缺字段、背书签名不符、时间戳超窗等）。
    #[error("社交 provider 响应非法: {0}")]
    Malformed(&'static str),
}

/// provider 身份客户端：授权页地址与用授权码换身份。
///
/// 返回 `+ Send` 的 future：handler 在多线程 runtime 下运行。
#[async_trait]
pub trait SocialProviderClient: Send + Sync {
    /// 该客户端服务的 provider。
    fn provider(&self) -> SocialProvider;

    /// 授权页地址；调用方已备好 `state` 与 PKCE `code_challenge`。
    fn authorize_url(&self, state: &str, code_challenge: &str) -> String;

    /// 用授权码换身份；`code_verifier` 为发起时落库的 PKCE verifier。
    ///
    /// # Errors
    /// 网络故障、provider 错误响应或响应结构非法时返回 [`SocialError`]。
    async fn fetch_identity(
        &self,
        code: &str,
        code_verifier: &str,
    ) -> Result<ProviderIdentity, SocialError>;
}

/// 已启用的 provider 客户端集合；装配层按配置构建，未启用的 provider 不在表内。
#[derive(Clone, Default)]
pub struct SocialClients {
    clients: HashMap<SocialProvider, Arc<dyn SocialProviderClient>>,
}

impl fmt::Debug for SocialClients {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("SocialClients")
            .field("enabled", &self.clients.len())
            .finish_non_exhaustive()
    }
}

impl SocialClients {
    /// 从客户端列表构建；同一 provider 后写入者覆盖前者。
    #[must_use]
    pub fn new(clients: Vec<Arc<dyn SocialProviderClient>>) -> Self {
        Self { clients: clients.into_iter().map(|client| (client.provider(), client)).collect() }
    }

    /// 取指定 provider 的客户端；未启用返回 `None`。
    #[must_use]
    pub fn get(&self, provider: SocialProvider) -> Option<&Arc<dyn SocialProviderClient>> {
        self.clients.get(&provider)
    }
}
