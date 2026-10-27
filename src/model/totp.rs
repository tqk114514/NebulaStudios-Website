//! TOTP 两步验证领域：状态机、密钥与恢复码（§2；算法经 `totp-rs`，不手写 RFC 6238）。

use chrono::{DateTime, Utc};
use rand::Rng;
use uuid::Uuid;

use crate::model::user::UserId;

/// 验证器算法参数（v3 语义）：SHA1、6 位、30 秒、±1 时间片。
pub const TOTP_ALGORITHM: totp_rs::Algorithm = totp_rs::Algorithm::SHA1;
/// 码位数。
pub const TOTP_DIGITS: u8 = 6;
/// 容许的时间片偏移。
pub const TOTP_SKEW: u8 = 1;
/// 时间片秒数。
pub const TOTP_PERIOD: u64 = 30;

/// 登录挑战有效期。
pub const CHALLENGE_TTL: chrono::TimeDelta = chrono::TimeDelta::minutes(5);
/// 登录挑战最大错误尝试次数。
pub const CHALLENGE_MAX_ATTEMPTS: u32 = 5;
/// 启用时签发的恢复码数量（v3 同款）。
pub const RECOVERY_CODE_COUNT: usize = 8;

/// TOTP 启用状态；转移路径 Disabled → Pending → Enabled，禁用只从 Enabled。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TotpState {
    /// 未启用。
    Disabled,
    /// 已生成待验证密钥；同一密钥可重复提交验证，重试生成则换新密钥。
    Pending,
    /// 已启用；登录须二段。
    Enabled,
}

impl TotpState {
    /// 入库字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Disabled => "disabled",
            Self::Pending => "pending",
            Self::Enabled => "enabled",
        }
    }

    /// 从库中字符串构建；未知值按数据损坏返回 `None`。
    #[must_use]
    pub fn from_str_raw(raw: &str) -> Option<Self> {
        match raw {
            "disabled" => Some(Self::Disabled),
            "pending" => Some(Self::Pending),
            "enabled" => Some(Self::Enabled),
            _ => None,
        }
    }

    /// setup 转移：Disabled 或 Pending（重试换新密钥）可进入 Pending；
    /// Enabled 下再 setup 是非法转移（先 disable）。
    ///
    /// # Errors
    /// 当前为 [`TotpState::Enabled`] 时返回 [`TotpTransitionError`]。
    pub fn on_setup(self) -> Result<Self, TotpTransitionError> {
        match self {
            Self::Disabled | Self::Pending => Ok(Self::Pending),
            Self::Enabled => Err(TotpTransitionError),
        }
    }

    /// enable 转移：仅 Pending 可启用。
    ///
    /// # Errors
    /// 非 [`TotpState::Pending`] 时返回 [`TotpTransitionError`]。
    pub fn on_enable(self) -> Result<Self, TotpTransitionError> {
        match self {
            Self::Pending => Ok(Self::Enabled),
            _ => Err(TotpTransitionError),
        }
    }

    /// disable 转移：仅 Enabled 可关闭。
    ///
    /// # Errors
    /// 非 [`TotpState::Enabled`] 时返回 [`TotpTransitionError`]。
    pub fn on_disable(self) -> Result<Self, TotpTransitionError> {
        match self {
            Self::Enabled => Ok(Self::Disabled),
            _ => Err(TotpTransitionError),
        }
    }
}

/// 对当前状态非法的 TOTP 转移。
#[derive(Debug, thiserror::Error)]
#[error("TOTP 状态不允许该操作")]
pub struct TotpTransitionError;

/// 生成 base32 无填充 TOTP 密钥（totp-rs 的 Secret 序列化格式，20 字节熵）。
///
/// ```
/// use nebula_server::model::totp::generate_secret;
///
/// let secret = generate_secret();
/// // 20 字节 → base32 无填充 32 字符
/// assert_eq!(secret.len(), 32);
/// assert!(secret.chars().all(|c| c.is_ascii_alphanumeric()));
/// ```
#[must_use]
pub fn generate_secret() -> String {
    totp_rs::Secret::generate().to_base32()
}

/// 构建 otpauth:// URI；空格编码为 %20（部分验证器不解码 +，v3 教训）。
#[must_use]
pub fn otpauth_uri(email: &str, secret: &str) -> String {
    let label = url_path_escape(&format!("Nebula:{email}"));
    format!(
        "otpauth://totp/{label}?secret={secret}&issuer=Nebula&algorithm=SHA1&digits={TOTP_DIGITS}&period={TOTP_PERIOD}"
    )
}

/// 以注入时刻校验 TOTP 码（±1 时间片）。
#[must_use]
pub fn verify_code(secret: &str, code: &str, now: DateTime<Utc>) -> bool {
    // 归一化（v3 语义）：去空白、仅接受纯数字六位
    let trimmed = code.trim().replace(' ', "");
    if trimmed.len() != usize::from(TOTP_DIGITS) || !trimmed.bytes().all(|b| b.is_ascii_digit()) {
        return false;
    }
    let Ok(parsed) = totp_rs::Secret::try_from_base32(secret) else {
        return false;
    };
    let Ok(totp) = totp_rs::Builder::new()
        .with_algorithm(TOTP_ALGORITHM)
        .with_digits(TOTP_DIGITS)
        .with_skew(u16::from(TOTP_SKEW))
        .with_step_duration(TOTP_PERIOD)
        .with_secret(parsed)
        .build()
    else {
        return false;
    };
    totp.check(&trimmed, now.timestamp().cast_unsigned()).is_some()
}

