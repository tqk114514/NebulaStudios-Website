//! HTTP 层边界与安全契约：写端点 CSRF 矩阵、会话 cookie 属性、认证边界、
//! 路由方法、未知路径与 ADR-0004 错误信封。
//!
//! 只补现有集成测试未覆盖的边界；全部经 `tower::ServiceExt::oneshot` 打 `Router`，
//! 不绑端口，时钟由 `ManualClock` 固定。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::net::SocketAddr;
use std::sync::Arc;

use axum::Router;
use axum::body::Body;
use axum::extract::ConnectInfo;
use axum::http::{Method, Request, StatusCode, header};
use axum::response::Response;
use chrono::TimeDelta;
use common::{
    OneshotPost, Session, body_text, cookie_header, login_as, login_session, raw_set_cookie,
    register_user, register_verified_user, set_cookie_of,
};
use nebula_server::api;
use nebula_server::context::Clock;
use nebula_server::model::oauth2::OAuthClient;
use nebula_server::model::tokens::{AccessClaims, sign_access};
use nebula_server::model::user::{USERNAME_MAX_CHARS, UserId};
use serde_json::Value;
use tempfile::TempDir;
use tower::ServiceExt;

const PASSWORD: &str = "Nebula!Passw0rd-2026";
const CLIENT_ID: &str = "client-edge";
const REDIRECT_URI: &str = "https://app.example.com/callback";
/// RFC 7636 附录 B 的 S256 challenge。
const CHALLENGE: &str = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";
/// `common::test_session` 的 access TTL（10 分钟）。
const ACCESS_MAX_AGE: i64 = 600;
/// `common::test_session` 的 refresh TTL（30 天）。
const REFRESH_MAX_AGE: i64 = 30 * 24 * 60 * 60;

/// 经 oneshot 发请求；总会注入限额键需要的对端地址。
async fn send(
    app: &Router,
    method: Method,
    uri: &str,
    headers: &[(&str, &str)],
    body: Body,
) -> Response {
    let mut builder = Request::builder().method(method).uri(uri);
    for (name, value) in headers {
        builder = builder.header(*name, *value);
    }
    let mut request = builder.body(body).expect("请求必须可构造");
    let addr: SocketAddr = "127.0.0.1:40000".parse().expect("测试地址必须合法");
    request.extensions_mut().insert(ConnectInfo(addr));
    app.clone().oneshot(request).await.expect("oneshot 必须成功")
}

async fn json_body(response: Response) -> Value {
    let status = response.status();
    let text = body_text(response).await;
    serde_json::from_str(&text)
        .unwrap_or_else(|err| panic!("{status} 响应不是 JSON: {err}: {text}"))
}

/// 断言状态码、JSON 形状与稳定 code；返回解析后的信封供进一步断言。
async fn assert_error(response: Response, status: StatusCode, code: &str) -> Value {
    assert_eq!(response.status(), status, "状态码不符合契约");
    let content_type = response
        .headers()
        .get(header::CONTENT_TYPE)
        .and_then(|value| value.to_str().ok())
        .unwrap_or_default()
        .to_owned();
    assert!(
        content_type.starts_with("application/json"),
        "错误响应必须是 JSON（Content-Type={content_type}）"
    );
    let json: Value =
        serde_json::from_str(&body_text(response).await).expect("错误响应必须是可解析的 JSON 信封");
    assert_eq!(json["code"].as_str(), Some(code), "错误码必须稳定: {json}");
    assert!(json["message"].is_string(), "message 必须是字符串: {json}");
    let object = json.as_object().expect("错误信封必须是对象");
    for key in object.keys() {
        assert!(
            matches!(key.as_str(), "code" | "message" | "details"),
            "ADR-0004 信封不允许键 {key}: {json}"
        );
    }
    json
}

