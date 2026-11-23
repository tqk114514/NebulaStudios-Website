//! 对抗性安全审查：TOTP / 恢复码 / 社交绑定与登录 / 管理后台授权与审计。
//!
//! 每个 `sec_tsa_*` 断言一条本应成立的安全性质；断言失败即代表可利用缺陷。
//! 仅使用测试装配与会话 cookie，不触外网；社交 provider 用本地假客户端或
//! 127.0.0.1 mock 替换。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashMap;
use std::sync::Arc;

use async_trait::async_trait;
use axum::Json;
use axum::Router;
use axum::extract::State as AxumState;
use axum::http::{StatusCode, header};
use axum::routing::post;
use base64::Engine as _;
use base64::engine::general_purpose::{STANDARD as BASE64, URL_SAFE_NO_PAD};
use chrono::{DateTime, TimeDelta, Utc};
use ed25519_dalek::pkcs8::EncodePublicKey as _;
use ed25519_dalek::{Signer as _, SigningKey};
use nebula_server::api::{self, AppState};
use nebula_server::context::{Clock, ManualClock};
use nebula_server::model::audit::{AuditAction, NewAuditEntry};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::social::{NewSocialIdentity, SocialProvider};
use nebula_server::model::tokens::{VerifiedAccessToken, verify_access};
use nebula_server::model::totp::{TOTP_ALGORITHM, TOTP_DIGITS, TOTP_PERIOD, TOTP_SKEW, TotpState};
use nebula_server::model::user::{Ban, User, UserId};
use nebula_server::social::google::{GoogleClient, GoogleConfig};
use nebula_server::social::{ProviderIdentity, SocialClients, SocialError, SocialProviderClient};
use serde_json::{Value, json};
use tempfile::TempDir;

use common::{
    Mailbox, OneshotPost, Session, body_text, cookie_header, login_as, register_user, set_cookie_of,
};

const PASSWORD: &str = "Nebula!Passw0rd-2026";
const WRONG_PASSWORD: &str = "Nebula!Passw0rd-2027";

// ---------------------------------------------------------------------------
// 装配
// ---------------------------------------------------------------------------

struct Fixture {
    app: Router,
    state: AppState,
    clock: Arc<ManualClock>,
    mailbox: Mailbox,
    _dir: TempDir,
}

fn assemble(state: AppState, clock: Arc<ManualClock>, mailbox: Mailbox, dir: TempDir) -> Fixture {
    let app = api::router(state.clone());
    Fixture { app, state, clock, mailbox, _dir: dir }
}

/// 所有测试共用的装配；`social` 非空时同时启用 Google 与 Microsoft 假客户端。
async fn fixture(admin_emails: &[&str], social: Option<ProviderIdentity>) -> Fixture {
    let (mut state, clock, mailbox, dir) = common::app_state().await;
    state.admin_emails = Arc::new(admin_emails.iter().map(|email| (*email).to_owned()).collect());
    if let Some(identity) = social {
        state.social_clients = Arc::new(SocialClients::new(vec![
            Arc::new(FakeClient { provider: SocialProvider::Google, identity: identity.clone() }),
            Arc::new(FakeClient { provider: SocialProvider::Microsoft, identity }),
        ]));
    }
    assemble(state, clock, mailbox, dir)
}

struct FakeClient {
    provider: SocialProvider,
    identity: ProviderIdentity,
}

#[async_trait]
impl SocialProviderClient for FakeClient {
    fn provider(&self) -> SocialProvider {
        self.provider
    }

    fn authorize_url(&self, state: &str, code_challenge: &str) -> String {
        format!(
            "https://{}.provider.test/authorize?state={state}&code_challenge={code_challenge}",
            self.provider.as_str()
        )
    }

    async fn fetch_identity(
        &self,
        _code: &str,
        _code_verifier: &str,
    ) -> Result<ProviderIdentity, SocialError> {
        Ok(self.identity.clone())
    }
}

fn identity(provider_user_id: &str) -> ProviderIdentity {
    ProviderIdentity {
        provider_user_id: provider_user_id.to_owned(),
        display_name: Some("Nova".to_owned()),
    }
}

// ---------------------------------------------------------------------------
// 通用助手
// ---------------------------------------------------------------------------

fn fixed_now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339(common::FIX_NOW)
        .expect("固定时钟字面量必须可解析")
        .with_timezone(&Utc)
}

async fn register_verified(app: &Router, mailbox: &Mailbox, email: &str, username: &str) {
    register_user(app, email, username, PASSWORD).await;
    let message = common::await_mail_for(mailbox, email).await;
    let code = common::extract_code(&message);
    let body = json!({ "email": email, "code": code }).to_string();
    let response = app.clone().oneshot_post("/api/auth/register/verify", &body).await;
    assert_eq!(response.status(), StatusCode::NO_CONTENT, "邮箱验证助手必须成功: {email}");
}

fn totp_code(secret: &str, now: DateTime<Utc>) -> String {
    let parsed = totp_rs::Secret::try_from_base32(secret).expect("setup 返回的密钥必须可解析");
    let totp = totp_rs::Builder::new()
        .with_algorithm(TOTP_ALGORITHM)
        .with_digits(TOTP_DIGITS)
        .with_skew(u16::from(TOTP_SKEW))
        .with_step_duration(TOTP_PERIOD)
        .with_secret(parsed)
        .build()
        .expect("TOTP 参数合法");
    totp.generate(now.timestamp().cast_unsigned()).to_string()
}

