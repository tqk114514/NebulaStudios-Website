//! HTTP 层集成测试：`tower::ServiceExt::oneshot` 直接打 Router，不绑端口（AGENTS.md §8）。

// 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use tower::ServiceExt;

#[tokio::test]
async fn healthz_responds_ok_with_empty_body() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response =
        app.oneshot(Request::get("/healthz").body(Body::empty()).unwrap()).await.unwrap();

    assert_eq!(response.status(), StatusCode::OK);
    let body = response.into_body().collect().await.unwrap().to_bytes();
    assert!(body.is_empty());
}
