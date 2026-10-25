//! 账户生命周期集成测试：改密整族下线、注销硬删、验证码用途隔离
//! （红线 #1：登出/改密/注销同走撤销路径）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{Mailbox, OneshotPost, Session, await_first_mail, cookie_header, extract_code};
use nebula_server::storage::UserRepo;

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";
const NEW_PASSWORD: &str = "Fresh!Passw0rd-2026";

/// 造一个已验证用户并登录，返回会话三件套。
async fn verified_session(
    app: &axum::Router,
    state: &nebula_server::api::AppState,
    mailbox: &Mailbox,
) -> Session {
    common::register_user(app, EMAIL, USERNAME, PASSWORD).await;
    let code = extract_code(&await_first_mail(mailbox).await);
    let body = format!(r#"{{"email":"{EMAIL}","code":"{code}"}}"#);
    let response = app.clone().oneshot_post("/api/auth/register/verify", &body).await;
    assert_eq!(response.status(), StatusCode::NO_CONTENT, "验证必须成功");
    let _ = state;
    common::login_session(app).await
}

async fn body_text(response: axum::response::Response) -> String {
    let bytes = http_body_util::BodyExt::collect(response.into_body()).await.unwrap().to_bytes();
    String::from_utf8_lossy(&bytes).into_owned()
}

#[tokio::test]
async fn change_password_revolves_hash_and_kills_all_sessions() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let first = verified_session(&app, &state, &mailbox).await;
    // 第二个并行会话
    let second = common::login_session(&app).await;

    // 当前口令错 → 401，且不改哈希
    let wrong = app
        .clone()
        .oneshot_post_with(
            "/api/auth/password/change",
            r#"{"current_password":"Wrong!Passw0rd-2026","new_password":"Fresh!Passw0rd-2026"}"#,
            &cookie_header(Some(&first.access), None, Some(&first.csrf)),
            Some(&first.csrf),
        )
        .await;
    assert_eq!(wrong.status(), StatusCode::UNAUTHORIZED);

    // 正确改密 → 204
    let ok = app
        .clone()
        .oneshot_post_with(
            "/api/auth/password/change",
            r#"{"current_password":"Nebula!Passw0rd-2026","new_password":"Fresh!Passw0rd-2026"}"#,
            &cookie_header(Some(&first.access), None, Some(&first.csrf)),
            Some(&first.csrf),
        )
        .await;
    assert_eq!(ok.status(), StatusCode::NO_CONTENT);

    // 红线 #1：两个会话的 refresh 家族全灭
    for session in [&first, &second] {
        let outcome = app
            .clone()
            .oneshot_post_with(
                "/api/auth/refresh",
                "",
                &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
                Some(&session.csrf),
            )
            .await;
        assert_eq!(outcome.status(), StatusCode::UNAUTHORIZED);
    }

    // 旧口令进不来，新口令可以
    let old_login = {
        let body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(old_login.status(), StatusCode::UNAUTHORIZED);
    let new_login = {
        let body = format!(r#"{{"identifier":"{EMAIL}","password":"{NEW_PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(new_login.status(), StatusCode::OK);
}

#[tokio::test]
async fn change_password_validates_new_password() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let session = verified_session(&app, &state, &mailbox).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/password/change",
            r#"{"current_password":"Nebula!Passw0rd-2026","new_password":"short"}"#,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(response).await.contains("password_too_short"));
}

#[tokio::test]
async fn change_password_requires_authentication() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app
        .clone()
        .oneshot_post("/api/auth/password/change", r#"{"current_password":"a","new_password":"b"}"#)
        .await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn delete_flow_requires_verified_email_and_correct_purpose_code() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let session = verified_session(&app, &state, &mailbox).await;

    // 发起注销 → 204，收到注销确认码
    let requested = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/request",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);

    // 等第二封邮件（第一封是注册验证码）
    let mut delete_mail = None;
    for _ in 0..200 {
        let messages = mailbox.messages();
        if messages.len() >= 2 {
            delete_mail = Some(messages[1].clone());
            break;
        }
        tokio::time::sleep(std::time::Duration::from_millis(5)).await;
    }
    let delete_mail = delete_mail.expect("注销确认邮件必须发出");
    assert_eq!(delete_mail.subject, "Nebula 账户注销确认码");
    let delete_code = extract_code(&delete_mail);

    // 注册用途的码不可用于注销（用途隔离）
    let register_code = extract_code(&mailbox.messages()[0]);
    let cross = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/confirm",
            &format!(r#"{{"code":"{register_code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(cross.status(), StatusCode::UNAUTHORIZED);

    // 正确注销码 → 204
    let confirmed = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/confirm",
            &format!(r#"{{"code":"{delete_code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT);

    // 硬删：用户消失，refresh 全灭，登录不可
    let gone = state.user_repo.find_by_email(EMAIL).await.unwrap();
    assert!(gone.is_none(), "账户必须被硬删");
    let login = {
        let body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
        app.clone().oneshot_post("/api/auth/login", &body).await
    };
    assert_eq!(login.status(), StatusCode::UNAUTHORIZED);
}

#[tokio::test]
async fn delete_request_rejects_unverified_email() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    // 注册但不验证邮箱
    common::register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let _ = await_first_mail(&mailbox).await;
    let session = common::login_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/request",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::CONFLICT);
    assert!(body_text(response).await.contains("email_unverified"));
}

#[tokio::test]
async fn delete_confirm_without_authentication_is_401() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response =
        app.clone().oneshot_post("/api/auth/account/delete/confirm", r#"{"code":"ABC123"}"#).await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
}
