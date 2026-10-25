//! 会话端点集成测试：登录、CSRF 门禁（ADR-0005）、刷新轮转、登出撤销
//! （ADR-0004、红线 #1、§8.1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{OneshotPost, cookie_header, register_user, set_cookie_of};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

async fn login(app: &axum::Router, identifier: &str, password: &str) -> axum::response::Response {
    let body = format!(r#"{{"identifier":"{identifier}","password":"{password}"}}"#);
    app.clone().oneshot_post("/api/auth/login", &body).await
}

/// 登录后的整套会话凭据（三 cookie 明文值）。
struct Session {
    access: String,
    refresh: String,
    csrf: String,
}

async fn login_session(app: &axum::Router) -> Session {
    let response = login(app, EMAIL, PASSWORD).await;
    assert_eq!(response.status(), StatusCode::OK, "登录助手必须成功");
    Session {
        access: set_cookie_of(&response, "nebula_access").unwrap(),
        refresh: set_cookie_of(&response, "nebula_refresh").unwrap(),
        csrf: set_cookie_of(&response, "nebula_csrf").unwrap(),
    }
}

/// 带 CSRF 配对的 refresh 请求。
async fn refresh_with_csrf(app: &axum::Router, session: &Session) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

async fn body_text(response: axum::response::Response) -> String {
    let bytes = http_body_util::BodyExt::collect(response.into_body()).await.unwrap().to_bytes();
    String::from_utf8_lossy(&bytes).into_owned()
}

fn raw_set_cookie(response: &axum::response::Response, name: &str) -> String {
    response
        .headers()
        .get_all(axum::http::header::SET_COOKIE)
        .iter()
        .map(|v| v.to_str().unwrap_or_default())
        .find(|v| v.starts_with(name))
        .unwrap_or_default()
        .to_owned()
}

#[tokio::test]
async fn login_returns_user_and_sets_split_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let response = login(&app, EMAIL, PASSWORD).await;

    assert_eq!(response.status(), StatusCode::OK);
    // 头先读（Response 不可 Clone，消费 body 前取 Set-Cookie）
    let access = set_cookie_of(&response, "nebula_access").expect("必须下发 access cookie");
    let refresh = set_cookie_of(&response, "nebula_refresh").expect("必须下发 refresh cookie");
    let csrf = set_cookie_of(&response, "nebula_csrf").expect("必须下发 csrf cookie");
    let raw_access = raw_set_cookie(&response, "nebula_access");
    assert!(raw_access.contains("Path=/"), "access 应全站路径: {raw_access}");
    let raw_refresh = raw_set_cookie(&response, "nebula_refresh");
    assert!(raw_refresh.contains("Path=/api/auth"), "refresh 应收窄路径: {raw_refresh}");
    for raw in [&raw_access, &raw_refresh] {
        assert!(raw.contains("HttpOnly"));
        assert!(raw.contains("Secure"));
        assert!(raw.contains("SameSite=Lax"));
    }
    // csrf cookie 非 HttpOnly（SPA 读取回显，ADR-0005）
    let raw_csrf = raw_set_cookie(&response, "nebula_csrf");
    assert!(raw_csrf.contains("Secure"));
    assert!(!raw_csrf.contains("HttpOnly"), "csrf 不可 HttpOnly: {raw_csrf}");
    // access 是 JWT（三段式），refresh 与 csrf 是 64 位十六进制
    assert_eq!(access.split('.').count(), 3);
    assert_eq!(refresh.len(), 64);
    assert_eq!(csrf.len(), 64);

    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["email"], EMAIL);
    assert_eq!(json["username"], USERNAME);
}

#[tokio::test]
async fn login_accepts_username_identifier() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let response = login(&app, USERNAME, PASSWORD).await;

    assert_eq!(response.status(), StatusCode::OK);
    assert!(set_cookie_of(&response, "nebula_access").is_some());
}

#[tokio::test]
async fn wrong_password_is_401_invalid_credentials() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let response = login(&app, EMAIL, "Wrong!Passw0rd-2026").await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    insta::assert_snapshot!(body_text(response).await);
}

#[tokio::test]
async fn unknown_identifier_yields_identical_body_to_wrong_password() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let wrong_password = body_text(login(&app, EMAIL, "Wrong!Passw0rd-2026").await).await;
    let unknown_user = body_text(login(&app, "ghost@nebula.st", PASSWORD).await).await;

    // 防枚举：两条路径的响应体必须逐字节一致
    assert_eq!(wrong_password, unknown_user);
    assert!(wrong_password.contains("invalid_credentials"));
}

#[tokio::test]
async fn empty_identifier_is_422() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = login(&app, " ", PASSWORD).await;

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(response).await.contains("identifier_missing"));
}

