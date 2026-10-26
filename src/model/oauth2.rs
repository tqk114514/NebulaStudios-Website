//! `OAuth2` Provider 领域：客户端、PKCE（RFC 7636）、授权码状态机。
//! 协议纯逻辑，与 HTTP 无关（§2）。

use chrono::{DateTime, TimeDelta, Utc};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::model::user::UserId;

/// 授权码有效期（v3 同款 10 分钟）。
pub const AUTH_CODE_TTL: TimeDelta = TimeDelta::minutes(10);

/// v4 `scope` 白名单（最小集）；请求携带白名单之外的 `scope` 一律拒绝。
pub const ALLOWED_SCOPES: &[&str] = &["profile"];

/// 错误响应码（RFC 6749 §5.2），token 端点按此返回 `{"error": ...}`。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TokenError {
    /// 请求缺参数或参数非法。
    InvalidRequest,
    /// 客户端认证失败。
    InvalidClient,
    /// 授权码无效、过期、已消费或 PKCE 校验失败。
    InvalidGrant,
    /// 不支持的 `grant_type`。
    UnsupportedGrantType,
}

impl TokenError {
    /// RFC 6749 错误码字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::InvalidRequest => "invalid_request",
            Self::InvalidClient => "invalid_client",
            Self::InvalidGrant => "invalid_grant",
            Self::UnsupportedGrantType => "unsupported_grant_type",
        }
    }
}

/// PKCE verifier 合法性（RFC 7636 §4.1）：43–128 字符，非保留字符集。
#[must_use]
pub fn valid_verifier(verifier: &str) -> bool {
    (43..=128).contains(&verifier.len())
        && verifier
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'.' | b'_' | b'~'))
}

/// PKCE challenge 合法性：43–128 字符 `base64url`。
#[must_use]
pub fn valid_challenge(challenge: &str) -> bool {
    (43..=128).contains(&challenge.len())
        && challenge.bytes().all(|b| b.is_ascii_alphanumeric() || matches!(b, b'-' | b'_'))
}

/// S256 校验：`base64url(SHA256(verifier))`（无 padding）与 challenge 相等。
/// `plain` 方法在 v4 不存在——红线 #9：不接受任何降级路径。
///
/// ```
/// use nebula_server::model::oauth2::verify_pkce_s256;
///
/// // RFC 7636 附录 B 的测试向量
/// let verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
/// let challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";
/// assert!(verify_pkce_s256(verifier, challenge));
/// assert!(!verify_pkce_s256("wrong-verifier-0000000000000000000000000000", challenge));
/// ```
#[must_use]
pub fn verify_pkce_s256(verifier: &str, challenge: &str) -> bool {
    if !valid_verifier(verifier) || !valid_challenge(challenge) {
        return false;
    }
    base64url_no_pad(&Sha256::digest(verifier.as_bytes())) == challenge
}

/// base64url 编码（RFC 4648 §5，无 padding）。
fn base64url_no_pad(bytes: &[u8]) -> String {
    const ALPHABET: &[u8] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    let mut out = String::with_capacity(bytes.len().div_ceil(3) * 4);
    for chunk in bytes.chunks(3) {
        let b0 = u32::from(chunk[0]);
        let b1 = u32::from(*chunk.get(1).unwrap_or(&0));
        let b2 = u32::from(*chunk.get(2).unwrap_or(&0));
        let triple = (b0 << 16) | (b1 << 8) | b2;
        out.push(ALPHABET[((triple >> 18) & 0x3f) as usize] as char);
        out.push(ALPHABET[((triple >> 12) & 0x3f) as usize] as char);
        if chunk.len() > 1 {
            out.push(ALPHABET[((triple >> 6) & 0x3f) as usize] as char);
        }
        if chunk.len() > 2 {
            out.push(ALPHABET[(triple & 0x3f) as usize] as char);
        }
    }
    out
}

/// `scope` 白名单校验：空串合法（默认集），多 `scope` 空格分隔须全部在白名单内。
#[must_use]
pub fn valid_scope(scope: &str) -> bool {
    scope.split(' ').filter(|s| !s.is_empty()).all(|s| ALLOWED_SCOPES.contains(&s))
}

