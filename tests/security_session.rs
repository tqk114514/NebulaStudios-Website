//! 会话 / JWT / refresh 轮转 / CSRF / cookie / 登出与撤销的对抗性回归测试。
//!
//! 每个用例断言一条「安全行为必须成立」；用例失败即代表当前实现可被利用。
//! 装配沿用 `tests/common`：临时文件库、`ManualClock`、`tower::ServiceExt::oneshot`，
//! 不绑端口、不访问外网（社交 provider 走本地假客户端）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::sync::Arc;

use async_trait::async_trait;
use axum::Router;
use axum::http::{StatusCode, header};
use common::{
    OneshotPost, Session, body_text, cookie_header, extract_code, login_session, register_user,
    register_verified_user, set_cookie_of,
};
use jsonwebtoken::{Algorithm, EncodingKey, Header};
use nebula_server::api;
use nebula_server::context::Clock as _;
use nebula_server::model::password::PasswordHash;
use nebula_server::model::social::{NewSocialIdentity, SocialProvider};
use nebula_server::model::user::{User, UserId};
use nebula_server::social::{ProviderIdentity, SocialClients, SocialError, SocialProviderClient};
use tower::ServiceExt as _;

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";
const NEW_PASSWORD: &str = "Reborn!Passw0rd-2026";

// ---------------------------------------------------------------- 请求助手

/// 带 csrf 配对的 refresh 请求（cookie 只带 refresh + csrf）。
async fn refresh(app: &Router, refresh: &str, csrf: &str) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(refresh), Some(csrf)),
            Some(csrf),
        )
        .await
}

/// 以 access cookie 读资料。
async fn get_profile(app: &Router, cookie: &str) -> axum::response::Response {
    app.clone().oneshot_get_with_cookie("/api/user", "", cookie).await
}

/// 以 Bearer 读资料。
async fn get_with_bearer(app: &Router, token: &str) -> axum::response::Response {
    let request = axum::http::Request::get("/api/user")
        .header(header::AUTHORIZATION, format!("Bearer {token}"))
        .body(axum::body::Body::empty())
        .expect("请求必须可构造");
    app.clone().oneshot(request).await.expect("oneshot 必须成功")
}

async fn seeded_user_id(state: &api::AppState) -> UserId {
    state.user_repo.find_by_email(EMAIL).await.expect("查询必须成功").expect("用户必须存在").id
}

fn cache_control(response: &axum::response::Response) -> Option<String> {
    response.headers().get(header::CACHE_CONTROL).and_then(|v| v.to_str().ok()).map(str::to_owned)
}

fn set_cookie_count(response: &axum::response::Response) -> usize {
    response.headers().get_all(header::SET_COOKIE).iter().count()
}

// ---------------------------------------------------------------- JWT 助手

/// 无填充 base64url；只用于构造攻击载荷，避免引入编解码依赖。
fn b64url(input: &[u8]) -> String {
    const TABLE: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
    let mut out = String::new();
    for chunk in input.chunks(3) {
        let second = *chunk.get(1).unwrap_or(&0);
        let third = *chunk.get(2).unwrap_or(&0);
        let n = (u32::from(chunk[0]) << 16) | (u32::from(second) << 8) | u32::from(third);
        out.push(TABLE[((n >> 18) & 63) as usize] as char);
        out.push(TABLE[((n >> 12) & 63) as usize] as char);
        if chunk.len() > 1 {
            out.push(TABLE[((n >> 6) & 63) as usize] as char);
        }
        if chunk.len() > 2 {
            out.push(TABLE[(n & 63) as usize] as char);
        }
    }
    out
}

