//! 注册端点集成测试：走真实 SQLite 路由（ADR-0004 响应契约 + §8.5 快照）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use nebula_server::model::password::PasswordHash;
use tower::ServiceExt;

const VALID_BODY: &str =
    r#"{"email":"Nova@Nebula.st","username":"nova","password":"Nebula!Passw0rd-2026"}"#;

fn post_register(body: &str) -> Request<Body> {
    let mut request = Request::post("/api/auth/register")
        .header("content-type", "application/json")
        .body(Body::from(body.to_owned()))
        .unwrap();
    // 注册路由挂了 IP 限额层，缺 ConnectInfo 会 500（同 tests/common 的注入理由）
    let addr: std::net::SocketAddr = "127.0.0.1:40000".parse().unwrap();
    request.extensions_mut().insert(axum::extract::ConnectInfo(addr));
    request
}

async fn body_text(response: axum::response::Response) -> String {
    let bytes = response.into_body().collect().await.unwrap().to_bytes();
    String::from_utf8_lossy(&bytes).into_owned()
}

#[tokio::test]
async fn register_creates_unverified_user_with_normalized_email() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app.oneshot(post_register(VALID_BODY)).await.unwrap();

    assert_eq!(response.status(), StatusCode::CREATED);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["email"], "nova@nebula.st");
    assert_eq!(json["username"], "nova");
    assert_eq!(json["email_verified"], false);
    // created_at 来自固定时钟（ManualClock），前缀断言不绑定时区后缀的写法
    assert!(
        json["created_at"].as_str().unwrap().starts_with("2026-10-26T08:00:00"),
        "created_at 应取自注入时钟: {}",
        json["created_at"]
    );
}

#[tokio::test]
async fn register_persists_verifiable_password_hash() {
    let (app, _state, _clock, _mailbox, dir) = common::app().await;
    let pool = common::migrated_pool(dir.path()).await;

    app.oneshot(post_register(VALID_BODY)).await.unwrap();

    // 测试脚手架用运行时查询（query_as），不进 .sqlx 编译期缓存
    let (hash,): (Option<String>,) =
        sqlx::query_as("SELECT password_hash FROM users WHERE email = 'nova@nebula.st'")
            .fetch_one(&pool)
            .await
            .expect("注册后必须能查到该行");
    let hash = PasswordHash::from_encoded(hash.expect("注册必须落库口令哈希")).unwrap();
    assert!(hash.verify("Nebula!Passw0rd-2026").unwrap());
    assert!(!hash.verify("Wrong!Passw0rd-2026").unwrap());
}

#[tokio::test]
async fn malformed_json_is_400_with_envelope() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app.oneshot(post_register(r#"{"email": broken"#)).await.unwrap();

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["code"], "malformed_json");
}

#[tokio::test]
async fn invalid_email_is_422_with_snapshot() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app
        .oneshot(post_register(
            r#"{"email":"not-an-email","username":"nova","password":"Nebula!Passw0rd-2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    insta::assert_snapshot!(body_text(response).await);
}

#[tokio::test]
async fn weak_password_reports_missing_class_codes() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    // 长度够但缺大写与标点
    let response = app
        .oneshot(post_register(
            r#"{"email":"nova@nebula.st","username":"nova","password":"nebulapassw0rd2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    insta::assert_snapshot!(body_text(response).await);
}

#[tokio::test]
async fn short_password_is_422_with_snapshot() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app
        .oneshot(post_register(
            r#"{"email":"nova@nebula.st","username":"nova","password":"Sh0rt!"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    insta::assert_snapshot!(body_text(response).await);
}

#[tokio::test]
async fn duplicate_email_is_409_email_taken() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    app.clone().oneshot(post_register(VALID_BODY)).await.unwrap();

    let response = app
        .oneshot(post_register(
            r#"{"email":"nova@nebula.st","username":"other","password":"Nebula!Passw0rd-2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::CONFLICT);
    insta::assert_snapshot!(body_text(response).await);
}

#[tokio::test]
async fn duplicate_username_is_409_username_taken() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    app.clone().oneshot(post_register(VALID_BODY)).await.unwrap();

    let response = app
        .oneshot(post_register(
            r#"{"email":"other@nebula.st","username":"NOVA","password":"Nebula!Passw0rd-2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::CONFLICT);
    insta::assert_snapshot!(body_text(response).await);
}

#[tokio::test]
async fn username_with_email_chars_is_422() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app
        .oneshot(post_register(
            r#"{"email":"nova@nebula.st","username":"nova@nebula.st","password":"Nebula!Passw0rd-2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["details"]["fields"][0]["code"], "username_invalid_characters");
}

#[tokio::test]
async fn one_character_username_is_422() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app
        .oneshot(post_register(
            r#"{"email":"nova@nebula.st","username":"n","password":"Nebula!Passw0rd-2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["details"]["fields"][0]["code"], "username_too_short");
}

#[tokio::test]
async fn overlong_username_is_422() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    // 16 个字符，超出 v3 上限 15
    let response = app
        .oneshot(post_register(
            r#"{"email":"nova@nebula.st","username":"aaaaaaaaaaaaaaaa","password":"Nebula!Passw0rd-2026"}"#,
        ))
        .await
        .unwrap();

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["details"]["fields"][0]["code"], "username_too_long");
}
