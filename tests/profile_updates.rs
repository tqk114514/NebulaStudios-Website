//! 资料修改集成测试：用户名大小写敏感、当前口令再认证、邮箱两步变更与会话保留。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::sync::Arc;

use axum::http::StatusCode;
use common::{
    OneshotPost, Session, await_mail_for, await_mail_subject, body_text, cookie_header,
    extract_code, login_as, login_session, register_user,
};
use nebula_server::api;

const PASSWORD: &str = "Nebula!Passw0rd-2026";

async fn profile(app: &axum::Router, session: &Session) -> serde_json::Value {
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    let response = app.clone().oneshot_get_with_cookie("/api/user", "", &cookie).await;
    assert_eq!(response.status(), StatusCode::OK);
    serde_json::from_str(&body_text(response).await).expect("资料响应必须是 JSON")
}

async fn update_username(
    app: &axum::Router,
    session: &Session,
    username: &str,
    password: &str,
) -> axum::response::Response {
    app.clone()
        .oneshot_patch_with(
            "/api/user/username",
            &format!(r#"{{"username":"{username}","current_password":"{password}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

async fn request_email_change(
    app: &axum::Router,
    session: &Session,
    new_email: &str,
    password: &str,
) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            "/api/user/email/change/request",
            &format!(r#"{{"new_email":"{new_email}","current_password":"{password}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

async fn confirm_email_change(
    app: &axum::Router,
    session: &Session,
    code: &str,
) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            "/api/user/email/change/confirm",
            &format!(r#"{{"code":"{code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

#[tokio::test]
async fn username_update_is_case_sensitive_and_requires_password() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

    let wrong = update_username(&app, &session, "Nova", "Wrong!Passw0rd-2026").await;
    assert_eq!(wrong.status(), StatusCode::UNAUTHORIZED);

    let invalid = update_username(&app, &session, "no", PASSWORD).await;
    assert_eq!(invalid.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(invalid).await.contains("username_too_short"));

    let updated = update_username(&app, &session, "Nova", PASSWORD).await;
    assert_eq!(updated.status(), StatusCode::NO_CONTENT);
    assert_eq!(profile(&app, &session).await["username"], "Nova");

    // 旧大小写不再可登录，新大小写可以；邮箱登录不受影响
    let old_case = {
        let body = format!(r#"{{"identifier":"nova","password":"{PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(old_case.status(), StatusCode::UNAUTHORIZED);
    let _new_case = login_as(&app, "Nova", PASSWORD).await;

    // 大小写敏感：另一个用户可以注册小写 nova
    register_user(&app, "other@nebula.st", "nova", PASSWORD).await;
    let taken = update_username(&app, &session, "nova", PASSWORD).await;
    assert_eq!(taken.status(), StatusCode::CONFLICT);
    assert!(body_text(taken).await.contains("username_taken"));

    let audit = state
        .admin_repo
        .list_audit_for_user(
            &state
                .user_repo
                .find_by_email("nova@nebula.st")
                .await
                .unwrap()
                .expect("用户必须存在")
                .id,
        )
        .await
        .expect("审计查询必须成功");
    assert!(audit.iter().any(|entry| entry.action == "user.username.change"));
}

#[tokio::test]
async fn email_change_updates_address_and_keeps_existing_session() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

    let wrong = request_email_change(&app, &session, "new@nebula.st", "Wrong!Passw0rd-2026").await;
    assert_eq!(wrong.status(), StatusCode::UNAUTHORIZED);

    let requested = request_email_change(&app, &session, "new@nebula.st", PASSWORD).await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);
    let mail = await_mail_for(&mailbox, "new@nebula.st").await;
    assert_eq!(mail.subject, "Nebula 邮箱变更验证码");
    let code = extract_code(&mail);

    let wrong_code = confirm_email_change(&app, &session, "000000").await;
    assert_eq!(wrong_code.status(), StatusCode::UNAUTHORIZED);

    let confirmed = confirm_email_change(&app, &session, &code).await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT);
    let current = profile(&app, &session).await;
    assert_eq!(current["email"], "new@nebula.st");
    assert_eq!(current["email_verified"], true);

    // 用户选择保留会话：旧 access cookie 仍可读资料
    assert_eq!(profile(&app, &session).await["email"], "new@nebula.st");

    let old_login = {
        let body = format!(r#"{{"identifier":"nova@nebula.st","password":"{PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(old_login.status(), StatusCode::UNAUTHORIZED);
    let _ = login_as(&app, "new@nebula.st", PASSWORD).await;

    await_mail_subject(&mailbox, "账户邮箱已更改").await;
    let user = state.user_repo.find_by_email("new@nebula.st").await.unwrap().expect("用户必须存在");
    let audit = state.admin_repo.list_audit_for_user(&user.id).await.expect("审计查询必须成功");
    assert!(audit.iter().any(|entry| entry.action == "user.email.change"
        && entry.details.as_ref().and_then(|details| details["email"].as_str())
            == Some("new@nebula.st")));

    // 请求已被消费，旧验证码不可再次使用
    let replay = confirm_email_change(&app, &session, &code).await;
    assert_eq!(replay.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn email_change_rejects_unchanged_and_disallowed_domains() {
    let (mut state, _clock, _mailbox, _dir) = common::app_state().await;
    state.allowed_email_domains = Arc::new(HashSet::from(["nebula.st".to_owned()]));
    let app = api::router(state.clone());
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

    let unchanged = request_email_change(&app, &session, "nova@nebula.st", PASSWORD).await;
    assert_eq!(unchanged.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(unchanged).await.contains("email_unchanged"));

    let disallowed = request_email_change(&app, &session, "nova@other.com", PASSWORD).await;
    assert_eq!(disallowed.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(disallowed).await.contains("email_domain_not_allowed"));
}

#[tokio::test]
async fn email_change_rejects_taken_and_invalid_addresses() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    register_user(&app, "taken@nebula.st", "taken", PASSWORD).await;
    let session = login_session(&app).await;

    let taken = request_email_change(&app, &session, "taken@nebula.st", PASSWORD).await;
    assert_eq!(taken.status(), StatusCode::CONFLICT);
    assert!(body_text(taken).await.contains("email_taken"));

    let invalid = request_email_change(&app, &session, "not-an-email", PASSWORD).await;
    assert_eq!(invalid.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(invalid).await.contains("invalid_email"));
}

#[tokio::test]
async fn profile_writes_require_authentication_and_csrf() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

    let anonymous = app
        .clone()
        .oneshot_patch_with(
            "/api/user/username",
            r#"{"username":"Nova","current_password":"Nebula!Passw0rd-2026"}"#,
            "",
            None,
        )
        .await;
    assert_eq!(anonymous.status(), StatusCode::UNAUTHORIZED);

    let no_csrf = app
        .clone()
        .oneshot_post_with(
            "/api/user/email/change/request",
            r#"{"new_email":"new@nebula.st","current_password":"Nebula!Passw0rd-2026"}"#,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            None,
        )
        .await;
    assert_eq!(no_csrf.status(), StatusCode::FORBIDDEN);
    assert!(body_text(no_csrf).await.contains("csrf_invalid"));
}
