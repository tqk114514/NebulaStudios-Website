//! 当前用户资料端点测试：登录态读取、未认证拒绝、验证状态透传。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{OneshotPost, body_text, cookie_header, login_session, register_user};

#[tokio::test]
async fn get_current_user_returns_profile_without_credentials() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026").await;
    let session = login_session(&app).await;

    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    let response = app.clone().oneshot_get_with_cookie("/api/user", "", &cookie).await;

    assert_eq!(response.status(), StatusCode::OK);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["username"], "nova");
    assert_eq!(json["email"], "nova@nebula.st");
    assert_eq!(json["email_verified"], false);
    assert!(json.get("password_hash").is_none());
}

#[tokio::test]
async fn get_current_user_requires_authentication() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app.clone().oneshot_get_with_cookie("/api/user", "", "").await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert!(body_text(response).await.contains("unauthorized"));
}

#[tokio::test]
async fn get_current_user_reflects_verified_email() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    common::register_verified_user(
        &app,
        "nova@nebula.st",
        "nova",
        "Nebula!Passw0rd-2026",
        &mailbox,
    )
    .await;
    let session = login_session(&app).await;

    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    let response = app.clone().oneshot_get_with_cookie("/api/user", "", &cookie).await;

    assert_eq!(response.status(), StatusCode::OK);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["email_verified"], true);
}
