//! `OAuth2` Provider（授权码 + PKCE）对抗性回归测试。
//!
//! 每个用例断言一条「安全行为必须成立」；用例失败即代表当前实现可被利用。
//! 装配沿用 `tests/common`：临时文件库、`ManualClock`、`tower::ServiceExt::oneshot`，
//! 不绑端口、不访问外网。失败用例即当前可被利用的缺陷，见各用例注释与审计报告。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::net::SocketAddr;
use std::path::Path;
use std::sync::Arc;

use axum::body::Body;
use axum::extract::ConnectInfo;
use axum::http::{Request, StatusCode, header};
use chrono::TimeDelta;
use common::{OneshotPost, Session, body_text, cookie_header, login_session, register_user};
use nebula_server::api::AppState;
use nebula_server::context::ManualClock;
use nebula_server::model::password::PasswordHash;
use nebula_server::model::tokens::{VerifiedAccessToken, verify_access};
use nebula_server::storage::sqlite::connect_pool;
use tower::ServiceExt;

const EMAIL: &str = "nova@nebula.st";
const USERNAME: &str = "nova";
const PASSWORD: &str = "Nebula!Passw0rd-2026";

/// RFC 7636 附录 B 测试向量。
const VERIFIER: &str = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
const CHALLENGE: &str = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";

const CLIENT_ID: &str = "client-0001";
const CLIENT_SECRET: &str = "Nebula!Client-2026";
const REDIRECT: &str = "https://app.example.com/callback";

const OTHER_CLIENT_ID: &str = "client-0002";
const OTHER_CLIENT_SECRET: &str = "Other!Client-2026";
const OTHER_REDIRECT: &str = "https://other.example.com/callback";

// ---------------------------------------------------------------- 装配

struct Fixture {
    app: axum::Router,
    state: AppState,
    clock: Arc<ManualClock>,
    session: Session,
    dir: tempfile::TempDir,
}

/// 临时库 + 默认已启用客户端 + 已登录用户。
async fn fixture() -> Fixture {
    let (app, state, clock, _mailbox, dir) = common::app().await;
    seed_client(dir.path(), CLIENT_ID, Some(CLIENT_SECRET), REDIRECT, true).await;
    register_user(&app, EMAIL, USERNAME, PASSWORD).await;
    let session = login_session(&app).await;
    Fixture { app, state, clock, session, dir }
}

/// 直插客户端；`secret` 为 `None` 时模拟未设密钥的公共客户端。
async fn seed_client(
    dir: &Path,
    id: &str,
    secret: Option<&str>,
    redirect_uri: &str,
    enabled: bool,
) {
    let hash = secret.map(|plain| PasswordHash::hash(plain).expect("哈希必须成功"));
    let pool = connect_pool(&dir.join("users.db")).await.expect("测试库必须可打开");
    sqlx::query(
        "INSERT INTO oauth_clients (id, name, secret_hash, redirect_uri, enabled, created_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
    )
    .bind(id)
    .bind("Example App")
    .bind(hash.as_ref().map(PasswordHash::as_str))
    .bind(redirect_uri)
    .bind(enabled)
    .bind("2026-10-26T00:00:00+00:00")
    .execute(&pool)
    .await
    .expect("客户端必须可插入");
    pool.close().await;
}

async fn set_client_enabled(dir: &Path, id: &str, enabled: bool) {
    let pool = connect_pool(&dir.join("users.db")).await.expect("测试库必须可打开");
    sqlx::query("UPDATE oauth_clients SET enabled = ?2 WHERE id = ?1")
        .bind(id)
        .bind(enabled)
        .execute(&pool)
        .await
        .expect("更新必须成功");
    pool.close().await;
}

async fn set_client_redirect(dir: &Path, id: &str, redirect_uri: &str) {
    let pool = connect_pool(&dir.join("users.db")).await.expect("测试库必须可打开");
    sqlx::query("UPDATE oauth_clients SET redirect_uri = ?2 WHERE id = ?1")
        .bind(id)
        .bind(redirect_uri)
        .execute(&pool)
        .await
        .expect("更新必须成功");
    pool.close().await;
}

async fn family_count(dir: &Path, client_id: &str) -> i64 {
    let pool = connect_pool(&dir.join("users.db")).await.expect("测试库必须可打开");
    let count: i64 =
        sqlx::query_scalar("SELECT COUNT(*) FROM refresh_token_families WHERE client_id = ?1")
            .bind(client_id)
            .fetch_one(&pool)
            .await
            .expect("计数必须成功");
    pool.close().await;
    count
}

