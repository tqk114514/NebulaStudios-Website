//! 令牌领域：ES256 access token 与不透明 refresh token 的纯逻辑（§2）。
//! 密钥与时间经参数注入，本模块不读文件、不含配置。

use chrono::{DateTime, TimeDelta, Utc};
use jsonwebtoken::{Algorithm, DecodingKey, EncodingKey, Header, Validation};
use rand::Rng;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::model::oauth2::valid_scope;
use crate::model::user::UserId;

/// 第一方用户会话的 audience；只有本 audience 能构造 [`VerifiedAccessToken::UserSession`]。
pub const USER_SESSION_AUDIENCE: &str = "nebula:user-session";

/// 第三方客户端授权的 audience；这类令牌不能调用第一方账户端点。
pub const CLIENT_GRANT_AUDIENCE: &str = "nebula:client-grant";

/// access token 载荷。字段私有，只能经 [`Self::user_session`] 或
/// [`Self::client_grant`] 构造，避免第一方会话与客户端授权的字段组合漂移。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AccessClaims {
    sub: String,
    iat: i64,
    exp: i64,
    aud: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    client_id: Option<String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    scope: String,
}

impl AccessClaims {
    /// 第一方用户会话：audience 固定，无客户端与 scope。
    ///
    /// # Panics
    /// ttl 非正时 panic——属调用侧配置错误，应在启动配置校验即暴露。
    ///
    /// ```
    /// use chrono::{DateTime, TimeDelta, Utc};
    /// use nebula_server::model::tokens::AccessClaims;
    /// use nebula_server::model::user::UserId;
    ///
    /// # fn main() -> Result<(), Box<dyn std::error::Error>> {
    /// let now = DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")?.with_timezone(&Utc);
    /// let claims = AccessClaims::user_session(UserId::new(), now, TimeDelta::minutes(10));
    /// assert_eq!(claims.exp() - claims.iat(), 600);
    /// # Ok(())
    /// # }
    /// ```
    #[must_use]
    pub fn user_session(user: UserId, now: DateTime<Utc>, ttl: TimeDelta) -> Self {
        Self::build(user, now, ttl, USER_SESSION_AUDIENCE, None, String::new())
    }

    /// 第三方客户端授权：`audience`、`client_id` 与 `scope` 在签发时固定。
    ///
    /// # Errors
    /// scope 不在白名单内时返回 [`InvalidScopeError`]。
    ///
    /// # Panics
    /// ttl 非正时 panic——属调用侧配置错误，应在启动配置校验即暴露。
    pub fn client_grant(
        user: UserId,
        client_id: &str,
        scope: &str,
        now: DateTime<Utc>,
        ttl: TimeDelta,
    ) -> Result<Self, InvalidScopeError> {
        if !valid_scope(scope) {
            return Err(InvalidScopeError(scope.to_owned()));
        }
        Ok(Self::build(
            user,
            now,
            ttl,
            CLIENT_GRANT_AUDIENCE,
            Some(client_id.to_owned()),
            scope.to_owned(),
        ))
    }

    fn build(
        user: UserId,
        now: DateTime<Utc>,
        ttl: TimeDelta,
        aud: &str,
        client_id: Option<String>,
        scope: String,
    ) -> Self {
        let ttl_secs = ttl.num_seconds();
        assert!(ttl_secs > 0, "access token ttl 必须为正");
        let iat = now.timestamp();
        Self {
            sub: user.to_string(),
            iat,
            exp: iat + ttl_secs,
            aud: aud.to_owned(),
            client_id,
            scope,
        }
    }

    /// 签发时刻，Unix 秒。
    #[must_use]
    pub fn iat(&self) -> i64 {
        self.iat
    }

    /// 过期时刻，Unix 秒。
    #[must_use]
    pub fn exp(&self) -> i64 {
        self.exp
    }

    /// audience。
    #[must_use]
    pub fn aud(&self) -> &str {
        &self.aud
    }

    /// 第三方客户端标识；第一方会话为 `None`。
    #[must_use]
    pub fn client_id(&self) -> Option<&str> {
        self.client_id.as_deref()
    }

    /// 授权范围；第一方会话为空串。
    #[must_use]
    pub fn scope(&self) -> &str {
        &self.scope
    }
}

/// 签发客户端授权令牌时 scope 不在白名单内。
#[derive(Debug, thiserror::Error)]
#[error("scope 不在白名单内: {0}")]
pub struct InvalidScopeError(String);

/// 校验后的 access token 主体；audience 与 claim 组合在此收敛为两个合法形态。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VerifiedAccessToken {
    /// 第一方用户会话。
    UserSession {
        /// 会话所属用户。
        user_id: UserId,
    },
    /// 第三方客户端授权。
    ClientGrant {
        /// 授权用户。
        user_id: UserId,
        /// 令牌所属客户端。
        client_id: String,
        /// 授权范围。
        scope: String,
    },
}

