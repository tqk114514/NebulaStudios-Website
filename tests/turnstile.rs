//! Turnstile 集成测试：启用时注册/重发/重置发码必须携带有效 token，未启用直接放行。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::sync::Arc;

use axum::http::StatusCode;
use chrono::TimeDelta;
use common::{OneshotPost, body_text};
use nebula_server::api::{self, AppState};
use nebula_server::context::ManualClock;
use nebula_server::turnstile::{TurnstileError, TurnstileOutcome, TurnstileVerifier};

const PASSWORD: &str = "Nebula!Passw0rd-2026";
const GOOD_TOKEN: &str = "good-token";

struct FakeTurnstile {
    accept: bool,
    unavailable: bool,
}

#[async_trait::async_trait]
impl TurnstileVerifier for FakeTurnstile {
    async fn verify(&self, token: &str) -> Result<TurnstileOutcome, TurnstileError> {
        if self.unavailable {
            return Err(TurnstileError::Status(reqwest::StatusCode::BAD_GATEWAY));
        }
        Ok(TurnstileOutcome {
            success: self.accept && token == GOOD_TOKEN,
            error_codes: Vec::new(),
        })
    }
}

async fn app_with_turnstile(
    accept: bool,
    unavailable: bool,
) -> (axum::Router, AppState, Arc<ManualClock>, tempfile::TempDir) {
    let (mut state, clock, _mailbox, dir) = common::app_state().await;
    state.turnstile = Some(Arc::new(FakeTurnstile { accept, unavailable }));
    state.turnstile_site_key = Some("test-site-key".to_owned());
    (api::router(state.clone()), state, clock, dir)
}

fn register_payload(token: Option<&str>) -> String {
    let token = token.map_or_else(String::new, |token| format!(r#","turnstile_token":"{token}""#));
    format!(r#"{{"email":"nova@nebula.st","username":"nova","password":"{PASSWORD}"{token}}}"#)
}

#[tokio::test]
async fn register_requires_valid_turnstile_when_enabled() {
    let (app, state, _clock, _dir) = app_with_turnstile(true, false).await;

    let missing = app.clone().oneshot_post("/api/auth/register", &register_payload(None)).await;
    assert_eq!(missing.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(missing).await.contains("turnstile_invalid"));

    let wrong =
        app.clone().oneshot_post("/api/auth/register", &register_payload(Some("wrong"))).await;
    assert_eq!(wrong.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(wrong).await.contains("turnstile_invalid"));
    assert!(state.user_repo.find_by_email("nova@nebula.st").await.expect("查询必须成功").is_none());

    let accepted =
        app.clone().oneshot_post("/api/auth/register", &register_payload(Some(GOOD_TOKEN))).await;
    assert_eq!(accepted.status(), StatusCode::CREATED);
}

#[tokio::test]
async fn resend_and_reset_request_require_turnstile() {
    let (app, _state, clock, _dir) = app_with_turnstile(true, false).await;

    let registered =
        app.clone().oneshot_post("/api/auth/register", &register_payload(Some(GOOD_TOKEN))).await;
    assert_eq!(registered.status(), StatusCode::CREATED);
    // 注册邮件已发出，重发需要越过冷却
    clock.advance(TimeDelta::seconds(61));

    let resend_missing = app
        .clone()
        .oneshot_post("/api/auth/register/resend", r#"{"email":"nova@nebula.st"}"#)
        .await;
    assert_eq!(resend_missing.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(resend_missing).await.contains("turnstile_invalid"));

    let resend_ok = app
        .clone()
        .oneshot_post(
            "/api/auth/register/resend",
            &format!(r#"{{"email":"nova@nebula.st","turnstile_token":"{GOOD_TOKEN}"}}"#),
        )
        .await;
    assert_eq!(resend_ok.status(), StatusCode::NO_CONTENT);

    let reset_missing = app
        .clone()
        .oneshot_post("/api/auth/password/reset/request", r#"{"email":"nova@nebula.st"}"#)
        .await;
    assert_eq!(reset_missing.status(), StatusCode::UNPROCESSABLE_ENTITY);

    let reset_ok = app
        .clone()
        .oneshot_post(
            "/api/auth/password/reset/request",
            &format!(r#"{{"email":"nova@nebula.st","turnstile_token":"{GOOD_TOKEN}"}}"#),
        )
        .await;
    assert_eq!(reset_ok.status(), StatusCode::NO_CONTENT);
}

#[tokio::test]
async fn settings_endpoint_exposes_site_key_only_when_enabled() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let disabled = app.clone().oneshot_get_with_cookie("/api/auth/turnstile", "", "").await;
    let disabled: serde_json::Value =
        serde_json::from_str(&body_text(disabled).await).expect("配置响应必须是 JSON");
    assert_eq!(disabled["enabled"], false);
    assert!(disabled.get("site_key").is_none());

    let (app, _state, _clock, _dir) = app_with_turnstile(true, false).await;
    let enabled = app.clone().oneshot_get_with_cookie("/api/auth/turnstile", "", "").await;
    let enabled: serde_json::Value =
        serde_json::from_str(&body_text(enabled).await).expect("配置响应必须是 JSON");
    assert_eq!(enabled["enabled"], true);
    assert_eq!(enabled["site_key"], "test-site-key");
}

#[tokio::test]
async fn verifier_outage_maps_to_503() {
    let (app, _state, _clock, _dir) = app_with_turnstile(true, true).await;

    let response =
        app.clone().oneshot_post("/api/auth/register", &register_payload(Some(GOOD_TOKEN))).await;

    assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
    assert!(body_text(response).await.contains("turnstile_unavailable"));
}