// ---------------------------------------------------------------- 请求助手

fn consent_body(
    client_id: &str,
    redirect_uri: &str,
    scope: &str,
    state: &str,
) -> serde_json::Value {
    serde_json::json!({
        "client_id": client_id,
        "redirect_uri": redirect_uri,
        "scope": scope,
        "code_challenge": CHALLENGE,
        "state": state,
    })
}

/// 以会话 + CSRF 提交同意。
async fn consent(
    app: &axum::Router,
    session: &Session,
    body: &serde_json::Value,
) -> axum::response::Response {
    app.clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &body.to_string(),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

fn code_from_location(response: &axum::response::Response) -> String {
    let location = response
        .headers()
        .get(header::LOCATION)
        .expect("302 必须带 Location")
        .to_str()
        .expect("Location 必须可读");
    location
        .split("code=")
        .nth(1)
        .expect("Location 必须含 code")
        .split('&')
        .next()
        .expect("code 值必须存在")
        .to_owned()
}

/// 走同意 → 取授权码（断言 302）。
async fn issue_code(
    app: &axum::Router,
    session: &Session,
    client_id: &str,
    redirect_uri: &str,
    scope: &str,
) -> String {
    let response =
        consent(app, session, &consent_body(client_id, redirect_uri, scope, "xyz")).await;
    assert_eq!(response.status(), StatusCode::FOUND, "同意必须签发授权码");
    code_from_location(&response)
}

fn code_exchange_form(
    code: &str,
    client_id: &str,
    secret: &str,
    redirect_uri: &str,
    verifier: &str,
) -> String {
    format!(
        "grant_type=authorization_code&code={code}&redirect_uri={redirect_uri}\
         &code_verifier={verifier}&client_id={client_id}&client_secret={secret}"
    )
}

async fn exchange(
    app: &axum::Router,
    code: &str,
    client_id: &str,
    secret: &str,
    redirect_uri: &str,
    verifier: &str,
) -> axum::response::Response {
    let form = code_exchange_form(code, client_id, secret, redirect_uri, verifier);
    app.clone().oneshot_post_form("/api/oauth/token", &form).await
}

async fn exchange_from(
    app: &axum::Router,
    peer: &str,
    code: &str,
    client_id: &str,
    secret: &str,
    redirect_uri: &str,
    verifier: &str,
) -> axum::response::Response {
    let form = code_exchange_form(code, client_id, secret, redirect_uri, verifier);
    let addr: SocketAddr = format!("{peer}:40000").parse().expect("测试对端地址必须合法");
    let mut request = Request::post("/api/oauth/token")
        .header("content-type", "application/x-www-form-urlencoded")
        .body(Body::from(form))
        .expect("请求必须可构造");
    request.extensions_mut().insert(ConnectInfo(addr));
    app.clone().oneshot(request).await.expect("oneshot 必须成功")
}

fn refresh_form(refresh_token: &str, client_id: &str, secret: &str) -> String {
    format!(
        "grant_type=refresh_token&refresh_token={refresh_token}&client_id={client_id}\
         &client_secret={secret}"
    )
}

async fn refresh(
    app: &axum::Router,
    refresh_token: &str,
    client_id: &str,
    secret: &str,
) -> axum::response::Response {
    let form = refresh_form(refresh_token, client_id, secret);
    app.clone().oneshot_post_form("/api/oauth/token", &form).await
}

/// RFC 6749 形状的 `(status, error)`。
async fn rfc_error_of(response: axum::response::Response) -> (StatusCode, String) {
    let status = response.status();
    let text = body_text(response).await;
    let json: serde_json::Value = serde_json::from_str(&text).expect("RFC 错误响应必须是 JSON");
    (status, json["error"].as_str().unwrap_or_default().to_owned())
}

/// consent 统一信封的 `(status, details.reason)`。
async fn consent_reason(response: axum::response::Response) -> (StatusCode, String) {
    let status = response.status();
    let text = body_text(response).await;
    let json: serde_json::Value = serde_json::from_str(&text).expect("consent 错误必须是 JSON");
    (status, json["details"]["reason"].as_str().unwrap_or_default().to_owned())
}

fn authorize_query(response_type: &str, scope: &str, challenge: &str, method: &str) -> String {
    format!(
        "client_id={CLIENT_ID}&redirect_uri=https%3A%2F%2Fapp.example.com%2Fcallback\
         &response_type={response_type}&scope={scope}&code_challenge={challenge}\
         &code_challenge_method={method}"
    )
}

// ---------------------------------------------------------------- 授权码重放与撤销

/// RFC 6749 §4.1.2：授权码被二次呈递时，应撤销此前基于该码签发的全部令牌。
/// 当前实现只拒绝重放，不撤销已经发出的 refresh 家族——被盗码先兑换后，
/// 合法客户端的重放无法踢掉攻击者会话。该用例应失败。
#[tokio::test]
async fn sec_oauth_01_code_replay_revokes_issued_refresh_family() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;

    let first = exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(first.status(), StatusCode::OK);
    let issued: serde_json::Value = serde_json::from_str(&body_text(first).await).unwrap();
    let refresh_token = issued["refresh_token"].as_str().unwrap().to_owned();

    let replay = exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(
        rfc_error_of(replay).await,
        (StatusCode::BAD_REQUEST, "invalid_grant".to_owned()),
        "二次呈递同一授权码必须返回 invalid_grant"
    );

    let after = refresh(&fixture.app, &refresh_token, CLIENT_ID, CLIENT_SECRET).await;
    assert_eq!(
        after.status(),
        StatusCode::BAD_REQUEST,
        "授权码重放必须撤销此前签发的 refresh 家族（RFC 6749 §4.1.2）；\
         当前仍可轮转，说明码泄露后攻击者会话不因重放而终止"
    );
}