/// `alg=none` 的无签名令牌。
fn alg_none_token(claims: &serde_json::Value) -> String {
    format!(
        "{}.{}.",
        b64url(br#"{"alg":"none","typ":"JWT"}"#),
        b64url(claims.to_string().as_bytes())
    )
}

/// 用测试密钥签发任意 claims 的令牌；HS256 以公钥 PEM 字节作为 HMAC 密钥
/// （经典算法混淆载荷）。
fn sign_test_token(alg: Algorithm, claims: &serde_json::Value) -> String {
    let key = match alg {
        Algorithm::HS256 => EncodingKey::from_secret(common::TEST_PUBLIC_PEM.as_bytes())
            .expect("测试公钥必须可作为 HMAC 密钥"),
        _ => EncodingKey::from_ec_pem(common::TEST_PRIVATE_PEM.as_bytes())
            .expect("测试私钥必须可解析"),
    };
    jsonwebtoken::encode(&Header::new(alg), claims, &key).expect("测试令牌必须可签发")
}

fn claims_for(user_id: UserId, ver: i64) -> serde_json::Value {
    serde_json::json!({
        "sub": user_id.to_string(),
        "iat": 1_790_000_000i64,
        "exp": 4_000_000_000i64,
        "aud": "nebula:user-session",
        "ver": ver,
    })
}

// ---------------------------------------------------------------- 假社交 provider

struct FakeGoogle;

#[async_trait]
impl SocialProviderClient for FakeGoogle {
    fn provider(&self) -> SocialProvider {
        SocialProvider::Google
    }

    fn authorize_url(&self, state: &str, _code_challenge: &str) -> String {
        format!("https://provider.test/authorize?state={state}")
    }

    async fn fetch_identity(
        &self,
        _code: &str,
        _code_verifier: &str,
    ) -> Result<ProviderIdentity, SocialError> {
        Ok(ProviderIdentity { provider_user_id: "attacker-google".to_owned(), display_name: None })
    }
}

// ================================================================ 用例

/// 同一 refresh token 并发刷新不得被兑换两次。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_session_01_concurrent_refresh_is_single_use() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let first_app = app.clone();
    let first_refresh = session.refresh.clone();
    let first_csrf = session.csrf.clone();
    let second_app = app.clone();
    let second_refresh = session.refresh.clone();
    let second_csrf = session.csrf.clone();
    let first =
        tokio::spawn(
            async move { refresh(&first_app, &first_refresh, &first_csrf).await.status() },
        );
    let second =
        tokio::spawn(
            async move { refresh(&second_app, &second_refresh, &second_csrf).await.status() },
        );
    let (first, second) = tokio::join!(first, second);
    let first = first.expect("任务不得 panic");
    let second = second.expect("任务不得 panic");
    eprintln!("并发刷新结果: {first} / {second}");

    let successes = [first, second].iter().filter(|status| status.is_success()).count();
    assert_eq!(successes, 1, "同一 refresh token 只能有一个赢家: {first} / {second}");
}

/// 密码重置后旧 access 与旧 refresh 全部失效（含其它设备）。
#[tokio::test]
async fn sec_session_02_password_reset_kills_all_sessions() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_verified_user(&app, EMAIL, USERNAME, PASSWORD, &mailbox).await;
    let first = login_session(&app).await;
    let second = login_session(&app).await;

    let body = format!(r#"{{"email":"{EMAIL}"}}"#);
    let requested = app.clone().oneshot_post("/api/auth/password/reset/request", &body).await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);
    let code = extract_code(&common::await_mail_subject(&mailbox, "密码重置验证码").await);
    let confirm =
        format!(r#"{{"email":"{EMAIL}","code":"{code}","new_password":"{NEW_PASSWORD}"}}"#);
    let confirmed = app.clone().oneshot_post("/api/auth/password/reset/confirm", &confirm).await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT);

    for session in [&first, &second] {
        let profile =
            get_profile(&app, &cookie_header(Some(&session.access), None, Some(&session.csrf)))
                .await;
        assert_eq!(profile.status(), StatusCode::UNAUTHORIZED, "重置后旧 access 必须失效");
        let rotated = refresh(&app, &session.refresh, &session.csrf).await;
        assert_eq!(rotated.status(), StatusCode::UNAUTHORIZED, "重置后旧 refresh 必须失效");
    }
}

/// 改密后其它会话一并下线，且旧 access / refresh 不可再用。
#[tokio::test]
async fn sec_session_03_password_change_kills_other_sessions() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let acting = login_session(&app).await;
    let other = login_session(&app).await;

    let body = format!(r#"{{"current_password":"{PASSWORD}","new_password":"{NEW_PASSWORD}"}}"#);
    let changed = app
        .clone()
        .oneshot_post_with(
            "/api/auth/password/change",
            &body,
            &cookie_header(Some(&acting.access), None, Some(&acting.csrf)),
            Some(&acting.csrf),
        )
        .await;
    assert_eq!(changed.status(), StatusCode::NO_CONTENT);

    for session in [&acting, &other] {
        let profile =
            get_profile(&app, &cookie_header(Some(&session.access), None, Some(&session.csrf)))
                .await;
        assert_eq!(profile.status(), StatusCode::UNAUTHORIZED, "改密后旧 access 必须失效");
        let rotated = refresh(&app, &session.refresh, &session.csrf).await;
        assert_eq!(rotated.status(), StatusCode::UNAUTHORIZED, "改密后旧 refresh 必须失效");
    }
}