/// access token 签发失败。
#[derive(Debug, thiserror::Error)]
#[error("access token 签发失败: {0}")]
pub struct SignAccessTokenError(#[source] jsonwebtoken::errors::Error);

/// access token 过期宽限，吸收跨节点时钟偏差；数值与 `jsonwebtoken` 默认一致。
const EXPIRY_LEEWAY_SECS: i64 = 60;

/// access token 校验失败（签名不符、过期、claims 组合非法等）。
#[derive(Debug, thiserror::Error)]
pub enum VerifyAccessTokenError {
    /// 签名不符、格式非法或缺少必需声明。
    #[error("access token 无效: {0}")]
    Invalid(#[source] jsonwebtoken::errors::Error),
    /// `audience`、`client_id`、`scope` 组合不合法，或 `sub` 不是 uuid。
    #[error("access token claims 非法: {0}")]
    Malformed(&'static str),
    /// 已过期：`now` 超过 `exp + EXPIRY_LEEWAY_SECS`。
    #[error("access token 已过期")]
    Expired,
}

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

/// 校验签名、有效期与 claim 形状；时间基准由调用方注入（§2 时钟注入），
/// leeway 60 秒。返回结果把 audience 收敛为第一方会话或客户端授权两种合法形态。
///
/// `jsonwebtoken` 内建的 `exp` 校验只能读墙上时钟，故在此关闭，改由 `now` 判定。
///
/// # Errors
/// 签名不符、格式非法或缺少 `exp` 时返回 [`VerifyAccessTokenError::Invalid`]；
/// `sub` 不是 uuid 或 audience/client/scope 组合非法时返回
/// [`VerifyAccessTokenError::Malformed`]；`now` 超过 `exp + 60s` 时返回
/// [`VerifyAccessTokenError::Expired`]。
pub fn verify_access(
    token: &str,
    key: &DecodingKey,
    now: DateTime<Utc>,
) -> Result<VerifiedAccessToken, VerifyAccessTokenError> {
    let mut validation = Validation::new(Algorithm::ES256);
    validation.validate_exp = false;
    // audience 由下方的类型化匹配判定；库内 aud 校验看到任意 aud 都会判失败
    validation.validate_aud = false;
    let claims = jsonwebtoken::decode::<AccessClaims>(token, key, &validation)
        .map_err(VerifyAccessTokenError::Invalid)?
        .claims;
    if now.timestamp() > claims.exp.saturating_add(EXPIRY_LEEWAY_SECS) {
        return Err(VerifyAccessTokenError::Expired);
    }
    let user_id = Uuid::parse_str(&claims.sub)
        .map(UserId::from_uuid)
        .map_err(|_| VerifyAccessTokenError::Malformed("sub 不是 uuid"))?;
    match (claims.aud.as_str(), claims.client_id.as_deref(), claims.scope.as_str()) {
        (USER_SESSION_AUDIENCE, None, "") => Ok(VerifiedAccessToken::UserSession { user_id }),
        (CLIENT_GRANT_AUDIENCE, Some(client_id), scope) if valid_scope(scope) => {
            Ok(VerifiedAccessToken::ClientGrant {
                user_id,
                client_id: client_id.to_owned(),
                scope: scope.to_owned(),
            })
        }
        _ => Err(VerifyAccessTokenError::Malformed("audience 与 client/scope 组合非法")),
    }
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
        sha256_hex(plain)
    }
}

/// 任意明文的 SHA-256 十六进制摘要；refresh、csrf 与验证码的入库哈希共用。
#[must_use]
pub fn sha256_hex(plain: &str) -> String {
    hex(&Sha256::digest(plain.as_bytes()))
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

/// 新 refresh 家族的落库描述；家族创建时固定主体、客户端与 scope，
/// 之后只轮转 token。明文不入本结构。
#[derive(Debug, Clone)]
pub struct NewRefreshFamily {
    /// 家族标识。
    pub family_id: Uuid,
    /// 所属用户。
    pub user_id: UserId,
    /// 归属客户端；`None` 为第一方用户会话。
    pub client_id: Option<String>,
    /// 授权范围（空格分隔）；第一方会话为空串。
    pub scope: String,
    /// 首个 token 明文的 SHA-256 十六进制摘要。
    pub token_hash: String,
    /// 首个 token 的过期时刻。
    pub expires_at: DateTime<Utc>,
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used, clippy::unwrap_used)]

    use super::{
        AccessClaims, CLIENT_GRANT_AUDIENCE, RefreshToken, USER_SESSION_AUDIENCE,
        VerifiedAccessToken, VerifyAccessTokenError, sign_access, verify_access,
    };
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

    fn private_key() -> jsonwebtoken::EncodingKey {
        jsonwebtoken::EncodingKey::from_ec_pem(TEST_PRIVATE_PEM.as_bytes())
            .expect("测试私钥必须可解析")
    }

    fn public_key() -> jsonwebtoken::DecodingKey {
        jsonwebtoken::DecodingKey::from_ec_pem(TEST_PUBLIC_PEM.as_bytes())
            .expect("测试公钥必须可解析")
    }

    #[test]
    fn user_session_roundtrips_as_user_session() {
        let now = fixed_now();
        let user = UserId::new();
        let claims = AccessClaims::user_session(user, now, TimeDelta::minutes(10));
        assert_eq!(claims.aud(), USER_SESSION_AUDIENCE);
        assert!(claims.client_id().is_none());
        assert_eq!(claims.scope(), "");

        let token = sign_access(&claims, &private_key()).expect("签发必须成功");
        // 校验基准是同一注入时刻，而非墙上时钟；换一天跑也仍应通过
        let decoded = verify_access(&token, &public_key(), now).expect("合法 token 必须可校验");
        assert_eq!(decoded, VerifiedAccessToken::UserSession { user_id: user });
    }

    #[test]
    fn client_grant_roundtrips_with_client_and_scope() {
        let now = fixed_now();
        let user = UserId::new();
        let claims =
            AccessClaims::client_grant(user, "client-1", "profile", now, TimeDelta::minutes(10))
                .expect("scope 在白名单内");
        assert_eq!(claims.aud(), CLIENT_GRANT_AUDIENCE);
        assert_eq!(claims.client_id(), Some("client-1"));

        let token = sign_access(&claims, &private_key()).expect("签发必须成功");
        let decoded = verify_access(&token, &public_key(), now).expect("合法 token 必须可校验");
        assert_eq!(
            decoded,
            VerifiedAccessToken::ClientGrant {
                user_id: user,
                client_id: "client-1".to_owned(),
                scope: "profile".to_owned(),
            }
        );
    }

    #[test]
    fn unknown_scope_is_rejected_at_issuance() {
        let result = AccessClaims::client_grant(
            UserId::new(),
            "client-1",
            "email",
            fixed_now(),
            TimeDelta::minutes(10),
        );
        assert!(result.is_err());
    }

    #[test]
    fn malformed_claim_combinations_are_rejected() {
        let now = fixed_now();

        // 第一方 audience 带客户端标识：不是合法会话
        let mut claims = AccessClaims::user_session(UserId::new(), now, TimeDelta::minutes(10));
        claims.aud = CLIENT_GRANT_AUDIENCE.to_owned();
        let token = sign_access(&claims, &private_key()).expect("签发必须成功");
        assert!(matches!(
            verify_access(&token, &public_key(), now),
            Err(VerifyAccessTokenError::Malformed(_))
        ));

        // 客户端 audience 缺 client_id：不是合法授权
        let mut claims = AccessClaims::client_grant(
            UserId::new(),
            "client-1",
            "profile",
            now,
            TimeDelta::minutes(10),
        )
        .expect("scope 在白名单内");
        claims.client_id = None;
        let token = sign_access(&claims, &private_key()).expect("签发必须成功");
        assert!(matches!(
            verify_access(&token, &public_key(), now),
            Err(VerifyAccessTokenError::Malformed(_))
        ));
    }

    #[test]
    fn expired_access_token_is_rejected_by_injected_now() {
        let now = fixed_now();
        let stale = AccessClaims::user_session(UserId::new(), now, TimeDelta::minutes(10));
        let token = sign_access(&stale, &private_key()).expect("签发必须成功");

        let expired_at = now + TimeDelta::minutes(10) + TimeDelta::seconds(61);
        assert!(matches!(
            verify_access(&token, &public_key(), expired_at),
            Err(VerifyAccessTokenError::Expired)
        ));
    }

    #[test]
    fn expiry_leeway_matches_jsonwebtoken_default() {
        let now = fixed_now();
        let claims = AccessClaims::user_session(UserId::new(), now, TimeDelta::minutes(10));
        let token = sign_access(&claims, &private_key()).expect("签发必须成功");
        let exp = now + TimeDelta::minutes(10);

        assert!(verify_access(&token, &public_key(), exp).is_ok(), "exp 当秒仍有效");
        assert!(verify_access(&token, &public_key(), exp + TimeDelta::seconds(60)).is_ok());
        assert!(verify_access(&token, &public_key(), exp + TimeDelta::seconds(61)).is_err());
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
        let _ = AccessClaims::user_session(UserId::new(), fixed_now(), TimeDelta::zero());
    }
}
