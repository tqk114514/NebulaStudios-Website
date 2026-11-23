//! HTTP 层对抗性安全测试：限流与 DoS、注入、加密边界、错误与日志泄漏、CORS、
//! 认证响应的缓存姿态。
//!
//! 每个用例断言「安全行为」，失败即被测代码存在可复现缺陷；通过的部分同时作为
//! 回归护栏（如 X-Forwarded-For 不得绕过限额、SQL 绑定参数、JWT 算法固定为 ES256）。
//! 全部经 `tower::ServiceExt::oneshot` 直接打 `Router`，不绑端口、不访问外网。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::{Duration, Instant};

use axum::Router;
use axum::body::Body;
use axum::extract::ConnectInfo;
use axum::http::{HeaderMap, HeaderValue, Method, Request, StatusCode};
use axum::response::Response;
use base64::Engine as _;
use common::{OneshotPost, Session};
use http_body_util::BodyExt;
use nebula_server::api::{self, AppState, JwtKeys};
use nebula_server::config::CryptoConfig;
use nebula_server::crypto::{AesGcmCipher, HmacCodeHasher};
use nebula_server::model::oauth2::OAuthClient;
use nebula_server::model::password::PasswordHash;
use nebula_server::model::user::UserId;
use nebula_server::turnstile::TurnstileClient;
use tower::ServiceExt;

const PASSWORD: &str = "Nebula!Passw0rd-2026";
/// RFC 7636 附录 B 的 S256 challenge。
const CHALLENGE: &str = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM";

/// 构造请求；`peer` 为 `None` 时不注入限额键所需的 `ConnectInfo`。
fn request(
    method: Method,
    uri: &str,
    peer: Option<&str>,
    headers: &[(&str, &str)],
    body: Body,
) -> Request<Body> {
    let mut builder = Request::builder().method(method).uri(uri);
    for (name, value) in headers {
        builder = builder.header(*name, *value);
    }
    let mut request = builder.body(body).expect("请求必须可构造");
    if let Some(peer) = peer {
        let addr: SocketAddr = format!("{peer}:40000").parse().expect("测试对端地址必须合法");
        request.extensions_mut().insert(ConnectInfo(addr));
    }
    request
}

fn json_request(uri: &str, peer: Option<&str>, body: &serde_json::Value) -> Request<Body> {
    request(
        Method::POST,
        uri,
        peer,
        &[("content-type", "application/json")],
        Body::from(body.to_string()),
    )
}

async fn send(app: &Router, request: Request<Body>) -> Response {
    app.clone().oneshot(request).await.expect("oneshot 必须成功")
}

async fn body_bytes(response: Response) -> Vec<u8> {
    BodyExt::collect(response.into_body()).await.expect("读体必须成功").to_bytes().to_vec()
}

async fn json_response(response: Response) -> (StatusCode, HeaderMap, serde_json::Value) {
    let status = response.status();
    let headers = response.headers().clone();
    let bytes = body_bytes(response).await;
    let value = serde_json::from_slice(&bytes)
        .unwrap_or_else(|_| panic!("响应必须是 JSON: {}", String::from_utf8_lossy(&bytes)));
    (status, headers, value)
}

fn header_value<'a>(headers: &'a HeaderMap, name: &str) -> Option<&'a str> {
    headers.get(name).and_then(|value| value.to_str().ok())
}

fn session_cookie(session: &Session) -> String {
    common::cookie_header(Some(&session.access), None, Some(&session.csrf))
}

/// 组装单个文件字段的 multipart 请求体。
fn multipart(field: &str, data: &[u8]) -> (String, Vec<u8>) {
    let boundary = "----nebula-sec-boundary";
    let mut body = Vec::with_capacity(data.len() + 256);
    body.extend_from_slice(
        format!(
            "--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"a.bin\"\r\nContent-Type: application/octet-stream\r\n\r\n"
        )
        .as_bytes(),
    );
    body.extend_from_slice(data);
    body.extend_from_slice(format!("\r\n--{boundary}--\r\n").as_bytes());
    (format!("multipart/form-data; boundary={boundary}"), body)
}

/// 装配路由；`admin_email` 非空时该邮箱既是已验证用户也是配置化管理员。
async fn app_fixture(
    admin_email: Option<&str>,
) -> (Router, AppState, common::Mailbox, tempfile::TempDir) {
    let (mut state, _clock, mailbox, dir) = common::app_state().await;
    if let Some(email) = admin_email {
        state.admin_emails = Arc::new(HashSet::from([email.to_owned()]));
    }
    let app = api::router(state.clone());
    (app, state, mailbox, dir)
}