/// 断言 422 信封并在 `details.fields` 中找到字段级 code。
async fn assert_validation(response: Response, field: &str, field_code: &str) -> Value {
    let json = assert_error(response, StatusCode::UNPROCESSABLE_ENTITY, "validation_failed").await;
    let fields = json["details"]["fields"].as_array().expect("422 必须带 details.fields");
    assert!(
        fields.iter().any(|entry| entry["field"] == field && entry["code"] == field_code),
        "details.fields 缺少 {field}/{field_code}: {json}"
    );
    json
}

fn assert_session_cookie_attributes(response: &Response, access: &str, refresh: &str, csrf: &str) {
    assert_eq!(
        raw_set_cookie(response, "nebula_access"),
        format!(
            "nebula_access={access}; Path=/; Max-Age={ACCESS_MAX_AGE}; HttpOnly; Secure; SameSite=Lax"
        )
    );
    assert_eq!(
        raw_set_cookie(response, "nebula_refresh"),
        format!(
            "nebula_refresh={refresh}; Path=/api/auth; Max-Age={REFRESH_MAX_AGE}; HttpOnly; Secure; SameSite=Lax"
        )
    );
    assert_eq!(
        raw_set_cookie(response, "nebula_csrf"),
        format!("nebula_csrf={csrf}; Path=/; Max-Age={REFRESH_MAX_AGE}; Secure; SameSite=Lax")
    );
}

/// 登出或刷新失败必须恰好清除三条会话 cookie，且保留各自 Path 与 `HttpOnly` 姿态。
fn assert_cleared_cookies(response: &Response) {
    let count = response.headers().get_all(header::SET_COOKIE).iter().count();
    assert_eq!(count, 3, "清除会话必须恰好下发三条 Set-Cookie");
    assert_eq!(
        raw_set_cookie(response, "nebula_access"),
        "nebula_access=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Lax"
    );
    assert_eq!(
        raw_set_cookie(response, "nebula_refresh"),
        "nebula_refresh=; Path=/api/auth; Max-Age=0; HttpOnly; Secure; SameSite=Lax"
    );
    assert_eq!(
        raw_set_cookie(response, "nebula_csrf"),
        "nebula_csrf=; Path=/; Max-Age=0; Secure; SameSite=Lax"
    );
}

#[derive(Debug, Clone, Copy)]
enum Actor {
    User,
    Admin,
}

/// 一个 cookie 鉴权写端点的请求快照；矩阵对同一批端点施加三种来源。
struct CookieWrite {
    label: &'static str,
    method: Method,
    uri: String,
    content_type: &'static str,
    body: Vec<u8>,
    actor: Actor,
}

struct EdgeFixture {
    app: Router,
    user: Session,
    admin: Session,
    target_id: UserId,
    _dir: TempDir,
}

async fn edge_fixture() -> EdgeFixture {
    let (mut state, clock, mailbox, dir) = common::app_state().await;
    state.admin_emails = Arc::new(HashSet::from(["admin@nebula.st".to_owned()]));
    state
        .oauth_repo
        .upsert_client(
            &OAuthClient {
                id: CLIENT_ID.to_owned(),
                name: "Edge App".to_owned(),
                redirect_uri: REDIRECT_URI.to_owned(),
                enabled: true,
            },
            "unused-client-secret-hash",
            clock.now(),
        )
        .await
        .expect("OAuth 客户端必须落库");
    let app = api::router(state.clone());
    register_verified_user(&app, "admin@nebula.st", "admin", PASSWORD, &mailbox).await;
    register_verified_user(&app, "nova@nebula.st", "nova", PASSWORD, &mailbox).await;
    let admin = login_as(&app, "admin@nebula.st", PASSWORD).await;
    let user = login_as(&app, "nova@nebula.st", PASSWORD).await;
    let target_id = state.user_repo.find_by_email("nova@nebula.st").await.unwrap().unwrap().id;
    EdgeFixture { app, user, admin, target_id, _dir: dir }
}

