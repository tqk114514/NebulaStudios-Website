//! Cloudflare Worker 对 Google 响应的 Ed25519 背书校验（ADR-0012）。
//!
//! Worker 对 `timestamp + "\n" + data` 签名，并把 `data`、`timestamp`、`signature`
//! 一并返回；公钥由配置提供（PKIX PEM）。时间戳窗口取 300 秒，与 v3 一致。

use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use chrono::{DateTime, Utc};
use ed25519_dalek::pkcs8::DecodePublicKey as _;
use ed25519_dalek::{Signature, VerifyingKey};
use serde::Deserialize;

use super::SocialError;

/// 背书新鲜度窗口（秒）。
const MAX_AGE_SECS: i64 = 300;

/// Worker 的签名信封。
#[derive(Debug, Deserialize)]
pub struct WorkerEnvelope {
    /// Google 的原始响应体。
    pub data: String,
    /// Worker 签名时刻，Unix 秒。
    pub timestamp: i64,
    /// `base64(Ed25519(timestamp + "\n" + data))`。
    pub signature: String,
}

/// 校验信封并返回其中的 `data`。
///
/// # Errors
/// 公钥非法、签名不是合法 base64 或长度不符、时间戳超出 300 秒窗口、
/// 签名不匹配时返回 [`SocialError::Malformed`]。
pub fn verify_envelope(
    envelope: &WorkerEnvelope,
    public_key_pem: &str,
    now: DateTime<Utc>,
) -> Result<String, SocialError> {
    if (now.timestamp() - envelope.timestamp).abs() > MAX_AGE_SECS {
        return Err(SocialError::Malformed("Worker 背书时间戳超窗"));
    }
    let key = VerifyingKey::from_public_key_pem(public_key_pem)
        .map_err(|_| SocialError::Malformed("Worker 签名公钥非法"))?;
    let signature_bytes = BASE64
        .decode(&envelope.signature)
        .map_err(|_| SocialError::Malformed("Worker 签名不是合法 base64"))?;
    let signature = Signature::from_slice(&signature_bytes)
        .map_err(|_| SocialError::Malformed("Worker 签名长度非法"))?;
    let message = format!("{}\n{}", envelope.timestamp, envelope.data);
    key.verify_strict(message.as_bytes(), &signature)
        .map_err(|_| SocialError::Malformed("Worker 签名不匹配"))?;
    Ok(envelope.data.clone())
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use base64::Engine as _;
    use base64::engine::general_purpose::STANDARD as BASE64;
    use chrono::{DateTime, TimeDelta, Utc};
    use ed25519_dalek::pkcs8::EncodePublicKey as _;
    use ed25519_dalek::{Signer as _, SigningKey};

    use super::{WorkerEnvelope, verify_envelope};

    /// 把公钥编码为 PKIX PEM；自行折行以避开 `LineEnding` 的路径依赖。
    fn public_key_pem(signing: &SigningKey) -> String {
        let der = signing.verifying_key().to_public_key_der().expect("公钥 DER 必须可编码");
        let encoded = BASE64.encode(der.as_bytes());
        let mut body = String::new();
        for chunk in encoded.as_bytes().chunks(64) {
            body.push_str(std::str::from_utf8(chunk).expect("base64 是 ASCII"));
            body.push('\n');
        }
        format!("-----BEGIN PUBLIC KEY-----\n{body}-----END PUBLIC KEY-----\n")
    }

    /// 固定种子的测试密钥对（不用于任何真实环境）。
    fn keypair() -> (SigningKey, String) {
        let signing = SigningKey::from_bytes(&[7u8; 32]);
        let pem = public_key_pem(&signing);
        (signing, pem)
    }

    fn fixed_now() -> DateTime<Utc> {
        DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
            .expect("固定字面量必须可解析")
            .with_timezone(&Utc)
    }

    fn signed_envelope(signing: &SigningKey, data: &str, timestamp: i64) -> WorkerEnvelope {
        let message = format!("{timestamp}\n{data}");
        let signature = BASE64.encode(signing.sign(message.as_bytes()).to_bytes());
        WorkerEnvelope { data: data.to_owned(), timestamp, signature: signature.clone() }
    }

    #[test]
    fn valid_envelope_returns_data() {
        let (signing, pem) = keypair();
        let now = fixed_now();
        let envelope = signed_envelope(&signing, r#"{"access_token":"x"}"#, now.timestamp());

        let data = verify_envelope(&envelope, &pem, now).expect("合法背书必须通过");
        assert_eq!(data, r#"{"access_token":"x"}"#);
    }

    #[test]
    fn tampered_data_is_rejected() {
        let (signing, pem) = keypair();
        let now = fixed_now();
        let mut envelope = signed_envelope(&signing, "original", now.timestamp());
        envelope.data = "tampered".to_owned();

        assert!(verify_envelope(&envelope, &pem, now).is_err());
    }

    #[test]
    fn stale_timestamp_is_rejected() {
        let (signing, pem) = keypair();
        let now = fixed_now();
        let envelope = signed_envelope(&signing, "data", now.timestamp());

        let later = now + TimeDelta::seconds(301);
        assert!(verify_envelope(&envelope, &pem, later).is_err());
        let earlier = now - TimeDelta::seconds(301);
        assert!(verify_envelope(&envelope, &pem, earlier).is_err());
    }

    #[test]
    fn wrong_key_is_rejected() {
        let (signing, _pem) = keypair();
        let other = SigningKey::from_bytes(&[9u8; 32]);
        let other_pem = public_key_pem(&other);
        let now = fixed_now();
        let envelope = signed_envelope(&signing, "data", now.timestamp());

        assert!(verify_envelope(&envelope, &other_pem, now).is_err());
    }
}