async fn seed_oauth_client(state: &AppState, id: &str, redirect_uri: &str, secret: &str) {
    let hash = PasswordHash::hash(secret).expect("测试口令哈希必须成功");
    state
        .oauth_repo
        .upsert_client(
            &OAuthClient {
                id: id.to_owned(),
                name: "Security Test Client".to_owned(),
                redirect_uri: redirect_uri.to_owned(),
                enabled: true,
            },
            hash.as_str(),
            state.clock.now(),
        )
        .await
        .expect("写入 OAuth 客户端必须成功");
}

// ---------------------------------------------------------------------------
// 限流与 DoS
// ---------------------------------------------------------------------------

/// 限额键取自对端 `ConnectInfo`：伪造 X-Forwarded-For / X-Real-IP / Forwarded
/// 不得换到新桶。反向代价（部署在反代后所有客户端共用一个桶）另见报告。
#[tokio::test]
async fn sec_http_01_forwarded_headers_do_not_bypass_rate_limit() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let peer = "10.66.0.1";
    let body =
        serde_json::json!({ "identifier": "ghost@nebula.st", "password": "Wrong!Passw0rd-2026" });

    // 登录突发额度 5；每次携带不同的转发头，只有对端 IP 相同
    for index in 0..5 {
        let response = send(
            &app,
            request(
                Method::POST,
                "/api/auth/login",
                Some(peer),
                &[
                    ("content-type", "application/json"),
                    ("x-forwarded-for", &format!("203.0.113.{index}")),
                    ("x-real-ip", &format!("198.51.100.{index}")),
                    ("forwarded", &format!("for=192.0.2.{index}")),
                ],
                Body::from(body.to_string()),
            ),
        )
        .await;
        assert_eq!(response.status(), StatusCode::UNAUTHORIZED, "前 5 次应只受突发额度约束");
    }

    let response = send(
        &app,
        request(
            Method::POST,
            "/api/auth/login",
            Some(peer),
            &[
                ("content-type", "application/json"),
                ("x-forwarded-for", "203.0.113.250"),
                ("x-real-ip", "198.51.100.250"),
                ("forwarded", "for=192.0.2.250"),
            ],
            Body::from(body.to_string()),
        ),
    )
    .await;
    assert_eq!(
        response.status(),
        StatusCode::TOO_MANY_REQUESTS,
        "转发头不得改变限额键：伪造新 IP 必须仍是同一桶"
    );
}

/// 429 必须同时给出机器可读的 `Retry-After` 头；仅把秒数塞进响应体
/// 会让通用 HTTP 客户端与中间层无法退避。
#[tokio::test]
async fn sec_http_02_rate_limit_response_carries_retry_after_header() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let peer = "10.66.0.2";
    let body = serde_json::json!({ "email": "" });

    // 空邮箱在限额层之后立即 422；突发 3
    for _ in 0..3 {
        let response =
            send(&app, json_request("/api/auth/password/reset/request", Some(peer), &body)).await;
        assert_eq!(response.status(), StatusCode::UNPROCESSABLE_ENTITY);
    }

    let (status, headers, json) = json_response(
        send(&app, json_request("/api/auth/password/reset/request", Some(peer), &body)).await,
    )
    .await;
    assert_eq!(status, StatusCode::TOO_MANY_REQUESTS);
    assert_eq!(json["code"], "rate_limited");
    assert!(json["details"]["retry_after"].as_u64().is_some(), "信封内秒数必须在");
    assert!(
        header_value(&headers, "retry-after").is_some(),
        "429 必须带 Retry-After 头，当前只有响应体 details.retry_after"
    );
}

/// `/api/oauth/token` 匿名可达，每次失败尝试都对 `client_secret` 跑一次
/// Argon2id（19 MiB / 2 轮），却未挂任何限额层——单请求即 CPU/内存放大型 `DoS`。
#[tokio::test]
async fn sec_http_03_oauth_token_endpoint_is_rate_limited() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let app = api::router(state.clone());
    seed_oauth_client(&state, "security-client", "https://app.example/cb", "correct-client-secret")
        .await;

    let form = "grant_type=authorization_code&client_id=security-client&client_secret=wrong-secret&code=deadbeef&redirect_uri=https%3A%2F%2Fapp.example%2Fcb&code_verifier=dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk";
    let peer = "10.66.0.3";
    let mut statuses = Vec::new();
    let started = Instant::now();
    // 实测每次失败尝试约 0.46 s（debug 构建）——全部来自对 client_secret 的 Argon2id
    for _ in 0..6 {
        let response = send(
            &app,
            request(
                Method::POST,
                "/api/oauth/token",
                Some(peer),
                &[("content-type", "application/x-www-form-urlencoded")],
                Body::from(form),
            ),
        )
        .await;
        statuses.push(response.status());
    }
    let elapsed = started.elapsed();

    assert!(
        statuses.contains(&StatusCode::TOO_MANY_REQUESTS),
        "匿名 token 端点必须限额（6 次错误密钥尝试，耗时 {elapsed:?}）：{statuses:?}"
    );
}