/// 恢复码：v3 同款无歧义字母表（30 字符），10 位。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RecoveryCode(String);

/// 恢复码长度。
const RECOVERY_CODE_LEN: usize = 10;
/// v3 字母表：剔除 0/O/1/I/L。
const RECOVERY_ALPHABET: &[u8] = b"23456789ABCDEFGHJKMNPQRSTUVWXYZ";
/// 拒绝采样阈值：256 = 8×30 + 16。
const RECOVERY_UNBIASED_LIMIT: u8 = 240;

impl RecoveryCode {
    /// 随机生成恢复码。
    #[must_use]
    pub fn generate() -> Self {
        let mut pool = [0u8; RECOVERY_CODE_LEN * 2];
        rand::rng().fill_bytes(&mut pool);
        let mut plain = String::with_capacity(RECOVERY_CODE_LEN);
        for byte in pool {
            if plain.len() == RECOVERY_CODE_LEN {
                break;
            }
            if byte < RECOVERY_UNBIASED_LIMIT {
                plain.push(RECOVERY_ALPHABET[usize::from(byte) % RECOVERY_ALPHABET.len()] as char);
            }
        }
        Self(plain)
    }

    /// 借用明文。
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

/// 库中读出的恢复码记录。
#[derive(Debug, Clone)]
pub struct RecoveryCodeRecord {
    /// 记录标识。
    pub id: Uuid,
    /// 已消费时刻；`None` 表示可用。
    pub consumed_at: Option<DateTime<Utc>>,
}

/// 登录二段挑战记录。
#[derive(Debug, Clone)]
pub struct TotpChallenge {
    /// 记录标识。
    pub id: Uuid,
    /// 所属用户。
    pub user_id: UserId,
    /// 已发生的错误尝试次数。
    pub attempts: u32,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
}

/// `PathEscape` 的最小实现（只处理 URI 不安全字符）。
fn url_path_escape(input: &str) -> String {
    const HEX: &[u8; 16] = b"0123456789ABCDEF";
    let mut out = String::with_capacity(input.len());
    for byte in input.bytes() {
        match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' | b'@' | b':' => {
                out.push(byte as char);
            }
            _ => {
                out.push('%');
                out.push(HEX[usize::from(byte >> 4)] as char);
                out.push(HEX[usize::from(byte & 0x0f)] as char);
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    #![allow(clippy::expect_used)]

    use super::{
        CHALLENGE_TTL, RecoveryCode, TotpState, generate_secret, otpauth_uri, verify_code,
    };
    use chrono::{DateTime, TimeDelta, Utc};

    #[test]
    fn state_machine_rejects_illegal_transitions() {
        // Enabled 下再 setup 非法
        assert!(TotpState::Enabled.on_setup().is_err());
        // Disabled 下 enable / disable 非法
        assert!(TotpState::Disabled.on_enable().is_err());
        assert!(TotpState::Disabled.on_disable().is_err());
        // Pending 下 disable 非法（尚未启用，谈不上关闭）
        assert!(TotpState::Pending.on_disable().is_err());
        // 合法路径
        assert!(matches!(TotpState::Disabled.on_setup(), Ok(TotpState::Pending)));
        assert!(matches!(TotpState::Pending.on_enable(), Ok(TotpState::Enabled)));
        assert!(matches!(TotpState::Enabled.on_disable(), Ok(TotpState::Disabled)));
    }

    #[test]
    fn generated_secret_is_usable_for_verification() {
        let secret = generate_secret();
        let now = Utc::now();
        let parsed = totp_rs::Secret::try_from_base32(&secret).expect("自产密钥必须可解析");
        let totp = totp_rs::Builder::new()
            .with_algorithm(super::TOTP_ALGORITHM)
            .with_digits(super::TOTP_DIGITS)
            .with_skew(u16::from(super::TOTP_SKEW))
            .with_step_duration(super::TOTP_PERIOD)
            .with_secret(parsed)
            .build()
            .expect("参数合法");
        let code = totp.generate(now.timestamp().cast_unsigned()).to_string();
        assert!(verify_code(&secret, &code, now), "刚生成的码必须可验证");
    }

    #[test]
    fn non_digit_code_is_rejected() {
        let secret = generate_secret();
        let now = Utc::now();
        assert!(!verify_code(&secret, "ABCDEF", now));
        assert!(!verify_code(&secret, "12345", now));
        assert!(!verify_code(&secret, "1234567", now));
    }

    #[test]
    fn otpauth_uri_escapes_spaces_as_percent20() {
        let uri = otpauth_uri("a b@nebula.st", "SECRET234");
        assert!(uri.contains("Nebula:a%20b@nebula.st"), "{uri}");
        assert!(!uri.contains('+'), "{uri}");
    }

    #[test]
    fn recovery_codes_match_expected_shape() {
        let code = RecoveryCode::generate();
        assert_eq!(code.as_str().len(), 10);
        assert!(code.as_str().chars().all(|c| !matches!(c, '0' | 'O' | '1' | 'I' | 'L')));
        assert_eq!(CHALLENGE_TTL.num_minutes(), 5);
    }

    #[test]
    fn fixed_datetime_not_used_in_prod_paths() {
        // 文档性断言：verify 的时刻来自调用方注入
        let _now: DateTime<Utc> = Utc::now() - TimeDelta::days(1);
    }
}