/// 登出撤销全部 refresh 家族，但待兑换的授权码仍然可换取新家族——
/// 客户端故意延迟兑换即可绕过用户主动登出（§9.1 撤销路径）。
/// 该用例应失败。
#[tokio::test]
async fn sec_oauth_02_pending_code_survives_logout() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;

    let logout = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf)),
            Some(&fixture.session.csrf),
        )
        .await;
    assert_eq!(logout.status(), StatusCode::NO_CONTENT, "登出必须成功");

    let response =
        exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(
        response.status(),
        StatusCode::BAD_REQUEST,
        "登出撤销会话后，待兑换授权码不得再换取新的 refresh 家族"
    );
}

/// 用户主动撤销第三方授权（ADR-0014）只撤销 refresh 家族；撤销前签发的
/// 待兑换码仍可换取新家族，令撤销对持续持码的客户端无效。该用例应失败。
#[tokio::test]
async fn sec_oauth_03_pending_code_survives_grant_revocation() {
    let fixture = fixture().await;
    // 先完成一次兑换，使撤销端点有可撤销的家族（否则返回 404）
    let warmup = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;
    let first = exchange(&fixture.app, &warmup, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(first.status(), StatusCode::OK);

    let pending = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;

    let revoke = fixture
        .app
        .clone()
        .oneshot_delete_with_cookie(
            &format!("/api/user/oauth/grants/{CLIENT_ID}"),
            &cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf)),
            Some(&fixture.session.csrf),
        )
        .await;
    assert_eq!(revoke.status(), StatusCode::NO_CONTENT, "撤销授权必须成功");

    let response =
        exchange(&fixture.app, &pending, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(
        response.status(),
        StatusCode::BAD_REQUEST,
        "撤销授权后，撤销前签发的待兑换码不得再建立新的 refresh 家族"
    );
}

// ---------------------------------------------------------------- 授权码单次消费

/// 同一授权码的并发兑换只能有一个赢家；原子消费失败则可能克隆出多个家族。
#[tokio::test]
async fn sec_oauth_04_concurrent_code_exchange_has_single_winner() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;
    let form = code_exchange_form(&code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER);

    let (first, second) = tokio::join!(
        fixture.app.clone().oneshot_post_form("/api/oauth/token", &form),
        fixture.app.clone().oneshot_post_form("/api/oauth/token", &form),
    );

    let winners = [first.status(), second.status()]
        .into_iter()
        .filter(|status| *status == StatusCode::OK)
        .count();
    assert_eq!(winners, 1, "并发兑换必须恰好一个请求成功");
    let losers = [first.status(), second.status()]
        .into_iter()
        .filter(|status| *status == StatusCode::BAD_REQUEST)
        .count();
    assert_eq!(losers, 1, "并发兑换的败者必须是 invalid_grant");
    assert_eq!(family_count(fixture.dir.path(), CLIENT_ID).await, 1, "只允许建立一个家族");
}

// ---------------------------------------------------------------- redirect_uri