/// 启用已消费当前时间片；回放保护下后续操作必须使用下一片（仍在 ±1 窗口内）。
fn totp_code_next(secret: &str, now: DateTime<Utc>) -> String {
    totp_code(secret, now + TimeDelta::seconds(30))
}

fn wrong_totp(code: &str) -> String {
    let mut chars: Vec<char> = code.chars().collect();
    chars[0] = if chars[0] == '0' { '1' } else { '0' };
    chars.into_iter().collect()
}

fn parse_json(text: &str) -> Value {
    serde_json::from_str(text).expect("响应体必须是 JSON")
}

async fn response_json(response: axum::response::Response) -> Value {
    parse_json(&body_text(response).await)
}

async fn post_auth(
    app: &Router,
    uri: &str,
    body: &str,
    session: &Session,
) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            uri,
            body,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

// 管理端测试需要额外的一个管理员会话；用显式封装而不是把 Session 塞进通用 Fixture。
struct AdminFixture {
    base: Fixture,
    admin: Session,
}

impl AdminFixture {
    fn cookie(&self) -> String {
        cookie_header(Some(&self.admin.access), None, Some(&self.admin.csrf))
    }

    async fn get(&self, uri: &str, query: &str) -> axum::response::Response {
        self.base.app.clone().oneshot_get_with_cookie(uri, query, &self.cookie()).await
    }
}

async fn admin_fixture(
    admin_emails: &[&str],
    admin_email: &str,
    admin_username: &str,
) -> AdminFixture {
    let base = fixture(admin_emails, None).await;
    register_verified(&base.app, &base.mailbox, admin_email, admin_username).await;
    let admin = login_as(&base.app, admin_email, PASSWORD).await;
    AdminFixture { base, admin }
}

async fn setup_totp(app: &Router, session: &Session) -> (String, Option<String>) {
    let response = post_auth(
        app,
        "/api/user/totp/setup",
        &json!({ "password": PASSWORD }).to_string(),
        session,
    )
    .await;
    assert_eq!(response.status(), StatusCode::OK, "TOTP setup 必须成功");
    let cache_control = response
        .headers()
        .get(header::CACHE_CONTROL)
        .and_then(|value| value.to_str().ok())
        .map(str::to_owned);
    let body = response_json(response).await;
    let secret = body["secret"].as_str().expect("setup 必须返回 secret").to_owned();
    (secret, cache_control)
}

async fn enable_totp(
    app: &Router,
    session: &Session,
    secret: &str,
    now: DateTime<Utc>,
) -> (Session, Vec<String>, Option<String>) {
    let code = totp_code(secret, now);
    let response = post_auth(
        app,
        "/api/user/totp/enable",
        &json!({ "password": PASSWORD, "code": code }).to_string(),
        session,
    )
    .await;
    assert_eq!(response.status(), StatusCode::OK, "TOTP enable 必须成功");
    let cache_control = response
        .headers()
        .get(header::CACHE_CONTROL)
        .and_then(|value| value.to_str().ok())
        .map(str::to_owned);
    // enable 会撤销旧会话并重签当前设备；后续请求必须使用新 cookie
    let session = Session {
        access: set_cookie_of(&response, "nebula_access").expect("enable 必须重签 access"),
        refresh: set_cookie_of(&response, "nebula_refresh").expect("enable 必须重签 refresh"),
        csrf: set_cookie_of(&response, "nebula_csrf").expect("enable 必须重签 csrf"),
    };
    let body = response_json(response).await;
    let codes = body["recovery_codes"]
        .as_array()
        .expect("enable 必须返回恢复码")
        .iter()
        .map(|value| value.as_str().expect("恢复码必须是字符串").to_owned())
        .collect();
    (session, codes, cache_control)
}

async fn enable_full_totp(
    fixture: &Fixture,
    email: &str,
    username: &str,
) -> (Session, Vec<String>, String) {
    register_verified(&fixture.app, &fixture.mailbox, email, username).await;
    let session = login_as(&fixture.app, email, PASSWORD).await;
    let (secret, _) = setup_totp(&fixture.app, &session).await;
    let (session, codes, _) =
        enable_totp(&fixture.app, &session, &secret, fixture.clock.now()).await;
    (session, codes, secret)
}

async fn login_challenge(app: &Router, email: &str) -> String {
    let body = json!({ "identifier": email, "password": PASSWORD }).to_string();
    let response = app.clone().oneshot_post("/api/auth/login", &body).await;
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED, "启用 TOTP 后密码登录必须先发挑战");
    let body = response_json(response).await;
    assert_eq!(body["code"], "totp_required");
    body["details"]["challenge_token"].as_str().expect("挑战响应必须带 challenge_token").to_owned()
}

async fn submit_totp(app: &Router, challenge_token: &str, code: &str) -> axum::response::Response {
    let body = json!({ "challenge_token": challenge_token, "code": code }).to_string();
    app.clone().oneshot_post("/api/auth/login/totp", &body).await
}

