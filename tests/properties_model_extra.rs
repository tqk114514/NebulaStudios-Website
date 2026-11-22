//! 纯模型 property 测试补充（AGENTS §8.3）：access token claims、TOTP、社交枚举与口令哈希。
//!
//! 覆盖 `tests/properties.rs` 之外的模型性质；不触碰 HTTP/SQLite，时钟与密钥均显式注入，
//! 失败时 proptest 会缩小到最小反例。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::model::oauth2::ALLOWED_SCOPES;
use nebula_server::model::password::PasswordHash;
use nebula_server::model::social::{SocialLoginAction, SocialProvider};
use nebula_server::model::tokens::{
    AccessClaims, CLIENT_GRANT_AUDIENCE, USER_SESSION_AUDIENCE, VerifiedAccessToken,
    VerifyAccessTokenError, sign_access, verify_access,
};
use nebula_server::model::totp::{
    TOTP_ALGORITHM, TOTP_DIGITS, TOTP_PERIOD, TOTP_SKEW, generate_secret, otpauth_uri, verify_code,
};
use nebula_server::model::user::UserId;
use proptest::prelude::*;
use uuid::Uuid;

// 仅测试用的 P-256 密钥对（PKCS#8 私钥 + 公钥），与 `src/model/tokens.rs` 的单测同源，
// 不用于任何真实环境。集成测试无法共享 `src/` 内的常量，故在此重复。
const TEST_PRIVATE_PEM: &str = "-----BEGIN PRIVATE KEY-----\nMIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQgHHnffUGVWWLk8Hz8\nT+NdLnsaBCFWKphSj4rcq30zzkShRANCAASIriVp5Ne81a6mczgeIzuAXgLonIjY\nBRQBVkauO2/OKAe/QmK5cQ8TkPMha0N9171Z6eBiUfhJUeZs9qieNjw8\n-----END PRIVATE KEY-----";
const TEST_PUBLIC_PEM: &str = "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEiK4laeTXvNWupnM4HiM7gF4C6JyI\n2AUUAVZGrjtvzigHv0JiuXEPE5DzIWtDfde9WengYlH4SVHmbPaonjY8PA==\n-----END PUBLIC KEY-----";

fn fixed_now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
        .expect("固定字面量必须可解析")
        .with_timezone(&Utc)
}

fn at_unix_seconds(secs: i64) -> DateTime<Utc> {
    DateTime::from_timestamp(secs, 0).expect("测试时间戳必须在 chrono 范围内")
}

fn fixed_user() -> UserId {
    UserId::from_uuid(Uuid::from_bytes([0x42; 16]))
}

fn encoding_key() -> jsonwebtoken::EncodingKey {
    jsonwebtoken::EncodingKey::from_ec_pem(TEST_PRIVATE_PEM.as_bytes()).expect("测试私钥必须可解析")
}

fn decoding_key() -> jsonwebtoken::DecodingKey {
    jsonwebtoken::DecodingKey::from_ec_pem(TEST_PUBLIC_PEM.as_bytes()).expect("测试公钥必须可解析")
}

/// 白名单 scope 的随机序列，空格连接；空序列即空串（默认集）。
fn allowed_scope_strategy() -> impl Strategy<Value = String> {
    prop::collection::vec(prop::sample::select(ALLOWED_SCOPES.to_vec()), 0..=4)
        .prop_map(|tokens| tokens.join(" "))
}

/// 绕过构造函数拼出任意 claim 组合，用于检验签发路径之外的非法形态。
fn forged_claims(aud: &str, client_id: Option<&str>, scope: &str, sub: &str) -> AccessClaims {
    let iat = fixed_now().timestamp();
    let mut value = serde_json::json!({
        "sub": sub,
        "iat": iat,
        "exp": iat + 600,
        "aud": aud,
        "ver": 0,
    });
    if let Some(client_id) = client_id {
        value["client_id"] = serde_json::Value::String(client_id.to_owned());
    }
    if !scope.is_empty() {
        value["scope"] = serde_json::Value::String(scope.to_owned());
    }
    serde_json::from_value(value).expect("测试 claims JSON 必须可反序列化")
}

fn totp_for(secret: &str) -> totp_rs::Totp {
    let parsed = totp_rs::Secret::try_from_base32(secret).expect("测试密钥必须可解析");
    totp_rs::Builder::new()
        .with_algorithm(TOTP_ALGORITHM)
        .with_digits(TOTP_DIGITS)
        .with_skew(u16::from(TOTP_SKEW))
        .with_step_duration(TOTP_PERIOD)
        .with_secret(parsed)
        .build()
        .expect("参数合法")
}

