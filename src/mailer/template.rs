//! 邮件模板与两种语言文案；模板只做纯文本渲染，不触碰 SMTP 或数据库。

use crate::model::verification::VerificationPurpose;

/// 邮件语言；跟随站点 2 语言决议，未匹配时用简体中文。
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub enum Locale {
    /// 简体中文。
    #[default]
    ZhCn,
    /// 英文。
    En,
}

impl Locale {
    /// 按 `Accept-Language` 的首个受支持语言协商；未知语言回落中文。
    ///
    /// ```
    /// use nebula_server::mailer::Locale;
    ///
    /// assert_eq!(Locale::from_accept_language("en-US,en;q=0.9"), Locale::En);
    /// assert_eq!(Locale::from_accept_language("zh-CN,zh;q=0.9"), Locale::ZhCn);
    /// assert_eq!(Locale::from_accept_language("fr-FR"), Locale::ZhCn);
    /// ```
    #[must_use]
    pub fn from_accept_language(raw: &str) -> Self {
        for part in raw.split(',') {
            let tag = part.split(';').next().unwrap_or_default().trim().to_ascii_lowercase();
            if tag == "en" || tag.starts_with("en-") {
                return Self::En;
            }
            if tag == "zh" || tag.starts_with("zh-") {
                return Self::ZhCn;
            }
        }
        Self::ZhCn
    }
}

/// 渲染后的主题与正文。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EmailContent {
    /// 邮件主题。
    pub subject: String,
    /// 邮件正文。
    pub body: String,
}

/// 验证码邮件；`ttl_minutes` 与数据库中验证码有效期一致。
#[must_use]
pub fn verification_code(
    locale: Locale,
    purpose: VerificationPurpose,
    code: &str,
    ttl_minutes: i64,
) -> EmailContent {
    match locale {
        Locale::ZhCn => {
            let subject = match purpose {
                VerificationPurpose::VerifyEmail => "Nebula 邮箱验证码",
                VerificationPurpose::DeleteAccount => "Nebula 账户注销确认码",
                VerificationPurpose::PasswordReset => "Nebula 密码重置验证码",
                VerificationPurpose::EmailChange => "Nebula 邮箱变更验证码",
            };
            EmailContent {
                subject: subject.to_owned(),
                body: format!("验证码：{code}，{ttl_minutes} 分钟内有效。"),
            }
        }
        Locale::En => {
            let subject = match purpose {
                VerificationPurpose::VerifyEmail => "Nebula email verification code",
                VerificationPurpose::DeleteAccount => "Nebula account deletion code",
                VerificationPurpose::PasswordReset => "Nebula password reset code",
                VerificationPurpose::EmailChange => "Nebula email change code",
            };
            EmailContent {
                subject: subject.to_owned(),
                body: format!("Your code: {code}. It expires in {ttl_minutes} minutes."),
            }
        }
    }
}

/// 安全变更通知的种类；覆盖凭据变更与两步验证开关（红线 #6）。
#[derive(Debug, Clone, Copy)]
pub enum SecurityNotice {
    /// 已登录态改密。
    PasswordChanged,
    /// 未登录态密码重置。
    PasswordReset,
    /// 两步验证启用。
    TotpEnabled,
    /// 两步验证关闭；账户认证强度降级。
    TotpDisabled,
    /// 账户邮箱变更；通知旧邮箱。
    EmailChanged,
}