impl EdgeFixture {
    fn cookie_writes(&self) -> Vec<CookieWrite> {
        vec![
            CookieWrite {
                label: "avatar",
                method: Method::POST,
                uri: "/api/user/avatar".to_owned(),
                content_type: "multipart/form-data; boundary=nebula-edge",
                body: b"--nebula-edge--\r\n".to_vec(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "export",
                method: Method::POST,
                uri: "/api/user/export".to_owned(),
                content_type: "application/json",
                body: Vec::new(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "consents",
                method: Method::POST,
                uri: "/api/user/consents".to_owned(),
                content_type: "application/json",
                body: serde_json::json!({ "policy": "privacy", "version": "v1" })
                    .to_string()
                    .into_bytes(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "username",
                method: Method::PATCH,
                uri: "/api/user/username".to_owned(),
                content_type: "application/json",
                body: serde_json::json!({ "username": "nova", "current_password": PASSWORD })
                    .to_string()
                    .into_bytes(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "email_change",
                method: Method::POST,
                uri: "/api/user/email/change/request".to_owned(),
                content_type: "application/json",
                body: serde_json::json!({
                    "new_email": "moved@nebula.st",
                    "current_password": PASSWORD,
                })
                .to_string()
                .into_bytes(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "totp_setup",
                method: Method::POST,
                uri: "/api/user/totp/setup".to_owned(),
                content_type: "application/json",
                body: serde_json::json!({ "password": PASSWORD }).to_string().into_bytes(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "oauth_consent",
                method: Method::POST,
                uri: "/api/oauth/authorize/consent".to_owned(),
                content_type: "application/json",
                body: serde_json::json!({
                    "client_id": CLIENT_ID,
                    "redirect_uri": REDIRECT_URI,
                    "scope": "profile",
                    "code_challenge": CHALLENGE,
                    "state": "edge",
                })
                .to_string()
                .into_bytes(),
                actor: Actor::User,
            },
            CookieWrite {
                label: "oauth_grant_revoke",
                method: Method::DELETE,
                uri: format!("/api/user/oauth/grants/{CLIENT_ID}"),
                content_type: "application/json",
                body: Vec::new(),
                actor: Actor::User,
            },
            // 封禁会使同一用户的后续请求 403，故必须排在矩阵最后。
            CookieWrite {
                label: "admin_ban",
                method: Method::POST,
                uri: format!("/api/admin/users/{}/ban", self.target_id),
                content_type: "application/json",
                body: serde_json::json!({ "kind": "permanent", "reason": "edge" })
                    .to_string()
                    .into_bytes(),
                actor: Actor::Admin,
            },
        ]
    }

    async fn send_cookie_write(&self, write: &CookieWrite, csrf_header: Option<&str>) -> Response {
        let cookie = match write.actor {
            Actor::User => cookie_header(Some(&self.user.access), None, Some(&self.user.csrf)),
            Actor::Admin => cookie_header(Some(&self.admin.access), None, Some(&self.admin.csrf)),
        };
        let mut headers = vec![("content-type", write.content_type), ("cookie", cookie.as_str())];
        if let Some(value) = csrf_header {
            headers.push(("x-csrf-token", value));
        }
        send(&self.app, write.method.clone(), &write.uri, &headers, Body::from(write.body.clone()))
            .await
    }

    async fn send_bearer(&self, write: &CookieWrite) -> Response {
        let token = match write.actor {
            Actor::User => self.user.access.as_str(),
            Actor::Admin => self.admin.access.as_str(),
        };
        let bearer = format!("Bearer {token}");
        let headers = [("content-type", write.content_type), ("authorization", bearer.as_str())];
        send(&self.app, write.method.clone(), &write.uri, &headers, Body::from(write.body.clone()))
            .await
    }

    async fn send_user_json(&self, method: Method, uri: &str, body: &str) -> Response {
        let cookie = cookie_header(Some(&self.user.access), None, Some(&self.user.csrf));
        send(
            &self.app,
            method,
            uri,
            &[
                ("cookie", cookie.as_str()),
                ("content-type", "application/json"),
                ("x-csrf-token", self.user.csrf.as_str()),
            ],
            Body::from(body.to_owned()),
        )
        .await
    }

    async fn send_admin_json(&self, method: Method, uri: &str, body: &str) -> Response {
        let cookie = cookie_header(Some(&self.admin.access), None, Some(&self.admin.csrf));
        send(
            &self.app,
            method,
            uri,
            &[
                ("cookie", cookie.as_str()),
                ("content-type", "application/json"),
                ("x-csrf-token", self.admin.csrf.as_str()),
            ],
            Body::from(body.to_owned()),
        )
        .await
    }
}

fn write_named<'a>(writes: &'a [CookieWrite], label: &str) -> &'a CookieWrite {
    writes.iter().find(|write| write.label == label).unwrap_or_else(|| panic!("缺少端点 {label}"))
}

#[tokio::test]
async fn cookie_write_endpoints_reject_missing_csrf_header() {
    let fixture = edge_fixture().await;

    for write in fixture.cookie_writes() {
        // cookie 来源且无 X-CSRF-Token：必须在 handler 之前被 403 拦下
        let response = fixture.send_cookie_write(&write, None).await;
        let json = assert_error(response, StatusCode::FORBIDDEN, "csrf_invalid").await;
        assert_eq!(json["message"], "CSRF 校验失败", "{} 未被 CSRF 门禁拦下", write.label);
    }
}

#[tokio::test]
async fn cookie_write_endpoints_reject_mismatched_csrf_header() {
    let fixture = edge_fixture().await;
    let wrong = "0123456789abcdef".repeat(4);

    for write in fixture.cookie_writes() {
        let response = fixture.send_cookie_write(&write, Some(&wrong)).await;
        assert_error(response, StatusCode::FORBIDDEN, "csrf_invalid").await;
    }
}

#[tokio::test]
async fn bearer_origin_is_exempt_from_csrf_on_cookie_write_endpoints() {
    let fixture = edge_fixture().await;

    for write in fixture.cookie_writes() {
        // 无 cookie、仅 Bearer：跨站页面无法携带该头，门禁必须放行到 handler
        let response = fixture.send_bearer(&write).await;
        match write.label {
            "avatar" => {
                assert_validation(response, "avatar", "avatar_missing").await;
            }
            "export" => {
                assert_eq!(response.status(), StatusCode::OK);
                let json = json_body(response).await;
                assert!(json["download_url"].as_str().is_some(), "导出必须返回下载地址");
            }
            "consents" => {
                assert_validation(response, "policy", "policy_unknown").await;
            }
            "username" | "email_change" | "admin_ban" => {
                assert_eq!(response.status(), StatusCode::NO_CONTENT);
            }
            "totp_setup" => {
                assert_eq!(response.status(), StatusCode::OK);
                let json = json_body(response).await;
                assert!(json["secret"].is_string(), "TOTP setup 必须返回密钥");
            }
            "oauth_consent" => {
                assert_eq!(response.status(), StatusCode::FOUND);
                assert!(response.headers().get(header::LOCATION).is_some(), "consent 必须回跳携码");
            }
            "oauth_grant_revoke" => {
                assert_error(response, StatusCode::NOT_FOUND, "oauth_grant_not_found").await;
            }
            other => panic!("未覆盖的写端点: {other}"),
        }
    }
}

#[tokio::test]
async fn session_cookies_carry_expected_attributes_across_login_refresh_and_logout() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;

    let login_body =
        serde_json::json!({ "identifier": "nova@nebula.st", "password": PASSWORD }).to_string();
    let login = app.clone().oneshot_post("/api/auth/login", &login_body).await;
    assert_eq!(login.status(), StatusCode::OK);
    let access = set_cookie_of(&login, "nebula_access").expect("必须下发 access cookie");
    let refresh = set_cookie_of(&login, "nebula_refresh").expect("必须下发 refresh cookie");
    let csrf = set_cookie_of(&login, "nebula_csrf").expect("必须下发 csrf cookie");
    assert_session_cookie_attributes(&login, &access, &refresh, &csrf);

    let rotated = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&refresh), Some(&csrf)),
            Some(&csrf),
        )
        .await;
    assert_eq!(rotated.status(), StatusCode::NO_CONTENT);
    let new_access = set_cookie_of(&rotated, "nebula_access").expect("刷新必须续发 access");
    let new_refresh = set_cookie_of(&rotated, "nebula_refresh").expect("刷新必须轮转 refresh");
    let new_csrf = set_cookie_of(&rotated, "nebula_csrf").expect("刷新必须轮转 csrf");
    assert_ne!(new_refresh, refresh);
    assert_ne!(new_csrf, csrf);
    assert_session_cookie_attributes(&rotated, &new_access, &new_refresh, &new_csrf);