/// `redirect_uri` 采用逐字节精确匹配：尾斜杠、后缀、前缀、userinfo、fragment、
/// 大小写与端口差异一律拒绝，避免开放重定向与授权码外泄。
#[tokio::test]
async fn sec_oauth_05_redirect_uri_requires_exact_match() {
    let fixture = fixture().await;
    let variants = [
        "https://app.example.com/callback/",
        "https://app.example.com/callback?extra=1",
        "https://app.example.com/callback#frag",
        "https://app.example.com/callbackk",
        "https://app.example.com/callbac",
        "https://app.example.com.evil.test/callback",
        "https://evil.test/https://app.example.com/callback",
        "https://user@app.example.com/callback",
        "HTTPS://APP.EXAMPLE.COM/callback",
        "https://app.example.com:443/callback",
        "http://app.example.com/callback",
    ];
    for variant in variants {
        let response = consent(
            &fixture.app,
            &fixture.session,
            &consent_body(CLIENT_ID, variant, "profile", "xyz"),
        )
        .await;
        assert_eq!(response.status(), StatusCode::BAD_REQUEST, "必须拒绝 {variant}");
        assert_eq!(
            consent_reason(response).await,
            (StatusCode::BAD_REQUEST, "invalid_redirect_uri".to_owned()),
            "必须以 invalid_redirect_uri 拒绝 {variant}"
        );
    }
}

// ---------------------------------------------------------------- 码的绑定

/// 授权码绑定签发时的 `client_id`：另一个已认证客户端不能兑换。
#[tokio::test]
async fn sec_oauth_06_code_is_bound_to_client() {
    let fixture = fixture().await;
    seed_client(
        fixture.dir.path(),
        OTHER_CLIENT_ID,
        Some(OTHER_CLIENT_SECRET),
        OTHER_REDIRECT,
        true,
    )
    .await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;

    let cross =
        exchange(&fixture.app, &code, OTHER_CLIENT_ID, OTHER_CLIENT_SECRET, REDIRECT, VERIFIER)
            .await;
    assert_eq!(
        rfc_error_of(cross).await,
        (StatusCode::BAD_REQUEST, "invalid_grant".to_owned()),
        "跨客户端兑换必须失败"
    );

    // 绑定失败不消费码：归属客户端仍能兑换
    let owner = exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(owner.status(), StatusCode::OK, "归属客户端必须仍可兑换");
}

/// 授权码绑定签发时的 `redirect_uri`：token 请求里的 `redirect_uri` 不一致即拒绝，
/// 且不一致不得消费码。
#[tokio::test]
async fn sec_oauth_07_code_is_bound_to_redirect_uri() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;

    let mismatched =
        exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, &format!("{REDIRECT}/"), VERIFIER)
            .await;
    assert_eq!(
        rfc_error_of(mismatched).await,
        (StatusCode::BAD_REQUEST, "invalid_grant".to_owned()),
        "redirect_uri 不一致必须失败"
    );

    let owner = exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(owner.status(), StatusCode::OK, "失败尝试不得消费授权码");
}

/// 红线 #9：不存在 plain 降级。以 verifier 自身作为 challenge（plain 的形状）
/// 签发的码，用同一 verifier 兑换必须失败；consent 体里的
/// `code_challenge_method` 也不得改变服务端固定的 S256。
#[tokio::test]
async fn sec_oauth_08_pkce_plain_downgrade_is_rejected() {
    let fixture = fixture().await;
    let body = serde_json::json!({
        "client_id": CLIENT_ID,
        "redirect_uri": REDIRECT,
        "scope": "profile",
        "code_challenge": VERIFIER,
        "code_challenge_method": "plain",
        "state": "xyz",
    });
    let response = consent(&fixture.app, &fixture.session, &body).await;
    assert_eq!(response.status(), StatusCode::FOUND, "consent 忽略 method 字段并按 S256 记录");
    let code = code_from_location(&response);

    for _ in 0..2 {
        let plain =
            exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
        assert_eq!(
            rfc_error_of(plain).await,
            (StatusCode::BAD_REQUEST, "invalid_grant".to_owned()),
            "verifier == challenge 的 plain 形态不得通过 S256 校验"
        );
    }

    // authorize GET 侧显式 plain 也拒绝
    let get = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &authorize_query("code", "profile", CHALLENGE, "plain"),
            "",
        )
        .await;
    assert_eq!(
        rfc_error_of(get).await,
        (StatusCode::BAD_REQUEST, "invalid_request".to_owned()),
        "code_challenge_method=plain 必须拒绝"
    );
}