/// 安全变更通知邮件；提示用户在非本人操作时的处置方式。
#[must_use]
pub fn security_notice(locale: Locale, notice: SecurityNotice) -> EmailContent {
    match locale {
        Locale::ZhCn => {
            let (subject, body) = match notice {
                SecurityNotice::PasswordChanged => (
                    "Nebula 安全提醒：登录口令已更改",
                    "你的 Nebula 账户登录口令刚刚被更改，所有已登录设备已下线。\n\
                     若这不是你本人的操作，请立即重置口令。",
                ),
                SecurityNotice::PasswordReset => (
                    "Nebula 安全提醒：登录口令已重置",
                    "你的 Nebula 账户口令刚刚通过邮箱验证码重置，所有已登录设备已下线。\n\
                     若这不是你本人的操作，请立即再次重置口令。",
                ),
                SecurityNotice::TotpEnabled => (
                    "Nebula 安全提醒：两步验证已启用",
                    "你的 Nebula 账户刚刚启用了两步验证。\n\
                     若这不是你本人的操作，请立即更改口令并关闭两步验证。",
                ),
                SecurityNotice::TotpDisabled => (
                    "Nebula 安全提醒：两步验证已关闭",
                    "你的 Nebula 账户刚刚关闭了两步验证，账户安全强度已降低。\n\
                     若这不是你本人的操作，请立即更改口令并重新启用两步验证。",
                ),
                SecurityNotice::EmailChanged => (
                    "Nebula 安全提醒：账户邮箱已更改",
                    "你的 Nebula 账户邮箱刚刚被更改，旧邮箱不再可用于登录或密码重置。\n\
                     若这不是你本人的操作，请立即检查账户安全。",
                ),
            };
            EmailContent { subject: subject.to_owned(), body: body.to_owned() }
        }
        Locale::En => {
            let (subject, body) = match notice {
                SecurityNotice::PasswordChanged => (
                    "Nebula security notice: password changed",
                    "Your Nebula password was just changed and all signed-in devices were \
                     signed out.\nIf this was not you, reset your password immediately.",
                ),
                SecurityNotice::PasswordReset => (
                    "Nebula security notice: password reset",
                    "Your Nebula password was just reset with an email code and all \
                     signed-in devices were signed out.\nIf this was not you, reset it again \
                     immediately.",
                ),
                SecurityNotice::TotpEnabled => (
                    "Nebula security notice: two-step verification enabled",
                    "Two-step verification was just enabled for your Nebula account.\n\
                     If this was not you, change your password and disable it immediately.",
                ),
                SecurityNotice::TotpDisabled => (
                    "Nebula security notice: two-step verification disabled",
                    "Two-step verification was just disabled for your Nebula account, \
                     lowering its security.\nIf this was not you, change your password and \
                     enable it again immediately.",
                ),
                SecurityNotice::EmailChanged => (
                    "Nebula security notice: email address changed",
                    "The email address for your Nebula account was just changed. The old \
                     address can no longer be used to sign in or reset your password.\n\
                     If this was not you, review your account security immediately.",
                ),
            };
            EmailContent { subject: subject.to_owned(), body: body.to_owned() }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{EmailContent, Locale, SecurityNotice, security_notice, verification_code};
    use crate::model::verification::VerificationPurpose;

    #[test]
    fn locale_negotiation_prefers_first_supported_tag() {
        assert_eq!(Locale::from_accept_language("en-US,en;q=0.9,zh-CN;q=0.8"), Locale::En);
        assert_eq!(Locale::from_accept_language("zh-CN,zh;q=0.9,en;q=0.8"), Locale::ZhCn);
        assert_eq!(Locale::from_accept_language("de-DE,*;q=0.1"), Locale::ZhCn);
        assert_eq!(Locale::from_accept_language(""), Locale::ZhCn);
    }

    #[test]
    fn verification_code_matches_chinese_code_format() {
        let content =
            verification_code(Locale::ZhCn, VerificationPurpose::VerifyEmail, "123456", 10);
        assert_eq!(content.subject, "Nebula 邮箱验证码");
        assert_eq!(content.body, "验证码：123456，10 分钟内有效。");
    }

    #[test]
    fn english_templates_do_not_leak_chinese_copy() {
        let code = verification_code(Locale::En, VerificationPurpose::PasswordReset, "123456", 10);
        assert_eq!(code.subject, "Nebula password reset code");
        assert!(code.body.contains("123456"));
        assert!(!code.body.contains('：'));

        let notice = security_notice(Locale::En, SecurityNotice::TotpDisabled);
        assert!(notice.subject.contains("disabled"));
        assert!(notice.body.contains("lowering its security"));
    }

    #[test]
    fn rendered_content_is_plain_text() {
        let content: EmailContent = security_notice(Locale::ZhCn, SecurityNotice::PasswordChanged);
        assert!(content.subject.starts_with("Nebula 安全提醒"));
        assert!(content.body.contains("重置口令"));
    }
}