    let logout = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&new_access), None, Some(&new_csrf)),
            Some(&new_csrf),
        )
        .await;
    assert_eq!(logout.status(), StatusCode::NO_CONTENT);
    assert_cleared_cookies(&logout);
}

#[tokio::test]
async fn refresh_failure_clears_all_session_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

    // CSRF 配对有效但 refresh 值未知：401 并清空三条会话 cookie
    let unknown_refresh = "f".repeat(64);
    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/refresh",
            "",
            &cookie_header(None, Some(&unknown_refresh), Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert_cleared_cookies(&response);
    assert_error(response, StatusCode::UNAUTHORIZED, "refresh_invalid").await;
}

#[tokio::test]
async fn first_party_endpoints_reject_requests_without_credentials() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let target = uuid::Uuid::now_v7();

    let cases = [
        (Method::GET, "/api/user".to_owned()),
        (Method::GET, "/api/user/consents".to_owned()),
        (Method::GET, "/api/user/oauth/grants".to_owned()),
        (Method::GET, "/api/user/social".to_owned()),
        (Method::GET, "/api/admin/stats".to_owned()),
        (Method::GET, "/api/admin/users".to_owned()),
        (Method::POST, "/api/user/export".to_owned()),
        (Method::POST, "/api/user/consents".to_owned()),
        (Method::PATCH, "/api/user/username".to_owned()),
        (Method::DELETE, "/api/user/oauth/grants/client-edge".to_owned()),
        (Method::DELETE, "/api/user/social/google".to_owned()),
        (Method::POST, format!("/api/admin/users/{target}/ban")),
        (Method::POST, "/api/oauth/authorize/consent".to_owned()),
        (Method::POST, "/api/auth/logout".to_owned()),
    ];

    for (method, uri) in cases {
        let response =
            send(&app, method, &uri, &[("content-type", "application/json")], Body::empty()).await;
        assert_error(response, StatusCode::UNAUTHORIZED, "unauthorized").await;
    }
}