fn location_of(response: &axum::response::Response) -> String {
    response
        .headers()
        .get(header::LOCATION)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .to_owned()
}

async fn start_social_state(fixture: &Fixture, provider: &str) -> (String, String) {
    let response =
        fixture.app.clone().oneshot_get_with_cookie(&format!("/api/auth/{provider}"), "", "").await;
    assert_eq!(response.status(), StatusCode::FOUND, "起授权必须 302");
    let cookie = set_cookie_of(&response, nebula_server::api::SOCIAL_STATE_COOKIE)
        .expect("起授权必须写 state cookie");
    let location = location_of(&response);
    let state = location
        .split("state=")
        .nth(1)
        .expect("授权地址必须带 state")
        .split('&')
        .next()
        .expect("state 必须以 & 结束")
        .to_owned();
    (state, format!("{}={cookie}", nebula_server::api::SOCIAL_STATE_COOKIE))
}

async fn social_callback(
    fixture: &Fixture,
    provider: &str,
    code: &str,
    state: &str,
    cookie: &str,
) -> axum::response::Response {
    fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            &format!("/api/auth/{provider}/callback"),
            &format!("code={code}&state={state}"),
            cookie,
        )
        .await
}

async fn seed_user(fixture: &Fixture, username: &str, email: &str) -> UserId {
    let user_id = UserId::new();
    let user = User {
        id: user_id,
        username: username.to_owned(),
        email: email.to_owned(),
        created_at: fixture.clock.now(),
        email_verified: true,
        avatar_version: 0,
    };
    let hash = PasswordHash::hash(PASSWORD).expect("测试口令哈希必须成功");
    fixture.state.user_repo.insert(&user, &hash).await.expect("种子用户必须可插入");
    user_id
}

async fn link_identity(fixture: &Fixture, user_id: UserId, provider: SocialProvider, sub: &str) {
    fixture
        .state
        .social_repo
        .link_identity(
            &NewSocialIdentity {
                user_id,
                provider,
                provider_user_id: sub.to_owned(),
                display_name: Some("Nova".to_owned()),
            },
            fixture.clock.now(),
        )
        .await
        .expect("绑定种子身份必须成功");
}

// ---------------------------------------------------------------------------
// 漏洞复现
// ---------------------------------------------------------------------------

/// [HIGH] 启用 TOTP 的账户仍可通过社交登录直接取得会话，第二因子被整体绕过。
#[tokio::test]
async fn sec_tsa_1_social_login_bypasses_totp_second_factor() {
    let fixture = fixture(&[], Some(identity("google-totp-sub"))).await;
    let (_session, _codes, _secret) =
        enable_full_totp(&fixture, "totp@nebula.st", "totpuser").await;
    let user = fixture
        .state
        .user_repo
        .find_by_email("totp@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");
    link_identity(&fixture, user.id, SocialProvider::Google, "google-totp-sub").await;

    // 对照：密码路径确实要求二段。
    let login_response = fixture
        .app
        .clone()
        .oneshot_post(
            "/api/auth/login",
            &json!({ "identifier": "totp@nebula.st", "password": PASSWORD }).to_string(),
        )
        .await;
    assert_eq!(login_response.status(), StatusCode::UNAUTHORIZED);
    let login_body = response_json(login_response).await;
    assert_eq!(login_body["code"], "totp_required", "密码登录必须先要求 TOTP");

    let (state, cookie) = start_social_state(&fixture, "google").await;
    let response = social_callback(&fixture, "google", "code", &state, &cookie).await;
    let location = location_of(&response);
    let access = set_cookie_of(&response, "nebula_access");
    assert!(
        access.is_none(),
        "启用 TOTP 后社交登录仍直接签发 access（第二因子被绕过）: location={location}, access={access:?}"
    );
}

/// [MEDIUM] 同一个 TOTP 码可以完成多个登录挑战，码被重放。
#[tokio::test]
async fn sec_tsa_2_totp_code_replay_across_challenges() {
    let fixture = fixture(&[], None).await;
    let (_session, _codes, secret) =
        enable_full_totp(&fixture, "replay@nebula.st", "replayuser").await;

    let first_challenge = login_challenge(&fixture.app, "replay@nebula.st").await;
    let second_challenge = login_challenge(&fixture.app, "replay@nebula.st").await;
    // 启用已消费当前片；首登用下一片，第二次挑战重放同一片必须被拒
    let code = totp_code_next(&secret, fixture.clock.now());

    let first = submit_totp(&fixture.app, &first_challenge, &code).await;
    assert_eq!(first.status(), StatusCode::OK, "首个挑战应接受当前码");
    let replay = submit_totp(&fixture.app, &second_challenge, &code).await;
    assert_eq!(
        replay.status(),
        StatusCode::UNAUTHORIZED,
        "同一 TOTP 码被第二个登录挑战再次接受（TOTP 可重放）"
    );
}

