//! 纯逻辑不变量测试（AGENTS §8.3）：用生成输入检验协议性质，而不是只列举样例。
//!
//! 这些用例不触碰 HTTP/SQLite，失败时 proptest 会缩小到最小反例。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use nebula_server::model::oauth2::{
    ALLOWED_SCOPES, code_challenge_s256, generate_code_verifier, valid_challenge, valid_scope,
    valid_verifier, verify_pkce_s256,
};
use nebula_server::model::tokens::{RefreshToken, sha256_hex};
use nebula_server::model::user::username_problem;
use nebula_server::model::verification::VerificationCode;
use proptest::prelude::*;

const VERIFIER_CHARS: &str = "[A-Za-z0-9._~-]{43,128}";

fn allowed_scope_strategy() -> impl Strategy<Value = String> {
    prop::collection::vec(prop::sample::select(ALLOWED_SCOPES.to_vec()), 0..=6)
        .prop_map(|tokens| tokens.join(" "))
}

proptest! {
    #[test]
    fn generated_code_verifier_is_valid(_seed in any::<u8>()) {
        let verifier = generate_code_verifier();
        prop_assert!(valid_verifier(&verifier));
        prop_assert!(verify_pkce_s256(&verifier, &code_challenge_s256(&verifier)));
    }

    #[test]
    fn generated_verifier_roundtrips_to_valid_challenge(verifier in VERIFIER_CHARS) {
        let challenge = code_challenge_s256(&verifier);
        prop_assert!(valid_verifier(&verifier));
        prop_assert!(valid_challenge(&challenge));
        prop_assert!(verify_pkce_s256(&verifier, &challenge));
    }

    #[test]
    fn s256_rejects_tampered_challenge(verifier in VERIFIER_CHARS, index in 0usize..128) {
        let challenge = code_challenge_s256(&verifier);
        let mut bytes = challenge.into_bytes();
        let index = index % bytes.len();
        bytes[index] = if bytes[index] == b'A' { b'B' } else { b'A' };
        let tampered = String::from_utf8(bytes).expect("base64url 必须是 ASCII");
        prop_assert!(!verify_pkce_s256(&verifier, &tampered));
    }

    #[test]
    fn verifier_length_bounds_are_exact(
        short in "[A-Za-z0-9._~-]{0,42}",
        long in "[A-Za-z0-9._~-]{129,256}",
    ) {
        prop_assert!(!valid_verifier(&short));
        prop_assert!(!valid_verifier(&long));
    }

    #[test]
    fn verifier_rejects_outside_alphabet(
        verifier in VERIFIER_CHARS,
        index in 0usize..128,
        bad in prop::sample::select(vec!['!', '/', '+', '=', ' ']),
    ) {
        let mut chars: Vec<char> = verifier.chars().collect();
        let index = index % chars.len();
        chars[index] = bad;
        let mutated: String = chars.into_iter().collect();
        prop_assert!(!valid_verifier(&mutated));
    }

    #[test]
    fn allowed_scope_sequences_are_valid(scope in allowed_scope_strategy()) {
        prop_assert!(valid_scope(&scope));
    }

    #[test]
    fn unknown_scope_token_is_rejected(
        scope in allowed_scope_strategy(),
        unknown in "[a-z]{1,16}",
    ) {
        prop_assume!(!ALLOWED_SCOPES.contains(&unknown.as_str()));
        let candidate = if scope.is_empty() { unknown } else { format!("{scope} {unknown}") };
        prop_assert!(!valid_scope(&candidate));
    }

    #[test]
    fn refresh_tokens_are_hex_and_hash_deterministically(_seed in any::<u8>()) {
        let token = RefreshToken::generate();
        prop_assert_eq!(token.as_str().len(), 64);
        prop_assert!(
            token
                .as_str()
                .bytes()
                .all(|byte| matches!(byte, b'0'..=b'9' | b'a'..=b'f'))
        );
        prop_assert_eq!(token.to_hash_hex().len(), 64);
        prop_assert_eq!(RefreshToken::hash_of(token.as_str()), token.to_hash_hex());
        prop_assert_eq!(sha256_hex(token.as_str()), token.to_hash_hex());
    }

    #[test]
    fn verification_codes_use_the_unambiguous_alphabet(_seed in any::<u8>()) {
        let code = VerificationCode::generate();
        prop_assert_eq!(code.as_str().len(), 6);
        prop_assert!(
            code.as_str()
                .chars()
                .all(|c| c.is_ascii_alphanumeric() && !"0OILl".contains(c))
        );
    }

    #[test]
    fn username_rules_match_the_reference_predicate(username in any::<String>()) {
        let reference = (3..=15).contains(&username.chars().count())
            && username
                .chars()
                .all(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '-'));
        prop_assert_eq!(username_problem(&username).is_none(), reference);
    }
}
