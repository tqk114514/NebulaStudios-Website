//! 限流集成测试：按 IP 突发限额、信封化 429、限额间互不干扰（ADR-0004 details）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::OneshotPost;

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

async fn body_text(response: axum::response::Response) -> String {
    let bytes = http_body_util::BodyExt::collect(response.into_body()).await.unwrap().to_bytes();
    String::from_utf8_lossy(&bytes).into_owned()
}

#[tokio::test]
async fn login_burst_exhausts_into_enveloped_429() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    common::register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let login_body = format!(r#"{{"identifier":"{EMAIL}","password":"Wrong!Passw0rd-2026"}}"#);

    // v3 参数：突发 5。前五次穿过限额层（凭据错而 401）
    for _ in 0..5 {
        let response =
            app.clone().oneshot_post_from_ip("10.1.1.1", "/api/auth/login", &login_body).await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    }

    // 第六次：429 + ADR-0004 信封 + details.retry_after
    let limited =
        app.clone().oneshot_post_from_ip("10.1.1.1", "/api/auth/login", &login_body).await;
    assert_eq!(limited.status(), StatusCode::TOO_MANY_REQUESTS);
    let json: serde_json::Value = serde_json::from_str(&body_text(limited).await).unwrap();
    assert_eq!(json["code"], "rate_limited");
    let retry_after =
        json["details"]["retry_after"].as_u64().expect("details.retry_after 必须是数字");
    assert!(retry_after >= 1, "retry_after 至少 1 秒: {retry_after}");
}

#[tokio::test]
async fn different_ip_has_independent_budget() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    common::register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let login_body = format!(r#"{{"identifier":"{EMAIL}","password":"Wrong!Passw0rd-2026"}}"#);
    // 10.2.2.2 打满
    for _ in 0..6 {
        let _ = app.clone().oneshot_post_from_ip("10.2.2.2", "/api/auth/login", &login_body).await;
    }

    // 另一个 IP 不受牵连
    let response =
        app.clone().oneshot_post_from_ip("10.3.3.3", "/api/auth/login", &login_body).await;
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn register_burst_is_three() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let body = format!(r#"{{"email":"{EMAIL}","username":"{USERNAME}","password":"{PASSWORD}"}}"#);

    for i in 0..3 {
        let response =
            app.clone().oneshot_post_from_ip("10.4.4.4", "/api/auth/register", &body).await;
        // 首次 201，其后 409（同邮箱）——都穿过限额层，消耗额度
        let expected = if i == 0 { StatusCode::CREATED } else { StatusCode::CONFLICT };
        assert_eq!(response.status(), expected);
    }

    let limited = app.clone().oneshot_post_from_ip("10.4.4.4", "/api/auth/register", &body).await;
    assert_eq!(limited.status(), StatusCode::TOO_MANY_REQUESTS);
    assert!(body_text(limited).await.contains("rate_limited"));
}

#[tokio::test]
async fn totp_login_burst_is_limited() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let body = r#"{"challenge_token":"0000000000000000000000000000000000000000000000000000000000000000","code":"123456"}"#;

    // TOTP 登录独立限额：突发 10；未知挑战在额度内返回 401
    for _ in 0..10 {
        let response =
            app.clone().oneshot_post_from_ip("10.6.6.6", "/api/auth/login/totp", body).await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    }

    let limited = app.clone().oneshot_post_from_ip("10.6.6.6", "/api/auth/login/totp", body).await;
    assert_eq!(limited.status(), StatusCode::TOO_MANY_REQUESTS);
    assert!(body_text(limited).await.contains("rate_limited"));
}

#[tokio::test]
async fn login_and_register_limits_are_independent() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let login_body = r#"{"identifier":"ghost@nebula.st","password":"Wrong!Passw0rd-2026"}"#;
    // 打满登录额度（5 次后第 6 次 429）
    for _ in 0..6 {
        let _ = app.clone().oneshot_post_from_ip("10.5.5.5", "/api/auth/login", login_body).await;
    }

    // 同 IP 的注册额度未被登录限额波及
    let register_body =
        r#"{"email":"fresh@nebula.st","username":"fresh","password":"Nebula!Passw0rd-2026"}"#;
    let response =
        app.clone().oneshot_post_from_ip("10.5.5.5", "/api/auth/register", register_body).await;
    assert_eq!(response.status(), StatusCode::CREATED);
}