/// 限额键缺失（无 `ConnectInfo`）时必须失败关闭，而不是放行。
#[tokio::test]
async fn sec_http_05_missing_peer_info_fails_closed() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let body =
        serde_json::json!({ "identifier": "ghost@nebula.st", "password": "Wrong!Passw0rd-2026" });

    let (status, _headers, json) = json_response(
        send(
            &app,
            request(
                Method::POST,
                "/api/auth/login",
                None,
                &[("content-type", "application/json")],
                Body::from(body.to_string()),
            ),
        )
        .await,
    )
    .await;

    assert_eq!(status, StatusCode::INTERNAL_SERVER_ERROR, "无法取限额键时不得跳过限额");
    assert_eq!(json["code"], "internal_error");
}

// ---------------------------------------------------------------------------
// 体积与解析
// ---------------------------------------------------------------------------

/// 头像路由把 body 上限放宽到 `MAX_REQUEST_BYTES`（5 MiB + 64 KiB）：
/// 低于它必须进入图像解码（422 字段错误），高于它必须以 413 信封结束。
#[tokio::test]
async fn sec_http_06_avatar_upload_limits_are_exact_and_enveloped() {
    let (app, _state, mailbox, _dir) = app_fixture(None).await;
    common::register_verified_user(&app, "nova@nebula.st", "nova", PASSWORD, &mailbox).await;
    let session = common::login_as(&app, "nova@nebula.st", PASSWORD).await;
    let cookie = session_cookie(&session);

    // 70 KiB > 全局 64 KiB：必须越过全局上限，进到图像解码再以字段级 422 结束
    let junk = vec![b'A'; 70 * 1024];
    let (content_type, body) = multipart("avatar", &junk);
    let (status, _headers, json) = json_response(
        send(
            &app,
            request(
                Method::POST,
                "/api/user/avatar",
                Some("10.66.1.1"),
                &[
                    ("content-type", &content_type),
                    ("cookie", &cookie),
                    ("x-csrf-token", &session.csrf),
                ],
                Body::from(body),
            ),
        )
        .await,
    )
    .await;
    assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY, "头像上限不得被全局 64 KiB 覆盖：{json}");
    assert_eq!(json["details"]["fields"][0]["code"], "avatar_unsupported_format");

    // 超过头像路由自己的上限：必须以 413 信封结束，而不是 400
    let oversized = vec![0u8; 5_400_000];
    let (content_type, body) = multipart("avatar", &oversized);
    let (status, _headers, json) = json_response(
        send(
            &app,
            request(
                Method::POST,
                "/api/user/avatar",
                Some("10.66.1.2"),
                &[
                    ("content-type", &content_type),
                    ("cookie", &cookie),
                    ("x-csrf-token", &session.csrf),
                ],
                Body::from(body),
            ),
        )
        .await,
    )
    .await;
    assert_eq!(status, StatusCode::PAYLOAD_TOO_LARGE, "超限必须是 413：{json}");
    assert_eq!(json["code"], "payload_too_large");
}

/// 上限按实际读取字节数生效，不信任 `Content-Length`；声明的长度再小也挡不住
/// 超出真实字节被计费。
#[tokio::test]
async fn sec_http_08_body_limit_counts_actual_bytes_not_declared_length() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let body = vec![b'a'; 100 * 1024];

    let (status, _headers, json) = json_response(
        send(
            &app,
            request(
                Method::POST,
                "/api/auth/register",
                Some("10.66.1.8"),
                &[("content-type", "application/json"), ("content-length", "12")],
                Body::from(body),
            ),
        )
        .await,
    )
    .await;

    assert_eq!(status, StatusCode::PAYLOAD_TOO_LARGE);
    assert_eq!(json["code"], "payload_too_large");
}

/// 深嵌套 JSON 必须被 serde 的递归上限拒绝（400），不得耗尽栈或阻塞 worker。
#[tokio::test]
async fn sec_http_09_deep_json_nesting_is_bounded() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let depth = 500;
    let body = format!(
        r#"{{"email":"nova@nebula.st","junk":{}{}}}"#,
        "[".repeat(depth),
        "]".repeat(depth)
    );

    let started = Instant::now();
    let (status, _headers, json) = json_response(
        send(
            &app,
            request(
                Method::POST,
                "/api/auth/register",
                Some("10.66.1.9"),
                &[("content-type", "application/json")],
                Body::from(body),
            ),
        )
        .await,
    )
    .await;
    let elapsed = started.elapsed();

    assert_eq!(status, StatusCode::BAD_REQUEST);
    assert_eq!(json["code"], "malformed_json");
    assert!(elapsed < Duration::from_secs(2), "深嵌套必须在解析上限处快速失败: {elapsed:?}");
}

// ---------------------------------------------------------------------------
// 注入
// ---------------------------------------------------------------------------

