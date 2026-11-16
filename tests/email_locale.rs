//! 邮件语言集成测试：注册验证码与安全通知按 Accept-Language 渲染，未知语言回落中文。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{OneshotPost, body_text, cookie_header, login_session};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

fn register_payload() -> String {
    format!(r#"{{"email":"{EMAIL}","username":"{USERNAME}","password":"{PASSWORD}"}}"#)
}

#[tokio::test]
async fn register_email_follows_accept_language() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;

    let response = app
        .clone()
        .oneshot_post_with_headers(
            "/api/auth/register",
            &register_payload(),
            "",
            None,
            &[("accept-language", "en-US,en;q=0.9")],
        )
        .await;
    assert_eq!(response.status(), StatusCode::CREATED);

    let mail = common::await_first_mail(&mailbox).await;
    assert_eq!(mail.subject, "Nebula email verification code");
    assert!(mail.body.contains("Your code:"));
    assert!(!mail.body.contains('：'));
}

#[tokio::test]
async fn unsupported_language_falls_back_to_chinese() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;

    let response = app
        .clone()
        .oneshot_post_with_headers(
            "/api/auth/register",
            &register_payload(),
            "",
            None,
            &[("accept-language", "fr-FR,fr;q=0.9")],
        )
        .await;
    assert_eq!(response.status(), StatusCode::CREATED);

    let mail = common::await_first_mail(&mailbox).await;
    assert_eq!(mail.subject, "Nebula 邮箱验证码");
    assert!(mail.body.contains("验证码："));
}

#[tokio::test]
async fn security_notice_follows_accept_language() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    common::register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with_headers(
            "/api/auth/password/change",
            &format!(r#"{{"current_password":"{PASSWORD}","new_password":"Fresh!Passw0rd-2026"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
            &[("accept-language", "en-GB")],
        )
        .await;
    assert_eq!(response.status(), StatusCode::NO_CONTENT);

    let mail =
        common::await_mail_subject(&mailbox, "Nebula security notice: password changed").await;
    assert!(mail.body.contains("password was just changed"));
    assert!(!mail.body.contains("口令"));
}

#[tokio::test]
async fn registration_validation_errors_are_unchanged_by_locale() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app
        .clone()
        .oneshot_post_with_headers(
            "/api/auth/register",
            r#"{"email":"not-an-email","username":"nova","password":"short"}"#,
            "",
            None,
            &[("accept-language", "en-US")],
        )
        .await;

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(response).await.contains("invalid_email"));
}