#[tokio::test]
async fn client_grant_token_cannot_call_first_party_endpoints() {
    let (app, state, clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let user = state
        .user_repo
        .find_by_email("nova@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");
    let claims = AccessClaims::client_grant(
        user.id,
        CLIENT_ID,
        "profile",
        clock.now(),
        TimeDelta::minutes(10),
    )
    .expect("scope 必须合法");
    let token = sign_access(&claims, state.jwt.encode()).expect("签发必须成功");
    let bearer = format!("Bearer {token}");

    // ADR-0006：audience 不同的令牌在第一方端点上与无效令牌同体返回 401
    let cases = [
        (Method::GET, "/api/user"),
        (Method::GET, "/api/user/oauth/grants"),
        (Method::POST, "/api/auth/logout"),
        (Method::POST, "/api/user/export"),
        (Method::POST, "/api/user/totp/setup"),
        (Method::GET, "/api/admin/stats"),
    ];
    for (method, uri) in cases {
        let response = send(
            &app,
            method,
            uri,
            &[("authorization", bearer.as_str()), ("content-type", "application/json")],
            Body::empty(),
        )
        .await;
        assert_error(response, StatusCode::UNAUTHORIZED, "unauthorized").await;
    }

    // cookie 来源同样只接受 user-session audience；CSRF 配对有效也不放行
    let csrf = "a".repeat(64);
    let cookie = cookie_header(Some(&token), None, Some(&csrf));
    let response = send(
        &app,
        Method::POST,
        "/api/user/consents",
        &[
            ("cookie", cookie.as_str()),
            ("content-type", "application/json"),
            ("x-csrf-token", csrf.as_str()),
        ],
        Body::from(serde_json::json!({ "policy": "privacy", "version": "v1" }).to_string()),
    )
    .await;
    assert_error(response, StatusCode::UNAUTHORIZED, "unauthorized").await;
}

#[tokio::test]
async fn banned_user_with_unexpired_access_token_is_forbidden() {
    let fixture = edge_fixture().await;
    let writes = fixture.cookie_writes();
    let ban = write_named(&writes, "admin_ban");

    let banned = fixture.send_cookie_write(ban, Some(&fixture.admin.csrf)).await;
    assert_eq!(banned.status(), StatusCode::NO_CONTENT);

    // 封禁发生在 access token 有效期内：读端点必须 403，而不是等 token 过期
    let profile_cookie = cookie_header(Some(&fixture.user.access), None, Some(&fixture.user.csrf));
    let profile =
        fixture.app.clone().oneshot_get_with_cookie("/api/user", "", &profile_cookie).await;
    assert_error(profile, StatusCode::FORBIDDEN, "banned").await;

    // 写端点同样在 CSRF 通过后由封禁状态拦截
    let consents = write_named(&writes, "consents");
    let write = fixture.send_cookie_write(consents, Some(&fixture.user.csrf)).await;
    assert_error(write, StatusCode::FORBIDDEN, "banned").await;
}

#[tokio::test]
async fn wrong_methods_are_405_and_unknown_paths_are_404() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let wrong_methods = [
        (Method::GET, "/api/auth/logout"),
        (Method::GET, "/api/auth/login"),
        (Method::GET, "/api/auth/register"),
        (Method::GET, "/api/auth/refresh"),
        (Method::GET, "/api/auth/password/change"),
        (Method::GET, "/api/user/avatar"),
        (Method::GET, "/api/user/export"),
        (Method::GET, "/api/user/email/change/confirm"),
        (Method::GET, "/api/oauth/token"),
        (Method::GET, "/api/oauth/authorize/consent"),
        (Method::DELETE, "/api/user/username"),
        (Method::POST, "/api/policies"),
        (Method::POST, "/api/admin/stats"),
    ];
    for (method, uri) in wrong_methods {
        let response = send(&app, method, uri, &[], Body::empty()).await;
        assert_eq!(response.status(), StatusCode::METHOD_NOT_ALLOWED, "{uri} 必须返回 405");
    }

    for uri in ["/api/nonexistent", "/api/user/nonexistent", "/api/users/not-a-uuid/avatar"] {
        let response = send(&app, Method::GET, uri, &[], Body::empty()).await;
        assert_eq!(response.status(), StatusCode::NOT_FOUND, "{uri} 必须返回 404");
    }
}

