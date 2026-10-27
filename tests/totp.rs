//! TOTP 两步验证集成测试：启用、二段登录、恢复码、关闭撤销（红线 #1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{
    Mailbox, OneshotPost, Session, await_first_mail, body_text, cookie_header, login_session,
    register_user, set_cookie_of,
};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

/// 以注入时钟的固定时刻计算六位 TOTP 码（密钥来自 setup 响应）。
fn totp_code_at_fixed_now(secret: &str, now: chrono::DateTime<chrono::Utc>) -> String {
    let parsed = totp_rs::Secret::try_from_base32(secret).expect("setup 返回的密钥必须可解析");
    let totp = totp_rs::Builder::new()
        .with_algorithm(nebula_server::model::totp::TOTP_ALGORITHM)
        .with_digits(nebula_server::model::totp::TOTP_DIGITS)
        .with_skew(u16::from(nebula_server::model::totp::TOTP_SKEW))
        .with_step_duration(nebula_server::model::totp::TOTP_PERIOD)
        .with_secret(parsed)
        .build()
        .expect("参数合法");
    totp.generate(now.timestamp().cast_unsigned()).to_string()
}

/// 造已验证用户 → setup → enable，返回（恢复码，机密）。
async fn enable_totp(
    app: &axum::Router,
    state: &nebula_server::api::AppState,
    mailbox: &Mailbox,
) -> (Session, Vec<String>, String) {
    register_user(app, EMAIL, USERNAME, PASSWORD).await;
    let code = common::extract_code(&await_first_mail(mailbox).await);
    let verified = app
        .clone()
        .oneshot_post(
            "/api/auth/register/verify",
            &format!(r#"{{"email":"{EMAIL}","code":"{code}"}}"#),
        )
        .await;
    assert_eq!(verified.status(), StatusCode::NO_CONTENT);
    let session = login_session(app).await;

    let setup = app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/setup",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(setup.status(), StatusCode::OK);
    let setup_json: serde_json::Value =
        serde_json::from_str(&common::body_text(setup).await).unwrap();
    let secret = setup_json["secret"].as_str().unwrap().to_owned();
    assert!(setup_json["otpauth_uri"].as_str().unwrap().starts_with("otpauth://totp/"));

    let totp_code = totp_code_at_fixed_now(&secret, state.clock.now());
    let enable = app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/enable",
            &format!(r#"{{"code":"{totp_code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(enable.status(), StatusCode::OK);
    let enable_json: serde_json::Value =
        serde_json::from_str(&common::body_text(enable).await).unwrap();
    let codes: Vec<String> = enable_json["recovery_codes"]
        .as_array()
        .unwrap()
        .iter()
        .map(|v| v.as_str().unwrap().to_owned())
        .collect();
    assert_eq!(codes.len(), 8);
    (session, codes, secret)
}

#[tokio::test]
async fn setup_is_illegal_when_already_enabled() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let (session, _codes, _secret) = enable_totp(&app, &state, &mailbox).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/setup",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::CONFLICT);
    assert!(body_text(response).await.contains("totp_already_enabled"));
}

#[tokio::test]
async fn enable_without_pending_is_409_totp_not_pending() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let code = common::extract_code(&await_first_mail(&mailbox).await);
    let verified = app
        .clone()
        .oneshot_post(
            "/api/auth/register/verify",
            &format!(r#"{{"email":"{EMAIL}","code":"{code}"}}"#),
        )
        .await;
    assert_eq!(verified.status(), StatusCode::NO_CONTENT);
    let session = login_session(&app).await;

    // 未 setup（Disabled）直接 enable：不是 Pending，必须返回专门的状态错误
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/enable",
            r#"{"code":"123456"}"#,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::CONFLICT);
    assert!(body_text(response).await.contains("totp_not_pending"));
}

#[tokio::test]
async fn enabled_login_requires_second_factor_then_completes() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let (_session, _codes, secret) = enable_totp(&app, &state, &mailbox).await;

    // 密码步骤：401 totp_required + challenge_token
    let login_body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
    let first = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    assert_eq!(first.status(), StatusCode::UNAUTHORIZED);
    let json: serde_json::Value = serde_json::from_str(&body_text(first).await).unwrap();
    assert_eq!(json["code"], "totp_required");
    let challenge = json["details"]["challenge_token"].as_str().unwrap().to_owned();

    // 错码 ×5 烧毁挑战
    for _ in 0..5 {
        let wrong = app
            .clone()
            .oneshot_post(
                "/api/auth/login/totp",
                &format!(r#"{{"challenge_token":"{challenge}","code":"000000"}}"#),
            )
            .await;
        assert_eq!(wrong.status(), StatusCode::UNAUTHORIZED);
    }
    let burned = app
        .clone()
        .oneshot_post(
            "/api/auth/login/totp",
            &format!(r#"{{"challenge_token":"{challenge}","code":"000000"}}"#),
        )
        .await;
    assert_eq!(burned.status(), StatusCode::UNAUTHORIZED);

    // 新挑战 + 正确码 → 200 会话
    let second = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    assert_eq!(second.status(), StatusCode::UNAUTHORIZED);
    let json: serde_json::Value = serde_json::from_str(&body_text(second).await).unwrap();
    let challenge = json["details"]["challenge_token"].as_str().unwrap().to_owned();
    let code = totp_code_at_fixed_now(&secret, state.clock.now());
    let done = app
        .clone()
        .oneshot_post(
            "/api/auth/login/totp",
            &format!(r#"{{"challenge_token":"{challenge}","code":"{code}"}}"#),
        )
        .await;
    assert_eq!(done.status(), StatusCode::OK);
    assert!(set_cookie_of(&done, "nebula_access").is_some());
    assert!(set_cookie_of(&done, "nebula_refresh").is_some());
}

#[tokio::test]
async fn recovery_code_logs_in_exactly_once() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let (_session, codes, _secret) = enable_totp(&app, &state, &mailbox).await;

    let login_body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
    let first = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    let json: serde_json::Value = serde_json::from_str(&body_text(first).await).unwrap();
    let challenge = json["details"]["challenge_token"].as_str().unwrap().to_owned();

    let done = app
        .clone()
        .oneshot_post(
            "/api/auth/login/totp",
            &format!(r#"{{"challenge_token":"{challenge}","code":"{}"}}"#, codes[0]),
        )
        .await;
    assert_eq!(done.status(), StatusCode::OK, "恢复码首用必须成功");

    // 同一恢复码第二次登录：消费过，失败
    let again = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    let json: serde_json::Value = serde_json::from_str(&body_text(again).await).unwrap();
    let challenge = json["details"]["challenge_token"].as_str().unwrap().to_owned();
    let replay = app
        .clone()
        .oneshot_post(
            "/api/auth/login/totp",
            &format!(r#"{{"challenge_token":"{challenge}","code":"{}"}}"#, codes[0]),
        )
        .await;
    assert_eq!(replay.status(), StatusCode::UNAUTHORIZED, "恢复码只能用一次");
}

#[tokio::test]
async fn disable_with_password_and_code_revokes_sessions() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let (session, _codes, secret) = enable_totp(&app, &state, &mailbox).await;

    let code = totp_code_at_fixed_now(&secret, state.clock.now());
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/disable",
            &format!(r#"{{"password":"{PASSWORD}","code":"{code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::NO_CONTENT);
    assert!(common::raw_set_cookie(&response, "nebula_access").contains("Max-Age=0"));

    // 撤销波及：原 refresh 全灭
    let after = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(after.status(), StatusCode::UNAUTHORIZED);

    // 登录不再要求二段（直接 200）
    let login_body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
    let plain_login = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    assert_eq!(plain_login.status(), StatusCode::OK);

    // 数据层面：状态回 disabled
    let user = state.user_repo.find_by_email(EMAIL).await.unwrap().expect("用户必须存在");
    assert_eq!(
        state.totp_repo.find(&user.id).await.unwrap().map(|(s, _)| s),
        Some(nebula_server::model::totp::TotpState::Disabled)
    );
}

#[tokio::test]
async fn disable_requires_correct_password() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    let (session, _codes, secret) = enable_totp(&app, &state, &mailbox).await;

    let code = totp_code_at_fixed_now(&secret, state.clock.now());
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/user/totp/disable",
            &format!(r#"{{"password":"Wrong!Passw0rd-2026","code":"{code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert!(body_text(response).await.contains("invalid_credentials"));
}

#[tokio::test]
async fn challenge_expires_after_five_minutes() {
    let (app, state, clock, mailbox, _dir) = common::app().await;
    let (_session, _codes, _secret) = enable_totp(&app, &state, &mailbox).await;

    let login_body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#);
    let first = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    let json: serde_json::Value = serde_json::from_str(&body_text(first).await).unwrap();
    let challenge = json["details"]["challenge_token"].as_str().unwrap().to_owned();

    clock.advance(chrono::TimeDelta::minutes(5) + chrono::TimeDelta::seconds(1));
    let response = app
        .clone()
        .oneshot_post(
            "/api/auth/login/totp",
            &format!(r#"{{"challenge_token":"{challenge}","code":"123456"}}"#),
        )
        .await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert!(body_text(response).await.contains("totp_invalid"));
}
