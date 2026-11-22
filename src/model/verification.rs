//! 邮箱验证码领域：生成、限次与时效（v3 语义，ADR-0002 保留验证流程）。

use chrono::{DateTime, TimeDelta, Utc};
use rand::Rng;
use uuid::Uuid;

use crate::model::user::UserId;

/// 验证码有效期。
pub const VERIFICATION_TTL: TimeDelta = TimeDelta::minutes(10);
/// 同一验证码最大错误尝试次数；打满即作废，须重新发送。
pub const MAX_ATTEMPTS: u32 = 5;
/// 同邮箱重发冷却。
pub const RESEND_COOLDOWN: TimeDelta = TimeDelta::seconds(60);

/// 验证码用途；不同用途的码互不相通（列 `purpose`）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VerificationPurpose {
    /// 注册邮箱验证。
    VerifyEmail,
    /// 账户注销确认。
    DeleteAccount,
    /// 密码重置（未登录态；v3 边界：邮箱所有权即凭证，不要求 TOTP）。
    PasswordReset,
    /// 邮箱变更确认（新邮箱地址）。
    EmailChange,
}

impl VerificationPurpose {
    /// 入库字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::VerifyEmail => "email_verify",
            Self::DeleteAccount => "account_delete",
            Self::PasswordReset => "password_reset",
            Self::EmailChange => "email_change",
        }
    }
}

/// v3 同款无歧义字母表（剔除 0/O/I/L/l），55 个字符。
const ALPHABET: &[u8] = b"123456789ABCDEFGHJKMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz";
/// 拒绝采样阈值：256 = 4×55 + 36，byte < 220 时取模无偏差。
const UNBIASED_LIMIT: u8 = 220;

/// 六位邮箱验证码明文；仅签发与邮件正文出现，入库只存带 pepper 的 HMAC-SHA256。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VerificationCode(String);

/// 逐字节拒绝采样：byte < 220 时对 55 取模无偏差；达到 6 个字符即停。
fn fill_from_pool(plain: &mut String, pool: &[u8]) {
    for &byte in pool {
        if plain.len() == 6 {
            return;
        }
        if byte < UNBIASED_LIMIT {
            plain.push(ALPHABET[usize::from(byte) % ALPHABET.len()] as char);
        }
    }
}

impl VerificationCode {
    /// 随机生成六位验证码（逐字符拒绝采样，无模偏差）。
    ///
    /// ```
    /// use nebula_server::model::verification::VerificationCode;
    ///
    /// let code = VerificationCode::generate();
    /// assert_eq!(code.as_str().len(), 6);
    /// ```
    #[must_use]
    pub fn generate() -> Self {
        let mut rng = rand::rng();
        let mut plain = String::with_capacity(6);
        // 单批 16 字节可能因拒绝采样不足 6 个字符；循环补足而不是返回短码。
        while plain.len() < 6 {
            let mut pool = [0u8; 16];
            rng.fill_bytes(&mut pool);
            fill_from_pool(&mut plain, &pool);
        }
        Self(plain)
    }

    /// 借用明文。
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// 落库的新验证码描述；明文不入本结构。
#[derive(Debug, Clone)]
pub struct NewEmailVerification {
    /// 所属用户。
    pub user_id: UserId,
    /// 验证码的 HMAC-SHA256（带服务端 pepper）十六进制。
    pub code_hash: String,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
    /// 用途。
    pub purpose: VerificationPurpose,
}

/// 库中读出的验证码记录。
#[derive(Debug, Clone)]
pub struct EmailVerification {
    /// 记录标识。
    pub id: Uuid,
    /// 验证码的 HMAC-SHA256（带服务端 pepper）十六进制；校验在 handler 恒时比较。
    pub code_hash: String,
    /// 所属用户。
    pub user_id: UserId,
    /// 已发生的错误尝试次数。
    pub attempts: u32,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
    /// 签发时刻；重发冷却以它为基准。
    pub created_at: DateTime<Utc>,
}

#[cfg(test)]
mod tests {
    use super::{ALPHABET, UNBIASED_LIMIT, VerificationCode, VerificationPurpose, fill_from_pool};

    #[test]
    fn alphabet_excludes_ambiguous_characters_and_rejection_limit_is_unbiased() {
        assert_eq!(ALPHABET.len(), 55);
        assert_eq!(usize::from(UNBIASED_LIMIT), (256 / ALPHABET.len()) * ALPHABET.len());
        for byte in b"0OILl" {
            assert!(!ALPHABET.contains(byte), "字母表不得包含易混淆字符 {}", *byte as char);
        }
    }

    #[test]
    fn generated_code_is_six_unambiguous_characters() {
        for _ in 0..64 {
            let code = VerificationCode::generate();
            assert_eq!(code.as_str().len(), 6);
            assert!(code.as_str().chars().all(|c| ALPHABET.contains(&(c as u8))));
        }
    }

    #[test]
    fn purpose_strings_are_stable() {
        assert_eq!(VerificationPurpose::VerifyEmail.as_str(), "email_verify");
        assert_eq!(VerificationPurpose::DeleteAccount.as_str(), "account_delete");
        assert_eq!(VerificationPurpose::PasswordReset.as_str(), "password_reset");
        assert_eq!(VerificationPurpose::EmailChange.as_str(), "email_change");
    }

    #[test]
    fn rejection_sampling_maps_known_bytes_and_drops_out_of_range() {
        let mut pool = [0u8; 16];
        pool[..6].copy_from_slice(&[0, 1, 219, 220, 221, 54]);
        let mut code = String::new();
        fill_from_pool(&mut code, &pool);
        // 0→'1'、1→'2'、219→'z'；220/221 被拒绝；54→'z'；余下 0 补到 6 位
        assert_eq!(code, "12zz11");
    }
}