/// `%XX` 解码；仅接受 `otpauth_uri` 自身产出的转义序列。
fn percent_decode(input: &str) -> String {
    let bytes = input.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' && index + 2 < bytes.len() {
            let hex = std::str::from_utf8(&bytes[index + 1..index + 3]).expect("转义必须为 ASCII");
            out.push(u8::from_str_radix(hex, 16).expect("转义必须为十六进制"));
            index += 3;
        } else {
            out.push(bytes[index]);
            index += 1;
        }
    }
    String::from_utf8(out).expect("解码结果必须是 UTF-8")
}

proptest! {
    #[test]
    fn user_session_ttl_defines_exp_minus_iat(
        ttl_secs in 1i64..=86_400,
        token_version in any::<i64>(),
    ) {
        let now = fixed_now();
        let claims =
            AccessClaims::user_session(fixed_user(), now, TimeDelta::seconds(ttl_secs), token_version);
        prop_assert_eq!(claims.iat(), now.timestamp());
        prop_assert_eq!(claims.exp() - claims.iat(), ttl_secs);
        prop_assert_eq!(claims.aud(), USER_SESSION_AUDIENCE);
        prop_assert_eq!(claims.client_id(), None);
        prop_assert_eq!(claims.scope(), "");
    }

    #[test]
    fn user_session_roundtrip_preserves_user_and_token_version(
        user_bytes in any::<[u8; 16]>(),
        token_version in any::<i64>(),
        ttl_secs in 1i64..=86_400,
    ) {
        let user = UserId::from_uuid(Uuid::from_bytes(user_bytes));
        let now = fixed_now();
        let claims =
            AccessClaims::user_session(user, now, TimeDelta::seconds(ttl_secs), token_version);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        prop_assert_eq!(
            verify_access(&token, &decoding_key(), now).expect("合法 token 必须可校验"),
            VerifiedAccessToken::UserSession { user_id: user, token_version },
        );
    }

    #[test]
    fn client_grant_roundtrip_preserves_client_and_scope(
        scope in allowed_scope_strategy(),
        client_id in "[A-Za-z0-9._-]{1,32}",
        ttl_secs in 1i64..=86_400,
    ) {
        let user = fixed_user();
        let now = fixed_now();
        let claims =
            AccessClaims::client_grant(user, &client_id, &scope, now, TimeDelta::seconds(ttl_secs))
                .expect("白名单 scope 必须可签发");
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        prop_assert_eq!(
            verify_access(&token, &decoding_key(), now).expect("合法 token 必须可校验"),
            VerifiedAccessToken::ClientGrant {
                user_id: user,
                client_id: client_id.clone(),
                scope: scope.clone(),
            },
        );
    }

    #[test]
    fn client_grant_accepts_whitelisted_scope_sequences(scope in allowed_scope_strategy()) {
        prop_assert!(
            AccessClaims::client_grant(
                fixed_user(),
                "client-1",
                &scope,
                fixed_now(),
                TimeDelta::minutes(10),
            )
            .is_ok()
        );
    }

    #[test]
    fn client_grant_rejects_unknown_scope_tokens(
        allowed in allowed_scope_strategy(),
        unknown in "[a-z]{1,12}",
    ) {
        prop_assume!(!ALLOWED_SCOPES.contains(&unknown.as_str()));
        let scope = if allowed.is_empty() { unknown } else { format!("{allowed} {unknown}") };
        prop_assert!(
            AccessClaims::client_grant(
                fixed_user(),
                "client-1",
                &scope,
                fixed_now(),
                TimeDelta::minutes(10),
            )
            .is_err()
        );
    }

    #[test]
    fn access_token_verifies_any_time_before_expiry(
        ttl_secs in 1i64..=86_400,
        offset_secs in 0i64..=86_400,
    ) {
        let now = fixed_now();
        let claims = AccessClaims::user_session(fixed_user(), now, TimeDelta::seconds(ttl_secs), 0);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        let observed = claims.iat() + offset_secs % (ttl_secs + 1);
        let verified = verify_access(&token, &decoding_key(), at_unix_seconds(observed));
        let is_user_session = matches!(verified, Ok(VerifiedAccessToken::UserSession { .. }));
        prop_assert!(is_user_session);
    }

    #[test]
    fn access_token_expiry_allows_exactly_sixty_seconds_of_leeway(
        ttl_secs in 1i64..=86_400,
        delta_secs in 0i64..=3_600,
    ) {
        let now = fixed_now();
        let claims = AccessClaims::user_session(fixed_user(), now, TimeDelta::seconds(ttl_secs), 0);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        let observed = at_unix_seconds(claims.exp() + delta_secs);
        let result = verify_access(&token, &decoding_key(), observed);
        if delta_secs <= 60 {
            let is_user_session = matches!(result, Ok(VerifiedAccessToken::UserSession { .. }));
            prop_assert!(is_user_session);
        } else {
            prop_assert!(matches!(result, Err(VerifyAccessTokenError::Expired)));
        }
    }

    #[test]
    fn session_audience_with_client_claims_is_rejected(
        scope in allowed_scope_strategy(),
        client_id in "[A-Za-z0-9._-]{1,32}",
    ) {
        let sub = fixed_user().to_string();
        let claims = forged_claims(USER_SESSION_AUDIENCE, Some(&client_id), &scope, &sub);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        prop_assert!(matches!(
            verify_access(&token, &decoding_key(), fixed_now()),
            Err(VerifyAccessTokenError::Malformed(_))
        ));
    }

    #[test]
    fn client_audience_without_client_id_is_rejected(scope in allowed_scope_strategy()) {
        let sub = fixed_user().to_string();
        let claims = forged_claims(CLIENT_GRANT_AUDIENCE, None, &scope, &sub);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        prop_assert!(matches!(
            verify_access(&token, &decoding_key(), fixed_now()),
            Err(VerifyAccessTokenError::Malformed(_))
        ));
    }

    #[test]
    fn client_audience_with_scope_outside_whitelist_is_rejected(
        unknown in "[a-z]{1,12}",
        client_id in "[A-Za-z0-9._-]{1,32}",
    ) {
        prop_assume!(!ALLOWED_SCOPES.contains(&unknown.as_str()));
        let sub = fixed_user().to_string();
        let claims = forged_claims(CLIENT_GRANT_AUDIENCE, Some(&client_id), &unknown, &sub);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        prop_assert!(matches!(
            verify_access(&token, &decoding_key(), fixed_now()),
            Err(VerifyAccessTokenError::Malformed(_))
        ));
    }

    #[test]
    fn non_uuid_subject_is_rejected(sub in "[a-z]{1,20}") {
        let claims = forged_claims(USER_SESSION_AUDIENCE, None, "", &sub);
        let token = sign_access(&claims, &encoding_key()).expect("签发必须成功");
        prop_assert!(matches!(
            verify_access(&token, &decoding_key(), fixed_now()),
            Err(VerifyAccessTokenError::Malformed(_))
        ));
    }

    #[test]
    fn generated_totp_secret_is_32_base32_chars(_seed in any::<u8>()) {
        let secret = generate_secret();
        prop_assert_eq!(secret.len(), 32);
        prop_assert!(
            secret.bytes().all(|byte| byte.is_ascii_uppercase() || (b'2'..=b'7').contains(&byte))
        );
        prop_assert!(totp_rs::Secret::try_from_base32(&secret).is_ok());
    }

    #[test]
    fn totp_code_is_accepted_within_one_step_window(step in 1u64..=60_000_000) {
        let secret = generate_secret();
        let totp = totp_for(&secret);
        let base = step * TOTP_PERIOD;
        let code = totp.generate(base).to_string();
        prop_assert!(verify_code(&secret, &code, at_unix_seconds(base.cast_signed())));
        prop_assert!(verify_code(
            &secret,
            &code,
            at_unix_seconds((base - TOTP_PERIOD).cast_signed())
        ));
        prop_assert!(verify_code(
            &secret,
            &code,
            at_unix_seconds((base + TOTP_PERIOD).cast_signed())
        ));
    }

    #[test]
    fn totp_code_outside_one_step_window_is_rejected(step in 2u64..=60_000_000) {
        let secret = generate_secret();
        let totp = totp_for(&secret);
        let base = step * TOTP_PERIOD;
        let inside = [
            totp.generate((step - 1) * TOTP_PERIOD).to_string(),
            totp.generate(base).to_string(),
            totp.generate((step + 1) * TOTP_PERIOD).to_string(),
        ];
        let early = totp.generate((step - 2) * TOTP_PERIOD).to_string();
        let late = totp.generate((step + 2) * TOTP_PERIOD).to_string();
        // 六位码空间有限，极小概率与相邻时间片的码相同；此时该用例不构成反例。
        prop_assume!(!inside.contains(&early));
        prop_assume!(!inside.contains(&late));
        let now = at_unix_seconds(base.cast_signed());
        prop_assert!(!verify_code(&secret, &early, now));
        prop_assert!(!verify_code(&secret, &late, now));
    }

    #[test]
    fn wrongly_shaped_codes_are_rejected(code in "[0-9A-Za-z]{0,12}") {
        prop_assume!(
            code.len() != usize::from(TOTP_DIGITS)
                || !code.bytes().all(|byte| byte.is_ascii_digit())
        );
        let secret = generate_secret();
        prop_assert!(totp_rs::Secret::try_from_base32(&secret).is_ok());
        prop_assert!(!verify_code(&secret, &code, fixed_now()));
    }

    #[test]
    fn spaces_and_surrounding_whitespace_are_ignored(_seed in any::<u8>()) {
        let secret = generate_secret();
        let now = fixed_now();
        let code = totp_for(&secret).generate(now.timestamp().cast_unsigned()).to_string();
        let spaced = format!("{} {}", &code[..3], &code[3..]);
        let padded_with_space = format!(" {spaced} ");
        let padded_with_tabs = format!("\t{code}\n");
        prop_assert!(verify_code(&secret, &code, now));
        prop_assert!(verify_code(&secret, &padded_with_space, now));
        prop_assert!(verify_code(&secret, &padded_with_tabs, now));
    }

    #[test]
    fn otpauth_uri_keeps_query_structure(email in ".{0,64}") {
        let secret = generate_secret();
        let uri = otpauth_uri(&email, &secret);
        prop_assert!(uri.starts_with("otpauth://totp/Nebula:"));
        prop_assert_eq!(uri.matches('?').count(), 1);
        prop_assert_eq!(uri.matches('&').count(), 4);
        prop_assert!(!uri.contains(' '));
        prop_assert!(!uri.contains('#'));
        let secret_query = format!("?secret={secret}&");
        prop_assert!(uri.ends_with("&issuer=Nebula&algorithm=SHA1&digits=6&period=30"));
        prop_assert!(uri.contains(&secret_query));
    }

    #[test]
    fn otpauth_label_percent_decodes_to_account(email in ".{0,32}") {
        // 密钥内容不在本性质的关注范围内，用固定合法基串隔离变量
        let uri = otpauth_uri(&email, "JBSWY3DPEHPK3PXP");
        let label = uri
            .strip_prefix("otpauth://totp/")
            .and_then(|rest| rest.split('?').next())
            .expect("otpauth URI 前缀固定");
        prop_assert_eq!(percent_decode(label), format!("Nebula:{email}"));
    }

    #[test]
    fn social_provider_raw_string_roundtrips(
        provider in prop::sample::select(vec![SocialProvider::Google, SocialProvider::Microsoft]),
    ) {
        prop_assert_eq!(SocialProvider::from_str_raw(provider.as_str()), Some(provider));
    }

    #[test]
    fn social_login_action_raw_string_roundtrips(
        action in prop::sample::select(vec![SocialLoginAction::Login, SocialLoginAction::Link]),
    ) {
        prop_assert_eq!(SocialLoginAction::from_str_raw(action.as_str()), Some(action));
    }

    #[test]
    fn unknown_social_provider_strings_are_rejected(raw in ".{0,24}") {
        prop_assume!(raw != "google" && raw != "microsoft");
        prop_assert_eq!(SocialProvider::from_str_raw(&raw), None);
    }

    #[test]
    fn unknown_social_login_action_strings_are_rejected(raw in ".{0,24}") {
        prop_assume!(raw != "login" && raw != "link");
        prop_assert_eq!(SocialLoginAction::from_str_raw(&raw), None);
    }
}

proptest! {
    // Argon2 默认参数下每次计算约几十毫秒，按任务约束固定为 4 组。
    #![proptest_config(ProptestConfig::with_cases(4))]

    #[test]
    fn password_hash_accepts_only_the_hashed_plain(plain in "[A-Za-z0-9!@#%&*_-]{0,24}") {
        let hash = PasswordHash::hash(&plain).expect("哈希必须成功");
        let other_plain = format!("{plain}extra");
        prop_assert!(hash.verify(&plain).expect("合法哈希必须可校验"));
        prop_assert!(!hash.verify(&other_plain).expect("合法哈希必须可校验"));
    }

    #[test]
    fn password_hash_salts_each_computation(plain in "[A-Za-z0-9!@#%&*_-]{0,24}") {
        let first = PasswordHash::hash(&plain).expect("哈希必须成功");
        let second = PasswordHash::hash(&plain).expect("哈希必须成功");
        prop_assert_ne!(first.as_str(), second.as_str());
        prop_assert!(first.verify(&plain).expect("合法哈希必须可校验"));
        prop_assert!(second.verify(&plain).expect("合法哈希必须可校验"));
    }
}