/// SQL 一律绑定参数：登录标识与 `client_id` 的注入串只能得到普通未命中。
#[tokio::test]
async fn sec_http_10_sql_injection_in_login_and_client_id_is_inert() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    for (index, identifier) in ["' OR '1'='1' --", "\" OR 1=1 --", "admin'/*"].iter().enumerate() {
        let body =
            serde_json::json!({ "identifier": identifier, "password": "Wrong!Passw0rd-2026" });
        let response =
            send(&app, json_request("/api/auth/login", Some(&format!("10.66.2.{index}")), &body))
                .await;
        assert_eq!(
            response.status(),
            StatusCode::UNAUTHORIZED,
            "登录标识 {identifier:?} 不得改变查询语义"
        );
    }

    let authorize = "/api/oauth/authorize?client_id=%27%20OR%20%271%27%3D%271&redirect_uri=https%3A%2F%2Fapp.example%2Fcb&response_type=code&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256";
    let (status, _headers, json) = json_response(
        send(&app, request(Method::GET, authorize, Some("10.66.2.10"), &[], Body::empty())).await,
    )
    .await;
    assert_eq!(status, StatusCode::BAD_REQUEST);
    assert_eq!(json["error"], "invalid_client", "注入串必须只是未知 client_id");

    // 注入尝试后正常流程仍可用（连接与语句未被污染）
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    assert_eq!(common::login_as(&app, "nova@nebula.st", PASSWORD).await.refresh.len(), 64);
}

/// 身份字段里的 CRLF 必须在进入邮件构造前被拒；否则收件人/用户名会成为
/// 追加邮件头的注入点。
#[tokio::test]
async fn sec_http_11_crlf_in_identity_fields_is_rejected_before_mail() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;

    let body = serde_json::json!({
        "email": "victim@nebula.st\r\nBcc: evil@example.com",
        "username": "nova",
        "password": PASSWORD,
    });
    let (status, _headers, json) = json_response(
        send(&app, json_request("/api/auth/register", Some("10.66.2.21"), &body)).await,
    )
    .await;
    assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY);
    assert_eq!(json["details"]["fields"][0]["field"], "email");
    assert_eq!(json["details"]["fields"][0]["code"], "invalid_email");

    let body = serde_json::json!({
        "email": "nova@nebula.st",
        "username": "n\r\nva",
        "password": PASSWORD,
    });
    let (status, _headers, json) = json_response(
        send(&app, json_request("/api/auth/register", Some("10.66.2.22"), &body)).await,
    )
    .await;
    assert_eq!(status, StatusCode::UNPROCESSABLE_ENTITY);
    assert_eq!(json["details"]["fields"][0]["code"], "username_invalid_characters");

    assert!(mailbox.messages().is_empty(), "被拒绝的输入不得触发任何邮件");
}

/// consent 的 302 只能落在已登记的 `redirect_uri`；`state` 必须百分号编码，
/// 不得把 CR/LF 带进 `Location`。
#[tokio::test]
async fn sec_http_12_oauth_consent_location_is_not_injectable_or_open() {
    let (app, state, mailbox, _dir) = app_fixture(None).await;
    seed_oauth_client(&state, "security-client", "https://app.example/cb", "correct-client-secret")
        .await;
    common::register_verified_user(&app, "nova@nebula.st", "nova", PASSWORD, &mailbox).await;
    let session = common::login_as(&app, "nova@nebula.st", PASSWORD).await;
    let cookie = session_cookie(&session);

    let body = serde_json::json!({
        "client_id": "security-client",
        "redirect_uri": "https://app.example/cb",
        "scope": "profile",
        "code_challenge": CHALLENGE,
        "state": "\r\nX-Injected: 1",
    });
    let response = send(
        &app,
        request(
            Method::POST,
            "/api/oauth/authorize/consent",
            Some("10.66.3.1"),
            &[
                ("content-type", "application/json"),
                ("cookie", &cookie),
                ("x-csrf-token", &session.csrf),
            ],
            Body::from(body.to_string()),
        ),
    )
    .await;
    assert_eq!(response.status(), StatusCode::FOUND);
    let location =
        header_value(response.headers(), "location").expect("302 必须带 Location").to_owned();
    assert!(
        location.starts_with("https://app.example/cb?code="),
        "回跳必须落在登记地址: {location}"
    );
    assert!(!location.contains('\r') && !location.contains('\n'), "Location 不得含 CR/LF");
    assert!(!response.headers().contains_key("x-injected"), "不得注入响应头");
    assert!(
        location.contains("&state=%0D%0AX-Injected%3A%201"),
        "state 必须百分号编码: {location}"
    );
    let code = location["https://app.example/cb?code=".len()..].split('&').next().unwrap();
    assert_eq!(code.len(), 64);
    assert!(code.bytes().all(|byte| byte.is_ascii_hexdigit()));

    // 未登记的 redirect_uri 不得产生任何跳转
    let body = serde_json::json!({
        "client_id": "security-client",
        "redirect_uri": "https://evil.example/cb",
        "scope": "profile",
        "code_challenge": CHALLENGE,
        "state": "",
    });
    let (status, headers, json) = json_response(
        send(
            &app,
            request(
                Method::POST,
                "/api/oauth/authorize/consent",
                Some("10.66.3.2"),
                &[
                    ("content-type", "application/json"),
                    ("cookie", &cookie),
                    ("x-csrf-token", &session.csrf),
                ],
                Body::from(body.to_string()),
            ),
        )
        .await,
    )
    .await;
    assert_eq!(status, StatusCode::BAD_REQUEST);
    assert_eq!(json["code"], "oauth_authorize_invalid");
    assert!(header_value(&headers, "location").is_none(), "参数非法时不得跳转");
}

