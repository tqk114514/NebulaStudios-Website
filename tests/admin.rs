//! 管理后台集成测试：配置化管理员、封禁拦截、会话撤销、临时封禁与审计。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::sync::Arc;

use axum::Router;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use chrono::TimeDelta;
use common::{
    OneshotPost, Session, body_text, cookie_header, login_as, register_user, register_verified_user,
};
use nebula_server::api;
use nebula_server::context::{Clock, ManualClock};
use nebula_server::model::user::UserId;
use tempfile::TempDir;
use tower::ServiceExt;

const PASSWORD: &str = "Nebula!Passw0rd-2026";

struct Fixture {
    app: Router,
    clock: Arc<ManualClock>,
    admin: Session,
    admin_id: UserId,
    target: UserId,
    _dir: TempDir,
}

async fn fixture() -> Fixture {
    let (mut state, clock, mailbox, dir) = common::app_state().await;
    state.admin_emails = Arc::new(HashSet::from(["admin@nebula.st".to_owned()]));
    let app = api::router(state.clone());
    // 管理员必须邮箱已验证；未验证邮箱不得因配置命中而取得管理权限
    register_verified_user(&app, "admin@nebula.st", "admin", PASSWORD, &mailbox).await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let admin = login_as(&app, "admin@nebula.st", PASSWORD).await;
    let admin_id = state.user_repo.find_by_email("admin@nebula.st").await.unwrap().unwrap().id;
    let target = state.user_repo.find_by_email("nova@nebula.st").await.unwrap().unwrap().id;
    Fixture { app, clock, admin, admin_id, target, _dir: dir }
}

fn admin_cookie(fixture: &Fixture) -> String {
    cookie_header(Some(&fixture.admin.access), None, Some(&fixture.admin.csrf))
}

async fn admin_get(fixture: &Fixture, uri: &str, query: &str) -> axum::response::Response {
    fixture.app.clone().oneshot_get_with_cookie(uri, query, &admin_cookie(fixture)).await
}

async fn admin_ban(fixture: &Fixture, user_id: UserId, body: &str) -> axum::response::Response {
    fixture
        .app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{user_id}/ban"),
            body,
            &admin_cookie(fixture),
            Some(&fixture.admin.csrf),
        )
        .await
}

async fn admin_unban(fixture: &Fixture) -> axum::response::Response {
    let request = Request::delete(format!("/api/admin/users/{}/ban", fixture.target))
        .header("cookie", admin_cookie(fixture))
        .header("x-csrf-token", &fixture.admin.csrf)
        .body(Body::empty())
        .unwrap();
    fixture.app.clone().oneshot(request).await.unwrap()
}

async fn setup_totp(fixture: &Fixture, session: &Session) -> axum::response::Response {
    fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/setup",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

#[tokio::test]
async fn non_admin_is_forbidden_on_admin_endpoints() {
    let fixture = fixture().await;
    let normal = login_as(&fixture.app, "nova@nebula.st", PASSWORD).await;

    let response = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/admin/stats",
            "",
            &cookie_header(Some(&normal.access), None, Some(&normal.csrf)),
        )
        .await;

    assert_eq!(response.status(), StatusCode::FORBIDDEN);
    assert!(body_text(response).await.contains("forbidden"));
}

#[tokio::test]
async fn unverified_admin_email_is_forbidden() {
    let (mut state, _clock, _mailbox, _dir) = common::app_state().await;
    state.admin_emails = Arc::new(HashSet::from(["admin@nebula.st".to_owned()]));
    let app = api::router(state);
    register_user(&app, "admin@nebula.st", "admin", PASSWORD).await;
    let admin = login_as(&app, "admin@nebula.st", PASSWORD).await;

    let response = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/admin/stats",
            "",
            &cookie_header(Some(&admin.access), None, Some(&admin.csrf)),
        )
        .await;

    assert_eq!(response.status(), StatusCode::FORBIDDEN);
}

#[tokio::test]
async fn admin_reads_stats_and_user_list() {
    let fixture = fixture().await;

    let stats = admin_get(&fixture, "/api/admin/stats", "").await;
    assert_eq!(stats.status(), StatusCode::OK);
    let stats_json: serde_json::Value = serde_json::from_str(&body_text(stats).await).unwrap();
    assert_eq!(stats_json["total_users"], 2);
    assert_eq!(stats_json["banned_users"], 0);

    let users = admin_get(&fixture, "/api/admin/users", "query=nova").await;
    assert_eq!(users.status(), StatusCode::OK);
    let users_json: serde_json::Value = serde_json::from_str(&body_text(users).await).unwrap();
    assert_eq!(users_json["total"], 1);
    assert_eq!(users_json["users"][0]["email"], "nova@nebula.st");
    assert_eq!(users_json["users"][0]["is_admin"], false);
    assert!(users_json["users"][0]["ban"].is_null());
}

#[tokio::test]
async fn ban_blocks_first_party_revokes_refresh_and_writes_audit() {
    let fixture = fixture().await;
    let normal = login_as(&fixture.app, "nova@nebula.st", PASSWORD).await;

    let banned =
        admin_ban(&fixture, fixture.target, r#"{"kind":"permanent","reason":"abuse"}"#).await;
    assert_eq!(banned.status(), StatusCode::NO_CONTENT);

    let blocked = setup_totp(&fixture, &normal).await;
    assert_eq!(blocked.status(), StatusCode::FORBIDDEN);
    assert!(body_text(blocked).await.contains("banned"));

    let refresh = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&normal.refresh), Some(&normal.csrf)),
            Some(&normal.csrf),
        )
        .await;
    assert_eq!(refresh.status(), StatusCode::UNAUTHORIZED);

    let audit = admin_get(&fixture, "/api/admin/audit", "").await;
    assert_eq!(audit.status(), StatusCode::OK);
    let audit_json: serde_json::Value = serde_json::from_str(&body_text(audit).await).unwrap();
    assert_eq!(audit_json["entries"][0]["action"], "admin.user.ban");
    assert_eq!(audit_json["entries"][0]["details"]["reason"], "abuse");
}