/// [MEDIUM] 抢注未验证的管理员邮箱：账号被显示为管理员且无法被封禁，
/// 真正的管理员也无法再用该邮箱注册。
#[tokio::test]
async fn sec_tsa_3_unverified_admin_squatter_is_privileged_and_unbannable() {
    let fixture =
        admin_fixture(&["root@nebula.st", "admin@nebula.st"], "root@nebula.st", "root").await;

    // 攻击者在真实管理员注册前抢占配置中的管理员邮箱，且不验证邮箱。
    register_user(&fixture.base.app, "admin@nebula.st", "squatter", PASSWORD).await;
    let squatter = login_as(&fixture.base.app, "admin@nebula.st", PASSWORD).await;
    let squatter_cookie = cookie_header(Some(&squatter.access), None, Some(&squatter.csrf));

    // 未验证邮箱不获得管理权（这一层成立）。
    let stats = fixture
        .base
        .app
        .clone()
        .oneshot_get_with_cookie("/api/admin/stats", "", &squatter_cookie)
        .await;
    assert_eq!(stats.status(), StatusCode::FORBIDDEN, "未验证邮箱不得进入管理面");

    let listed = fixture.get("/api/admin/users", "query=admin").await;
    assert_eq!(listed.status(), StatusCode::OK);
    let listed_body = response_json(listed).await;
    let squatter_row = listed_body["users"]
        .as_array()
        .expect("users 必须是数组")
        .iter()
        .find(|row| row["email"] == "admin@nebula.st")
        .expect("抢注账号必须出现在用户列表");
    let listed_is_admin = squatter_row["is_admin"].as_bool().expect("is_admin 必须是布尔");

    let squatter_id = fixture
        .base
        .state
        .user_repo
        .find_by_email("admin@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("抢注账号必须存在")
        .id;
    let ban = fixture
        .base
        .app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{squatter_id}/ban"),
            &json!({ "kind": "permanent", "reason": "squatting" }).to_string(),
            &fixture.cookie(),
            Some(&fixture.admin.csrf),
        )
        .await;
    let ban_status = ban.status();

    // 真实管理员可重新注册该邮箱：未验证抢注账号被释放（已验证账号仍不可接管）。
    let retake = fixture
        .base
        .app
        .clone()
        .oneshot_post(
            "/api/auth/register",
            &json!({
                "email": "admin@nebula.st",
                "username": "realroot",
                "password": PASSWORD,
            })
            .to_string(),
        )
        .await;
    assert_eq!(retake.status(), StatusCode::CREATED, "未验证抢注账号必须可被真实所有者释放");

    assert!(
        !listed_is_admin && ban_status == StatusCode::NO_CONTENT,
        "未验证邮箱的抢注者不得列为管理员(is_admin={listed_is_admin})且必须可被封禁(ban={ban_status})"
    );
}

/// [MEDIUM] 社交登录的 state 不绑定发起者浏览器：另一个已登录账户的浏览器
/// 完成回调后会被切换到攻击者账户（登录 CSRF / 会话固定式切换）。
#[tokio::test]
async fn sec_tsa_4_social_login_state_not_bound_to_browser() {
    let fixture = fixture(&[], Some(identity("attacker-sub"))).await;
    let attacker = seed_user(&fixture, "attacker", "attacker@nebula.st").await;
    link_identity(&fixture, attacker, SocialProvider::Google, "attacker-sub").await;
    seed_user(&fixture, "victim", "victim@nebula.st").await;
    let victim = login_as(&fixture.app, "victim@nebula.st", PASSWORD).await;

    // 攻击者在自己的浏览器起授权，但不完成；把回调地址交给受害者浏览器。
    let (state, _attacker_cookie) = start_social_state(&fixture, "google").await;
    let response = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/auth/google/callback",
            &format!("code=attacker-code&state={state}"),
            &cookie_header(Some(&victim.access), Some(&victim.refresh), Some(&victim.csrf)),
        )
        .await;
    let location = location_of(&response);
    let access = set_cookie_of(&response, "nebula_access");
    let owner = access.as_deref().and_then(|token| {
        match verify_access(token, fixture.state.jwt.decode(), fixture.clock.now()) {
            Ok(VerifiedAccessToken::UserSession { user_id, .. }) => Some(user_id),
            _ => None,
        }
    });
    assert!(
        access.is_none(),
        "回调未绑定发起者浏览器：已登录受害者被切换到攻击者账户 {attacker}，签发 access={access:?}，owner={owner:?}，location={location}"
    );
}

/// [LOW] 含 TOTP 明文密钥与恢复码的响应没有 `Cache-Control: no-store`。
#[tokio::test]
async fn sec_tsa_5_totp_secret_responses_missing_no_store() {
    let fixture = fixture(&[], None).await;
    register_verified(&fixture.app, &fixture.mailbox, "cache@nebula.st", "cacheuser").await;
    let session = login_as(&fixture.app, "cache@nebula.st", PASSWORD).await;

    let (secret, setup_cache) = setup_totp(&fixture.app, &session).await;
    let (_session, _codes, enable_cache) =
        enable_totp(&fixture.app, &session, &secret, fixture.clock.now()).await;

    assert_eq!(
        (setup_cache.as_deref(), enable_cache.as_deref()),
        (Some("no-store"), Some("no-store")),
        "setup（含 secret）与 enable（含恢复码）响应必须禁止缓存: setup={setup_cache:?}, enable={enable_cache:?}"
    );
}