/// 导出令牌与头像 id 都只可能是固定形状：路径穿越串必须落到 404，
/// 不得触达 `AvatarStore` 之外的任何文件。
#[tokio::test]
async fn sec_http_13_path_traversal_attempts_are_rejected() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    for uri in [
        "/api/user/export/..%2F..%2Fetc%2Fpasswd",
        "/api/users/..%2F..%2Fetc%2Fpasswd/avatar",
        "/api/users/%2E%2E%2F%2E%2E%2Fetc%2Fpasswd/avatar",
        "/api/user/export/0000000000000000000000000000000000000000000000000000000000000000%2F..%2F..",
    ] {
        let response =
            send(&app, request(Method::GET, uri, Some("10.66.3.10"), &[], Body::empty())).await;
        let status = response.status();
        let body = String::from_utf8_lossy(&body_bytes(response).await).into_owned();
        assert_eq!(status, StatusCode::NOT_FOUND, "{uri} 必须 404");
        assert!(!body.contains("root:"), "{uri} 不得回传任意文件内容");
    }
}

// ---------------------------------------------------------------------------
// 加密与令牌
// ---------------------------------------------------------------------------

/// 签名算法固定为 ES256：`alg=none`、用公钥 PEM 作 HMAC 密钥的 HS256、
/// 以及被篡改签名的真实令牌都必须 401。
#[tokio::test]
async fn sec_http_14_forged_access_tokens_are_rejected() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let now = chrono::Utc::now().timestamp();
    let payload = serde_json::json!({
        "sub": UserId::new().to_string(),
        "iat": now,
        "exp": now + 600,
        "aud": "nebula:user-session",
        "ver": 0,
    });
    let encode = |value: &serde_json::Value| {
        base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(value.to_string())
    };

    let none_token = format!(
        "{}.{}.",
        encode(&serde_json::json!({ "alg": "none", "typ": "JWT" })),
        encode(&payload)
    );
    let hs_token = jsonwebtoken::encode(
        &jsonwebtoken::Header::new(jsonwebtoken::Algorithm::HS256),
        &payload,
        &jsonwebtoken::EncodingKey::from_secret(common::TEST_PUBLIC_PEM.as_bytes())
            .expect("HMAC 密钥必须可构建"),
    )
    .expect("HS256 签发必须成功");

    // 真实令牌的签名被改一个字符
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let session = common::login_as(&app, "nova@nebula.st", PASSWORD).await;
    let mut tampered = session.access.into_bytes();
    let last = tampered.len() - 1;
    tampered[last] = if tampered[last] == b'A' { b'B' } else { b'A' };
    let tampered = String::from_utf8(tampered).expect("JWT 是 ASCII");

    for token in [none_token, hs_token, tampered] {
        let (status, _headers, json) = json_response(
            send(
                &app,
                request(
                    Method::GET,
                    "/api/user",
                    Some("10.66.4.1"),
                    &[("authorization", &format!("Bearer {token}"))],
                    Body::empty(),
                ),
            )
            .await,
        )
        .await;
        assert_eq!(status, StatusCode::UNAUTHORIZED, "伪造令牌必须 401: {token}");
        assert_eq!(json["code"], "unauthorized");
    }
}

/// refresh/csrf 必须是 256 位随机数的十六进制（64 字符）且逐次轮换；
/// access JWT 头必须是 ES256。
#[tokio::test]
async fn sec_http_15_session_tokens_are_high_entropy_and_rotated() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let first = common::login_as(&app, "nova@nebula.st", PASSWORD).await;
    let second = common::login_as(&app, "nova@nebula.st", PASSWORD).await;

    for session in [&first, &second] {
        assert_eq!(session.refresh.len(), 64);
        assert_eq!(session.csrf.len(), 64);
        assert!(session.refresh.bytes().all(|byte| byte.is_ascii_hexdigit()));
        assert!(session.csrf.bytes().all(|byte| byte.is_ascii_hexdigit()));
    }
    assert_ne!(first.refresh, second.refresh, "refresh 必须逐会话随机");
    assert_ne!(first.csrf, second.csrf, "csrf 必须逐会话随机");
    assert_ne!(first.access, second.access, "access 必须含签发时刻/版本");

    let header = first.access.split('.').next().expect("JWT 必须有三段");
    let decoded = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(header)
        .expect("JWT 头必须是 base64url");
    assert!(
        String::from_utf8_lossy(&decoded).contains(r#""alg":"ES256""#),
        "access token 必须固定 ES256"
    );
}

