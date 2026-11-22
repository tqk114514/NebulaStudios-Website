//! Turnstile 客户端与本地 mock siteverify 的契约测试：表单编码、成功/失败码、
//! 非 2xx 与非法 JSON 的映射，不依赖外网。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::collections::HashMap;

use axum::Router;
use axum::extract::{Form, State};
use axum::http::StatusCode;
use axum::routing::post;
use nebula_server::turnstile::{TurnstileClient, TurnstileError, TurnstileVerifier};
use serde_json::json;
use tokio::net::TcpListener;

#[derive(Debug, Clone, Copy)]
enum Mode {
    Success,
    Failure,
    BadStatus,
    BadJson,
}

async fn verify(
    State(mode): State<Mode>,
    Form(form): Form<HashMap<String, String>>,
) -> (StatusCode, String) {
    assert_eq!(form.get("secret").map(String::as_str), Some("secret"));
    assert_eq!(form.get("response").map(String::as_str), Some("token"));
    match mode {
        Mode::Success => (StatusCode::OK, r#"{"success":true}"#.to_owned()),
        Mode::Failure => (
            StatusCode::OK,
            r#"{"success":false,"error-codes":["invalid-input-response"]}"#.to_owned(),
        ),
        Mode::BadStatus => (StatusCode::BAD_GATEWAY, "upstream down".to_owned()),
        Mode::BadJson => (StatusCode::OK, "not-json".to_owned()),
    }
}

async fn mock_siteverify(mode: Mode) -> String {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("mock 端口必须可监听");
    let addr = listener.local_addr().expect("本地地址必须可读");
    let app = Router::new().route("/verify", post(verify)).with_state(mode);
    tokio::spawn(async move {
        axum::serve(listener, app).await.expect("mock 服务不得退出");
    });
    format!("http://{addr}/verify")
}

#[tokio::test]
async fn success_response_is_reported() {
    let url = mock_siteverify(Mode::Success).await;
    let client = TurnstileClient::new(url, "secret").expect("客户端必须可构建");

    let outcome = client.verify("token").await.expect("请求必须成功");

    assert!(outcome.success);
    assert_eq!(outcome.error_codes.len(), 0);
}

#[tokio::test]
async fn failure_response_keeps_error_codes() {
    let url = mock_siteverify(Mode::Failure).await;
    let client = TurnstileClient::new(url, "secret").expect("客户端必须可构建");

    let outcome = client.verify("token").await.expect("请求必须成功");

    assert!(!outcome.success);
    assert_eq!(outcome.error_codes, vec!["invalid-input-response".to_owned()]);
}

#[tokio::test]
async fn non_2xx_is_a_transport_error() {
    let url = mock_siteverify(Mode::BadStatus).await;
    let client = TurnstileClient::new(url, "secret").expect("客户端必须可构建");

    let error = client.verify("token").await.expect_err("非 2xx 必须失败");

    assert!(matches!(error, TurnstileError::Status(status) if status.as_u16() == 502));
}

#[tokio::test]
async fn invalid_json_is_a_transport_error() {
    let url = mock_siteverify(Mode::BadJson).await;
    let client = TurnstileClient::new(url, "secret").expect("客户端必须可构建");

    let error = client.verify("token").await.expect_err("非法 JSON 必须失败");

    assert!(matches!(error, TurnstileError::Request(_)));
}

#[test]
fn debug_redacts_secret() {
    let client =
        TurnstileClient::new("http://example/verify", "super-secret").expect("客户端必须可构建");

    let debug = format!("{client:?}");

    assert!(debug.contains("verify_url"));
    assert!(debug.contains("<redacted>"));
    assert!(!debug.contains("super-secret"));
}

#[tokio::test]
async fn network_failure_is_a_transport_error() {
    let client =
        TurnstileClient::new("http://127.0.0.1:1/verify", "secret").expect("客户端必须可构建");

    assert!(matches!(client.verify("token").await, Err(TurnstileError::Request(_))));
}

#[tokio::test]
async fn empty_token_is_rejected_without_http() {
    let client =
        TurnstileClient::new("http://127.0.0.1:1/verify", "secret").expect("客户端必须可构建");

    let outcome = client.verify("").await.expect("空 token 不触发网络错误");

    assert!(!outcome.success);
    assert_eq!(outcome.error_codes, vec!["empty-token".to_owned()]);
}

#[tokio::test]
async fn response_shape_matches_siteverify_contract() {
    let url = mock_siteverify(Mode::Success).await;
    let client = TurnstileClient::new(url, "secret").expect("客户端必须可构建");

    let outcome = client.verify("token").await.expect("请求必须成功");
    assert_eq!(
        serde_json::to_value(json!({
            "success": outcome.success,
            "error_codes": outcome.error_codes,
        }))
        .expect("JSON 必须可序列化"),
        json!({ "success": true, "error_codes": [] })
    );
}
