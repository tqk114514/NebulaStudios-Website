//! 令牌领域：ES256 access token 与不透明 refresh token 的纯逻辑（§2）。
//! 密钥与时间经参数注入，本模块不读文件、不含配置。

use chrono::{DateTime, TimeDelta, Utc};
use jsonwebtoken::{Algorithm, DecodingKey, EncodingKey, Header, Validation};
use rand::Rng;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::model::user::UserId;

/// access token 载荷；`verify_access` 按 `exp` 拒绝过期 token。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AccessClaims {
    /// 用户标识（uuid 文本）。
    pub sub: String,
    /// 签发时刻，Unix 秒。
    pub iat: i64,
    /// 过期时刻，Unix 秒。
    pub exp: i64,
}

impl AccessClaims {
    /// 以注入时钟构造，`exp = now + ttl`。
    ///
    /// # Panics
    /// ttl 非正时 panic——属调用侧配置错误，应在启动配置校验即暴露。
    ///
    /// ```
    /// use chrono::{DateTime, TimeDelta, Utc};
    /// use nebula_server::model::tokens::AccessClaims;
    /// use nebula_server::model::user::UserId;
    ///
    /// let now = DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
    ///     .unwrap()
    ///     .with_timezone(&Utc);
    /// let claims = AccessClaims::new(UserId::new(), now, TimeDelta::minutes(10));
    /// assert_eq!(claims.exp - claims.iat, 600);
    /// ```
    #[must_use]
    pub fn new(user: UserId, now: DateTime<Utc>, ttl: TimeDelta) -> Self {
        let ttl_secs = ttl.num_seconds();
        assert!(ttl_secs > 0, "access token ttl 必须为正");
        let iat = now.timestamp();
        Self { sub: user.to_string(), iat, exp: iat + ttl_secs }
    }
}

/// access token 签发失败。
#[derive(Debug, thiserror::Error)]
#[error("access token 签发失败: {0}")]
pub struct SignAccessTokenError(#[source] jsonwebtoken::errors::Error);

/// access token 校验失败（签名不符、过期、格式非法等）。
#[derive(Debug, thiserror::Error)]
#[error("access token 校验失败: {0}")]
pub struct VerifyAccessTokenError(#[source] jsonwebtoken::errors::Error);

/// 以 ES256 签发 access token。
///
/// # Errors
/// 序列化或签名失败时返回错误。
pub fn sign_access(
    claims: &AccessClaims,
    key: &EncodingKey,
) -> Result<String, SignAccessTokenError> {
    jsonwebtoken::encode(&Header::new(Algorithm::ES256), claims, key).map_err(SignAccessTokenError)
}

/// 校验签名与有效期（exp 校验开启，leeway 用库默认 60 秒）。
///
/// # Errors
/// 签名不符、已过期或格式非法时返回错误。
pub fn verify_access(
    token: &str,
    key: &DecodingKey,
) -> Result<AccessClaims, VerifyAccessTokenError> {
    jsonwebtoken::decode::<AccessClaims>(token, key, &Validation::new(Algorithm::ES256))
        .map_err(VerifyAccessTokenError)
        .map(|data| data.claims)
}

/// 不透明 refresh token 明文；仅在签发处出现一次，库中只存其 SHA-256。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RefreshToken(String);

impl RefreshToken {
    /// 生成 256 位随机 token。
    ///
    /// ```
    /// use nebula_server::model::tokens::RefreshToken;
    ///
    /// let token = RefreshToken::generate();
    /// // 32 字节 → 64 个十六进制字符
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
        Self::hash_of(&self.0)
    }

    /// 明文（如 cookie 中读出的值）的入库哈希形态，与 [`Self::to_hash_hex`] 同源。
    #[must_use]
    pub fn hash_of(plain: &str) -> String {
        hex(&Sha256::digest(plain.as_bytes()))
    }
}

/// 生成 32 字节密码学随机数的十六进制串（64 字符）；refresh 与 csrf 令牌共用。
#[must_use]
pub fn random_hex_32() -> String {
    let mut bytes = [0u8; 32];
    rand::rng().fill_bytes(&mut bytes);
    hex(&bytes)
}

fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes.iter().fold(String::with_capacity(bytes.len() * 2), |mut out, byte| {
        let _ = write!(out, "{byte:02x}");
        out
    })
}

/// refresh 家族状态：登录创建 Active，重放或撤销性事件转 Revoked（终态，不可逆）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FamilyState {
    /// 可继续轮转。
    Active,
    /// 终态；家族内所有 token 一律失效。
    Revoked,
}