/// 运行时密钥容器的 `Debug` 必须脱敏；否则一次 `tracing::debug!(?state)`
/// 就会把静态密钥写进日志。
#[tokio::test]
async fn sec_http_16_debug_output_redacts_runtime_secrets() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let state_debug = format!("{state:?}");
    assert!(!state_debug.contains("mailer"));
    assert!(!state_debug.contains("070707"));

    let keys =
        JwtKeys::from_pem(common::TEST_PRIVATE_PEM.as_bytes(), common::TEST_PUBLIC_PEM.as_bytes())
            .expect("测试密钥必须可解析");
    let keys_debug = format!("{keys:?}");
    assert!(!keys_debug.contains("MIGHAgEAMBMGByqGSM49AgEGCCqGSM49AwEHBG0wawIBAQQg"));

    let cipher = AesGcmCipher::from_key([7u8; 32]);
    assert_eq!(format!("{cipher:?}"), "AesGcmCipher { .. }");

    let hasher = HmacCodeHasher::from_key([9u8; 32]);
    assert_eq!(format!("{hasher:?}"), "HmacCodeHasher { .. }");

    let turnstile = TurnstileClient::new("https://verify.example", "cf-secret-value")
        .expect("客户端必须可构建");
    assert!(!format!("{turnstile:?}").contains("cf-secret-value"));
}

// ---------------------------------------------------------------------------
// 缓存与错误泄漏
// ---------------------------------------------------------------------------

/// 认证响应含个人资料与后台数据，必须显式 `Cache-Control: no-store`，
/// 不得依赖「中间层不会缓存带 Cookie 的请求」这一默认假设。
#[tokio::test]
async fn sec_http_17_authenticated_get_responses_are_not_cacheable() {
    let (app, _state, mailbox, _dir) = app_fixture(Some("root@nebula.st")).await;
    common::register_verified_user(&app, "root@nebula.st", "root", PASSWORD, &mailbox).await;
    let session = common::login_as(&app, "root@nebula.st", PASSWORD).await;
    let cookie = common::cookie_header(Some(&session.access), None, Some(&session.csrf));

    for uri in ["/api/user", "/api/user/consents", "/api/user/oauth/grants", "/api/admin/users"] {
        let response = app.clone().oneshot_get_with_cookie(uri, "", &cookie).await;
        let status = response.status();
        let cache_control =
            header_value(response.headers(), "cache-control").unwrap_or("").to_owned();
        assert_eq!(status, StatusCode::OK, "{uri} 前置条件必须是 200");
        assert!(
            cache_control.contains("no-store"),
            "{uri} 的认证响应缺少 Cache-Control: no-store（实际 {cache_control:?}）"
        );
    }
}

/// TOTP setup 的响应体含明文种子与 otpauth URI；漏掉 `no-store` 时
/// 任何中间缓存命中都等于泄漏第二因素。
#[tokio::test]
async fn sec_http_18_totp_setup_response_is_not_cacheable() {
    let (app, _state, mailbox, _dir) = app_fixture(None).await;
    common::register_verified_user(&app, "nova@nebula.st", "nova", PASSWORD, &mailbox).await;
    let session = common::login_as(&app, "nova@nebula.st", PASSWORD).await;
    let cookie = session_cookie(&session);

    let response = send(
        &app,
        request(
            Method::POST,
            "/api/user/totp/setup",
            Some("10.66.5.1"),
            &[
                ("content-type", "application/json"),
                ("cookie", &cookie),
                ("x-csrf-token", &session.csrf),
            ],
            Body::from(serde_json::json!({ "password": PASSWORD }).to_string()),
        ),
    )
    .await;
    let status = response.status();
    let cache_control = header_value(response.headers(), "cache-control").unwrap_or("").to_owned();
    let (_, _, json) = json_response(response).await;
    assert_eq!(status, StatusCode::OK);
    assert!(json["secret"].as_str().is_some(), "前置条件：响应确实携带 TOTP 种子");
    assert!(
        cache_control.contains("no-store"),
        "携带 TOTP 种子的响应缺少 Cache-Control: no-store（实际 {cache_control:?}）"
    );
}

