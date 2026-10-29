//! 社交登录领域：provider、绑定身份与 OAuth 中转态（ADR-0012；§2 零 I/O）。

use chrono::{DateTime, Utc};
use uuid::Uuid;

use crate::model::user::UserId;

/// 支持的社交 provider；入库字符串即 OAuth 供应商标识。
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum SocialProvider {
    /// Google；取数经 Cloudflare Worker 代理（ADR-0012）。
    Google,
    /// Microsoft；取数直连 Graph。
    Microsoft,
}

impl SocialProvider {
    /// 入库字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Google => "google",
            Self::Microsoft => "microsoft",
        }
    }

    /// 从库中字符串构建；未知值按数据损坏返回 `None`。
    #[must_use]
    pub fn from_str_raw(raw: &str) -> Option<Self> {
        match raw {
            "google" => Some(Self::Google),
            "microsoft" => Some(Self::Microsoft),
            _ => None,
        }
    }
}

/// 库中读出的社交身份绑定。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SocialIdentity {
    /// 记录标识。
    pub id: Uuid,
    /// 所属用户。
    pub user_id: UserId,
    /// provider。
    pub provider: SocialProvider,
    /// provider 侧用户 id；绑定的唯一凭据。
    pub provider_user_id: String,
    /// 展示昵称；provider 未提供时为 `None`。
    pub display_name: Option<String>,
    /// 绑定时刻。
    pub created_at: DateTime<Utc>,
}

/// 待落库的新绑定；明文不含任何 provider 密钥。
#[derive(Debug, Clone)]
pub struct NewSocialIdentity {
    /// 所属用户。
    pub user_id: UserId,
    /// provider。
    pub provider: SocialProvider,
    /// provider 侧用户 id。
    pub provider_user_id: String,
    /// 展示昵称。
    pub display_name: Option<String>,
}

/// OAuth 中转态的用途；决定回调时按身份找绑定还是绑到发起用户。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SocialLoginAction {
    /// 登录：回调时按身份查绑定，未绑定即拒绝（不建号、不比对邮箱）。
    Login,
    /// 绑定：回调时把身份绑到发起时的用户。
    Link,
}

impl SocialLoginAction {
    /// 入库字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Login => "login",
            Self::Link => "link",
        }
    }

    /// 从库中字符串构建；未知值按数据损坏返回 `None`。
    #[must_use]
    pub fn from_str_raw(raw: &str) -> Option<Self> {
        match raw {
            "login" => Some(Self::Login),
            "link" => Some(Self::Link),
            _ => None,
        }
    }
}

/// 落库的 OAuth 中转态：授权请求发起时写入，回调时单次取出。
#[derive(Debug, Clone)]
pub struct SocialLoginState {
    /// 防 CSRF/重放的随机 state，同时作主键。
    pub state: String,
    /// provider。
    pub provider: SocialProvider,
    /// PKCE verifier；授权请求时生成，兑换时回呈。
    pub code_verifier: String,
    /// 用途。
    pub action: SocialLoginAction,
    /// 绑定目标的用户；仅 [`SocialLoginAction::Link`] 时非空。
    pub user_id: Option<UserId>,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
}

#[cfg(test)]
mod tests {
    use super::{SocialLoginAction, SocialProvider};

    #[test]
    fn provider_and_action_strings_roundtrip() {
        for provider in [SocialProvider::Google, SocialProvider::Microsoft] {
            assert_eq!(SocialProvider::from_str_raw(provider.as_str()), Some(provider));
        }
        for action in [SocialLoginAction::Login, SocialLoginAction::Link] {
            assert_eq!(SocialLoginAction::from_str_raw(action.as_str()), Some(action));
        }
    }

    #[test]
    fn unknown_raw_values_are_rejected() {
        assert_eq!(SocialProvider::from_str_raw("github"), None);
        assert_eq!(SocialLoginAction::from_str_raw("unlink"), None);
    }
}