/// [MEDIUM] 启用 TOTP 不会撤销启用前签发的会话：被盗的旧 access/refresh
/// 在二段开启后仍可继续使用，第二因子对既有会话不生效。
#[tokio::test]
async fn sec_tsa_15_pre_totp_sessions_survive_enable() {
    let fixture = fixture(&[], None).await;
    register_verified(&fixture.app, &fixture.mailbox, "stale@nebula.st", "staleuser").await;
    let enrolling = login_as(&fixture.app, "stale@nebula.st", PASSWORD).await;
    let stale = login_as(&fixture.app, "stale@nebula.st", PASSWORD).await;

    let (secret, _) = setup_totp(&fixture.app, &enrolling).await;
    let (_session, _codes, _) =
        enable_totp(&fixture.app, &enrolling, &secret, fixture.clock.now()).await;

    let profile = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/user",
            "",
            &cookie_header(Some(&stale.access), None, Some(&stale.csrf)),
        )
        .await;
    let profile_status = profile.status();

    let refresh = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&stale.refresh), Some(&stale.csrf)),
            Some(&stale.csrf),
        )
        .await;
    let refresh_status = refresh.status();

    assert!(
        profile_status == StatusCode::UNAUTHORIZED && refresh_status == StatusCode::UNAUTHORIZED,
        "启用 TOTP 后，启用前签发的 access 仍可用(profile={profile_status})、refresh 仍可轮转(refresh={refresh_status})，被盗旧会话绕过第二因子"
    );
}

// ---------------------------------------------------------------------------
// 正向验证（断言安全性质成立）
// ---------------------------------------------------------------------------

/// enable 只接受本账户 pending 密钥下的码，不能借用他人密钥。
#[tokio::test]
async fn sec_tsa_6_enable_rejects_foreign_totp_code() {
    let fixture = fixture(&[], None).await;

    register_verified(&fixture.app, &fixture.mailbox, "alice@nebula.st", "alice").await;
    let alice = login_as(&fixture.app, "alice@nebula.st", PASSWORD).await;
    let (alice_secret, _) = setup_totp(&fixture.app, &alice).await;

    register_verified(&fixture.app, &fixture.mailbox, "bob@nebula.st", "bob").await;
    let bob = login_as(&fixture.app, "bob@nebula.st", PASSWORD).await;
    let (bob_secret, _) = setup_totp(&fixture.app, &bob).await;
    assert_ne!(alice_secret, bob_secret, "两个 pending 密钥必须不同");

    let foreign_code = totp_code(&alice_secret, fixture.clock.now());
    let rejected = post_auth(
        &fixture.app,
        "/api/user/totp/enable",
        &json!({ "password": PASSWORD, "code": foreign_code }).to_string(),
        &bob,
    )
    .await;
    assert_eq!(rejected.status(), StatusCode::UNAUTHORIZED, "不得用他人密钥的码启用");
    assert_eq!(response_json(rejected).await["code"], "totp_invalid");

    let own_code = totp_code(&bob_secret, fixture.clock.now());
    let accepted = post_auth(
        &fixture.app,
        "/api/user/totp/enable",
        &json!({ "password": PASSWORD, "code": own_code }).to_string(),
        &bob,
    )
    .await;
    assert_eq!(accepted.status(), StatusCode::OK, "失败后仍应能用本账户的码完成启用");
}

/// 恢复码并发消费只允许一个胜利者，且用后不可再次登录。
#[tokio::test]
async fn sec_tsa_7_recovery_code_concurrent_single_use() {
    let fixture = fixture(&[], None).await;
    let (_session, codes, _secret) =
        enable_full_totp(&fixture, "recovery@nebula.st", "recoveryuser").await;
    let code = codes[0].clone();

    let first_challenge = login_challenge(&fixture.app, "recovery@nebula.st").await;
    let second_challenge = login_challenge(&fixture.app, "recovery@nebula.st").await;
    let first_body = json!({ "challenge_token": first_challenge, "code": code }).to_string();
    let second_body = json!({ "challenge_token": second_challenge, "code": code }).to_string();
    let first = fixture.app.clone().oneshot_post("/api/auth/login/totp", &first_body);
    let second = fixture.app.clone().oneshot_post("/api/auth/login/totp", &second_body);
    let (first, second) = tokio::join!(first, second);
    let successes = [first.status(), second.status()]
        .iter()
        .filter(|status| **status == StatusCode::OK)
        .count();
    assert_eq!(successes, 1, "同一恢复码并发消费必须恰好成功一次");

    let third_challenge = login_challenge(&fixture.app, "recovery@nebula.st").await;
    let replay = submit_totp(&fixture.app, &third_challenge, &code).await;
    assert_eq!(replay.status(), StatusCode::UNAUTHORIZED, "已消费的恢复码不得再次登录");
}