// ---------------------------------------------------------------- consent 门禁

/// consent 是第一方 cookie 写端点：必须同时具备会话与 CSRF 双提交令牌。
#[tokio::test]
async fn sec_oauth_09_consent_requires_authentication_and_csrf() {
    let fixture = fixture().await;
    let body = consent_body(CLIENT_ID, REDIRECT, "profile", "xyz");

    let anonymous = fixture
        .app
        .clone()
        .oneshot_post_with("/api/oauth/authorize/consent", &body.to_string(), "", None)
        .await;
    assert_eq!(anonymous.status(), StatusCode::UNAUTHORIZED, "未认证必须 401");

    let no_header = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &body.to_string(),
            &cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf)),
            None,
        )
        .await;
    assert_eq!(no_header.status(), StatusCode::FORBIDDEN, "缺 CSRF 头必须 403");
    assert!(body_text(no_header).await.contains("csrf_invalid"));

    let no_cookie = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &body.to_string(),
            &cookie_header(Some(&fixture.session.access), None, None),
            Some("forged-csrf"),
        )
        .await;
    assert_eq!(no_cookie.status(), StatusCode::FORBIDDEN, "缺 CSRF cookie 必须 403");

    let wrong = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/oauth/authorize/consent",
            &body.to_string(),
            &cookie_header(Some(&fixture.session.access), None, Some(&fixture.session.csrf)),
            Some("wrong-csrf"),
        )
        .await;
    assert_eq!(wrong.status(), StatusCode::FORBIDDEN, "CSRF 值不匹配必须 403");
}

// ---------------------------------------------------------------- scope

/// scope 在同意时固定，兑换时附带的 scope 参数不得提升权限。
#[tokio::test]
async fn sec_oauth_10_scope_cannot_be_escalated_at_exchange() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;
    let form = format!(
        "{}&scope=email",
        code_exchange_form(&code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER)
    );

    let response = fixture.app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(response.status(), StatusCode::OK);
    let json: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(json["scope"], "profile", "兑换响应不得回显提升后的 scope");

    let access = json["access_token"].as_str().unwrap();
    match verify_access(access, fixture.state.jwt.decode(), fixture.state.clock.now())
        .expect("令牌必须可验证")
    {
        VerifiedAccessToken::ClientGrant { scope, .. } => {
            assert_eq!(scope, "profile", "JWT scope 必须是授权时的 scope");
        }
        other @ VerifiedAccessToken::UserSession { .. } => {
            panic!("必须解析为客户端授权: {other:?}")
        }
    }
}

/// refresh 流不接受 scope 提升：家族 scope 原样回传。
#[tokio::test]
async fn sec_oauth_11_refresh_cannot_escalate_scope() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;
    let first = exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    let issued: serde_json::Value = serde_json::from_str(&body_text(first).await).unwrap();
    let refresh_token = issued["refresh_token"].as_str().unwrap().to_owned();

    let form = format!("{}&scope=email", refresh_form(&refresh_token, CLIENT_ID, CLIENT_SECRET));
    let response = fixture.app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(response.status(), StatusCode::OK);
    let rotated: serde_json::Value = serde_json::from_str(&body_text(response).await).unwrap();
    assert_eq!(rotated["scope"], "profile", "refresh 不得把 scope 扩到 email");

    let access = rotated["access_token"].as_str().unwrap();
    match verify_access(access, fixture.state.jwt.decode(), fixture.state.clock.now())
        .expect("令牌必须可验证")
    {
        VerifiedAccessToken::ClientGrant { scope, .. } => {
            assert_eq!(scope, "profile", "轮转后的 JWT scope 不得提升");
        }
        other @ VerifiedAccessToken::UserSession { .. } => {
            panic!("必须解析为客户端授权: {other:?}")
        }
    }
}

// ---------------------------------------------------------------- 令牌面隔离

