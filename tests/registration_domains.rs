//! 注册邮箱域名白名单集成测试：精确匹配、大小写归一与子域不自动放行。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::sync::Arc;

use axum::http::StatusCode;
use common::{OneshotPost, body_text};
use nebula_server::api;

const PASSWORD: &str = "Nebula!Passw0rd-2026";

fn payload(email: &str, username: &str) -> String {
    format!(r#"{{"email":"{email}","username":"{username}","password":"{PASSWORD}"}}"#)
}

async fn app_with_domain(
    domain: &str,
) -> (axum::Router, nebula_server::api::AppState, tempfile::TempDir) {
    let (mut state, _clock, _mailbox, dir) = common::app_state().await;
    state.allowed_email_domains = Arc::new(HashSet::from([domain.to_owned()]));
    (api::router(state.clone()), state, dir)
}

#[tokio::test]
async fn allowlisted_domain_registers_case_insensitively() {
    let (app, _state, _dir) = app_with_domain("nebula.st").await;

    let response =
        app.clone().oneshot_post("/api/auth/register", &payload("nova@nebula.st", "nova")).await;
    assert_eq!(response.status(), StatusCode::CREATED);

    // 归一化后与白名单小写值一致
    let upper =
        app.clone().oneshot_post("/api/auth/register", &payload("other@NEBULA.ST", "other")).await;
    assert_eq!(upper.status(), StatusCode::CREATED);
}

#[tokio::test]
async fn other_domain_and_subdomain_are_rejected() {
    let (app, state, _dir) = app_with_domain("nebula.st").await;

    for (email, username) in [("nova@other.com", "nova"), ("nova@mail.nebula.st", "nova-sub")] {
        let response =
            app.clone().oneshot_post("/api/auth/register", &payload(email, username)).await;
        assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
        assert!(body_text(response).await.contains("email_domain_not_allowed"));
    }

    assert!(state.user_repo.find_by_email("nova@other.com").await.expect("查询必须成功").is_none());
    assert!(
        state.user_repo.find_by_email("nova@mail.nebula.st").await.expect("查询必须成功").is_none()
    );
}
