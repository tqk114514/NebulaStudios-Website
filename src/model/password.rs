//! 口令哈希领域类型：Argon2id，纯计算无 I/O（§2）。

use argon2::password_hash::phc::PasswordHash as ParsedHash;
use argon2::password_hash::{Error as HashError, phc};
use argon2::{Argon2, PasswordHasher, PasswordVerifier};

/// Argon2id 口令哈希（PHC 字符串格式）。明文口令只出现在注册与登录的
/// 边界处，入库与比较一律经由本类型。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PasswordHash(String);

/// 口令哈希操作失败；均为服务端环境或存储数据问题——
/// 口令不匹配不是错误，`verify` 对此返回 `Ok(false)`。
#[derive(Debug, thiserror::Error)]
pub enum PasswordHashError {
    /// 哈希计算失败（系统随机源或参数问题）。
    #[error("口令哈希计算失败: {0}")]
    Hash(#[source] HashError),
    /// 存储的哈希串不是合法 PHC 格式。
    #[error("存储的口令哈希格式非法: {0}")]
    Malformed(#[source] phc::Error),
    /// 校验过程中发现哈希参数或算法不受支持（存储数据问题）。
    #[error("口令哈希校验失败: {0}")]
    Verify(#[source] HashError),
}

impl PasswordHash {
    /// 以库默认参数（Argon2id，OWASP 推荐档）计算明文口令的哈希，盐自动生成。
    /// 计算耗时约几十毫秒，调用方应放 `spawn_blocking`。
    ///
    /// # Errors
    /// 盐生成或哈希计算失败时返回 [`PasswordHashError::Hash`]。
    pub fn hash(plain: &str) -> Result<Self, PasswordHashError> {
        let encoded = Argon2::default()
            .hash_password(plain.as_bytes())
            .map_err(PasswordHashError::Hash)?
            .to_string();
        Ok(Self(encoded))
    }

    /// 从库中读出的 PHC 串构建；格式非法立即报错而非推迟到校验时。
    ///
    /// # Errors
    /// 串不符合 PHC 格式时返回 [`PasswordHashError::Malformed`]。
    pub fn from_encoded(encoded: String) -> Result<Self, PasswordHashError> {
        encoded.parse::<ParsedHash>().map_err(PasswordHashError::Malformed)?;
        Ok(Self(encoded))
    }

    /// 借用底层 PHC 串。
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// 校验明文是否匹配。明文不匹配返回 `Ok(false)`；存储串或参数有问题才返回
    /// `Err`——两者语义不同，口令错误不是服务端故障。
    ///
    /// # Errors
    /// 存储的哈希串非法或算法参数不受支持时返回错误。
    pub fn verify(&self, plain: &str) -> Result<bool, PasswordHashError> {
        let parsed = self.0.parse::<ParsedHash>().map_err(PasswordHashError::Malformed)?;
        match Argon2::default().verify_password(plain.as_bytes(), &parsed) {
            Ok(()) => Ok(true),
            // PasswordInvalid = 口令不匹配，即校验结论本身
            Err(HashError::PasswordInvalid) => Ok(false),
            Err(err) => Err(PasswordHashError::Verify(err)),
        }
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use super::PasswordHash;

    #[test]
    fn hash_then_verify_roundtrips_and_wrong_plain_fails() {
        let hash = PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功");

        assert!(hash.verify("Nebula!Passw0rd-2026").expect("合法哈希必须可校验"));
        assert!(!hash.verify("Wrong!Passw0rd-2026").expect("合法哈希必须可校验"));
    }

    #[test]
    fn malformed_encoded_is_rejected_on_construction() {
        assert!(PasswordHash::from_encoded("not-a-phc-string".to_owned()).is_err());
    }
}
