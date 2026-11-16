//! Turnstile 服务端校验（ADR-0002、§9.12）。
//!
//! 只用于不可见或单按钮模式——不要求用户记忆、转写或解谜；Token 由前端
//! 从 Turnstile 组件取得后随请求提交，服务端向 Cloudflare `siteverify` 核验。

use std::fmt;
use std::time::Duration;

use async_trait::async_trait;
use serde::Deserialize;

/// 一次服务端校验的结果；`error_codes` 保留 Cloudflare 的机器可读原因。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TurnstileOutcome {
    /// 是否通过。
    pub success: bool,
    /// 未通过时的 `error-codes`；通过时通常为空。
    pub error_codes: Vec<String>,
}

/// Turnstile 校验请求本身失败（网络、非 2xx 或响应不可解析）。
#[derive(Debug, thiserror::Error)]
pub enum TurnstileError {
    /// HTTP 请求或响应解析失败。
    #[error("Turnstile 请求失败: {0}")]
    Request(#[from] reqwest::Error),
    /// `siteverify` 返回非成功状态码。
    #[error("Turnstile 返回非成功状态: {0}")]
    Status(reqwest::StatusCode),
}

/// Turnstile 校验端口；测试可注入假实现。
#[async_trait]
pub trait TurnstileVerifier: Send + Sync {
    /// 核验前端提交的 Turnstile token。
    ///
    /// # Errors
    /// 网络失败、`siteverify` 非 2xx 或响应无法解析时返回 [`TurnstileError`]。
    async fn verify(&self, token: &str) -> Result<TurnstileOutcome, TurnstileError>;
}

/// Cloudflare `siteverify` 客户端。
pub struct TurnstileClient {
    http: reqwest::Client,
    verify_url: String,
    secret: String,
}

impl TurnstileClient {
    /// 构建客户端；请求超时固定 5 秒，避免外部依赖拖住认证端点。
    ///
    /// # Errors
    /// HTTP 客户端构建失败时返回 [`TurnstileError::Request`]。
    pub fn new(
        verify_url: impl Into<String>,
        secret: impl Into<String>,
    ) -> Result<Self, TurnstileError> {
        let http = reqwest::Client::builder().timeout(Duration::from_secs(5)).build()?;
        Ok(Self { http, verify_url: verify_url.into(), secret: secret.into() })
    }
}

impl fmt::Debug for TurnstileClient {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("TurnstileClient")
            .field("verify_url", &self.verify_url)
            .field("secret", &"<redacted>")
            .finish_non_exhaustive()
    }
}

#[derive(Debug, Deserialize)]
struct SiteVerifyResponse {
    success: bool,
    #[serde(default, rename = "error-codes")]
    error_codes: Vec<String>,
}

#[async_trait]
impl TurnstileVerifier for TurnstileClient {
    async fn verify(&self, token: &str) -> Result<TurnstileOutcome, TurnstileError> {
        if token.is_empty() {
            return Ok(TurnstileOutcome {
                success: false,
                error_codes: vec!["empty-token".to_owned()],
            });
        }
        let response = self
            .http
            .post(&self.verify_url)
            .form(&[("secret", self.secret.as_str()), ("response", token)])
            .send()
            .await?;
        if !response.status().is_success() {
            return Err(TurnstileError::Status(response.status()));
        }
        let body: SiteVerifyResponse = response.json().await?;
        Ok(TurnstileOutcome { success: body.success, error_codes: body.error_codes })
    }
}
