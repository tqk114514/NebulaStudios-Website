//! 未登录态密码重置集成测试：全链、防枚举、冷却、旧会话全灭（红线 #1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{
    Mailbox, OneshotPost, await_first_mail, body_text, cookie_header, extract_code, login_session,
    raw_set_cookie, register_user,
};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";
const NEW_PASSWORD: &str = "Reborn!Passw0rd-2026";

async fn request_reset(app: &axum::Router, email: &str) -> axum::response::Response {
    let body = format!(r#"{{"email":"{email}"}}"#);
    app.clone().oneshot_post("/api/auth/password/reset/request", &body).await
}

async fn confirm_reset(app: &axum::Router, code: &str, password: &str) -> axum::response::Response {
    confirm_reset_from(app, "127.0.0.1", code, password).await
}

/// 以指定源 IP 发起——confirm 与 request 共享 3/20s 的 IP 限额，
/// 连续多次的用例须换 IP 以免撞限额层而非被测逻辑。
async fn confirm_reset_from(
    app: &axum::Router,
    ip: &str,
    code: &str,
    password: &str,
) -> axum::response::Response {
    let body = format!(r#"{{"email":"{EMAIL}","code":"{code}","new_password":"{password}"}}"#);
    app.clone().oneshot_post_from_ip(ip, "/api/auth/password/reset/confirm", &body).await
}

async fn wait_second_mail(mailbox: &Mailbox) -> common::EmailMessage {
    for _ in 0..200 {
        let messages = mailbox.messages();
        if messages.len() >= 2 {
            return messages[1].clone();
        }
        tokio::time::sleep(std::time::Duration::from_millis(5)).await;
    }
    panic!("第二封邮件未在预期时间内发出");
}

async fn verified_and_logged_in(app: &axum::Router, mailbox: &Mailbox) -> common::Session {
    register_user(app, EMAIL, USERNAME, PASSWORD).await;
    let code = extract_code(&await_first_mail(mailbox).await);
    let verified = app
        .clone()
        .oneshot_post(
            "/api/auth/register/verify",
            &format!(r#"{{"email":"{EMAIL}","code":"{code}"}}"#),
        )
        .await;
    assert_eq!(verified.status(), StatusCode::NO_CONTENT);
    login_session(app).await
}

#[tokio::test]
async fn full_reset_flow_rotates_password_and_kills_sessions() {
    let (app, state, clock, mailbox, _dir) = common::app().await;
    let session = verified_and_logged_in(&app, &mailbox).await;

    // 发起重置 → 204 收码
    let requested = request_reset(&app, EMAIL).await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);
    let mail = wait_second_mail(&mailbox).await;
    assert_eq!(mail.subject, "Nebula 密码重置验证码");
    let code = extract_code(&mail);

    // 完成 → 204
    let confirmed = confirm_reset(&app, &code, NEW_PASSWORD).await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT);

    // 红线 #1：旧会话全灭
    let old_refresh = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(old_refresh.status(), StatusCode::UNAUTHORIZED);

    // 新密码可登录，旧密码不可
    let old_login = {
        let body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(old_login.status(), StatusCode::UNAUTHORIZED);
    let new_login = {
        let body = format!(r#"{{"identifier":"{EMAIL}","password":"{NEW_PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(new_login.status(), StatusCode::OK);
    let _ = (state, clock);
}

#[tokio::test]
async fn unknown_email_is_204_without_mail() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;

    let response = request_reset(&app, "ghost@nebula.st").await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    assert!(mailbox.messages().is_empty(), "未知邮箱不得发信");
}

#[tokio::test]
async fn confirm_treats_unknown_email_like_wrong_code() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let _ = await_first_mail(&mailbox).await;

    let unknown = {
        let body =
            r#"{"email":"ghost@nebula.st","code":"AAAAAA","new_password":"Nebula!Passw0rd-2026"}"#;
        let response = app.clone().oneshot_post("/api/auth/password/reset/confirm", body).await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
        body_text(response).await
    };
    let wrong_code = {
        let body =
            format!(r#"{{"email":"{EMAIL}","code":"AAAAAA","new_password":"{NEW_PASSWORD}"}}"#);
        let response = app.clone().oneshot_post("/api/auth/password/reset/confirm", &body).await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
        body_text(response).await
    };
    // 防枚举：逐字节同体
    assert_eq!(unknown, wrong_code);
    assert!(unknown.contains("invalid_code"));
}

#[tokio::test]
async fn reset_request_is_cooling_down() {
    let (app, _state, clock, mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let _ = await_first_mail(&mailbox).await;

    let first = request_reset(&app, EMAIL).await;
    assert_eq!(first.status(), StatusCode::NO_CONTENT);

    let second = request_reset(&app, EMAIL).await;
    assert_eq!(second.status(), StatusCode::TOO_MANY_REQUESTS);
    let json: serde_json::Value = serde_json::from_str(&body_text(second).await).unwrap();
    assert_eq!(json["code"], "resend_cooldown");
    assert!(json["details"]["retry_after"].as_u64().is_some());

    // 冷却结束后成功
    clock.advance(chrono::TimeDelta::seconds(61));
    let third = request_reset(&app, EMAIL).await;
    assert_eq!(third.status(), StatusCode::NO_CONTENT);
}

#[tokio::test]
async fn weak_new_password_is_422() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let _ = await_first_mail(&mailbox).await;
    let _first = request_reset(&app, EMAIL).await;
    let code = extract_code(&wait_second_mail(&mailbox).await);

    let response = confirm_reset(&app, &code, "short").await;

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(response).await.contains("password_too_short"));
}

#[tokio::test]
async fn used_code_cannot_reset_twice() {
    let (app, _state, clock, mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let _ = await_first_mail(&mailbox).await;
    let _first = request_reset(&app, EMAIL).await;
    let code = extract_code(&wait_second_mail(&mailbox).await);

    let first = confirm_reset(&app, &code, NEW_PASSWORD).await;
    assert_eq!(first.status(), StatusCode::NO_CONTENT);

    clock.advance(chrono::TimeDelta::seconds(61));
    let _again = request_reset(&app, EMAIL).await;
    // 此时信箱已有三封（注册、首次重置、本次重置），取最新一封
    let mut third_mail = None;
    for _ in 0..200 {
        let messages = mailbox.messages();
        if messages.len() >= 3 {
            third_mail = Some(messages[2].clone());
            break;
        }
        tokio::time::sleep(std::time::Duration::from_millis(5)).await;
    }
    let second_code = extract_code(&third_mail.expect("第三封邮件必须发出"));

    // 已消费的码再试无效（即使新码已发；换源 IP 绕开端点自身的 IP 限额）
    let replay = confirm_reset_from(&app, "10.9.9.1", &code, "Another!Passw0rd-2026").await;
    assert_eq!(replay.status(), StatusCode::UNAUTHORIZED);
    let fresh = confirm_reset_from(&app, "10.9.9.2", &second_code, "Another!Passw0rd-2026").await;
    assert_eq!(fresh.status(), StatusCode::NO_CONTENT);
    let _ = raw_set_cookie(&fresh, "nebula_access");
}