/// 500 响应只允许稳定信封，不得回传文件系统路径、SQL 或底层 OS 错误。
#[tokio::test]
async fn sec_http_19_internal_errors_do_not_leak_internals() {
    let (app, state, _mailbox, _dir) = app_fixture(None).await;
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;
    let user = state
        .user_repo
        .find_by_email("nova@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");
    // 把头像文件位置占成目录：读取报 IsADirectory/PermissionDenied，走 500 分支
    let avatar_path = state.avatar_store.dir().join(format!("{}-1.webp", user.id));
    std::fs::create_dir_all(&avatar_path).expect("测试目录必须可创建");

    let response = send(
        &app,
        request(
            Method::GET,
            &format!("/api/users/{}/avatar?v=1", user.id),
            Some("10.66.5.2"),
            &[],
            Body::empty(),
        ),
    )
    .await;
    assert_eq!(response.status(), StatusCode::INTERNAL_SERVER_ERROR);
    assert_eq!(header_value(response.headers(), "x-content-type-options"), Some("nosniff"));
    let body = String::from_utf8_lossy(&body_bytes(response).await).into_owned();
    assert_eq!(body, r#"{"code":"internal_error","message":"internal server error"}"#);
    for needle in ["avatars", "os error", "Is a directory", "Access is denied", "users.db"] {
        assert!(!body.contains(needle), "500 响应泄漏了内部细节 {needle:?}: {body}");
    }
}

/// 「用户不存在」与「验证码错误」必须同形同体，不得成为账号存在性 oracle。
#[tokio::test]
async fn sec_http_20_verification_does_not_reveal_account_existence() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;

    let unknown = serde_json::json!({ "email": "ghost@nebula.st", "code": "ABC123" });
    let known = serde_json::json!({ "email": "nova@nebula.st", "code": "ABC123" });

    let unknown_response =
        send(&app, json_request("/api/auth/register/verify", Some("10.66.6.1"), &unknown)).await;
    let unknown_status = unknown_response.status();
    let unknown_body = body_bytes(unknown_response).await;

    let known_response =
        send(&app, json_request("/api/auth/register/verify", Some("10.66.6.2"), &known)).await;
    let known_status = known_response.status();
    let known_body = body_bytes(known_response).await;

    assert_eq!(unknown_status, StatusCode::UNAUTHORIZED);
    assert_eq!(known_status, unknown_status, "存在性不得改变状态码");
    assert_eq!(known_body, unknown_body, "存在性不得改变响应体");
}

/// 重发冷却只对已注册账号出现；同一邮箱两次请求即可把账号存在性区分出来。
/// 注册端点的 409 已公开邮箱占用，故此处定性为低危信息面（见报告）。
#[tokio::test]
async fn sec_http_21_reset_cooldown_does_not_reveal_account_existence() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;

    let known = serde_json::json!({ "email": "nova@nebula.st" });
    let unknown = serde_json::json!({ "email": "ghost@nebula.st" });

    // 每个对端 IP 只发一次，避免端点自身的限额干扰；冷却按账号计
    let first =
        send(&app, json_request("/api/auth/password/reset/request", Some("10.66.6.11"), &known))
            .await;
    assert_eq!(first.status(), StatusCode::NO_CONTENT);
    let known_second =
        send(&app, json_request("/api/auth/password/reset/request", Some("10.66.6.12"), &known))
            .await
            .status();

    let first =
        send(&app, json_request("/api/auth/password/reset/request", Some("10.66.6.13"), &unknown))
            .await;
    assert_eq!(first.status(), StatusCode::NO_CONTENT);
    let unknown_second =
        send(&app, json_request("/api/auth/password/reset/request", Some("10.66.6.14"), &unknown))
            .await
            .status();

    assert_eq!(
        known_second, unknown_second,
        "冷却响应不得区分已注册与未注册邮箱：已注册 {known_second} / 未注册 {unknown_second}"
    );
}

// ---------------------------------------------------------------------------
// CORS 与安全头
// ---------------------------------------------------------------------------

/// 只允许配置的精确来源：不得反射请求 Origin，也不得下发通配符；
/// 反射、`null`、后缀拼接、大小写变体都必须拿不到「与自己相等」的 ACAO。
#[tokio::test]
async fn sec_http_22_cors_rejects_foreign_null_and_suffix_origins() {
    let (mut state, _clock, _mailbox, _dir) = common::app_state().await;
    state.cors_allowed_origin = Some(HeaderValue::from_static("http://localhost:3000"));
    let app = api::router(state);

    for origin in [
        "http://evil.example",
        "null",
        "http://localhost:3000.evil.example",
        "HTTP://LOCALHOST:3000",
        "http://localhost:3000/",
    ] {
        let response = send(
            &app,
            request(
                Method::GET,
                "/healthz",
                Some("10.66.7.1"),
                &[("origin", origin)],
                Body::empty(),
            ),
        )
        .await;
        let allowed = header_value(response.headers(), "access-control-allow-origin");
        assert_ne!(allowed, Some(origin), "不得反射外来 Origin: {origin}");
        assert_ne!(allowed, Some("*"), "不得在带凭据时下发通配符");
    }

    let response = send(
        &app,
        request(
            Method::GET,
            "/healthz",
            Some("10.66.7.1"),
            &[("origin", "http://localhost:3000")],
            Body::empty(),
        ),
    )
    .await;
    assert_eq!(
        header_value(response.headers(), "access-control-allow-origin"),
        Some("http://localhost:3000")
    );
    assert_eq!(header_value(response.headers(), "access-control-allow-credentials"), Some("true"));
}

