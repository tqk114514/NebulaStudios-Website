//! 口令哈希领域类型：Argon2id，纯计算无 I/O（§2）。

use argon2::password_hash::phc::{ParamsString, PasswordHash as ParsedHash};
use argon2::password_hash::{Error as HashError, phc};
use argon2::{Algorithm, Argon2, Params, PasswordHasher, PasswordVerifier, Version};

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
    /// 当前参数无法编码为 PHC 参数串（内部常量错误）。
    #[error("口令哈希参数编码失败: {0}")]
    Params(#[source] HashError),
    /// 校验过程中发现哈希参数或算法不受支持（存储数据问题）。
    #[error("口令哈希校验失败: {0}")]
    Verify(#[source] HashError),
}

/// 口令校验结论；参数落后的哈希携带按当前配置重算的新值。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PasswordCheck {
    /// 口令不匹配。
    Mismatch,
    /// 口令匹配，哈希参数已是最新。
    Current,
    /// 口令匹配，但哈希参数落后；新哈希已按当前配置重算。
    Outdated(PasswordHash),
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
        let parsed = self.parsed()?;
        Self::verify_parsed(&parsed, plain)
    }

    /// 校验明文，并在存储哈希的参数落后于 `Argon2::default()` 时按当前参数重算
    /// 一份新哈希。重算与校验在同一调用内完成，调用方应整体放到 `spawn_blocking`。
    ///
    /// # Errors
    /// 存储的哈希串非法、算法参数不受支持，或重算失败时返回错误；口令不匹配
    /// 返回 [`PasswordCheck::Mismatch`]，不是错误。
    pub fn verify_and_upgrade(&self, plain: &str) -> Result<PasswordCheck, PasswordHashError> {
        let parsed = self.parsed()?;
        if !Self::verify_parsed(&parsed, plain)? {
            return Ok(PasswordCheck::Mismatch);
        }
        if Self::is_outdated(&parsed)? {
            Self::hash(plain).map(PasswordCheck::Outdated)
        } else {
            Ok(PasswordCheck::Current)
        }
    }

    /// 解析存储的 PHC 串；格式非法立即报错。
    fn parsed(&self) -> Result<ParsedHash, PasswordHashError> {
        self.0.parse::<ParsedHash>().map_err(PasswordHashError::Malformed)
    }

    /// 用哈希串自带的算法、版本与参数校验明文。
    fn verify_parsed(parsed: &ParsedHash, plain: &str) -> Result<bool, PasswordHashError> {
        match Argon2::default().verify_password(plain.as_bytes(), parsed) {
            Ok(()) => Ok(true),
            // PasswordInvalid = 口令不匹配，即校验结论本身
            Err(HashError::PasswordInvalid) => Ok(false),
            Err(err) => Err(PasswordHashError::Verify(err)),
        }
    }

    /// 存储哈希是否落后于当前默认参数（算法、版本、PHC 参数与输出长度）。
    ///
    /// 当前没有可配置的自定义参数，默认值即目标；任何差异都按目标重算。
    fn is_outdated(parsed: &ParsedHash) -> Result<bool, PasswordHashError> {
        let current = Argon2::default();
        let current_params =
            ParamsString::try_from(current.params()).map_err(PasswordHashError::Params)?;
        let output_len = current.params().output_len().unwrap_or(Params::DEFAULT_OUTPUT_LEN);
        Ok(parsed.algorithm.as_str() != Algorithm::default().as_str()
            || parsed.version != Some(u32::from(Version::default()))
            || parsed.params != current_params
            || parsed.hash.as_ref().is_none_or(|hash| hash.len() != output_len))
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use argon2::{Algorithm, Argon2, Params, PasswordHasher, Version};

    use super::{PasswordCheck, PasswordHash};

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

    #[test]
    fn verify_and_upgrade_keeps_current_hash() {
        let hash = PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功");

        assert_eq!(
            hash.verify_and_upgrade("Nebula!Passw0rd-2026").expect("合法哈希必须可校验"),
            PasswordCheck::Current
        );
        assert_eq!(
            hash.verify_and_upgrade("Wrong!Passw0rd-2026").expect("合法哈希必须可校验"),
            PasswordCheck::Mismatch
        );
    }

    #[test]
    fn verify_and_upgrade_rehashes_outdated_params() {
        let legacy = Argon2::new(
            Algorithm::Argon2id,
            Version::V0x13,
            Params::new(8, 1, 1, None).expect("参数合法"),
        )
        .hash_password(b"Nebula!Passw0rd-2026")
        .expect("哈希必须成功")
        .to_string();
        let hash = PasswordHash::from_encoded(legacy).expect("合法 PHC");

        let upgraded =
            match hash.verify_and_upgrade("Nebula!Passw0rd-2026").expect("合法哈希必须可校验")
            {
                PasswordCheck::Outdated(upgraded) => upgraded,
                other => panic!("旧参数必须触发重算: {other:?}"),
            };
        assert!(upgraded.verify("Nebula!Passw0rd-2026").expect("新哈希必须可校验"));
        assert_eq!(
            upgraded.verify_and_upgrade("Nebula!Passw0rd-2026").expect("新哈希必须可校验"),
            PasswordCheck::Current
        );
    }
}