/// 第一方 refresh 与第三方 refresh 互不通用：家族绑定在存储层校验。
#[tokio::test]
async fn sec_oauth_12_token_surfaces_are_separated() {
    let fixture = fixture().await;

    // 第一方 refresh 明文送 OAuth token 端点
    let form = format!(
        "grant_type=refresh_token&refresh_token={}&client_id={CLIENT_ID}&client_secret={CLIENT_SECRET}",
        fixture.session.refresh
    );
    let response = fixture.app.clone().oneshot_post_form("/api/oauth/token", &form).await;
    assert_eq!(
        rfc_error_of(response).await,
        (StatusCode::BAD_REQUEST, "invalid_grant".to_owned()),
        "第一方 refresh 不得在 OAuth 端点兑换"
    );

    // 第三方 refresh 送第一方刷新端点（csrf 双提交自洽即可通过门禁）
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;
    let first = exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    let issued: serde_json::Value = serde_json::from_str(&body_text(first).await).unwrap();
    let oauth_refresh = issued["refresh_token"].as_str().unwrap().to_owned();

    let response = fixture
        .app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&oauth_refresh), Some("csrf-test")),
            Some("csrf-test"),
        )
        .await;
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED, "第三方 refresh 不得续发第一方会话");
    assert!(body_text(response).await.contains("refresh_invalid"));
}

// ---------------------------------------------------------------- 过期与失效

/// 授权码超过 10 分钟 TTL 后不可兑换（时钟注入，不 sleep）。
#[tokio::test]
async fn sec_oauth_13_authorization_code_expires() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;

    fixture.clock.advance(TimeDelta::minutes(10) + TimeDelta::seconds(1));

    let response =
        exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(
        rfc_error_of(response).await,
        (StatusCode::BAD_REQUEST, "invalid_grant".to_owned()),
        "过期授权码必须拒绝"
    );
}

