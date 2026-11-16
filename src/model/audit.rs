//! 审计日志领域类型；追加写，不随用户删除（ADR-0008）。

use chrono::{DateTime, Utc};
use serde_json::Value as JsonValue;
use uuid::Uuid;

use crate::model::user::UserId;

/// 库中读出的审计记录。
#[derive(Debug, Clone, serde::Serialize)]
pub struct AuditEntry {
    /// 记录标识。
    pub id: Uuid,
    /// 操作者；用户删除后仍保留其 UUID 文本。
    pub actor_id: UserId,
    /// 机器可读动作，如 `admin.user.ban`。
    pub action: String,
    /// 被操作对象；无目标时为 `None`。
    pub target_id: Option<UserId>,
    /// 结构化附加信息（JSON）。
    pub details: Option<JsonValue>,
    /// 记录时刻。
    pub created_at: DateTime<Utc>,
}

/// 审计动作；字符串是对外稳定契约，新增变体只追加不改名。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AuditAction {
    /// 注册。
    AuthRegister,
    /// 邮箱验证完成。
    AuthEmailVerify,
    /// 登录成功；`details.method` 区分 password / totp / provider。
    AuthLogin,
    /// 登出。
    AuthLogout,
    /// 已登录态改密。
    AuthPasswordChange,
    /// 未登录态密码重置。
    AuthPasswordReset,
    /// 账户注销。
    AuthAccountDelete,
    /// 启用两步验证。
    UserTotpEnabled,
    /// 关闭两步验证。
    UserTotpDisabled,
    /// 修改用户名。
    UserUsernameChanged,
    /// 修改账户邮箱。
    UserEmailChanged,
    /// 绑定社交身份。
    UserSocialLinked,
    /// 解绑社交身份。
    UserSocialUnlinked,
    /// 同意第三方客户端授权。
    OAuthConsent,
    /// 撤销第三方客户端授权。
    OAuthGrantRevoked,
    /// 管理员封禁。
    AdminUserBan,
    /// 管理员解封。
    AdminUserUnban,
}

impl AuditAction {
    /// 入库字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::AuthRegister => "auth.register",
            Self::AuthEmailVerify => "auth.email.verify",
            Self::AuthLogin => "auth.login",
            Self::AuthLogout => "auth.logout",
            Self::AuthPasswordChange => "auth.password.change",
            Self::AuthPasswordReset => "auth.password.reset",
            Self::AuthAccountDelete => "auth.account.delete",
            Self::UserTotpEnabled => "user.totp.enable",
            Self::UserTotpDisabled => "user.totp.disable",
            Self::UserUsernameChanged => "user.username.change",
            Self::UserEmailChanged => "user.email.change",
            Self::UserSocialLinked => "user.social.link",
            Self::UserSocialUnlinked => "user.social.unlink",
            Self::OAuthConsent => "oauth.consent",
            Self::OAuthGrantRevoked => "oauth.grant.revoke",
            Self::AdminUserBan => "admin.user.ban",
            Self::AdminUserUnban => "admin.user.unban",
        }
    }
}

/// 待写入的审计记录；明文不在此结构之外出现。
#[derive(Debug, Clone)]
pub struct NewAuditEntry {
    /// 操作者。
    pub actor_id: UserId,
    /// 机器可读动作。
    pub action: AuditAction,
    /// 被操作对象。
    pub target_id: Option<UserId>,
    /// 结构化附加信息。
    pub details: Option<JsonValue>,
}