/// 账户删除后旧 access 与旧 refresh 都不可用。
#[tokio::test]
async fn sec_session_04_account_deletion_kills_tokens() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_verified_user(&app, EMAIL, USERNAME, PASSWORD, &mailbox).await;
    let session = login_session(&app).await;
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));

    let requested = app
        .clone()
        .oneshot_post_with("/api/auth/account/delete/request", "", &cookie, Some(&session.csrf))
        .await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);
    let code = extract_code(&common::await_mail_subject(&mailbox, "账户注销确认码").await);
    let confirmed = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/confirm",
            &format!(r#"{{"code":"{code}"}}"#),
            &cookie,
            Some(&session.csrf),
        )
        .await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT);

    assert_eq!(
        get_profile(&app, &cookie).await.status(),
        StatusCode::UNAUTHORIZED,
        "删除后旧 access 必须失效"
    );
    assert_eq!(
        refresh(&app, &session.refresh, &session.csrf).await.status(),
        StatusCode::UNAUTHORIZED,
        "删除后旧 refresh 必须失效"
    );
}

/// 签名剥离（alg=none）的令牌不得通过认证。
#[tokio::test]
async fn sec_session_05_alg_none_is_rejected() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let user_id = seeded_user_id(&state).await;
    let token = alg_none_token(&claims_for(user_id, 0));

    assert_eq!(
        get_with_bearer(&app, &token).await.status(),
        StatusCode::UNAUTHORIZED,
        "alg=none 令牌不得作为 Bearer 使用"
    );
    let cookie = format!("nebula_access={token}");
    assert_eq!(
        get_profile(&app, &cookie).await.status(),
        StatusCode::UNAUTHORIZED,
        "alg=none 令牌不得作为 access cookie 使用"
    );
}

/// 以公钥 PEM 作 HMAC 密钥的 HS256 混淆令牌不得通过认证。
#[tokio::test]
async fn sec_session_06_hs256_key_confusion_is_rejected() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let user_id = seeded_user_id(&state).await;
    let token = sign_test_token(Algorithm::HS256, &claims_for(user_id, 0));

    assert_eq!(
        get_with_bearer(&app, &token).await.status(),
        StatusCode::UNAUTHORIZED,
        "HS256 混淆令牌必须被算法白名单拒绝"
    );
}

/// 缺 `exp` 的令牌不得通过认证（不能靠「永不过期」绕过）。
#[tokio::test]
async fn sec_session_07_missing_exp_is_rejected() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let user_id = seeded_user_id(&state).await;
    let claims = serde_json::json!({
        "sub": user_id.to_string(),
        "iat": 1_790_000_000i64,
        "aud": "nebula:user-session",
        "ver": 0,
    });
    let token = sign_test_token(Algorithm::ES256, &claims);

    assert_eq!(
        get_with_bearer(&app, &token).await.status(),
        StatusCode::UNAUTHORIZED,
        "缺 exp 的令牌必须被拒绝"
    );
}

/// `ver` 与库中 `token_version` 不一致的 access token 必须被拒绝。
#[tokio::test]
async fn sec_session_08_token_version_mismatch_is_rejected() {
    let (app, state, clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let user_id = seeded_user_id(&state).await;
    let access = state
        .user_repo
        .find_access_state(&user_id, clock.now())
        .await
        .expect("读取必须成功")
        .expect("用户必须存在");
    let token = sign_test_token(Algorithm::ES256, &claims_for(user_id, access.token_version + 1));

    assert_eq!(
        get_with_bearer(&app, &token).await.status(),
        StatusCode::UNAUTHORIZED,
        "token_version 不匹配必须拒绝"
    );
}

/// access JWT 不能被当作 refresh token 兑换，且伪造呈递不得影响真实家族。
#[tokio::test]
async fn sec_session_09_access_jwt_is_not_a_refresh_token() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let forged = refresh(&app, &session.access, &session.csrf).await;
    assert_eq!(forged.status(), StatusCode::UNAUTHORIZED);
    assert!(body_text(forged).await.contains("refresh_invalid"));

    let real = refresh(&app, &session.refresh, &session.csrf).await;
    assert_eq!(real.status(), StatusCode::NO_CONTENT, "伪造呈递不得撤销真实家族");
}

