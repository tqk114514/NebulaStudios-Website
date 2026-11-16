//! 数据导出：一次性下载令牌（ADR-0002；进程内状态入库，§9.4）。

use chrono::TimeDelta;

use crate::model::tokens::{random_hex_32, sha256_hex};

/// 下载令牌有效期；短时效降低链接泄漏后的可利用窗口。
pub const EXPORT_TOKEN_TTL: TimeDelta = TimeDelta::minutes(10);

/// 一次性数据导出下载令牌。
///
/// 明文只在申请响应中出现一次，库中只存 SHA-256；校验时按路径中的明文重算摘要。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ExportToken(String);

impl ExportToken {
    /// 生成 256 位随机令牌。
    ///
    /// ```
    /// use nebula_server::model::export::ExportToken;
    ///
    /// let token = ExportToken::generate();
    /// assert_eq!(token.as_str().len(), 64);
    /// assert_eq!(token.to_hash_hex().len(), 64);
    /// ```
    #[must_use]
    pub fn generate() -> Self {
        Self(random_hex_32())
    }

    /// 借用明文。
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// SHA-256 十六进制摘要，即入库形态。
    #[must_use]
    pub fn to_hash_hex(&self) -> String {
        sha256_hex(&self.0)
    }

    /// 路径中读出的明文对应的入库摘要。
    #[must_use]
    pub fn hash_of(plain: &str) -> String {
        sha256_hex(plain)
    }
}