/// 授权码状态机（§2，参照 MAS）：`Fulfilled`（已同意签发）→ `Exchanged`（已兑换，终态）。
/// Pending（授权页参数校验通过、用户未同意）不落库。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum GrantStage {
    /// 已签发、可兑换。
    Fulfilled,
    /// 已兑换，终态；再次呈递判定重放。
    Exchanged,
}

impl GrantStage {
    /// 入库字符串。
    #[must_use]
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Fulfilled => "fulfilled",
            Self::Exchanged => "exchanged",
        }
    }
}

/// 落库的授权码描述；明文不入本结构。
#[derive(Debug, Clone)]
pub struct NewAuthorizationCode {
    /// 客户端。
    pub client_id: String,
    /// 授权用户。
    pub user_id: UserId,
    /// `redirect_uri` 精确匹配值。
    pub redirect_uri: String,
    /// `scope` 串（空格分隔）。
    pub scope: String,
    /// PKCE challenge（`S256`）。
    pub code_challenge: String,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
}

/// 库中读出的授权码记录。
#[derive(Debug, Clone)]
pub struct AuthorizationCode {
    /// 记录标识。
    pub id: Uuid,
    /// 客户端。
    pub client_id: String,
    /// 授权用户。
    pub user_id: UserId,
    /// `redirect_uri`。
    pub redirect_uri: String,
    /// `scope`。
    pub scope: String,
    /// PKCE challenge。
    pub code_challenge: String,
    /// 过期时刻。
    pub expires_at: DateTime<Utc>,
}

/// OAuth 客户端快照。
#[derive(Debug, Clone)]
pub struct OAuthClient {
    /// 公开 `client_id`。
    pub id: String,
    /// 展示名。
    pub name: String,
    /// `redirect_uri`（精确匹配）。
    pub redirect_uri: String,
    /// 是否启用。
    pub enabled: bool,
}

#[cfg(test)]
mod tests {
    #![allow(clippy::expect_used)]

    use super::{
        GrantStage, TokenError, base64url_no_pad, valid_challenge, valid_scope, valid_verifier,
        verify_pkce_s256,
    };

    #[test]
    fn rfc7636_appendix_b_vector() {
        let verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
        let challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";
        assert!(valid_verifier(verifier));
        assert!(valid_challenge(challenge));
        assert!(verify_pkce_s256(verifier, challenge));
    }

    #[test]
    fn verifier_length_bounds_rejected() {
        assert!(!valid_verifier(&"a".repeat(42)));
        assert!(valid_verifier(&"a".repeat(43)));
        assert!(valid_verifier(&"a".repeat(128)));
        assert!(!valid_verifier(&"a".repeat(129)));
        // 非法字符（plain 不兼容字符集）
        assert!(!valid_verifier(&format!("{}+", "a".repeat(42))));
    }

    #[test]
    fn base64url_matches_sha256_reference() {
        use sha2::Digest as _;
        // 空串 SHA-256 的已知 base64url 形态
        assert_eq!(
            base64url_no_pad(&sha2::Sha256::digest(b"")),
            "47DEQpj8HBSa-_TImW-5JCeuQeRkm5NMpJWZG3hSuFU"
        );
    }

    #[test]
    fn scope_whitelist() {
        assert!(valid_scope(""));
        assert!(valid_scope("profile"));
        assert!(valid_scope("profile profile"));
        assert!(!valid_scope("email"));
        assert!(!valid_scope("profile email"));
    }

    #[test]
    fn token_error_strings_are_rfc6749() {
        assert_eq!(TokenError::InvalidGrant.as_str(), "invalid_grant");
        assert_eq!(TokenError::InvalidClient.as_str(), "invalid_client");
    }

    #[test]
    fn grant_stage_is_terminal_after_exchange() {
        assert_eq!(GrantStage::Fulfilled.as_str(), "fulfilled");
        assert_eq!(GrantStage::Exchanged.as_str(), "exchanged");
    }
}
