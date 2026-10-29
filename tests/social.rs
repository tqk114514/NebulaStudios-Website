//! 社交登录集成测试：起授权、回调登录（按绑定）、未绑定拒绝、state 单次消费与过期、
//! PKCE verifier 回呈（ADR-0012）。provider 出站以假客户端注入，不打真实网络。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use axum::Router;
use axum::http::{StatusCode, header};
use nebula_server::api;
use nebula_server::context::{Clock, ManualClock};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::social::{NewSocialIdentity, SocialProvider};
use nebula_server::model::user::{User, UserId};
use nebula_server::social::{ProviderIdentity, SocialClients, SocialError, SocialProviderClient};

use common::{OneshotPost, set_cookie_of};

/// 假 provider 客户端：返回预置身份，并记录收到的 PKCE verifier。
struct FakeClient {
    identity: Option<ProviderIdentity>,
    seen_verifier: Arc<Mutex<Option<String>>>,
}

#[async_trait]
impl SocialProviderClient for FakeClient {
    fn provider(&self) -> SocialProvider {
        SocialProvider::Google
    }

    fn authorize_url(&self, state: &str, code_challenge: &str) -> String {
        format!("https://provider.test/authorize?state={state}&code_challenge={code_challenge}")
    }

    async fn fetch_identity(
        &self,
        _code: &str,
        code_verifier: &str,
    ) -> Result<ProviderIdentity, SocialError> {
        *self.seen_verifier.lock().expect("锁不应中毒") = Some(code_verifier.to_owned());
        self.identity.clone().ok_or_else(|| SocialError::Provider("fake provider 拒绝".to_owned()))
    }
}

struct Fixture {
    app: Router,
    state: api::AppState,
    clock: Arc<ManualClock>,
    seen_verifier: Arc<Mutex<Option<String>>>,
    _dir: tempfile::TempDir,
}

/// 装配启用 Google（假客户端）的 router；`identity` 为 None 时回调整体失败。
async fn fixture(identity: Option<ProviderIdentity>) -> Fixture {
    let (mut state, clock, _mailbox, dir) = common::app_state().await;
    let seen_verifier = Arc::new(Mutex::new(None));
    state.social_clients = Arc::new(SocialClients::new(vec![Arc::new(FakeClient {
        identity,
        seen_verifier: seen_verifier.clone(),
    })]));
    let app = api::router(state.clone());
    Fixture { app, state, clock, seen_verifier, _dir: dir }
}

/// 起授权并返回 Location 里的 state。
async fn start_and_capture_state(fixture: &Fixture) -> String {
    let response = fixture.app.clone().oneshot_get_with_cookie("/api/auth/google", "", "").await;
    assert_eq!(response.status(), StatusCode::FOUND);
    let location = response.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.starts_with("https://provider.test/authorize"), "{location}");
    assert!(location.contains("code_challenge="), "必须带 PKCE challenge: {location}");
    location.split("state=").nth(1).unwrap().split('&').next().unwrap().to_owned()
}

async fn seed_bound_user(fixture: &Fixture, provider_user_id: &str) -> UserId {
    let user_id = UserId::new();
    fixture
        .state
        .user_repo
        .insert(
            &User {
                id: user_id,
                username: "nova".to_owned(),
                email: "nova@nebula.st".to_owned(),
                created_at: fixture.clock.now(),
                email_verified: true,
            },
            &PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功"),
        )
        .await
        .expect("种子用户必须可插入");
    fixture
        .state
        .social_repo
        .link_identity(
            &NewSocialIdentity {
                user_id,
                provider: SocialProvider::Google,
                provider_user_id: provider_user_id.to_owned(),
                display_name: Some("Nova".to_owned()),
            },
            fixture.clock.now(),
        )
        .await
        .expect("绑定必须成功");
    user_id
}

fn identity(provider_user_id: &str) -> ProviderIdentity {
    ProviderIdentity { provider_user_id: provider_user_id.to_owned(), display_name: None }
}

#[tokio::test]
async fn unknown_provider_is_404() {
    let fixture = fixture(None).await;

    let response = fixture.app.clone().oneshot_get_with_cookie("/api/auth/github", "", "").await;

    assert_eq!(response.status(), StatusCode::NOT_FOUND);
    assert!(common::body_text(response).await.contains("social_provider_disabled"));
}

#[tokio::test]
async fn bound_identity_logs_in_with_session() {
    let fixture = fixture(Some(identity("google-1"))).await;
    let user_id = seed_bound_user(&fixture, "google-1").await;
    let state = start_and_capture_state(&fixture).await;

    let response = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/auth/google/callback",
            &format!("code=abc&state={state}"),
            "",
        )
        .await;

    assert_eq!(response.status(), StatusCode::FOUND);
    let location = response.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.ends_with("/account/login?social=ok"), "{location}");
    assert!(set_cookie_of(&response, "nebula_access").is_some(), "登录必须下发会话");
    // PKCE：回调把落库的 verifier 回呈给 provider
    let seen = fixture.seen_verifier.lock().unwrap().clone();
    assert!(seen.as_deref().is_some_and(|v| v.len() == 43), "必须回呈 43 字符 verifier: {seen:?}");

    // 绑定仍在原用户
    let found = fixture
        .state
        .social_repo
        .find_identity(SocialProvider::Google, "google-1")
        .await
        .unwrap()
        .unwrap();
    assert_eq!(found.user_id, user_id);
}

#[tokio::test]
async fn unbound_identity_is_rejected_without_account_creation() {
    let fixture = fixture(Some(identity("stranger"))).await;
    let state = start_and_capture_state(&fixture).await;

    let response = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/auth/google/callback",
            &format!("code=abc&state={state}"),
            "",
        )
        .await;

    assert_eq!(response.status(), StatusCode::FOUND);
    let location = response.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.ends_with("/account/login?social_error=not_linked"), "{location}");
    assert!(set_cookie_of(&response, "nebula_access").is_none(), "不得签发会话");
    // 不自动建号
    assert!(fixture.state.user_repo.find_by_email("stranger@nebula.st").await.unwrap().is_none());
}

#[tokio::test]
async fn replayed_state_is_rejected() {
    let fixture = fixture(Some(identity("google-1"))).await;
    seed_bound_user(&fixture, "google-1").await;
    let state = start_and_capture_state(&fixture).await;
    let query = format!("code=abc&state={state}");

    let first =
        fixture.app.clone().oneshot_get_with_cookie("/api/auth/google/callback", &query, "").await;
    assert!(first.headers().get(header::LOCATION).unwrap().to_str().unwrap().contains("social=ok"));

    let replay =
        fixture.app.clone().oneshot_get_with_cookie("/api/auth/google/callback", &query, "").await;
    let location = replay.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.ends_with("social_error=invalid_state"), "{location}");
}

#[tokio::test]
async fn expired_state_is_rejected() {
    let fixture = fixture(Some(identity("google-1"))).await;
    seed_bound_user(&fixture, "google-1").await;
    let state = start_and_capture_state(&fixture).await;

    fixture.clock.advance(chrono::TimeDelta::minutes(10) + chrono::TimeDelta::seconds(1));
    let response = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/auth/google/callback",
            &format!("code=abc&state={state}"),
            "",
        )
        .await;

    let location = response.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.ends_with("social_error=expired"), "{location}");
}

#[tokio::test]
async fn missing_query_is_invalid_request() {
    let fixture = fixture(Some(identity("google-1"))).await;

    let response =
        fixture.app.clone().oneshot_get_with_cookie("/api/auth/google/callback", "", "").await;

    let location = response.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.ends_with("social_error=invalid_request"), "{location}");
}