/// 关闭 TOTP 需要口令 + 第二因素，并撤销全部会话。
#[tokio::test]
async fn sec_tsa_8_disable_requires_both_factors_and_revokes() {
    let fixture = fixture(&[], None).await;
    let (session, _codes, secret) =
        enable_full_totp(&fixture, "disable@nebula.st", "disableuser").await;
    fixture.clock.advance(TimeDelta::seconds(60));

    let good_code = totp_code(&secret, fixture.clock.now());
    let wrong_code = wrong_totp(&good_code);

    let no_second_factor = post_auth(
        &fixture.app,
        "/api/user/totp/disable",
        &json!({ "password": PASSWORD, "code": wrong_code }).to_string(),
        &session,
    )
    .await;
    assert_eq!(no_second_factor.status(), StatusCode::UNAUTHORIZED, "错码不得关闭 TOTP");

    let no_password = post_auth(
        &fixture.app,
        "/api/user/totp/disable",
        &json!({ "password": WRONG_PASSWORD, "code": good_code }).to_string(),
        &session,
    )
    .await;
    assert_eq!(no_password.status(), StatusCode::UNAUTHORIZED, "错口令不得关闭 TOTP");
    assert_eq!(response_json(no_password).await["code"], "invalid_credentials");

    let missing_code = post_auth(
        &fixture.app,
        "/api/user/totp/disable",
        &json!({ "password": PASSWORD, "code": "" }).to_string(),
        &session,
    )
    .await;
    assert_eq!(missing_code.status(), StatusCode::UNPROCESSABLE_ENTITY, "空第二因素必须拒绝");

    let user = fixture
        .state
        .user_repo
        .find_by_email("disable@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");
    assert_eq!(
        fixture.state.totp_repo.find(&user.id).await.expect("查询必须成功").map(|(state, _)| state),
        Some(TotpState::Enabled),
        "失败的关闭尝试不得改变状态"
    );

    let disabled = post_auth(
        &fixture.app,
        "/api/user/totp/disable",
        &json!({ "password": PASSWORD, "code": good_code }).to_string(),
        &session,
    )
    .await;
    assert_eq!(disabled.status(), StatusCode::NO_CONTENT, "双因素正确时必须关闭");
    assert!(common::raw_set_cookie(&disabled, "nebula_access").contains("Max-Age=0"));
    assert_eq!(
        fixture.state.totp_repo.find(&user.id).await.expect("查询必须成功").map(|(state, _)| state),
        Some(TotpState::Disabled)
    );

    // 关闭即撤销：旧 refresh 立即失效。
    let refresh = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&session.refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(refresh.status(), StatusCode::UNAUTHORIZED, "关闭 TOTP 必须撤销旧 refresh");

    // 降级后密码登录不再需要二段。
    let plain_login = fixture
        .app
        .clone()
        .oneshot_post(
            "/api/auth/login",
            &json!({ "identifier": "disable@nebula.st", "password": PASSWORD }).to_string(),
        )
        .await;
    assert_eq!(plain_login.status(), StatusCode::OK, "关闭后密码登录应直接成功");
}

/// 审计接口仅管理员可读；封禁理由作为参数存储，不触发 SQL 注入；
/// 审计响应不包含口令、TOTP 密钥或恢复码。
#[tokio::test]
async fn sec_tsa_9_admin_audit_resists_injection_and_omits_secrets() {
    let fixture = admin_fixture(&["admin@nebula.st"], "admin@nebula.st", "admin").await;

    let (normal_session, codes, secret) =
        enable_full_totp(&fixture.base, "target@nebula.st", "target").await;
    let normal_cookie =
        cookie_header(Some(&normal_session.access), None, Some(&normal_session.csrf));
    let totp_code_used = totp_code(&secret, fixture.base.clock.now());

    let forbidden = fixture
        .base
        .app
        .clone()
        .oneshot_get_with_cookie("/api/admin/audit", "", &normal_cookie)
        .await;
    assert_eq!(forbidden.status(), StatusCode::FORBIDDEN, "普通用户不得读审计");

    let target = fixture
        .base
        .state
        .user_repo
        .find_by_email("target@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("目标必须存在")
        .id;
    let reason = "</script><script>alert(1)</script>'; DROP TABLE audit_logs;--";
    let banned = fixture
        .base
        .app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{target}/ban"),
            &json!({ "kind": "permanent", "reason": reason }).to_string(),
            &fixture.cookie(),
            Some(&fixture.admin.csrf),
        )
        .await;
    assert_eq!(banned.status(), StatusCode::NO_CONTENT);

    let audit = fixture.get("/api/admin/audit", "limit=100").await;
    assert_eq!(audit.status(), StatusCode::OK);
    let audit_text = body_text(audit).await;
    let audit_body = parse_json(&audit_text);

    let entry = audit_body["entries"]
        .as_array()
        .expect("entries 必须是数组")
        .iter()
        .find(|entry| entry["action"] == "admin.user.ban")
        .expect("封禁必须留痕");
    assert_eq!(entry["details"]["reason"], reason, "审计必须原样保留理由文本（参数化写入）");
    let target_text = target.to_string();
    assert_eq!(entry["target_id"], target_text, "审计目标必须是被封禁用户");

    for sensitive in [PASSWORD, secret.as_str(), codes[0].as_str(), totp_code_used.as_str()] {
        assert!(!audit_text.contains(sensitive), "审计响应泄漏敏感值 {sensitive}: {audit_text}");
    }

    // 表仍在，注入没有生效。
    let after = fixture.get("/api/admin/audit", "limit=1").await;
    assert_eq!(after.status(), StatusCode::OK, "审计表必须仍然存在");
}

