//! 数据导出集成测试：一次性链接、单次消费、过期与新令牌失效；快照包含
//! 资料、社交绑定、TOTP 状态与审计记录。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use chrono::TimeDelta;
use common::{OneshotPost, Session, body_text, cookie_header, login_session, register_user};
use nebula_server::context::Clock;
use nebula_server::model::export::EXPORT_TOKEN_TTL;
use nebula_server::model::social::{NewSocialIdentity, SocialProvider};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

async fn request_export(app: &axum::Router, session: &Session) -> serde_json::Value {
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/user/export",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(response.status(), StatusCode::OK, "申请导出必须成功");
    serde_json::from_str(&body_text(response).await).expect("申请响应必须是 JSON")
}

#[tokio::test]
async fn export_requires_authentication_and_csrf() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let anonymous = app.clone().oneshot_post("/api/user/export", "").await;
    assert_eq!(anonymous.status(), StatusCode::UNAUTHORIZED);

    let without_csrf = app
        .clone()
        .oneshot_post_with(
            "/api/user/export",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            None,
        )
        .await;
    assert_eq!(without_csrf.status(), StatusCode::FORBIDDEN);
    assert!(body_text(without_csrf).await.contains("csrf_invalid"));
}

#[tokio::test]
async fn export_downloads_full_snapshot_exactly_once() {
    let (app, state, clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;
    let user = state.user_repo.find_by_email(EMAIL).await.unwrap().expect("用户必须存在");

    state
        .social_repo
        .link_identity(
            &NewSocialIdentity {
                user_id: user.id,
                provider: SocialProvider::Google,
                provider_user_id: "google-1".to_owned(),
                display_name: Some("Nova".to_owned()),
            },
            clock.now(),
        )
        .await
        .expect("绑定必须成功");
    state.totp_repo.set_pending(&user.id, "cipher", clock.now()).await.expect("pending 必须成功");
    state.totp_repo.enable(&user.id, clock.now()).await.expect("启用必须成功");
    state
        .totp_repo
        .add_recovery_codes(&user.id, vec!["hash-1".to_owned(), "hash-2".to_owned()], clock.now())
        .await
        .expect("恢复码必须落库");

    let issued = request_export(&app, &session).await;
    let download_url = issued["download_url"].as_str().expect("必须返回下载地址").to_owned();
    assert!(download_url.starts_with("/api/user/export/"));
    assert!(issued["expires_at"].is_string());

    let response = app.clone().oneshot_get_with_cookie(&download_url, "", "").await;
    assert_eq!(response.status(), StatusCode::OK);
    assert!(
        response
            .headers()
            .get("content-type")
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| value.starts_with("application/json"))
    );
    assert!(
        response
            .headers()
            .get("content-disposition")
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| value.starts_with("attachment"))
    );
    assert_eq!(
        response.headers().get("cache-control").and_then(|value| value.to_str().ok()),
        Some("no-store")
    );

    let document: serde_json::Value =
        serde_json::from_str(&body_text(response).await).expect("下载内容必须是 JSON");
    assert_eq!(document["format_version"], 1);
    assert_eq!(document["user"]["username"], USERNAME);
    assert_eq!(document["user"]["email"], EMAIL);
    assert!(document["user"].get("password_hash").is_none());
    assert!(document["user"].get("avatar_version").is_none());
    assert!(document["avatar_url"].is_null());
    assert_eq!(document["totp"]["enabled"], true);
    assert_eq!(document["totp"]["recovery_codes_remaining"], 2);
    assert_eq!(document["social_identities"][0]["provider"], "google");
    assert_eq!(document["social_identities"][0]["provider_user_id"], "google-1");
    assert_eq!(document["social_identities"][0]["display_name"], "Nova");
    let actions: Vec<&str> = document["audit_log"]
        .as_array()
        .expect("审计记录必须是数组")
        .iter()
        .filter_map(|entry| entry["action"].as_str())
        .collect();
    assert!(actions.contains(&"auth.register"));
    assert!(actions.contains(&"auth.login"));

    // 令牌一次性：第二次下载必须失败
    let replay = app.clone().oneshot_get_with_cookie(&download_url, "", "").await;
    assert_eq!(replay.status(), StatusCode::NOT_FOUND);
    assert!(body_text(replay).await.contains("export_token_invalid"));
}

#[tokio::test]
async fn issuing_new_export_invalidates_previous_link() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let first = request_export(&app, &session).await;
    let first_url = first["download_url"].as_str().expect("必须返回下载地址");
    let second = request_export(&app, &session).await;
    let second_url = second["download_url"].as_str().expect("必须返回下载地址");
    assert_ne!(first_url, second_url);

    let stale = app.clone().oneshot_get_with_cookie(first_url, "", "").await;
    assert_eq!(stale.status(), StatusCode::NOT_FOUND);

    let fresh = app.clone().oneshot_get_with_cookie(second_url, "", "").await;
    assert_eq!(fresh.status(), StatusCode::OK);
}

#[tokio::test]
async fn expired_and_unknown_tokens_are_rejected() {
    let (app, _state, clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let issued = request_export(&app, &session).await;
    let download_url = issued["download_url"].as_str().expect("必须返回下载地址").to_owned();
    clock.advance(EXPORT_TOKEN_TTL + TimeDelta::seconds(1));

    let expired = app.clone().oneshot_get_with_cookie(&download_url, "", "").await;
    assert_eq!(expired.status(), StatusCode::NOT_FOUND);
    assert!(body_text(expired).await.contains("export_token_invalid"));

    let unknown = app
        .clone()
        .oneshot_get_with_cookie(&format!("/api/user/export/{}", "0".repeat(64)), "", "")
        .await;
    assert_eq!(unknown.status(), StatusCode::NOT_FOUND);

    let malformed = app.clone().oneshot_get_with_cookie("/api/user/export/short", "", "").await;
    assert_eq!(malformed.status(), StatusCode::NOT_FOUND);
}