/// refresh 明文不能当作 access token 使用。
#[tokio::test]
async fn sec_session_10_refresh_plaintext_is_not_an_access_token() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let cookie = format!("nebula_access={}", session.refresh);
    assert_eq!(
        get_profile(&app, &cookie).await.status(),
        StatusCode::UNAUTHORIZED,
        "refresh 明文不得作为 access cookie 通过认证"
    );
    assert_eq!(
        refresh(&app, &session.refresh, &session.csrf).await.status(),
        StatusCode::NO_CONTENT,
        "伪造 access 不得影响真实 refresh 家族"
    );
}

/// 同名 cookie 重复出现时必须拒绝，不得任取其一（cookie tossing 可绕过双提交 CSRF）。
#[tokio::test]
async fn sec_session_11_duplicate_csrf_cookie_is_rejected() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    // 同站子域可注入 `Domain=<父域>; Path=/` 的 nebula_csrf；浏览器按创建时间
    // 先发注入值、后发真实值，服务端取第一个即采用攻击者已知的值。
    let attacker_csrf = "a".repeat(64);
    let cookie = format!(
        "nebula_access={}; nebula_csrf={attacker_csrf}; nebula_csrf={}",
        session.access, session.csrf
    );
    let response =
        app.clone().oneshot_post_with("/api/auth/logout", "", &cookie, Some(&attacker_csrf)).await;

    assert_eq!(
        response.status(),
        StatusCode::FORBIDDEN,
        "重复的会话 cookie 名必须被拒绝，否则注入的 csrf 值即可通过门禁"
    );
}

/// 携带会话与用户数据的响应必须禁止缓存。
#[tokio::test]
async fn sec_session_12_session_responses_are_not_cacheable() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    let login = app
        .clone()
        .oneshot_post(
            "/api/auth/login",
            &format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}"}}"#),
        )
        .await;
    assert_eq!(login.status(), StatusCode::OK);
    let session = Session {
        access: set_cookie_of(&login, "nebula_access").expect("必须下发 access"),
        refresh: set_cookie_of(&login, "nebula_refresh").expect("必须下发 refresh"),
        csrf: set_cookie_of(&login, "nebula_csrf").expect("必须下发 csrf"),
    };
    assert_eq!(
        cache_control(&login).as_deref(),
        Some("no-store"),
        "登录响应同时下发了会话与用户数据，必须声明不可缓存"
    );

    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    let profile = get_profile(&app, &cookie).await;
    assert_eq!(profile.status(), StatusCode::OK);
    assert_eq!(
        cache_control(&profile).as_deref(),
        Some("no-store"),
        "鉴权响应必须声明不可缓存，否则共享缓存可能跨用户复用"
    );
}

/// 跨站表单能发送的 content-type 不得驱动 JSON 端点改变会话（登录 CSRF）。
#[tokio::test]
async fn sec_session_13_form_content_type_cannot_log_in_victim() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;

    // `<form enctype="text/plain">` 单字段提交时浏览器可产生的 body：
    // 整体仍是合法 JSON，`=` 落在无害的字符串值里，端点忽略未知字段。
    let body = format!(r#"{{"identifier":"{EMAIL}","password":"{PASSWORD}","_pad":"="}}"#);
    let response = app
        .clone()
        .oneshot_post_bytes("/api/auth/login", "text/plain", body.into_bytes(), "", None)
        .await;

    assert!(
        !response.status().is_success(),
        "text/plain 表单请求不得完成登录: {}",
        response.status()
    );
    assert!(
        set_cookie_of(&response, "nebula_access").is_none(),
        "跨站表单不得让受害者浏览器持有攻击者的会话"
    );
}

