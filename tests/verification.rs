//! 邮箱验证闭环集成测试：发信、验证、限次、时效、重发冷却（ADR-0002 语义）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{OneshotPost, await_first_mail, extract_code};
use nebula_server::context::Clock;

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

async fn register(app: &axum::Router) {
    let body = format!(r#"{{"email":"{EMAIL}","username":"{USERNAME}","password":"{PASSWORD}"}}"#);
    let response = app.clone().oneshot_post("/api/auth/register", &body).await;
    assert_eq!(response.status(), StatusCode::CREATED, "注册助手必须成功");
}

async fn verify(app: &axum::Router, code: &str) -> axum::response::Response {
    verify_from(app, "127.0.0.1", code).await
}

/// 以指定源 IP 发起验证——验证端点自身有 IP 限额（5/10s），
/// 连续多次的用例须换 IP，以免撞限额层而非被测逻辑。
async fn verify_from(app: &axum::Router, ip: &str, code: &str) -> axum::response::Response {
    let body = format!(r#"{{"email":"{EMAIL}","code":"{code}"}}"#);
    app.clone().oneshot_post_from_ip(ip, "/api/auth/register/verify", &body).await
}

async fn body_text(response: axum::response::Response) -> String {
    let bytes = http_body_util::BodyExt::collect(response.into_body()).await.unwrap().to_bytes();
    String::from_utf8_lossy(&bytes).into_owned()
}

#[tokio::test]
async fn register_mails_code_and_verification_marks_user_verified() {
    let (app, state, clock, mailbox, _dir) = common::app().await;
    register(&app).await;

    let message = await_first_mail(&mailbox).await;
    let code = extract_code(&message);
    assert_eq!(code.len(), 6, "六位验证码: {}", message.body);
    assert_eq!(message.to, EMAIL);

    let response = verify(&app, &code).await;
    assert_eq!(response.status(), StatusCode::NO_CONTENT);

    let user = state.user_repo.find_by_email(EMAIL).await.unwrap().expect("用户必须存在");
    assert!(user.email_verified, "验证后必须置位");
    // 时钟未被验证流程拨动——过期判断全部经注入时钟
    let expected =
        chrono::DateTime::parse_from_rfc3339(common::FIX_NOW).unwrap().with_timezone(&chrono::Utc);
    assert_eq!(clock.now(), expected);
}

#[tokio::test]
async fn verification_code_hash_uses_hmac_pepper() {
    let (app, state, _clock, mailbox, dir) = common::app().await;
    register(&app).await;
    let code = extract_code(&await_first_mail(&mailbox).await);

    let pool = nebula_server::storage::sqlite::connect_pool(&dir.path().join("users.db"))
        .await
        .expect("测试库必须可打开");
    let stored: String = sqlx::query_scalar(
        "SELECT code_hash FROM email_verifications ORDER BY created_at DESC LIMIT 1",
    )
    .fetch_one(&pool)
    .await
    .expect("验证码哈希必须可读");
    pool.close().await;

    assert_ne!(
        stored,
        nebula_server::model::tokens::sha256_hex(&code),
        "不得使用无 pepper 的裸 SHA-256"
    );
    assert_eq!(stored, state.code_hasher.hash(&code));
}

#[tokio::test]
async fn wrong_codes_burn_after_max_attempts() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register(&app).await;
    let real_code = extract_code(&await_first_mail(&mailbox).await);

    // 五次错码全部 401；v3 语义：打满即作废（换源 IP 绕开端点自身的 IP 限额）
    for i in 0..5u8 {
        let ip = format!("10.7.7.{i}");
        let response = verify_from(&app, &ip, "AAAAAA").await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
        assert!(body_text(response).await.contains("invalid_code"));
    }
    // 正确的码也进不来了
    let burned = verify_from(&app, "10.7.7.9", &real_code).await;
    assert_eq!(burned.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn unknown_email_yields_identical_body_to_wrong_code() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register(&app).await;
    let _ = extract_code(&await_first_mail(&mailbox).await);

    let wrong_code = body_text(verify(&app, "AAAAAA").await).await;
    let unknown = {
        let body = r#"{"email":"ghost@nebula.st","code":"AAAAAA"}"#;
        let response = app.clone().oneshot_post("/api/auth/register/verify", body).await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
        body_text(response).await
    };
    // 防枚举：不存在的邮箱与错码逐字节同体
    assert_eq!(wrong_code, unknown);
}

#[tokio::test]
async fn expired_code_is_rejected() {
    let (app, _state, clock, mailbox, _dir) = common::app().await;
    register(&app).await;
    let code = extract_code(&await_first_mail(&mailbox).await);

    clock.advance(chrono::TimeDelta::minutes(10) + chrono::TimeDelta::seconds(1));
    let response = verify(&app, &code).await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert!(body_text(response).await.contains("invalid_code"));
}

#[tokio::test]
async fn resend_cooldown_then_new_code_invalidates_old() {
    let (app, _state, clock, mailbox, _dir) = common::app().await;
    register(&app).await;
    let first_code = extract_code(&await_first_mail(&mailbox).await);

    // 立即重发：冷却中
    let body = format!(r#"{{"email":"{EMAIL}"}}"#);
    let blocked = app.clone().oneshot_post("/api/auth/register/resend", &body).await;
    assert_eq!(blocked.status(), StatusCode::TOO_MANY_REQUESTS);
    let json: serde_json::Value = serde_json::from_str(&body_text(blocked).await).unwrap();
    assert_eq!(json["code"], "resend_cooldown");
    assert!(json["details"]["retry_after"].as_u64().is_some());

    // 冷却结束后重发成功，旧码失效（生效的是最新未消费）
    clock.advance(chrono::TimeDelta::seconds(61));
    let resent = app.clone().oneshot_post("/api/auth/register/resend", &body).await;
    assert_eq!(resent.status(), StatusCode::NO_CONTENT);

    // 等第二封邮件（捕获信箱里现在应有两封）
    let mut second = None;
    for _ in 0..200 {
        let messages = mailbox.messages();
        if messages.len() >= 2 {
            second = Some(messages[1].clone());
            break;
        }
        tokio::time::sleep(std::time::Duration::from_millis(5)).await;
    }
    let second_code = extract_code(&second.expect("第二封邮件必须发出"));
    assert_ne!(second_code, first_code);

    let old = verify(&app, &first_code).await;
    assert_eq!(old.status(), StatusCode::UNAUTHORIZED);
    let fresh = verify(&app, &second_code).await;
    assert_eq!(fresh.status(), StatusCode::NO_CONTENT);
}

#[tokio::test]
async fn resend_unknown_email_is_204_without_mail() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;

    let body = r#"{"email":"ghost@nebula.st"}"#;
    let response = app.clone().oneshot_post("/api/auth/register/resend", body).await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    assert!(mailbox.messages().is_empty(), "未知邮箱不得发信");
}

#[tokio::test]
async fn resend_verified_user_is_204_without_mail() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register(&app).await;
    let code = extract_code(&await_first_mail(&mailbox).await);
    let verified = verify(&app, &code).await;
    assert_eq!(verified.status(), StatusCode::NO_CONTENT);

    let body = format!(r#"{{"email":"{EMAIL}"}}"#);
    let response = app.clone().oneshot_post("/api/auth/register/resend", &body).await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    assert_eq!(mailbox.messages().len(), 1, "已验证用户不得再收验证码");
}