/// 安全头必须覆盖错误路径（404/429/500），否则错误页可被 MIME 嗅探或框入。
#[tokio::test]
async fn sec_http_23_security_headers_cover_error_responses() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;

    let not_found =
        send(&app, request(Method::GET, "/api/nope", Some("10.66.7.2"), &[], Body::empty())).await;
    assert_eq!(not_found.status(), StatusCode::NOT_FOUND);

    let empty = serde_json::json!({ "identifier": "", "password": "" });
    for _ in 0..5 {
        let _ = send(&app, json_request("/api/auth/login", Some("10.66.7.3"), &empty)).await;
    }
    let limited = send(&app, json_request("/api/auth/login", Some("10.66.7.3"), &empty)).await;
    assert_eq!(limited.status(), StatusCode::TOO_MANY_REQUESTS);

    let body = serde_json::json!({ "identifier": "ghost@nebula.st", "password": "x" });
    let internal = send(
        &app,
        request(
            Method::POST,
            "/api/auth/login",
            None,
            &[("content-type", "application/json")],
            Body::from(body.to_string()),
        ),
    )
    .await;
    assert_eq!(internal.status(), StatusCode::INTERNAL_SERVER_ERROR);

    for (label, response) in [("404", not_found), ("429", limited), ("500", internal)] {
        assert_eq!(
            header_value(response.headers(), "x-content-type-options"),
            Some("nosniff"),
            "{label} 缺 nosniff"
        );
        assert_eq!(header_value(response.headers(), "x-frame-options"), Some("DENY"), "{label}");
        assert!(response.headers().get("content-security-policy").is_some(), "{label} 缺 CSP");
        assert!(response.headers().get("referrer-policy").is_some(), "{label} 缺 referrer-policy");
    }
}

/// 认证端点必须声明 HSTS，否则首次明文访问可被降级。
#[tokio::test]
async fn sec_http_24_hsts_header_is_present() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let response =
        send(&app, request(Method::GET, "/healthz", Some("10.66.7.4"), &[], Body::empty())).await;

    let hsts = header_value(response.headers(), "strict-transport-security");
    let hsts = hsts.expect("响应必须带 Strict-Transport-Security（或由文档化的反代统一添加）");
    assert!(hsts.contains("max-age="), "HSTS 必须含 max-age: {hsts}");
}

/// `Config` 系的子结构直接派生 `Debug`，密钥以明文出现在格式化输出里；
/// 当前没有调用点打印它，但任何一次 `?config` 都会泄漏。
#[tokio::test]
async fn sec_http_25_config_debug_does_not_print_keys() {
    let config = CryptoConfig { totp_secret_key: "ab".repeat(32), code_pepper: "cd".repeat(32) };
    let debug = format!("{config:?}");
    assert!(!debug.contains(&"ab".repeat(32)), "CryptoConfig 的 Debug 泄漏了 TOTP 密钥: {debug}");
    assert!(
        !debug.contains(&"cd".repeat(32)),
        "CryptoConfig 的 Debug 泄漏了验证码 pepper: {debug}"
    );
}

/// 登录会写会话 cookie，属状态变更：跨站来源必须被拒。端点既不校验
/// `Origin`，也不校验 `Content-Type`——跨站页面可用 `enctype="text/plain"`
/// 的表单伪造 JSON 体（把等号藏进字符串值），从而把受害者登入攻击者账户。
#[tokio::test]
async fn sec_http_26_cross_site_login_is_rejected() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    common::register_user(&app, "nova@nebula.st", "nova", PASSWORD).await;

    let body = serde_json::json!({
        // 末尾追加的 `=` 让 text/plain 表单体仍是合法 JSON
        "identifier": "nova@nebula.st",
        "password": PASSWORD,
        "junk": "=",
    });
    let response = send(
        &app,
        request(
            Method::POST,
            "/api/auth/login",
            Some("10.66.8.1"),
            &[("content-type", "text/plain"), ("origin", "https://evil.example")],
            Body::from(body.to_string()),
        ),
    )
    .await;

    assert_eq!(
        response.status(),
        StatusCode::UNSUPPORTED_MEDIA_TYPE,
        "text/plain 跨站表单必须被媒体类型门禁拒绝（登录 CSRF）"
    );
    assert!(common::set_cookie_of(&response, "nebula_access").is_none(), "不得下发会话 cookie");
}