/// 社交回调必须与发起浏览器绑定；否则攻击者可用自己的回调 URL 替换受害者会话。
#[tokio::test]
async fn sec_session_14_social_callback_requires_browser_bound_state() {
    let (mut state, clock, _mailbox, _dir) = common::app_state().await;
    state.social_clients = Arc::new(SocialClients::new(vec![Arc::new(FakeGoogle)]));
    let app = api::router(state.clone());

    // 攻击者自己的账户已绑定 Google
    let user_id = UserId::new();
    state
        .user_repo
        .insert(
            &User {
                id: user_id,
                username: "attacker".to_owned(),
                email: "attacker@nebula.st".to_owned(),
                created_at: clock.now(),
                email_verified: true,
                avatar_version: 0,
            },
            &PasswordHash::hash(PASSWORD).expect("哈希必须成功"),
        )
        .await
        .expect("种子用户必须可插入");
    state
        .social_repo
        .link_identity(
            &NewSocialIdentity {
                user_id,
                provider: SocialProvider::Google,
                provider_user_id: "attacker-google".to_owned(),
                display_name: None,
            },
            clock.now(),
        )
        .await
        .expect("绑定必须成功");

    // 攻击者的浏览器（无 cookie）起授权并拿到 state，随后自行完成 provider 授权；
    // 回调 URL 被原样转给受害者。
    let started = app.clone().oneshot_get_with_cookie("/api/auth/google", "", "").await;
    assert_eq!(started.status(), StatusCode::FOUND);
    let location = started
        .headers()
        .get(header::LOCATION)
        .expect("必须回跳 provider")
        .to_str()
        .expect("Location 必须可读")
        .to_owned();
    let flow_state = location
        .split("state=")
        .nth(1)
        .expect("必须带 state")
        .split('&')
        .next()
        .unwrap()
        .to_owned();

    // 受害者浏览器（无 cookie）被诱导访问该回调
    let victim = app
        .clone()
        .oneshot_get_with_cookie(
            "/api/auth/google/callback",
            &format!("code=attacker-code&state={flow_state}"),
            "",
        )
        .await;
    assert_eq!(victim.status(), StatusCode::FOUND);
    let issued = set_cookie_of(&victim, "nebula_access");
    let mut observed = "未签发会话".to_owned();
    if let Some(token) = &issued {
        let profile = get_profile(&app, &format!("nebula_access={token}")).await;
        observed = format!("回调签发了会话并把受害者置入该账户: {}", body_text(profile).await);
    }
    assert!(
        issued.is_none(),
        "未持有发起端 state cookie 的回调不得签发会话（登录 CSRF）；实际: {observed}"
    );
}

/// 未携带任何会话的失败响应不得改写浏览器里的会话 cookie（CSRF 强制登出）。
#[tokio::test]
async fn sec_session_15_unauthenticated_failures_do_not_clear_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    // 跨站 `<form method=post action=/api/auth/logout>`：Lax 不带 cookie，但响应里的
    // Set-Cookie 会被浏览器接受，因此未认证路径不能下发清 cookie 头。
    let logout = app.clone().oneshot_post("/api/auth/logout", "").await;
    assert_eq!(logout.status(), StatusCode::UNAUTHORIZED);
    assert_eq!(
        set_cookie_count(&logout),
        0,
        "未认证的登出不得下发清 cookie 头，否则任意站点可强制登出"
    );

    let refresh_without_cookie = app.clone().oneshot_post("/api/auth/refresh", "").await;
    assert_eq!(refresh_without_cookie.status(), StatusCode::FORBIDDEN);
    assert_eq!(
        set_cookie_count(&refresh_without_cookie),
        0,
        "未认证的刷新失败不得下发清 cookie 头"
    );
}

/// 注销确认码不得当作密码重置码使用（用途隔离）。
#[tokio::test]
async fn sec_session_16_verification_codes_are_purpose_bound() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_verified_user(&app, EMAIL, USERNAME, PASSWORD, &mailbox).await;
    let session = login_session(&app).await;
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));

    let requested = app
        .clone()
        .oneshot_post_with("/api/auth/account/delete/request", "", &cookie, Some(&session.csrf))
        .await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);
    let delete_code = extract_code(&common::await_mail_subject(&mailbox, "账户注销确认码").await);

    let body =
        format!(r#"{{"email":"{EMAIL}","code":"{delete_code}","new_password":"{NEW_PASSWORD}"}}"#);
    let reset = app.clone().oneshot_post("/api/auth/password/reset/confirm", &body).await;
    assert_eq!(reset.status(), StatusCode::UNAUTHORIZED, "注销确认码不得重置口令");
    assert_eq!(
        get_profile(&app, &cookie).await.status(),
        StatusCode::OK,
        "错误的用途尝试不得撤销现有会话"
    );
}

