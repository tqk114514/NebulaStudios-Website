//! 用户侧审计覆盖：注册、登录、登出写入同一张 `audit_logs`。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{OneshotPost, cookie_header, login_session, register_user};

#[tokio::test]
async fn auth_lifecycle_events_are_audited() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026").await;
    let session = login_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(response.status(), StatusCode::NO_CONTENT);

    let entries = state.admin_repo.list_audit(50, 0).await.expect("审计列表必须可读");
    let actions: Vec<&str> = entries.iter().map(|entry| entry.action.as_str()).collect();
    assert!(actions.contains(&"auth.register"));
    assert!(actions.contains(&"auth.login"));
    assert!(actions.contains(&"auth.logout"));

    let login =
        entries.iter().find(|entry| entry.action == "auth.login").expect("登录事件必须存在");
    assert_eq!(
        login.details.as_ref().and_then(|details| details["method"].as_str()),
        Some("password")
    );
}