/// 解绑只删除当前用户自己的绑定，不能跨用户删除；写操作必须过 CSRF。
#[tokio::test]
async fn sec_tsa_10_social_unlink_scoped_to_owner() {
    let fixture = fixture(&[], Some(identity("seed-sub"))).await;
    let alice = seed_user(&fixture, "alice", "alice@nebula.st").await;
    let bob = seed_user(&fixture, "bob", "bob@nebula.st").await;
    link_identity(&fixture, alice, SocialProvider::Google, "alice-sub").await;
    link_identity(&fixture, bob, SocialProvider::Google, "bob-sub").await;
    let session = login_as(&fixture.app, "alice@nebula.st", PASSWORD).await;
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));

    let without_csrf = fixture
        .app
        .clone()
        .oneshot_delete_with_cookie("/api/user/social/google", &cookie, None)
        .await;
    assert_eq!(without_csrf.status(), StatusCode::FORBIDDEN, "解绑必须过 CSRF");

    let removed = fixture
        .app
        .clone()
        .oneshot_delete_with_cookie("/api/user/social/google", &cookie, Some(&session.csrf))
        .await;
    assert_eq!(removed.status(), StatusCode::NO_CONTENT);
    assert_eq!(fixture.state.social_repo.list_for_user(&alice).await.unwrap().len(), 0);
    let bob_bindings = fixture.state.social_repo.list_for_user(&bob).await.unwrap();
    assert_eq!(bob_bindings.len(), 1, "不得删除其他用户的绑定");
    assert_eq!(bob_bindings[0].provider_user_id, "bob-sub");
}

/// 被封禁账户即使有已绑定社交身份也不能通过社交回调取得会话。
#[tokio::test]
async fn sec_tsa_11_social_login_rejects_banned_account() {
    let fixture = fixture(&[], Some(identity("banned-sub"))).await;
    let user_id = seed_user(&fixture, "banned", "banned@nebula.st").await;
    link_identity(&fixture, user_id, SocialProvider::Google, "banned-sub").await;
    fixture
        .state
        .admin_repo
        .ban_user(
            &user_id,
            &Ban::Permanent,
            "abuse",
            &NewAuditEntry {
                actor_id: UserId::new(),
                action: AuditAction::AdminUserBan,
                target_id: Some(user_id),
                details: None,
            },
            fixture.clock.now(),
        )
        .await
        .expect("封禁必须成功");

    let (state, cookie) = start_social_state(&fixture, "google").await;
    let response = social_callback(&fixture, "google", "code", &state, &cookie).await;
    let location = location_of(&response);
    assert!(
        location.ends_with("social_error=account_unavailable"),
        "封禁账户必须被拒绝: {location}"
    );
    assert!(set_cookie_of(&response, "nebula_access").is_none(), "封禁账户不得取得会话");
}

/// 用 Google 的 state 走 Microsoft 回调必须被拒绝；state 单次消费。
#[tokio::test]
async fn sec_tsa_12_social_cross_provider_state_rejected() {
    let fixture = fixture(&[], Some(identity("cross-sub"))).await;
    let user_id = seed_user(&fixture, "cross", "cross@nebula.st").await;
    link_identity(&fixture, user_id, SocialProvider::Google, "cross-sub").await;

    let (state, cookie) = start_social_state(&fixture, "google").await;
    let mismatched = social_callback(&fixture, "microsoft", "code", &state, &cookie).await;
    let mismatched_location = location_of(&mismatched);
    assert!(
        mismatched_location.ends_with("social_error=invalid_state"),
        "provider 不匹配必须按无效 state 拒绝: {mismatched_location}"
    );
    assert!(set_cookie_of(&mismatched, "nebula_access").is_none());

    // state 已被消费，回到正确 provider 也不能重放。
    let replayed = social_callback(&fixture, "google", "code", &state, &cookie).await;
    let replayed_location = location_of(&replayed);
    assert!(
        replayed_location.ends_with("social_error=invalid_state"),
        "同一 state 不得重放: {replayed_location}"
    );
    assert!(set_cookie_of(&replayed, "nebula_access").is_none());
}

/// TOTP 接受窗口向前恰为 1 个时间片；更远的码被拒绝（回放保护另行禁止旧片）。
#[tokio::test]
async fn sec_tsa_13_totp_skew_window_is_bounded() {
    let fixture = fixture(&[], None).await;
    let (_session, _codes, secret) = enable_full_totp(&fixture, "skew@nebula.st", "skewuser").await;

    let next_step = totp_code_next(&secret, fixture.clock.now());
    let accepted_challenge = login_challenge(&fixture.app, "skew@nebula.st").await;
    let accepted = submit_totp(&fixture.app, &accepted_challenge, &next_step).await;
    assert_eq!(accepted.status(), StatusCode::OK, "±1 时间片的下一片码按 TOTP_SKEW=1 应被接受");

    let two_steps_ahead = totp_code(&secret, fixture.clock.now() + TimeDelta::seconds(60));
    let rejected_challenge = login_challenge(&fixture.app, "skew@nebula.st").await;
    let rejected = submit_totp(&fixture.app, &rejected_challenge, &two_steps_ahead).await;
    assert_eq!(rejected.status(), StatusCode::UNAUTHORIZED, "窗口外的码必须被拒绝");
}