/// 并发登出与刷新不得留下可控会话：登出成功返回后，刷新拿到的一切凭据都必须失效。
#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sec_session_17_concurrent_logout_and_refresh_leave_no_live_session() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;

    let refresh_app = app.clone();
    let refresh_token = session.refresh.clone();
    let refresh_csrf = session.csrf.clone();
    let logout_app = app.clone();
    let access = session.access.clone();
    let logout_csrf = session.csrf.clone();
    let refreshed = tokio::spawn(async move {
        let response = refresh(&refresh_app, &refresh_token, &refresh_csrf).await;
        let status = response.status();
        let access = set_cookie_of(&response, "nebula_access");
        let rotated = set_cookie_of(&response, "nebula_refresh");
        (status, access, rotated)
    });
    let logged_out = tokio::spawn(async move {
        logout_app
            .clone()
            .oneshot_post_with(
                "/api/auth/logout",
                "",
                &cookie_header(Some(&access), None, Some(&logout_csrf)),
                Some(&logout_csrf),
            )
            .await
            .status()
    });
    let (refreshed, logged_out) = tokio::join!(refreshed, logged_out);
    let (refresh_status, refreshed_access, rotated_refresh) = refreshed.expect("任务不得 panic");
    let logout_status = logged_out.expect("任务不得 panic");
    assert_eq!(logout_status, StatusCode::NO_CONTENT, "登出必须完成撤销");

    if let Some(token) = refreshed_access {
        assert_eq!(
            get_with_bearer(&app, &token).await.status(),
            StatusCode::UNAUTHORIZED,
            "登出后并发刷新得到的 access 必须失效（{refresh_status}）"
        );
    }
    if let Some(token) = rotated_refresh {
        let response = refresh(&app, &token, "b".repeat(64).as_str()).await;
        assert_eq!(
            response.status(),
            StatusCode::UNAUTHORIZED,
            "登出后并发刷新得到的 refresh 必须失效"
        );
    }
}

/// 同名会话 cookie 重复出现时不得任取其一（cookie tossing 可把受害者切到攻击者会话）。
#[tokio::test]
async fn sec_session_18_duplicate_access_cookie_is_rejected() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let victim = login_session(&app).await;
    // 攻击者以自己的账户登录，得到一个合法 access 值
    register_user(&app, "attacker@nebula.st", "attacker", NEW_PASSWORD).await;
    let attacker = common::login_as(&app, "attacker@nebula.st", NEW_PASSWORD).await;

    let cookie = format!("nebula_access={}; nebula_access={}", attacker.access, victim.access);
    let response = get_profile(&app, &cookie).await;
    let status = response.status();
    let observed = body_text(response).await;
    assert_eq!(
        status,
        StatusCode::UNAUTHORIZED,
        "重复 access cookie 必须被拒绝，而不是静默采用注入的那一个；实际响应: {observed}"
    );
    assert!(!observed.contains("attacker"), "不得把重复 cookie 解析成攻击者会话: {observed}");
}

/// 封禁必须撤销 refresh 家族：被封禁账户的 refresh 不得再轮转出 access。
#[tokio::test]
async fn sec_session_19_banned_user_cannot_refresh() {
    let (mut state, _clock, mailbox, _dir) = common::app_state().await;
    state.admin_emails = Arc::new(std::collections::HashSet::from(["admin@nebula.st".to_owned()]));
    let app = api::router(state.clone());
    register_verified_user(&app, "admin@nebula.st", "admin", PASSWORD, &mailbox).await;
    register_verified_user(&app, EMAIL, USERNAME, PASSWORD, &mailbox).await;
    let admin = common::login_as(&app, "admin@nebula.st", PASSWORD).await;
    let target = common::login_as(&app, EMAIL, PASSWORD).await;
    let target_id = seeded_user_id(&state).await;

    let banned = app
        .clone()
        .oneshot_post_with(
            &format!("/api/admin/users/{target_id}/ban"),
            r#"{"kind":"permanent","reason":"security-review"}"#,
            &cookie_header(Some(&admin.access), None, Some(&admin.csrf)),
            Some(&admin.csrf),
        )
        .await;
    assert_eq!(banned.status(), StatusCode::NO_CONTENT);

    assert_eq!(
        refresh(&app, &target.refresh, &target.csrf).await.status(),
        StatusCode::UNAUTHORIZED,
        "封禁后 refresh 家族必须已撤销"
    );
}