#[tokio::test]
async fn malformed_missing_and_overlong_inputs_are_4xx_with_stable_codes() {
    let fixture = edge_fixture().await;

    // 畸形 JSON：登录与鉴权后的写端点都必须走 malformed_json，而不是 500
    let malformed_login = send(
        &fixture.app,
        Method::POST,
        "/api/auth/login",
        &[("content-type", "application/json")],
        Body::from("{"),
    )
    .await;
    assert_error(malformed_login, StatusCode::BAD_REQUEST, "malformed_json").await;

    let malformed_consents = fixture.send_user_json(Method::POST, "/api/user/consents", "{").await;
    assert_error(malformed_consents, StatusCode::BAD_REQUEST, "malformed_json").await;

    // 缺字段在 serde 边界统一为 400 malformed_json
    let missing_username =
        fixture.send_user_json(Method::PATCH, "/api/user/username", r#"{"username":"Nova"}"#).await;
    assert_error(missing_username, StatusCode::BAD_REQUEST, "malformed_json").await;

    let missing_version =
        fixture.send_user_json(Method::POST, "/api/user/consents", r#"{"policy":"privacy"}"#).await;
    assert_error(missing_version, StatusCode::BAD_REQUEST, "malformed_json").await;

    // 超长字段在业务校验层得到字段级 422
    let long_username = serde_json::json!({
        "username": "a".repeat(USERNAME_MAX_CHARS + 1),
        "current_password": PASSWORD,
    })
    .to_string();
    let response =
        fixture.send_user_json(Method::PATCH, "/api/user/username", &long_username).await;
    assert_validation(response, "username", "username_too_long").await;

    let long_email = serde_json::json!({
        "new_email": format!("{}@nebula.st", "a".repeat(250)),
        "current_password": PASSWORD,
    })
    .to_string();
    let response =
        fixture.send_user_json(Method::POST, "/api/user/email/change/request", &long_email).await;
    assert_validation(response, "new_email", "invalid_email").await;

    let long_reason =
        serde_json::json!({ "kind": "permanent", "reason": "x".repeat(501) }).to_string();
    let ban_uri = format!("/api/admin/users/{}/ban", fixture.target_id);
    let response = fixture.send_admin_json(Method::POST, &ban_uri, &long_reason).await;
    assert_validation(response, "reason", "reason_too_long").await;

    // 登录口令输入上限先于 Argon2：超长输入按字段错误拒绝
    let long_password = serde_json::json!({
        "identifier": "nova@nebula.st",
        "password": "a".repeat(129),
    })
    .to_string();
    let response = send(
        &fixture.app,
        Method::POST,
        "/api/auth/login",
        &[("content-type", "application/json")],
        Body::from(long_password),
    )
    .await;
    assert_validation(response, "password", "password_invalid").await;

    // 查询串解析失败与分页边界同样是稳定信封
    let admin_cookie = cookie_header(Some(&fixture.admin.access), None, Some(&fixture.admin.csrf));
    let malformed_query = send(
        &fixture.app,
        Method::GET,
        "/api/admin/users?limit=abc",
        &[("cookie", admin_cookie.as_str())],
        Body::empty(),
    )
    .await;
    assert_error(malformed_query, StatusCode::BAD_REQUEST, "malformed_query").await;

    let zero_limit = send(
        &fixture.app,
        Method::GET,
        "/api/admin/users?limit=0",
        &[("cookie", admin_cookie.as_str())],
        Body::empty(),
    )
    .await;
    assert_validation(zero_limit, "limit", "limit_out_of_range").await;
}

#[tokio::test]
async fn logout_without_credentials_clears_session_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let response = app.clone().oneshot_post("/api/auth/logout", "").await;

    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert_cleared_cookies(&response);
}

#[tokio::test]
async fn refresh_csrf_failure_clears_session_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

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
    assert_cleared_cookies(&response);
}

#[tokio::test]
async fn logout_csrf_failure_clears_session_cookies() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;

    let response = app
        .clone()
        .oneshot_post_with(
            "/api/auth/logout",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            None,
        )
        .await;
    assert_eq!(response.status(), StatusCode::FORBIDDEN);
    assert_cleared_cookies(&response);
}

#[tokio::test]
async fn avatar_with_non_multipart_content_type_uses_error_envelope() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = login_session(&app).await;
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));

    let response = send(
        &app,
        Method::POST,
        "/api/user/avatar",
        &[
            ("content-type", "application/json"),
            ("cookie", cookie.as_str()),
            ("x-csrf-token", session.csrf.as_str()),
        ],
        Body::from("{}"),
    )
    .await;

    assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    assert_error(response, StatusCode::BAD_REQUEST, "malformed_multipart").await;
}
