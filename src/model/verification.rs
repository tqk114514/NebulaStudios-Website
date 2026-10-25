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

/// v3 同款无歧义字母表（剔除 0/O/I/L/l），57 个字符。
const ALPHABET: &[u8] = b"123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz";
/// 拒绝采样阈值：256 = 4×57 + 28，byte < 228 时取模无偏差。
const UNBIASED_LIMIT: u8 = 228;

/// 六位邮箱验证码明文；仅签发与邮件正文出现，入库只存 SHA-256。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VerificationCode(String);

impl VerificationCode {
    /// 随机生成六位验证码（逐字符拒绝采样，无模偏差）。
    ///
    /// ```
    /// use nebula_server::model::verification::VerificationCode;
    ///
    /// let code = VerificationCode::generate();
    /// assert_eq!(code.as_str().len(), 6);
    /// assert_eq!(code.to_hash_hex().len(), 64);
    /// ```
    #[must_use]
    pub fn generate() -> Self {
        let mut pool = [0u8; 16];
        rand::rng().fill_bytes(&mut pool);
        let mut plain = String::with_capacity(6);
        // 逐字节拒绝采样：byte < 228 时对 57 取模无偏差
        for byte in pool {
            if plain.len() == 6 {
                break;
            }
            if byte < UNBIASED_LIMIT {
                plain.push(ALPHABET[usize::from(byte) % ALPHABET.len()] as char);
            }
        }
        Self(plain)
    }

    /// 借用明文。
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// SHA-256 十六进制摘要，即入库形态。
    #[must_use]
    pub fn to_hash_hex(&self) -> String {
        crate::model::tokens::sha256_hex(&self.0)
    }
}

/// 落库的新验证码描述；明文不入本结构。
#[derive(Debug, Clone)]
pub struct NewEmailVerification {
    /// 所属用户。
    pub user_id: UserId,
    /// 验证码的 SHA-256 十六进制。
    pub code_hash: String,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
}

/// 库中读出的验证码记录。
#[derive(Debug, Clone)]
pub struct EmailVerification {
    /// 记录标识。
    pub id: Uuid,
    /// 验证码的 SHA-256 十六进制；校验在 handler 恒时比较。
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
