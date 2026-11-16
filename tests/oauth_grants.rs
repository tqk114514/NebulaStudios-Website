//! 第三方授权管理集成测试：列表聚合、撤销 refresh 家族、审计与鉴权门禁。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use chrono::{DateTime, TimeDelta, Utc};
use common::{OneshotPost, Session, body_text, cookie_header, login_session, register_user};
use nebula_server::api::{self, AppState};
use nebula_server::context::Clock;
use nebula_server::model::oauth2::OAuthClient;
use nebula_server::model::tokens::NewRefreshFamily;
use nebula_server::model::user::UserId;
use nebula_server::storage::{ClientBinding, RotateRefreshError};

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

struct Fixture {
    app: axum::Router,
    state: AppState,
    session: Session,
    user_id: UserId,
    now: DateTime<Utc>,
    _dir: tempfile::TempDir,
}

async fn fixture() -> Fixture {
    let (state, clock, _mailbox, dir) = common::app_state().await;
    let app = api::router(state.clone());
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;
    let user =
        state.user_repo.find_by_email(EMAIL).await.expect("查询必须成功").expect("用户必须存在");
    let now = clock.now();

    for (id, name) in
        [("client-a", "App A"), ("client-b", "App B"), ("client-c", "App C"), ("client-d", "App D")]
    {
        state
            .oauth_repo
            .upsert_client(
                &OAuthClient {
                    id: id.to_owned(),
                    name: name.to_owned(),
                    redirect_uri: format!("https://{id}.example/cb"),
                    enabled: true,
                },
                "client-hash",
                now,
            )
            .await
            .expect("客户端必须可插入");
    }

    seed_family(&state, user.id, "client-a", "profile", "token-a1", now, now + TimeDelta::days(30))
        .await;
    seed_family(
        &state,
        user.id,
        "client-a",
        "profile email",
        "token-a2",
        now + TimeDelta::minutes(5),
        now + TimeDelta::days(30),
    )
    .await;
    seed_family(&state, user.id, "client-b", "profile", "token-b1", now, now + TimeDelta::days(30))
        .await;
    seed_family(&state, user.id, "client-c", "profile", "token-c1", now, now + TimeDelta::days(30))
        .await;
    seed_family(
        &state,
        user.id,
        "client-d",
        "profile",
        "token-d1",
        now,
        now - TimeDelta::seconds(1),
    )
    .await;
    state.oauth_repo.revoke_client(&user.id, "client-c", now).await.expect("预置撤销必须成功");

    Fixture { app, state, session, user_id: user.id, now, _dir: dir }
}

async fn seed_family(
    state: &AppState,
    user_id: UserId,
    client_id: &str,
    scope: &str,
    token_hash: &str,
    created_at: DateTime<Utc>,
    expires_at: DateTime<Utc>,
) {
    state
        .refresh_repo
        .create_family(
            &NewRefreshFamily {
                family_id: uuid::Uuid::now_v7(),
                user_id,
                client_id: Some(client_id.to_owned()),
                scope: scope.to_owned(),
                token_hash: token_hash.to_owned(),
                expires_at,
            },
            created_at,
        )
        .await
        .expect("家族必须可插入");
}

async fn grants(fixture: &Fixture) -> serde_json::Value {
    let cookie = cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf));
    let response =
        fixture.app.clone().oneshot_get_with_cookie("/api/user/oauth/grants", "", &cookie).await;
    assert_eq!(response.status(), StatusCode::OK);
    serde_json::from_str(&body_text(response).await).expect("授权响应必须是 JSON")
}

async fn revoke(fixture: &Fixture, client_id: &str) -> axum::response::Response {
    fixture
        .app
        .clone()
        .oneshot_delete_with_cookie(
            &format!("/api/user/oauth/grants/{client_id}"),
            &cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf)),
            Some(&fixture.session.csrf),
        )
        .await
}

#[tokio::test]
async fn grants_are_aggregated_and_revocation_kills_refresh_families() {
    let fixture = fixture().await;

    let listed = grants(&fixture).await;
    let entries = listed["grants"].as_array().expect("grants 必须是数组");
    assert_eq!(entries.len(), 2, "已撤销与过期授权不得出现");
    assert_eq!(entries[0]["client_id"], "client-a");
    assert_eq!(entries[0]["client_name"], "App A");
    assert_eq!(entries[0]["scope"], "email profile");
    let authorized_at = DateTime::parse_from_rfc3339(
        entries[0]["authorized_at"].as_str().expect("authorized_at 必须是字符串"),
    )
    .expect("authorized_at 必须是 RFC3339")
    .with_timezone(&Utc);
    assert_eq!(authorized_at, fixture.now);
    assert_eq!(entries[1]["client_id"], "client-b");

    let revoked = revoke(&fixture, "client-a").await;
    assert_eq!(revoked.status(), StatusCode::NO_CONTENT);

    let listed = grants(&fixture).await;
    let entries = listed["grants"].as_array().expect("grants 必须是数组");
    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0]["client_id"], "client-b");

    // 撤销后 refresh 家族不可再轮转
    let rotation = fixture
        .state
        .refresh_repo
        .rotate(
            "token-a1",
            "token-a1-new",
            fixture.now + TimeDelta::days(30),
            fixture.now,
            ClientBinding::Client("client-a"),
        )
        .await;
    assert!(matches!(rotation, Err(RotateRefreshError::Invalid)));

    let audit = fixture
        .state
        .admin_repo
        .list_audit_for_user(&fixture.user_id)
        .await
        .expect("审计查询必须成功");
    assert!(audit.iter().any(|entry| entry.action == "oauth.grant.revoke"
        && entry.details.as_ref().and_then(|details| details["client_id"].as_str())
            == Some("client-a")));

    assert_eq!(revoke(&fixture, "client-a").await.status(), StatusCode::NOT_FOUND);
    assert_eq!(revoke(&fixture, "ghost").await.status(), StatusCode::NOT_FOUND);
}

#[tokio::test]
async fn grants_require_authentication_and_csrf() {
    let fixture = fixture().await;

    let anonymous =
        fixture.app.clone().oneshot_get_with_cookie("/api/user/oauth/grants", "", "").await;
    assert_eq!(anonymous.status(), StatusCode::UNAUTHORIZED);

    let no_csrf = fixture
        .app
        .clone()
        .oneshot_delete_with_cookie(
            "/api/user/oauth/grants/client-a",
            &cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf)),
            None,
        )
        .await;
    assert_eq!(no_csrf.status(), StatusCode::FORBIDDEN);
    assert!(body_text(no_csrf).await.contains("csrf_invalid"));
}