/// 端到端校验 Worker 背书：篡改数据、过期时间戳、空 sub 一律不签发会话；
/// 合法背书被接受并进入绑定查询。
#[tokio::test]
async fn sec_tsa_14_worker_envelope_end_to_end_validation() {
    let signing = SigningKey::from_bytes(&[7u8; 32]);
    let pem = public_key_pem(&signing);
    let now = fixed_now();
    let good_data = json!({
        "access_token": "worker-access-token",
        "id_token": fake_id_token("worker-sub"),
    })
    .to_string();
    let empty_sub_data = json!({
        "access_token": "worker-access-token",
        "id_token": fake_id_token(""),
    })
    .to_string();

    let listener =
        tokio::net::TcpListener::bind(("127.0.0.1", 0)).await.expect("mock Worker 端口必须可监听");
    let addr = listener.local_addr().expect("mock 地址必须可读");
    let mock = WorkerMock { signing: Arc::new(signing), now, good_data, empty_sub_data };
    let mock_app = Router::new().route("/token", post(worker_responder)).with_state(mock);
    tokio::spawn(async move {
        axum::serve(listener, mock_app).await.expect("mock Worker 不得退出");
    });

    let (mut state, clock, mailbox, dir) = common::app_state().await;
    let config = GoogleConfig {
        client_id: "google-client".to_owned(),
        client_secret: "google-secret".to_owned(),
        redirect_uri: "http://127.0.0.1/callback".to_owned(),
        proxy_url: format!("http://{addr}"),
        proxy_access_client_id: "cf-id".to_owned(),
        proxy_access_client_secret: "cf-secret".to_owned(),
        worker_signing_public_key: pem,
    };
    let client = GoogleClient::new(config, state.clock.clone()).expect("Google 客户端必须可构建");
    state.social_clients = Arc::new(SocialClients::new(vec![Arc::new(client)]));
    let fixture = assemble(state, clock, mailbox, dir);

    for code in ["tampered", "stale", "empty"] {
        let (state, cookie) = start_social_state(&fixture, "google").await;
        let response = social_callback(&fixture, "google", code, &state, &cookie).await;
        let location = location_of(&response);
        assert!(
            location.ends_with("social_error=provider_error"),
            "code={code} 的非法 Worker 背书必须被拒绝: {location}"
        );
        assert!(set_cookie_of(&response, "nebula_access").is_none(), "code={code} 不得签发会话");
    }

    let (state, cookie) = start_social_state(&fixture, "google").await;
    let response = social_callback(&fixture, "google", "good", &state, &cookie).await;
    let location = location_of(&response);
    assert!(
        location.ends_with("social_error=not_linked"),
        "合法背书必须被接受并进入绑定查询: {location}"
    );
    assert!(set_cookie_of(&response, "nebula_access").is_none(), "未绑定身份不得签发会话");
}

// ---------------------------------------------------------------------------
// Worker mock
// ---------------------------------------------------------------------------

#[derive(Clone)]
struct WorkerMock {
    signing: Arc<SigningKey>,
    now: DateTime<Utc>,
    good_data: String,
    empty_sub_data: String,
}

async fn worker_responder(AxumState(mock): AxumState<WorkerMock>, body: String) -> Json<Value> {
    let form: HashMap<String, String> = serde_urlencoded::from_str(&body).unwrap_or_default();
    let code = form.get("code").map_or("", String::as_str);
    let timestamp = mock.now.timestamp();
    let envelope = match code {
        "tampered" => {
            let mut envelope = signed_envelope(&mock.signing, &mock.good_data, timestamp);
            envelope["data"] = json!(format!("{}tampered", mock.good_data));
            envelope
        }
        "stale" => signed_envelope(&mock.signing, &mock.good_data, timestamp - 301),
        "empty" => signed_envelope(&mock.signing, &mock.empty_sub_data, timestamp),
        _ => signed_envelope(&mock.signing, &mock.good_data, timestamp),
    };
    Json(envelope)
}

fn signed_envelope(signing: &SigningKey, data: &str, timestamp: i64) -> Value {
    let message = format!("{timestamp}\n{data}");
    let signature = BASE64.encode(signing.sign(message.as_bytes()).to_bytes());
    json!({ "data": data, "timestamp": timestamp, "signature": signature })
}

fn fake_id_token(sub: &str) -> String {
    let payload = URL_SAFE_NO_PAD.encode(
        serde_json::to_vec(&json!({ "sub": sub, "name": "Nova" })).expect("序列化必须成功"),
    );
    format!("header.{payload}.signature")
}

fn public_key_pem(signing: &SigningKey) -> String {
    let der = signing.verifying_key().to_public_key_der().expect("公钥 DER 必须可编码");
    let encoded = BASE64.encode(der.as_bytes());
    let mut body = String::new();
    for chunk in encoded.as_bytes().chunks(64) {
        body.push_str(std::str::from_utf8(chunk).expect("base64 是 ASCII"));
        body.push('\n');
    }
    format!("-----BEGIN PUBLIC KEY-----\n{body}-----END PUBLIC KEY-----\n")
}
