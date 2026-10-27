//! TOTP 密钥的静态加密（AES-256-GCM）。
//!
//! 密文格式为 `v1:<12 字节 nonce hex>:<ciphertext+tag hex>`；版本前缀为将来
//! 轮换算法留出迁移位。密钥只从配置读取，不在内存中做日志输出。

use std::fmt;

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use rand::Rng;

/// 密钥加解密失败。
#[derive(Debug, thiserror::Error)]
pub enum CipherError {
    /// 配置的密钥不是 32 字节十六进制串。
    #[error("TOTP 加密密钥格式非法")]
    InvalidKey,
    /// 加密失败（随机源或参数问题）。
    #[error("TOTP 密钥加密失败")]
    Encrypt,
    /// 密文不是本模块产生的格式。
    #[error("TOTP 密钥密文格式非法")]
    Malformed,
    /// 认证标签校验失败或密钥不匹配。
    #[error("TOTP 密钥解密失败")]
    Decrypt,
}

/// 密钥加密端口；测试可替换实现，生产使用 [`AesGcmCipher`]。
pub trait SecretCipher: Send + Sync {
    /// 加密明文，返回带版本前缀的密文。
    ///
    /// # Errors
    /// 密钥或随机源异常时返回 [`CipherError`]。
    fn encrypt(&self, plaintext: &str) -> Result<String, CipherError>;

    /// 解密 [`Self::encrypt`] 产生的密文。
    ///
    /// # Errors
    /// 密文格式、版本或认证标签不合法时返回 [`CipherError`]。
    fn decrypt(&self, ciphertext: &str) -> Result<String, CipherError>;
}

/// AES-256-GCM 实现；密钥为 32 字节，nonce 每条消息随机生成。
#[derive(Clone)]
pub struct AesGcmCipher {
    key: [u8; 32],
}

impl AesGcmCipher {
    /// 从 32 字节密钥构建。
    #[must_use]
    pub fn from_key(key: [u8; 32]) -> Self {
        Self { key }
    }

    /// 从 64 个十六进制字符构建；长度或字符非法返回 [`CipherError::InvalidKey`]。
    ///
    /// # Errors
    /// 长度不是 64 或包含非十六进制字符时返回 [`CipherError::InvalidKey`]。
    pub fn from_hex(encoded: &str) -> Result<Self, CipherError> {
        if encoded.len() != 64 || !encoded.is_ascii() {
            return Err(CipherError::InvalidKey);
        }
        let mut key = [0u8; 32];
        for (index, byte) in key.iter_mut().enumerate() {
            let start = index * 2;
            *byte = u8::from_str_radix(&encoded[start..start + 2], 16)
                .map_err(|_| CipherError::InvalidKey)?;
        }
        Ok(Self { key })
    }

    fn cipher(&self) -> Result<Aes256Gcm, CipherError> {
        Aes256Gcm::new_from_slice(&self.key).map_err(|_| CipherError::InvalidKey)
    }
}

impl fmt::Debug for AesGcmCipher {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("AesGcmCipher").finish_non_exhaustive()
    }
}

impl SecretCipher for AesGcmCipher {
    fn encrypt(&self, plaintext: &str) -> Result<String, CipherError> {
        let cipher = self.cipher()?;
        let mut nonce_bytes = [0u8; 12];
        rand::rng().fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from(nonce_bytes);
        let ciphertext =
            cipher.encrypt(&nonce, plaintext.as_bytes()).map_err(|_| CipherError::Encrypt)?;
        Ok(format!("v1:{}:{}", hex(&nonce_bytes), hex(&ciphertext)))
    }

    fn decrypt(&self, ciphertext: &str) -> Result<String, CipherError> {
        let mut parts = ciphertext.split(':');
        let (Some("v1"), Some(nonce_hex), Some(payload_hex), None) =
            (parts.next(), parts.next(), parts.next(), parts.next())
        else {
            return Err(CipherError::Malformed);
        };
        let nonce_bytes: [u8; 12] = decode_hex(nonce_hex)
            .and_then(|bytes| bytes.try_into().ok())
            .ok_or(CipherError::Malformed)?;
        let payload = decode_hex(payload_hex).ok_or(CipherError::Malformed)?;
        let cipher = self.cipher()?;
        let plaintext = cipher
            .decrypt(&Nonce::from(nonce_bytes), payload.as_ref())
            .map_err(|_| CipherError::Decrypt)?;
        String::from_utf8(plaintext).map_err(|_| CipherError::Malformed)
    }
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes.iter().fold(String::with_capacity(bytes.len() * 2), |mut out, byte| {
        let _ = write!(out, "{byte:02x}");
        out
    })
}

fn decode_hex(value: &str) -> Option<Vec<u8>> {
    if !value.len().is_multiple_of(2) || !value.is_ascii() {
        return None;
    }
    (0..value.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&value[index..index + 2], 16).ok())
        .collect()
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use super::{AesGcmCipher, CipherError, SecretCipher};

    const KEY: [u8; 32] = [7u8; 32];

    #[test]
    fn roundtrip_and_random_nonce() {
        let cipher = AesGcmCipher::from_key(KEY);
        let first = cipher.encrypt("JBSWY3DPEHPK3PXP").expect("加密必须成功");
        let second = cipher.encrypt("JBSWY3DPEHPK3PXP").expect("加密必须成功");

        assert_ne!(first, second, "nonce 必须随机");
        assert_eq!(cipher.decrypt(&first).expect("解密必须成功"), "JBSWY3DPEHPK3PXP");
        assert_eq!(cipher.decrypt(&second).expect("解密必须成功"), "JBSWY3DPEHPK3PXP");
    }

    #[test]
    fn wrong_key_and_tampering_are_rejected() {
        let cipher = AesGcmCipher::from_key(KEY);
        let ciphertext = cipher.encrypt("secret").expect("加密必须成功");
        let other = AesGcmCipher::from_key([8u8; 32]);
        assert!(matches!(other.decrypt(&ciphertext), Err(CipherError::Decrypt)));

        let mut tampered = ciphertext.into_bytes();
        let last = tampered.len() - 1;
        tampered[last] = if tampered[last] == b'0' { b'1' } else { b'0' };
        let tampered = String::from_utf8(tampered).expect("密文本必须是 ASCII");
        assert!(cipher.decrypt(&tampered).is_err());
    }

    #[test]
    fn malformed_and_invalid_keys_are_rejected() {
        let cipher = AesGcmCipher::from_key(KEY);
        assert!(matches!(cipher.decrypt("not-a-ciphertext"), Err(CipherError::Malformed)));
        assert!(matches!(cipher.decrypt("v1:00:00"), Err(CipherError::Malformed)));
        assert!(matches!(AesGcmCipher::from_hex("00"), Err(CipherError::InvalidKey)));
        assert!(matches!(AesGcmCipher::from_hex(&"z".repeat(64)), Err(CipherError::InvalidKey)));
        assert!(matches!(AesGcmCipher::from_hex(&"é".repeat(32)), Err(CipherError::InvalidKey)));
        assert!(matches!(cipher.decrypt("v1:é:00"), Err(CipherError::Malformed)));
    }
}