impl FamilyState {
    /// 家族内 token 正常轮转后的家族状态；对已撤销家族轮转是非法转移。
    ///
    /// # Errors
    /// 家族已处于 [`FamilyState::Revoked`] 时返回 [`RevokedFamilyError`]。
    ///
    /// ```
    /// use nebula_server::model::tokens::FamilyState;
    ///
    /// assert!(matches!(FamilyState::Active.on_rotation(), Ok(FamilyState::Active)));
    /// assert!(FamilyState::Revoked.on_rotation().is_err());
    /// ```
    pub fn on_rotation(self) -> Result<Self, RevokedFamilyError> {
        match self {
            Self::Active => Ok(Self::Active),
            Self::Revoked => Err(RevokedFamilyError),
        }
    }

    /// 撤销：两态均落 Revoked，幂等。
    #[must_use]
    pub fn on_revocation(self) -> Self {
        Self::Revoked
    }
}

/// 对已撤销家族执行操作。
#[derive(Debug, thiserror::Error)]
#[error("refresh 家族已撤销")]
pub struct RevokedFamilyError;

/// 家族内单个 token 的生命周期。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TokenLifetime {
    /// 可用于轮转。
    Active,
    /// 已被成功轮转；再次呈递即重放。
    Rotated,
    /// 家族撤销波及，终态。
    Revoked,
}

/// 落库的新 refresh token 描述；明文不入本结构。
#[derive(Debug, Clone)]
pub struct NewRefreshToken {
    /// 所属家族。
    pub family_id: Uuid,
    /// 所属用户。
    pub user_id: UserId,
    /// 明文的 SHA-256 十六进制摘要。
    pub token_hash: String,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used, clippy::unwrap_used)]

    use super::{AccessClaims, RefreshToken, sign_access, verify_access};
    use crate::model::user::UserId;
    use chrono::{DateTime, TimeDelta, Utc};

    // 仅测试用的 P-256 密钥对（PKCS#8 私钥 + 公钥），不用于任何真实环境
    const TEST_PRIVATE_PEM: &str = "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgHHnffUGVWWLk8Hz8\nT+NdLnsaBCFWKphSj4rcq30zzkShRANCAASIriVp5Ne81a6mczgeIzuAXgLonIjY\nBRQBVkauO2/OKAe/QmK5cQ8TkPMha0N9171Z6eBiUfhJUeZs9qieNjw8\n-----END PRIVATE KEY-----";
    const TEST_PUBLIC_PEM: &str = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEiK4laeTXvNWupnM4HiM7gF4C6JyI\n2AUUAVZGrjtvzigHv0JiuXEPE5DzIWtDfde9WengYlH4SVHmbPaonjY8PA==\n-----END PUBLIC KEY-----";

    fn fixed_now() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
            .expect("固定字面量必须可解析")
            .with_timezone(&Utc)
    }

    #[test]
    fn access_token_signs_and_verifies_roundtrip() {
        let now = fixed_now();
        let claims = AccessClaims::new(UserId::new(), now, TimeDelta::minutes(10));
        let key = jsonwebtoken::EncodingKey::from_ec_pem(TEST_PRIVATE_PEM.as_bytes()).unwrap();

        let token = sign_access(&claims, &key).expect("签发必须成功");

        let public = jsonwebtoken::DecodingKey::from_ec_pem(TEST_PUBLIC_PEM.as_bytes()).unwrap();
        let decoded = verify_access(&token, &public).expect("合法 token 必须可校验");
        assert_eq!(decoded, claims);
    }

    #[test]
    fn expired_access_token_is_rejected() {
        // exp 校验以真实墙上时钟为基准，须相对 Utc::now 构造出确定过期
        let stale = AccessClaims::new(
            UserId::new(),
            Utc::now() - TimeDelta::hours(2),
            TimeDelta::minutes(10),
        );
        let key = jsonwebtoken::EncodingKey::from_ec_pem(TEST_PRIVATE_PEM.as_bytes()).unwrap();
        let token = sign_access(&stale, &key).expect("签发必须成功");

        let public = jsonwebtoken::DecodingKey::from_ec_pem(TEST_PUBLIC_PEM.as_bytes()).unwrap();
        assert!(verify_access(&token, &public).is_err());
    }

    #[test]
    fn refresh_tokens_are_unique_and_hash_is_stable() {
        let first = RefreshToken::generate();
        let second = RefreshToken::generate();
        assert_ne!(first, second);
        assert_eq!(first.to_hash_hex(), first.to_hash_hex());
        assert_ne!(first.to_hash_hex(), second.to_hash_hex());
    }

    #[test]
    #[should_panic(expected = "ttl 必须为正")]
    fn zero_ttl_claims_panics() {
        let _ = AccessClaims::new(UserId::new(), fixed_now(), TimeDelta::zero());
    }
}