#[tokio::test]
async fn refresh_without_csrf_header_is_403() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    // 带 csrf cookie 但不回头，门禁必须拦下
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            None,
        )
        .await;

    assert_eq!(response.status(), StatusCode::FORBIDDEN);
    assert!(body_text(response).await.contains("csrf_invalid"));
}

#[tokio::test]
async fn refresh_with_mismatched_csrf_is_403() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
        )
        .await;

    assert_eq!(response.status(), StatusCode::FORBIDDEN);
    assert!(body_text(response).await.contains("csrf_invalid"));
}

#[tokio::test]
async fn refresh_rotates_and_sets_new_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let response = refresh_with_csrf(&app, &session).await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    let new_access = set_cookie_of(&response, "nebula_access").expect("续发 access");
    let new_refresh = set_cookie_of(&response, "nebula_refresh").expect("轮转出新的 refresh");
    let new_csrf = set_cookie_of(&response, "nebula_csrf").expect("轮转出新的 csrf");
    assert_ne!(new_refresh, session.refresh);
    assert_ne!(new_csrf, session.csrf);
    assert_eq!(new_access.split('.').count(), 3);
}

#[tokio::test]
async fn refresh_replay_is_detected_and_revokes_family() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    // 第一次轮转成功，拿到链上第二代
    let second_response = refresh_with_csrf(&app, &session).await;
    assert_eq!(second_response.status(), StatusCode::NO_CONTENT);
    let second = Session {
        access: set_cookie_of(&second_response, "nebula_access").unwrap(),
        refresh: set_cookie_of(&second_response, "nebula_refresh").unwrap(),
        csrf: set_cookie_of(&second_response, "nebula_csrf").unwrap(),
    };

    // 重放第一代：门禁配对有效（旧 cookie 自带旧 csrf），重放判定在其后
    let replay = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(replay.status(), StatusCode::UNAUTHORIZED);
    // 头先读（body 消费后 Response 不可再用）
    let cleared = raw_set_cookie(&replay, "nebula_refresh");
    assert!(cleared.contains("Max-Age=0"), "401 应附带清 cookie: {cleared}");
    assert!(body_text(replay).await.contains("refresh_reused"));

    // 撤销波及第二代（本为合法 token）
    let after = refresh_with_csrf(&app, &second).await;
    assert_eq!(after.status(), StatusCode::UNAUTHORIZED);
    assert!(body_text(after).await.contains("refresh_invalid"));
}

#[tokio::test]
async fn refresh_without_cookie_is_401() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    // CSRF 配对有效，仅缺 refresh cookie
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    let cleared = raw_set_cookie(&response, "nebula_access");
    assert!(cleared.contains("Max-Age=0"), "401 应附带清 cookie: {cleared}");
    assert!(body_text(response).await.contains("refresh_invalid"));
}

#[tokio::test]
async fn logout_without_authentication_is_401() {
    // 红线 #1 回归：登出必须挂在鉴权前置之后——无凭据时 handler 不得执行
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    // 只有 refresh + csrf cookie、无 access：仍须 401
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    // 未鉴权即未撤销：refresh 仍可轮转
    let still_valid = refresh_with_csrf(&app, &session).await;
    assert_eq!(still_valid.status(), StatusCode::NO_CONTENT);
}

#[tokio::test]
async fn logout_with_cookie_requires_csrf() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    // cookie 认证的写方法：缺 X-CSRF-Token 头 → 403（ADR-0005）
    let blocked = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            None,
        )
        .await;
    assert_eq!(blocked.status(), StatusCode::FORBIDDEN);

    // 配对齐全 → 204
    let ok = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(ok.status(), StatusCode::NO_CONTENT);
}

#[tokio::test]
async fn logout_with_bearer_skips_csrf() {
    // Bearer 认证豁免 CSRF：跨站页面无法携带自定义头（ADR-0005）
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let response = app.clone().oneshot_post_bearer("/api/auth/logout", "", &session.access).await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    // 撤销确实发生：refresh 家族已死
    let after = refresh_with_csrf(&app, &session).await;
    assert_eq!(after.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn logout_revokes_all_sessions() {
    // 红线 #1 回归：登出必须真的撤销会话
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    // 两个并行会话（两次登录 = 两个家族）
    let first = login_session(&app).await;
    let second = login_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&first.access), None, Some(&first.csrf)),
            Some(&first.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    let cleared = raw_set_cookie(&response, "nebula_access");
    assert!(cleared.contains("Max-Age=0"), "登出须清 cookie: {cleared}");

    // 两个家族都被撤销——不只是当前会话
    for session in [&first, &second] {
        let outcome = refresh_with_csrf(&app, session).await;
        assert_eq!(outcome.status(), StatusCode::UNAUTHORIZED, "登出后任一会话不得再轮转");
    }
}