#[tokio::test]
async fn banned_user_cannot_login() {
    let fixture = fixture().await;
    assert_eq!(
        admin_ban(&fixture, fixture.target, r#"{"kind":"permanent","reason":"abuse"}"#)
            .await
            .status(),
        StatusCode::NO_CONTENT
    );

    let body =
        serde_json::json!({ "identifier": "nova@nebula.st", "password": PASSWORD }).to_string();
    let response = fixture.app.clone().oneshot_post("/api/auth/login", &body).await;

    assert_eq!(response.status(), StatusCode::FORBIDDEN);
    assert!(body_text(response).await.contains("banned"));
}

#[tokio::test]
async fn unban_restores_access_and_writes_audit() {
    let fixture = fixture().await;
    let normal = login_as(&fixture.app, "nova@nebula.st", PASSWORD).await;

    assert_eq!(
        admin_ban(&fixture, fixture.target, r#"{"kind":"permanent","reason":"abuse"}"#)
            .await
            .status(),
        StatusCode::NO_CONTENT
    );
    assert_eq!(setup_totp(&fixture, &normal).await.status(), StatusCode::FORBIDDEN);

    assert_eq!(admin_unban(&fixture).await.status(), StatusCode::NO_CONTENT);
    assert_eq!(setup_totp(&fixture, &normal).await.status(), StatusCode::OK);

    let audit = admin_get(&fixture, "/api/admin/audit", "").await;
    let audit_json: serde_json::Value = serde_json::from_str(&body_text(audit).await).unwrap();
    let actions: Vec<&str> = audit_json["entries"]
        .as_array()
        .unwrap()
        .iter()
        .map(|e| e["action"].as_str().unwrap())
        .collect();
    assert!(actions.contains(&"admin.user.unban"));
}

#[tokio::test]
async fn temporary_ban_expires_and_stops_counting() {
    let mut fixture = fixture().await;
    let normal = login_as(&fixture.app, "nova@nebula.st", PASSWORD).await;
    let expires_at = (fixture.clock.now() + TimeDelta::hours(1)).to_rfc3339();
    let body =
        format!(r#"{{"kind":"temporary","expires_at":"{expires_at}","reason":"cool down"}}"#);

    assert_eq!(admin_ban(&fixture, fixture.target, &body).await.status(), StatusCode::NO_CONTENT);
    assert_eq!(setup_totp(&fixture, &normal).await.status(), StatusCode::FORBIDDEN);

    // 原 access token 已过期，重新登录拿新会话；封禁行仍在，但解码时已过期
    fixture.clock.advance(TimeDelta::hours(2));
    let fresh = login_as(&fixture.app, "nova@nebula.st", PASSWORD).await;
    assert_eq!(setup_totp(&fixture, &fresh).await.status(), StatusCode::OK);

    // admin 的 access token 同样已过期，读统计前重新登录
    fixture.admin = login_as(&fixture.app, "admin@nebula.st", PASSWORD).await;
    let stats = admin_get(&fixture, "/api/admin/stats", "").await;
    let stats_json: serde_json::Value = serde_json::from_str(&body_text(stats).await).unwrap();
    assert_eq!(stats_json["banned_users"], 0);
}

#[tokio::test]
async fn admin_cannot_ban_another_admin() {
    let (mut state, _clock, mailbox, _dir) = common::app_state().await;
    state.admin_emails =
        Arc::new(HashSet::from(["admin@nebula.st".to_owned(), "other-admin@nebula.st".to_owned()]));
    let app = api::router(state.clone());
    register_verified_user(&app, "admin@nebula.st", "admin", PASSWORD, &mailbox).await;
    register_verified_user(&app, "other-admin@nebula.st", "otheradmin", PASSWORD, &mailbox).await;
    let admin = login_as(&app, "admin@nebula.st", PASSWORD).await;
    let other = state.user_repo.find_by_email("other-admin@nebula.st").await.unwrap().unwrap().id;

    let response = app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{other}/ban"),
            r#"{"kind":"permanent","reason":"no"}"#,
            &cookie_header(Some(&admin.access), None, Some(&admin.csrf)),
            Some(&admin.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::CONFLICT);
    assert!(body_text(response).await.contains("cannot_ban_admin"));
}

#[tokio::test]
async fn admin_cannot_ban_self() {
    let fixture = fixture().await;

    let response =
        admin_ban(&fixture, fixture.admin_id, r#"{"kind":"permanent","reason":"self"}"#).await;
    assert_eq!(response.status(), StatusCode::CONFLICT);
    assert!(body_text(response).await.contains("cannot_ban_self"));
}

#[tokio::test]
async fn temporary_ban_requires_future_expiry() {
    let fixture = fixture().await;

    let missing =
        admin_ban(&fixture, fixture.target, r#"{"kind":"temporary","reason":"cool down"}"#).await;
    assert_eq!(missing.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(missing).await.contains("expiry_required"));

    let past = admin_ban(
        &fixture,
        fixture.target,
        r#"{"kind":"temporary","expires_at":"2020-01-01T00:00:00Z","reason":"cool down"}"#,
    )
    .await;
    assert_eq!(past.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(past).await.contains("expiry_in_past"));
}

#[tokio::test]
async fn unban_without_ban_is_404() {
    let fixture = fixture().await;

    let response = admin_unban(&fixture).await;
    assert_eq!(response.status(), StatusCode::NOT_FOUND);
    assert!(body_text(response).await.contains("ban_not_found"));
}
