//! 基础设施集成测试：请求 ID、安全头、指标端点、请求体上限与 CORS。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::body::Body;
use axum::http::{HeaderValue, Request, StatusCode};
use common::OneshotPost;
use nebula_server::api;
use tower::ServiceExt;

#[tokio::test]
async fn healthz_carries_v7_request_id_and_security_headers() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response =
        app.oneshot(Request::get("/healthz").body(Body::empty()).unwrap()).await.unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let request_id =
        response.headers().get(api::REQUEST_ID_HEADER).expect("必须回显请求 ID").to_str().unwrap();
    let parsed = uuid::Uuid::parse_str(request_id).expect("请求 ID 必须是合法 uuid");
    assert_eq!(parsed.get_version_num(), 7);

    for (name, expected) in [
        ("x-content-type-options", "nosniff"),
        ("referrer-policy", "no-referrer"),
        ("x-frame-options", "DENY"),
    ] {
        assert_eq!(response.headers().get(name).unwrap().to_str().unwrap(), expected);
    }
    assert!(response.headers().get("content-security-policy").is_some());
    assert!(response.headers().get("permissions-policy").is_some());
}

#[tokio::test]
async fn metrics_returns_404_when_no_recorder_is_installed() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response =
        app.oneshot(Request::get("/metrics").body(Body::empty()).unwrap()).await.unwrap();

    // 测试装配不安装全局 recorder；线上由 main 安装，端点才有内容
    assert_eq!(response.status(), StatusCode::NOT_FOUND);
}

#[tokio::test]
async fn oversized_body_is_413_with_error_envelope() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let body = format!(
        r#"{{"email":"nova@nebula.st","username":"nova","password":"{}"}}"#,
        "a".repeat(api::MAX_BODY_BYTES + 1)
    );

    let response = app.oneshot_post("/api/auth/register", &body).await;

    assert_eq!(response.status(), StatusCode::PAYLOAD_TOO_LARGE);
    let json: serde_json::Value = serde_json::from_str(&common::body_text(response).await).unwrap();
    assert_eq!(json["code"], "payload_too_large");
}

#[tokio::test]
async fn cors_preflight_allows_configured_origin_with_credentials() {
    let (mut state, _clock, _mailbox, _dir) = common::app_state().await;
    state.cors_allowed_origin = Some(HeaderValue::from_static("http://localhost:3000"));
    let app = api::router(state);

    let request = Request::builder()
        .method("OPTIONS")
        .uri("/api/auth/login")
        .header("origin", "http://localhost:3000")
        .header("access-control-request-method", "POST")
        .body(Body::empty())
        .unwrap();
    let response = app.oneshot(request).await.unwrap();

    assert!(response.status().is_success());
    assert_eq!(
        response.headers().get("access-control-allow-origin").unwrap().to_str().unwrap(),
        "http://localhost:3000"
    );
    assert_eq!(
        response.headers().get("access-control-allow-credentials").unwrap().to_str().unwrap(),
        "true"
    );
}