/// 客户端在签发码之后被禁用：兑换必须失败（客户端认证每次重查 enabled）。
#[tokio::test]
async fn sec_oauth_14_disabled_client_cannot_exchange_pending_code() {
    let fixture = fixture().await;
    let code = issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await;
    set_client_enabled(fixture.dir.path(), CLIENT_ID, false).await;

    let response =
        exchange(&fixture.app, &code, CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(
        rfc_error_of(response).await,
        (StatusCode::UNAUTHORIZED, "invalid_client".to_owned()),
        "禁用客户端不得兑换授权码"
    );
}

// ---------------------------------------------------------------- 爆破与限流

/// token 端点的 `client_secret` 校验每次消耗一次 Argon2id 且无限额：
/// 可被用来做在线口令爆破，更可直接放大为 CPU/内存 `DoS`。
/// 该用例应失败（当前无任何 429）。
#[tokio::test]
async fn sec_oauth_15_token_endpoint_throttles_client_secret_guessing() {
    let fixture = fixture().await;
    let form = code_exchange_form("deadbeef", CLIENT_ID, "Wrong!Secret-2026", REDIRECT, VERIFIER);

    let mut throttled = false;
    let mut statuses = Vec::new();
    // 8 次已超过登录端点的突发额度（5），足够区分「有限流」与「无限流」；
    // 每次尝试在 debug 下消耗数百毫秒 Argon2id，取 8 次让用例稳定在数秒内。
    for _ in 0..8 {
        let response = fixture.app.clone().oneshot_post_form("/api/oauth/token", &form).await;
        statuses.push(response.status());
        if response.status() == StatusCode::TOO_MANY_REQUESTS {
            throttled = true;
            break;
        }
    }
    assert!(
        throttled,
        "token 端点必须对 client_secret 猜测设限；8 次连续错误尝试的状态: {statuses:?}"
    );
}

// ---------------------------------------------------------------- 响应头

/// RFC 6749 §4.1.2：携带授权码的授权响应必须带 `Cache-Control: no-store`。
/// 缺失时 302 的 Location（含 code）可被中间层缓存/留存。该用例应失败。
#[tokio::test]
async fn sec_oauth_16_consent_authorization_response_is_not_cacheable() {
    let fixture = fixture().await;
    let response = consent(
        &fixture.app,
        &fixture.session,
        &consent_body(CLIENT_ID, REDIRECT, "profile", "xyz"),
    )
    .await;
    assert_eq!(response.status(), StatusCode::FOUND);

    let cache_control = response
        .headers()
        .get(header::CACHE_CONTROL)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default();
    assert!(
        cache_control.contains("no-store"),
        "授权响应必须带 Cache-Control: no-store（当前为 {cache_control:?}）"
    );
}

/// token 端点的成功与错误响应都不得可缓存。
#[tokio::test]
async fn sec_oauth_17_token_responses_are_not_cacheable() {
    let fixture = fixture().await;

    let success = exchange(
        &fixture.app,
        &issue_code(&fixture.app, &fixture.session, CLIENT_ID, REDIRECT, "profile").await,
        CLIENT_ID,
        CLIENT_SECRET,
        REDIRECT,
        VERIFIER,
    )
    .await;
    assert_eq!(success.status(), StatusCode::OK);
    let cache_control = success
        .headers()
        .get(header::CACHE_CONTROL)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .to_owned();
    assert!(cache_control.contains("no-store"), "成功响应必须 no-store");

    let failure =
        exchange(&fixture.app, "deadbeef", CLIENT_ID, CLIENT_SECRET, REDIRECT, VERIFIER).await;
    assert_eq!(failure.status(), StatusCode::BAD_REQUEST);
    let cache_control = failure
        .headers()
        .get(header::CACHE_CONTROL)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .to_owned();
    assert!(cache_control.contains("no-store"), "错误响应必须 no-store");
}

// ---------------------------------------------------------------- 客户端认证

/// 未知 / 无密钥 / 密钥错误 / 已禁用 / `client_id` 大小写不符，一律
/// 401 `invalid_client`，响应体不区分归属。
#[tokio::test]
async fn sec_oauth_18_client_auth_failures_are_indistinguishable() {
    let fixture = fixture().await;
    seed_client(fixture.dir.path(), "client-public", None, "https://public.example.com/cb", true)
        .await;
    seed_client(
        fixture.dir.path(),
        "client-disabled",
        Some("Disabled!Client-2026"),
        "https://disabled.example.com/cb",
        false,
    )
    .await;

    let cases = [
        ("ghost-client", "whatever"),
        (CLIENT_ID, "Wrong!Secret-2026"),
        (CLIENT_ID, ""),
        ("client-public", "any-secret"),
        ("client-disabled", "Disabled!Client-2026"),
        ("CLIENT-0001", CLIENT_SECRET),
    ];
    for (index, (client_id, secret)) in cases.into_iter().enumerate() {
        // 每例用独立对端 IP，避免合法限流把后续用例变成 429，掩盖认证错误形状
        let peer = format!("10.61.0.{}", index + 1);
        let response =
            exchange_from(&fixture.app, &peer, "deadbeef", client_id, secret, REDIRECT, VERIFIER)
                .await;
        assert_eq!(
            rfc_error_of(response).await,
            (StatusCode::UNAUTHORIZED, "invalid_client".to_owned()),
            "客户端认证失败必须统一为 401 invalid_client: {client_id}"
        );
    }
}

// ---------------------------------------------------------------- Location 编码

/// state 回传必须完整百分号编码：不得注入额外查询参数、不得产生 CRLF。
#[tokio::test]
async fn sec_oauth_19_state_is_encoded_in_location() {
    let fixture = fixture().await;
    let state = "a&code=evil\r\nX-Injected: 1";
    let response = consent(
        &fixture.app,
        &fixture.session,
        &consent_body(CLIENT_ID, REDIRECT, "profile", state),
    )
    .await;
    assert_eq!(response.status(), StatusCode::FOUND);
    let location = response.headers().get(header::LOCATION).unwrap().to_str().unwrap();

    assert!(!location.contains("code=evil"), "state 内的参数不得原样注入: {location}");
    assert!(!location.contains('\r') && !location.contains('\n'), "Location 不得含 CRLF");
    assert!(
        location.contains("state=a%26code%3Devil%0D%0AX-Injected%3A%201"),
        "state 必须被百分号编码: {location}"
    );
    assert_eq!(location.matches("code=").count(), 1, "Location 只允许一个 code 参数");
}

// ---------------------------------------------------------------- authorize 入参

/// authorize 只接受 `response_type=code`、白名单 `scope` 与合法 S256 challenge。
#[tokio::test]
async fn sec_oauth_20_authorize_rejects_non_code_and_malformed_pkce() {
    let fixture = fixture().await;

    let cases = [
        (authorize_query("token", "profile", CHALLENGE, "S256"), "unsupported_response_type"),
        (
            authorize_query("code%20token", "profile", CHALLENGE, "S256"),
            "unsupported_response_type",
        ),
        (authorize_query("code", "profile%0Aemail", CHALLENGE, "S256"), "invalid_scope"),
        (authorize_query("code", "profile%20email", CHALLENGE, "S256"), "invalid_scope"),
        (authorize_query("code", "profile", CHALLENGE, "plain"), "invalid_request"),
        (authorize_query("code", "profile", CHALLENGE, ""), "invalid_request"),
        (authorize_query("code", "profile", &"a".repeat(42), "S256"), "invalid_request"),
        (authorize_query("code", "profile", &"a".repeat(129), "S256"), "invalid_request"),
        (authorize_query("code", "profile", "short", "S256"), "invalid_request"),
    ];
    for (query, expected) in cases {
        let response =
            fixture.app.clone().oneshot_get_with_cookie("/api/oauth/authorize", &query, "").await;
        assert_eq!(
            rfc_error_of(response).await,
            (StatusCode::BAD_REQUEST, expected.to_owned()),
            "authorize 必须拒绝: {query}"
        );
    }
}

// ---------------------------------------------------------------- consent 重校验

/// GET 校验通过后客户端状态被改（禁用 / `redirect_uri` 变更），consent 必须重新
/// 读取客户端并按新状态判定，不得复用 GET 的结论（TOCTOU）。
#[tokio::test]
async fn sec_oauth_21_consent_revalidates_client_state() {
    let fixture = fixture().await;
    let body = consent_body(CLIENT_ID, REDIRECT, "profile", "xyz");

    let read = fixture
        .app
        .clone()
        .oneshot_get_with_cookie(
            "/api/oauth/authorize",
            &authorize_query("code", "profile", CHALLENGE, "S256"),
            "",
        )
        .await;
    assert_eq!(read.status(), StatusCode::OK);

    set_client_enabled(fixture.dir.path(), CLIENT_ID, false).await;
    let disabled = consent(&fixture.app, &fixture.session, &body).await;
    assert_eq!(
        consent_reason(disabled).await,
        (StatusCode::BAD_REQUEST, "invalid_client".to_owned()),
        "GET 后禁用客户端，consent 必须按新状态拒绝"
    );

    set_client_enabled(fixture.dir.path(), CLIENT_ID, true).await;
    let changed = "https://app.example.com/callback-v2";
    set_client_redirect(fixture.dir.path(), CLIENT_ID, changed).await;

    let stale = consent(&fixture.app, &fixture.session, &body).await;
    assert_eq!(
        consent_reason(stale).await,
        (StatusCode::BAD_REQUEST, "invalid_redirect_uri".to_owned()),
        "旧 redirect_uri 必须在 consent 时被重新拒绝"
    );

    let fresh = consent(
        &fixture.app,
        &fixture.session,
        &consent_body(CLIENT_ID, changed, "profile", "xyz"),
    )
    .await;
    assert_eq!(fresh.status(), StatusCode::FOUND, "新 redirect_uri 必须按新配置放行");
    let location = fresh.headers().get(header::LOCATION).unwrap().to_str().unwrap();
    assert!(location.starts_with(changed), "{location}");
}

// ---------------------------------------------------------------- 缺参

/// token 端点缺参：不得因缺 code / verifier 而 500 或泄露内部状态。
#[tokio::test]
async fn sec_oauth_22_missing_token_parameters_are_rejected() {
    let fixture = fixture().await;
    let cases = [
        (
            "grant_type=authorization_code&redirect_uri=https://app.example.com/callback\
             &code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk&client_id=client-0001\
             &client_secret=Nebula!Client-2026"
                .to_owned(),
            "invalid_grant",
        ),
        (
            "grant_type=authorization_code&code=deadbeef\
             &redirect_uri=https://app.example.com/callback&client_id=client-0001\
             &client_secret=Nebula!Client-2026"
                .to_owned(),
            "invalid_grant",
        ),
        (
            "grant_type=authorization_code&code=deadbeef\
             &redirect_uri=https://app.example.com/callback\
             &code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk\
             &client_secret=Nebula!Client-2026"
                .to_owned(),
            "invalid_request",
        ),
        ("grant_type=implicit&client_id=client-0001".to_owned(), "unsupported_grant_type"),
    ];
    for (form, expected) in cases {
        let response = fixture.app.clone().oneshot_post_form("/api/oauth/token", &form).await;
        assert_eq!(
            rfc_error_of(response).await,
            (StatusCode::BAD_REQUEST, expected.to_owned()),
            "缺参/非法 grant_type 必须以 RFC 错误拒绝: {form}"
        );
    }

    // JSON 体不是表单编码：同样按 invalid_request 拒绝，不得 500
    let json = serde_json::json!({
        "grant_type": "authorization_code",
        "code": "deadbeef",
        "client_id": CLIENT_ID,
        "client_secret": CLIENT_SECRET,
    });
    let response = fixture
        .app
        .clone()
        .oneshot_post_with("/api/oauth/token", &json.to_string(), "", None)
        .await;
    assert_eq!(
        rfc_error_of(response).await,
        (StatusCode::BAD_REQUEST, "invalid_request".to_owned()),
        "JSON 体必须被当作非表单请求拒绝"
    );
}
