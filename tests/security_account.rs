//! 账号生命周期对抗性安全测试：邮箱归一化/域名白名单、验证码用途与地址绑定、
//! 密码重置、邮箱/用户名变更、数据导出、头像，以及注销相关能力。
//!
//! 每个用例断言「安全行为应当成立」；失败即暴露可复现缺陷。用例名前缀
//! `sec_account_` 便于单独筛选。请求一律经 `tower::ServiceExt::oneshot` 打
//! `Router`，不绑端口；限额按 IP 计，需要多次同类请求的用例显式换源 IP，
//! 避免测试撞上限额层而非被测逻辑（§8）。
//!
//! 攻击面与已排除怀疑的对应关系见测试注释；只做少量请求证明「限制缺失」，
//! 不实施真实爆破。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::collections::HashSet;
use std::net::SocketAddr;
use std::sync::Arc;

use axum::Router;
use axum::body::Body;
use axum::extract::ConnectInfo;
use axum::http::{HeaderMap, Method, Request, StatusCode, header};
use axum::response::Response;
use chrono::TimeDelta;
use common::{Session, body_text, cookie_header, extract_code};
use nebula_server::api;
use nebula_server::context::Clock;
use serde_json::{Value, json};
use tower::ServiceExt;

const PASSWORD: &str = "Nebula!Passw0rd-2026";
const NEW_PASSWORD: &str = "Reborn!Passw0rd-2026";
const MULTIPART_BOUNDARY: &str = "nebula-security-boundary";

// ---------------------------------------------------------------------------
// 请求助手：显式指定源 IP，便于在限额分层下仍测到被测逻辑
// ---------------------------------------------------------------------------

#[allow(clippy::too_many_arguments)]
async fn send(
    app: &Router,
    method: Method,
    ip: &str,
    uri: &str,
    content_type: Option<&str>,
    body: Vec<u8>,
    cookie: Option<&str>,
    csrf: Option<&str>,
) -> Response {
    let mut builder = Request::builder().method(method).uri(uri);
    if let Some(value) = content_type {
        builder = builder.header(header::CONTENT_TYPE, value);
    }
    if let Some(value) = cookie {
        builder = builder.header(header::COOKIE, value);
    }
    if let Some(value) = csrf {
        builder = builder.header("x-csrf-token", value);
    }
    let mut request = builder.body(Body::from(body)).expect("请求必须可构造");
    let addr: SocketAddr = format!("{ip}:40000").parse().expect("测试地址必须合法");
    request.extensions_mut().insert(ConnectInfo(addr));
    app.clone().oneshot(request).await.expect("oneshot 必须成功")
}

async fn post_json(app: &Router, ip: &str, uri: &str, body: &str) -> Response {
    send(app, Method::POST, ip, uri, Some("application/json"), body.as_bytes().to_vec(), None, None)
        .await
}

async fn post_json_auth(
    app: &Router,
    ip: &str,
    uri: &str,
    body: &str,
    session: &Session,
) -> Response {
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    send(
        app,
        Method::POST,
        ip,
        uri,
        Some("application/json"),
        body.as_bytes().to_vec(),
        Some(&cookie),
        Some(&session.csrf),
    )
    .await
}

async fn patch_json_auth(
    app: &Router,
    ip: &str,
    uri: &str,
    body: &str,
    session: &Session,
) -> Response {
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    send(
        app,
        Method::PATCH,
        ip,
        uri,
        Some("application/json"),
        body.as_bytes().to_vec(),
        Some(&cookie),
        Some(&session.csrf),
    )
    .await
}

async fn get(app: &Router, ip: &str, uri: &str) -> Response {
    send(app, Method::GET, ip, uri, None, Vec::new(), None, None).await
}

async fn get_with_cookie(app: &Router, ip: &str, uri: &str, cookie: &str) -> Response {
    send(app, Method::GET, ip, uri, None, Vec::new(), Some(cookie), None).await
}

async fn body_bytes(response: Response) -> Vec<u8> {
    http_body_util::BodyExt::collect(response.into_body())
        .await
        .expect("读体必须成功")
        .to_bytes()
        .to_vec()
}

fn json_body(text: &str) -> Value {
    serde_json::from_str(text).unwrap_or_else(|err| panic!("响应不是 JSON: {err}: {text}"))
}

/// 以指定源 IP 注册；返回原始响应供断言状态码。
async fn register_at(
    app: &Router,
    ip: &str,
    email: &str,
    username: &str,
    password: &str,
) -> Response {
    let body = json!({ "email": email, "username": username, "password": password }).to_string();
    post_json(app, ip, "/api/auth/register", &body).await
}

async fn register_ok(app: &Router, ip: &str, email: &str, username: &str) {
    let response = register_at(app, ip, email, username, PASSWORD).await;
    assert_eq!(response.status(), StatusCode::CREATED, "注册助手必须成功: {email}");
}

/// 以指定源 IP 登录并取回三 cookie 会话。
async fn login_at(app: &Router, ip: &str, identifier: &str, password: &str) -> Session {
    let body = json!({ "identifier": identifier, "password": password }).to_string();
    let response = post_json(app, ip, "/api/auth/login", &body).await;
    assert_eq!(response.status(), StatusCode::OK, "登录助手必须成功");
    Session {
        access: common::set_cookie_of(&response, "nebula_access").expect("必须下发 access"),
        refresh: common::set_cookie_of(&response, "nebula_refresh").expect("必须下发 refresh"),
        csrf: common::set_cookie_of(&response, "nebula_csrf").expect("必须下发 csrf"),
    }
}

async fn request_reset(app: &Router, ip: &str, email: &str) -> Response {
    post_json(app, ip, "/api/auth/password/reset/request", &json!({ "email": email }).to_string())
        .await
}

async fn confirm_reset(app: &Router, ip: &str, email: &str, code: &str) -> Response {
    let body = json!({ "email": email, "code": code, "new_password": NEW_PASSWORD }).to_string();
    post_json(app, ip, "/api/auth/password/reset/confirm", &body).await
}

async fn request_email_change(
    app: &Router,
    ip: &str,
    session: &Session,
    new_email: &str,
    password: &str,
) -> Response {
    let body = json!({ "new_email": new_email, "current_password": password }).to_string();
    post_json_auth(app, ip, "/api/user/email/change/request", &body, session).await
}

async fn confirm_email_change(app: &Router, ip: &str, session: &Session, code: &str) -> Response {
    post_json_auth(
        app,
        ip,
        "/api/user/email/change/confirm",
        &json!({ "code": code }).to_string(),
        session,
    )
    .await
}

async fn profile_json(app: &Router, ip: &str, session: &Session) -> Value {
    let cookie = cookie_header(Some(&session.access), None, None);
    let response = get_with_cookie(app, ip, "/api/user", &cookie).await;
    assert_eq!(response.status(), StatusCode::OK, "资料读取必须成功");
    json_body(&body_text(response).await)
}

// ---------------------------------------------------------------------------
// 头像助手
// ---------------------------------------------------------------------------

fn png_of(width: u32, height: u32, color: [u8; 4]) -> Vec<u8> {
    use image::{DynamicImage, ImageFormat, Rgba, RgbaImage};

    let image = RgbaImage::from_pixel(width, height, Rgba(color));
    let mut bytes = std::io::Cursor::new(Vec::new());
    DynamicImage::ImageRgba8(image)
        .write_to(&mut bytes, ImageFormat::Png)
        .expect("测试图片必须可编码");
    bytes.into_inner()
}

fn multipart_field(name: &str, filename: &str, content_type: &str, data: &[u8]) -> Vec<u8> {
    let mut part = Vec::new();
    part.extend_from_slice(format!("--{MULTIPART_BOUNDARY}\r\n").as_bytes());
    part.extend_from_slice(
        format!("Content-Disposition: form-data; name=\"{name}\"; filename=\"{filename}\"\r\n")
            .as_bytes(),
    );
    part.extend_from_slice(format!("Content-Type: {content_type}\r\n\r\n").as_bytes());
    part.extend_from_slice(data);
    part.extend_from_slice(b"\r\n");
    part
}

fn multipart_text_field(name: &str, value: &str) -> Vec<u8> {
    let mut part = Vec::new();
    part.extend_from_slice(format!("--{MULTIPART_BOUNDARY}\r\n").as_bytes());
    part.extend_from_slice(
        format!("Content-Disposition: form-data; name=\"{name}\"\r\n\r\n").as_bytes(),
    );
    part.extend_from_slice(value.as_bytes());
    part.extend_from_slice(b"\r\n");
    part
}

fn multipart_body(parts: &[Vec<u8>]) -> Vec<u8> {
    let mut body = Vec::new();
    for part in parts {
        body.extend_from_slice(part);
    }
    body.extend_from_slice(format!("--{MULTIPART_BOUNDARY}--\r\n").as_bytes());
    body
}

async fn upload(app: &Router, ip: &str, session: &Session, parts: &[Vec<u8>]) -> Response {
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    send(
        app,
        Method::POST,
        ip,
        "/api/user/avatar",
        Some(&format!("multipart/form-data; boundary={MULTIPART_BOUNDARY}")),
        multipart_body(parts),
        Some(&cookie),
        Some(&session.csrf),
    )
    .await
}

async fn avatar_bytes(app: &Router, ip: &str, uri: &str) -> (StatusCode, HeaderMap, Vec<u8>) {
    let response = get(app, ip, uri).await;
    let status = response.status();
    let headers = response.headers().clone();
    (status, headers, body_bytes(response).await)
}

// ---------------------------------------------------------------------------
// 缺陷 1：验证码只绑定 user_id，邮箱变更后旧地址收到的码仍然有效
// ---------------------------------------------------------------------------

/// 攻击场景：攻击者掌握账户旧邮箱 A（离职/邮箱被接管），在受害者把账户邮箱
/// 改成 B 之前申请一次密码重置码并留存；受害者完成邮箱变更后，攻击者用 B 作为
/// `email` 提交旧码，仍能重置口令——「改邮箱即切断旧邮箱能力」不成立。
#[tokio::test]
async fn sec_account_01_reset_code_survives_email_change() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_ok(&app, "10.31.0.1", "old@nebula.st", "oldie").await;
    let session = login_at(&app, "10.31.0.2", "old@nebula.st", PASSWORD).await;

    let requested = request_reset(&app, "10.31.0.3", "old@nebula.st").await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT, "重置发码必须成功");
    let reset_code = extract_code(&common::await_mail_subject(&mailbox, "密码重置验证码").await);

    let changed =
        request_email_change(&app, "10.31.0.4", &session, "new@nebula.st", PASSWORD).await;
    assert_eq!(changed.status(), StatusCode::NO_CONTENT, "邮箱变更申请必须成功");
    let change_code = extract_code(&common::await_mail_for(&mailbox, "new@nebula.st").await);
    let confirmed = confirm_email_change(&app, "10.31.0.5", &session, &change_code).await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT, "邮箱变更必须完成");

    // 旧地址已不属于该账户；它收到的重置码应随地址生命周期结束而失效。
    let takeover = confirm_reset(&app, "10.31.0.6", "new@nebula.st", &reset_code).await;
    assert_eq!(
        takeover.status(),
        StatusCode::UNAUTHORIZED,
        "旧邮箱收到的重置码在邮箱变更后仍可重置口令（账户已被接管）"
    );
}

/// 同类缺陷的破坏性变体：注销确认码发往旧邮箱，邮箱变更后仍能删除账户。
#[tokio::test]
async fn sec_account_02_delete_code_survives_email_change() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    register_ok(&app, "10.32.0.1", "gone@nebula.st", "goner").await;
    let verify = extract_code(&common::await_mail_for(&mailbox, "gone@nebula.st").await);
    let verified = post_json(
        &app,
        "10.32.0.2",
        "/api/auth/register/verify",
        &json!({ "email": "gone@nebula.st", "code": verify }).to_string(),
    )
    .await;
    assert_eq!(verified.status(), StatusCode::NO_CONTENT, "邮箱验证必须成功");
    let session = login_at(&app, "10.32.0.3", "gone@nebula.st", PASSWORD).await;

    let delete_requested =
        post_json_auth(&app, "10.32.0.4", "/api/auth/account/delete/request", "", &session).await;
    assert_eq!(delete_requested.status(), StatusCode::NO_CONTENT, "注销发码必须成功");
    let delete_code = extract_code(&common::await_mail_subject(&mailbox, "账户注销确认码").await);

    let changed =
        request_email_change(&app, "10.32.0.5", &session, "moved@nebula.st", PASSWORD).await;
    assert_eq!(changed.status(), StatusCode::NO_CONTENT, "邮箱变更申请必须成功");
    let change_code = extract_code(&common::await_mail_for(&mailbox, "moved@nebula.st").await);
    let confirmed = confirm_email_change(&app, "10.32.0.6", &session, &change_code).await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT, "邮箱变更必须完成");

    let deleted = post_json_auth(
        &app,
        "10.32.0.7",
        "/api/auth/account/delete/confirm",
        &json!({ "code": delete_code }).to_string(),
        &session,
    )
    .await;
    assert_eq!(
        deleted.status(),
        StatusCode::UNAUTHORIZED,
        "旧邮箱收到的注销码在邮箱变更后仍能删除账户"
    );
    let still_there = state.user_repo.find_by_email("moved@nebula.st").await.expect("查询必须成功");
    assert!(still_there.is_some(), "账户不得被旧地址的注销码删除");
}

// ---------------------------------------------------------------------------
// 缺陷 2：重置/重发的冷却 429 与 204 形成账户存在性/验证状态判别
// ---------------------------------------------------------------------------

/// 攻击场景：对同一邮箱连发两次重置请求。不存在的邮箱两次都 204；存在的邮箱
/// 第二次命中 60 秒冷却返回 429。无需注册即可枚举账户存在性，与文件头
/// 「未知邮箱同样 204（防枚举）」的声明相矛盾。
#[tokio::test]
async fn sec_account_03_reset_request_reveals_account_existence() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.33.0.1", "known@nebula.st", "known1").await;

    let known_first = request_reset(&app, "10.33.1.1", "known@nebula.st").await;
    let known_second = request_reset(&app, "10.33.1.2", "known@nebula.st").await;
    let ghost_first = request_reset(&app, "10.33.1.3", "ghost@nebula.st").await;
    let ghost_second = request_reset(&app, "10.33.1.4", "ghost@nebula.st").await;

    assert_eq!(known_first.status(), StatusCode::NO_CONTENT, "首次请求应 204");
    assert_eq!(ghost_first.status(), StatusCode::NO_CONTENT, "未知邮箱应 204");
    // 对外响应必须与账户是否存在无关。
    assert_eq!(
        known_second.status(),
        ghost_second.status(),
        "重置冷却的 429 泄露账户存在性: known={:?} ghost={:?}",
        known_second.status(),
        ghost_second.status()
    );
}

/// 同类判别：重发端点对「已注册但未验证」的邮箱第二次返回 429，对未知邮箱
/// 始终 204，从而泄露「该地址存在且尚未验证」这一状态。
#[tokio::test]
async fn sec_account_04_resend_reveals_unverified_state() {
    let (app, _state, clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.34.0.1", "unverified@nebula.st", "unver1").await;
    // 注册自身签发过一封验证码；越过 60 秒冷却后观察「首次重发」与「冷却重发」。
    clock.advance(TimeDelta::seconds(61));

    let unverified_first = post_json(
        &app,
        "10.34.1.1",
        "/api/auth/register/resend",
        &json!({ "email": "unverified@nebula.st" }).to_string(),
    )
    .await;
    let unverified_second = post_json(
        &app,
        "10.34.1.2",
        "/api/auth/register/resend",
        &json!({ "email": "unverified@nebula.st" }).to_string(),
    )
    .await;
    let ghost_first = post_json(
        &app,
        "10.34.1.3",
        "/api/auth/register/resend",
        &json!({ "email": "ghost2@nebula.st" }).to_string(),
    )
    .await;
    let ghost_second = post_json(
        &app,
        "10.34.1.4",
        "/api/auth/register/resend",
        &json!({ "email": "ghost2@nebula.st" }).to_string(),
    )
    .await;

    assert_eq!(unverified_first.status(), StatusCode::NO_CONTENT, "首次重发应 204");
    assert_eq!(ghost_first.status(), StatusCode::NO_CONTENT, "未知邮箱应 204");
    assert_eq!(
        unverified_second.status(),
        ghost_second.status(),
        "重发冷却的 429 泄露未验证账户状态: unverified={:?} ghost={:?}",
        unverified_second.status(),
        ghost_second.status()
    );
}

// ---------------------------------------------------------------------------
// 缺陷 3：注册接受多字节口令，登录按字节上限拒绝，账户注册后不可登录
// ---------------------------------------------------------------------------

/// 攻击/误用场景：注册的长度规则按字符计（≤64），登录/再认证的输入上限按
/// 字节计（≤128）。含多字节字符的合法口令注册成功，却永远无法登录或改资料。
#[tokio::test]
async fn sec_account_05_multibyte_password_register_login_mismatch() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    // 64 个字符（满足 16–64 与四类字符要求），UTF-8 184 字节（超过登录 128 字节上限）。
    let password = format!("Aa1!{}", "密".repeat(60));
    assert_eq!(password.chars().count(), 64);
    assert!(password.len() > 128);

    let registered =
        register_at(&app, "10.35.0.1", "multibyte@nebula.st", "multiby", &password).await;
    assert_eq!(registered.status(), StatusCode::CREATED, "注册必须接受该口令");

    // 注册接受的凭据必须能用于登录；否则账户从创建起即不可用。
    let login = post_json(
        &app,
        "10.35.0.2",
        "/api/auth/login",
        &json!({ "identifier": "multibyte@nebula.st", "password": password }).to_string(),
    )
    .await;
    let login_status = login.status();
    let login_body = body_text(login).await;
    assert_eq!(
        login_status,
        StatusCode::OK,
        "注册接受的口令无法登录（字符/字节长度规则不一致）: {login_body}"
    );
}

// ---------------------------------------------------------------------------
// 缺陷 4：口令重置未撤销已签发的一次性导出令牌
// ---------------------------------------------------------------------------

/// 攻击场景：会话被盗后攻击者已申请导出链接；受害者重置口令「踢出所有会话」，
/// 但导出令牌不在撤销路径内，10 分钟内仍可下载完整数据快照。
#[tokio::test]
async fn sec_account_06_export_token_survives_password_reset() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_ok(&app, "10.36.0.1", "exfil@nebula.st", "exfil1").await;
    let session = login_at(&app, "10.36.0.2", "exfil@nebula.st", PASSWORD).await;

    let issued = post_json_auth(&app, "10.36.0.3", "/api/user/export", "", &session).await;
    assert_eq!(issued.status(), StatusCode::OK, "导出申请必须成功");
    let download_url = json_body(&body_text(issued).await)["download_url"]
        .as_str()
        .expect("必须返回地址")
        .to_owned();

    let requested = request_reset(&app, "10.36.0.4", "exfil@nebula.st").await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT, "重置发码必须成功");
    let reset_code = extract_code(&common::await_mail_subject(&mailbox, "密码重置验证码").await);
    let reset = confirm_reset(&app, "10.36.0.5", "exfil@nebula.st", &reset_code).await;
    assert_eq!(reset.status(), StatusCode::NO_CONTENT, "重置必须成功");

    // 重置是撤销路径：所有既有凭据（会话与一次性导出令牌）都应失效。
    let download = get(&app, "10.36.0.6", &download_url).await;
    assert_eq!(
        download.status(),
        StatusCode::NOT_FOUND,
        "口令重置后未消费的导出令牌仍可下载完整数据"
    );
}

// ---------------------------------------------------------------------------
// 缺陷 5：未验证注册永不释放邮箱/用户名，可被用来长期占用他人地址
// ---------------------------------------------------------------------------

/// 攻击场景：攻击者用受害者邮箱注册但从不验证；没有任何保留期/清理任务，
/// 地址与用户名被永久占用，受害者注册持续 409（只能走重置夺回被预置的账户）。
#[tokio::test]
async fn sec_account_07_unverified_account_never_expires() {
    let (state, clock, _mailbox, _dir) = common::app_state().await;
    let app = api::router(state.clone());
    register_ok(&app, "10.37.0.1", "squat@nebula.st", "squat1").await;

    clock.advance(TimeDelta::days(365));
    state.maintenance_repo.purge_expired(clock.now()).await.expect("清理任务必须成功");

    let retry = register_at(&app, "10.37.0.2", "squat@nebula.st", "squat2", PASSWORD).await;
    assert_eq!(
        retry.status(),
        StatusCode::CREATED,
        "一年后未验证注册仍占用邮箱（无保留期），可被用来阻塞他人注册"
    );
}

// ---------------------------------------------------------------------------
// 阴性对照：以下用例断言应有的安全行为，预期通过
// ---------------------------------------------------------------------------

/// 用途隔离：注册验证码不能用于密码重置，反之亦然；邮箱变更码只在变更表内。
#[tokio::test]
async fn sec_account_08_code_purpose_isolation() {
    let (app, _state, _clock, mailbox, _dir) = common::app().await;
    register_ok(&app, "10.38.0.1", "purpose@nebula.st", "purpose").await;
    let verify_code = extract_code(&common::await_mail_for(&mailbox, "purpose@nebula.st").await);
    let session = login_at(&app, "10.38.0.2", "purpose@nebula.st", PASSWORD).await;

    assert_eq!(
        request_reset(&app, "10.38.0.3", "purpose@nebula.st").await.status(),
        StatusCode::NO_CONTENT
    );
    let reset_code = extract_code(&common::await_mail_subject(&mailbox, "密码重置验证码").await);
    assert_eq!(
        request_email_change(&app, "10.38.0.4", &session, "purpose-new@nebula.st", PASSWORD)
            .await
            .status(),
        StatusCode::NO_CONTENT
    );
    let change_code =
        extract_code(&common::await_mail_for(&mailbox, "purpose-new@nebula.st").await);

    // 注册验证码用在重置端点。
    let verify_at_reset = confirm_reset(&app, "10.38.1.1", "purpose@nebula.st", &verify_code).await;
    assert_eq!(verify_at_reset.status(), StatusCode::UNAUTHORIZED, "注册码不得用于重置");
    // 重置码用在验证端点。
    let reset_at_verify = post_json(
        &app,
        "10.38.1.2",
        "/api/auth/register/verify",
        &json!({ "email": "purpose@nebula.st", "code": reset_code }).to_string(),
    )
    .await;
    assert_eq!(reset_at_verify.status(), StatusCode::UNAUTHORIZED, "重置码不得用于验证邮箱");
    // 邮箱变更码用在验证/重置端点。
    let change_at_verify = post_json(
        &app,
        "10.38.1.3",
        "/api/auth/register/verify",
        &json!({ "email": "purpose@nebula.st", "code": change_code }).to_string(),
    )
    .await;
    assert_eq!(change_at_verify.status(), StatusCode::UNAUTHORIZED, "变更码不得用于验证邮箱");
    let change_at_reset = confirm_reset(&app, "10.38.1.4", "purpose@nebula.st", &change_code).await;
    assert_eq!(change_at_reset.status(), StatusCode::UNAUTHORIZED, "变更码不得用于重置");

    // 各自的正确码仍可用。
    let verified = post_json(
        &app,
        "10.38.1.5",
        "/api/auth/register/verify",
        &json!({ "email": "purpose@nebula.st", "code": verify_code }).to_string(),
    )
    .await;
    assert_eq!(verified.status(), StatusCode::NO_CONTENT, "正确注册码必须可用");
    let reset = confirm_reset(&app, "10.38.1.6", "purpose@nebula.st", &reset_code).await;
    assert_eq!(reset.status(), StatusCode::NO_CONTENT, "正确重置码必须可用");
}

/// 并发消费：同一验证码的两个并发请求只能有一个成功（原子消费）。
#[tokio::test]
async fn sec_account_09_verify_code_concurrent_consumption() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    register_ok(&app, "10.39.0.1", "race@nebula.st", "race1").await;
    let code = extract_code(&common::await_mail_for(&mailbox, "race@nebula.st").await);
    let body = json!({ "email": "race@nebula.st", "code": code }).to_string();

    let (first, second) = tokio::join!(
        post_json(&app, "10.39.1.1", "/api/auth/register/verify", &body),
        post_json(&app, "10.39.1.2", "/api/auth/register/verify", &body),
    );
    let mut statuses = [first.status(), second.status()];
    statuses.sort_by_key(StatusCode::as_u16);
    assert_eq!(
        statuses,
        [StatusCode::NO_CONTENT, StatusCode::UNAUTHORIZED],
        "并发消费必须恰好成功一次"
    );
    let user = state.user_repo.find_by_email("race@nebula.st").await.expect("查询必须成功");
    assert!(user.expect("用户必须存在").email_verified);
}

/// 邮箱变更码绑定新地址：换目标地址重发后旧码作废；大小写变体的已占用邮箱仍冲突。
#[tokio::test]
async fn sec_account_10_email_change_binds_code_to_address() {
    let (app, _state, clock, mailbox, _dir) = common::app().await;
    register_ok(&app, "10.40.0.1", "owner@nebula.st", "owner1").await;
    register_ok(&app, "10.40.0.2", "taken@nebula.st", "taken1").await;
    let session = login_at(&app, "10.40.0.3", "owner@nebula.st", PASSWORD).await;

    let case_variant =
        request_email_change(&app, "10.40.0.4", &session, "TAKEN@NEBULA.ST", PASSWORD).await;
    assert_eq!(case_variant.status(), StatusCode::CONFLICT, "大小写变体不得绕过邮箱唯一性");

    let first =
        request_email_change(&app, "10.40.0.5", &session, "first@nebula.st", PASSWORD).await;
    assert_eq!(first.status(), StatusCode::NO_CONTENT);
    let old_code = extract_code(&common::await_mail_for(&mailbox, "first@nebula.st").await);

    clock.advance(TimeDelta::seconds(61));
    let second =
        request_email_change(&app, "10.40.0.6", &session, "second@nebula.st", PASSWORD).await;
    assert_eq!(second.status(), StatusCode::NO_CONTENT);
    let new_code = extract_code(&common::await_mail_for(&mailbox, "second@nebula.st").await);

    let stale = confirm_email_change(&app, "10.40.0.7", &session, &old_code).await;
    assert_eq!(stale.status(), StatusCode::UNAUTHORIZED, "重发后旧码必须作废");
    let applied = confirm_email_change(&app, "10.40.0.8", &session, &new_code).await;
    assert_eq!(applied.status(), StatusCode::NO_CONTENT);
    let profile = profile_json(&app, "10.40.0.9", &session).await;
    assert_eq!(profile["email"], "second@nebula.st");
}

/// 邮箱归一化与域名白名单：大小写/空白不产生重复账户；后缀、子域、尾点、
/// Unicode 点与多 @ 都不能绕过精确域名匹配。
#[tokio::test]
async fn sec_account_11_email_case_and_domain_lookalikes_cannot_duplicate() {
    let (mut state, _clock, _mailbox, dir) = common::app_state().await;
    state.allowed_email_domains = Arc::new(HashSet::from(["nebula.st".to_owned()]));
    let app = api::router(state.clone());

    let base = register_at(&app, "10.41.0.1", "nova@nebula.st", "base1", PASSWORD).await;
    assert_eq!(base.status(), StatusCode::CREATED);

    for (index, email) in
        ["NOVA@NEBULA.ST", " nova@nebula.st ", "nova@NEBULA.ST\n"].iter().enumerate()
    {
        let response =
            register_at(&app, &format!("10.41.1.{index}"), email, &format!("dup{index}"), PASSWORD)
                .await;
        assert_eq!(
            response.status(),
            StatusCode::CONFLICT,
            "归一化后重复的邮箱必须 409: {email:?}"
        );
    }

    let lookalikes = [
        "x1@nebula.st.evil.com",
        "x2@evil.nebula.st",
        "x3@mail.nebula.st",
        "x4@nebula.st.",
        "x6@nebula.st@evil.com",
    ];
    for (index, email) in lookalikes.iter().enumerate() {
        let response = register_at(
            &app,
            &format!("10.41.2.{index}"),
            email,
            &format!("look{index}"),
            PASSWORD,
        )
        .await;
        assert_eq!(
            response.status(),
            StatusCode::UNPROCESSABLE_ENTITY,
            "白名单近似域名必须被拒绝: {email:?}"
        );
    }

    // U+3002 是 IDNA 的标签分隔符，规范化为 '.' 后就是白名单内域名；
    // 必须落库为 A-label 形式，而不是另立一个 Unicode 域名账户。
    let unicode_dot =
        register_at(&app, "10.41.3.1", "x5@nebula\u{3002}st", "look5", PASSWORD).await;
    assert_eq!(unicode_dot.status(), StatusCode::CREATED);

    let pool = common::schema_pool(dir.path()).await;
    let total: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM users")
        .fetch_one(&pool)
        .await
        .expect("计数必须成功");
    let canonical: i64 =
        sqlx::query_scalar("SELECT COUNT(*) FROM users WHERE email = 'x5@nebula.st'")
            .fetch_one(&pool)
            .await
            .expect("计数必须成功");
    pool.close().await;
    assert_eq!(total, 2, "近似域名不得落库；Unicode 点只允许规范化为同一域名");
    assert_eq!(canonical, 1, "Unicode 点必须规范化为白名单内的 A-label 邮箱");
}

/// 用户名：Unicode、零宽字符、SQL 通配符与超长值一律拒绝；首尾空白按 trim 归一。
#[tokio::test]
async fn sec_account_12_username_rejects_unicode_and_wildcards() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.42.0.1", "names@nebula.st", "names").await;
    let session = login_at(&app, "10.42.0.2", "names@nebula.st", PASSWORD).await;

    let rejected = [
        "ａｂｃ",           // 全角字母
        "ab\u{200b}c",      // 零宽空格
        "ab%cd",            // SQL LIKE 通配符
        "a_b",              // 下划线允许，但长度合法时用于阴性对照之外的检查
        "aaaaaaaaaaaaaaaa", // 16 字符，超出上限
        "a",                // 过短
    ];
    for (index, username) in rejected.iter().enumerate() {
        if *username == "a_b" {
            // 下划线属于允许字符集：应被接受（防止把合法字符误判为注入）。
            let accepted = patch_json_auth(
                &app,
                &format!("10.42.1.{index}"),
                "/api/user/username",
                &json!({ "username": "a_b", "current_password": PASSWORD }).to_string(),
                &session,
            )
            .await;
            assert_eq!(accepted.status(), StatusCode::NO_CONTENT, "合法字符不得被误拒");
            continue;
        }
        let response = patch_json_auth(
            &app,
            &format!("10.42.1.{index}"),
            "/api/user/username",
            &json!({ "username": username, "current_password": PASSWORD }).to_string(),
            &session,
        )
        .await;
        assert_eq!(
            response.status(),
            StatusCode::UNPROCESSABLE_ENTITY,
            "非法用户名必须 422: {username:?}"
        );
    }

    let trimmed = patch_json_auth(
        &app,
        "10.42.2.1",
        "/api/user/username",
        &json!({ "username": "  spaced  ", "current_password": PASSWORD }).to_string(),
        &session,
    )
    .await;
    assert_eq!(trimmed.status(), StatusCode::NO_CONTENT, "首尾空白应被 trim");
    assert_eq!(profile_json(&app, "10.42.2.2", &session).await["username"], "spaced");
}

/// 导出文档不得包含口令哈希、TOTP 密钥或恢复码摘要；响应必须 no-store 且一次性。
#[tokio::test]
async fn sec_account_13_export_document_has_no_credentials() {
    let (app, state, clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.43.0.1", "vault@nebula.st", "vault1").await;
    let session = login_at(&app, "10.43.0.2", "vault@nebula.st", PASSWORD).await;
    let user = state
        .user_repo
        .find_by_email("vault@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");
    state
        .totp_repo
        .set_pending(&user.id, "totp-secret-marker", clock.now())
        .await
        .expect("pending 必须成功");
    state.totp_repo.enable(&user.id, clock.now()).await.expect("启用必须成功");
    state
        .totp_repo
        .add_recovery_codes(&user.id, vec!["recovery-hash-marker".to_owned()], clock.now())
        .await
        .expect("恢复码必须落库");

    let issued = post_json_auth(&app, "10.43.0.3", "/api/user/export", "", &session).await;
    assert_eq!(issued.status(), StatusCode::OK);
    let download_url = json_body(&body_text(issued).await)["download_url"]
        .as_str()
        .expect("必须返回地址")
        .to_owned();
    let token = download_url.rsplit('/').next().expect("地址必须含令牌").to_owned();
    assert_eq!(token.len(), 64, "导出令牌必须 256 位");
    assert!(token.chars().all(|c| c.is_ascii_hexdigit()), "令牌必须是十六进制");

    let download = get(&app, "10.43.0.4", &download_url).await;
    assert_eq!(download.status(), StatusCode::OK);
    assert_eq!(
        download.headers().get(header::CACHE_CONTROL).and_then(|v| v.to_str().ok()),
        Some("no-store"),
        "导出内容必须禁止缓存"
    );
    let text = body_text(download).await;
    assert!(!text.contains("totp-secret-marker"), "导出不得包含 TOTP 密钥密文");
    assert!(!text.contains("recovery-hash-marker"), "导出不得包含恢复码摘要");
    assert!(!text.contains("password_hash"), "导出不得包含口令哈希字段");
    assert!(!text.contains(PASSWORD), "导出不得包含明文口令");
    let document = json_body(&text);
    assert_eq!(document["totp"]["recovery_codes_remaining"], 1);

    let replay = get(&app, "10.43.0.5", &download_url).await;
    assert_eq!(replay.status(), StatusCode::NOT_FOUND, "导出令牌必须一次性");
}

/// 头像：SVG/HTML 与伪造 MIME 不能产生主动内容；路径穿越与非 UUID 一律 404。
#[tokio::test]
async fn sec_account_14_avatar_rejects_active_content_and_traversal() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.44.0.1", "canvas@nebula.st", "canvas1").await;
    let session = login_at(&app, "10.44.0.2", "canvas@nebula.st", PASSWORD).await;
    let user = state
        .user_repo
        .find_by_email("canvas@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");

    let html = upload(
        &app,
        "10.44.1.1",
        &session,
        &[multipart_field(
            "avatar",
            "avatar.png",
            "image/png",
            b"<html><script>alert(1)</script></html>",
        )],
    )
    .await;
    assert_eq!(html.status(), StatusCode::UNPROCESSABLE_ENTITY, "HTML 不得作为头像接受");
    assert!(body_text(html).await.contains("avatar_unsupported_format"));

    let svg = upload(
        &app,
        "10.44.1.2",
        &session,
        &[multipart_field(
            "avatar",
            "avatar.svg",
            "image/svg+xml",
            br#"<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>"#,
        )],
    )
    .await;
    assert_eq!(svg.status(), StatusCode::UNPROCESSABLE_ENTITY, "SVG 不得作为头像接受");
    assert!(profile_json(&app, "10.44.1.3", &session).await["avatar_url"].is_null());

    // 合法 PNG 尾部拼接 HTML，且声明 text/html：输出必须仍是重编码的 WebP。
    let mut polyglot = png_of(64, 64, [200, 30, 30, 255]);
    polyglot.extend_from_slice(b"<script>alert(1)</script>");
    let uploaded = upload(
        &app,
        "10.44.1.4",
        &session,
        &[multipart_field("avatar", "x", "text/html", &polyglot)],
    )
    .await;
    assert_eq!(uploaded.status(), StatusCode::OK);
    let avatar_url = json_body(&body_text(uploaded).await)["avatar_url"]
        .as_str()
        .expect("必须返回地址")
        .to_owned();
    let (status, headers, bytes) = avatar_bytes(&app, "10.44.1.5", &avatar_url).await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(
        headers.get(header::CONTENT_TYPE).and_then(|v| v.to_str().ok()),
        Some("image/webp"),
        "服务端不得按上传时声明的 MIME 分发"
    );
    assert_eq!(&bytes[..4], b"RIFF");
    assert!(!bytes.windows(6).any(|window| window == b"script"), "输出不得夹带上传内容");

    // 路径穿越 / 非 UUID：不得读到头像目录之外的文件。
    for uri in [
        "/api/users/..%2F..%2Fusers.db/avatar".to_owned(),
        "/api/users/../avatar".to_owned(),
        format!("/api/users/{}/avatar", "0".repeat(36)),
    ] {
        let (status, _headers, body) = avatar_bytes(&app, "10.44.1.6", &uri).await;
        assert_eq!(status, StatusCode::NOT_FOUND, "非法 user_id 必须 404: {uri}");
        assert!(
            !body.windows(15).any(|window| window == b"SQLite format 3"),
            "响应不得包含数据库文件内容: {uri}"
        );
    }
    assert!(state.avatar_store.load(&user.id, 1).await.expect("读取必须成功").is_some());
}

/// 头像 IDOR/覆盖：上传始终写到会话所属用户；携带他人 `user_id` 字段无效。
#[tokio::test]
async fn sec_account_15_avatar_upload_is_bound_to_session_user() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.45.0.1", "alice@nebula.st", "alice1").await;
    register_ok(&app, "10.45.0.2", "bob@nebula.st", "bob1").await;
    let alice = login_at(&app, "10.45.0.3", "alice@nebula.st", PASSWORD).await;
    let bob = login_at(&app, "10.45.0.4", "bob@nebula.st", PASSWORD).await;
    let alice_id = state
        .user_repo
        .find_by_email("alice@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("存在")
        .id;
    let bob_id = state
        .user_repo
        .find_by_email("bob@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("存在")
        .id;

    let alice_upload = upload(
        &app,
        "10.45.1.1",
        &alice,
        &[multipart_field("avatar", "a.png", "image/png", &png_of(80, 80, [220, 20, 20, 255]))],
    )
    .await;
    assert_eq!(alice_upload.status(), StatusCode::OK);
    let alice_url = json_body(&body_text(alice_upload).await)["avatar_url"]
        .as_str()
        .expect("必须返回地址")
        .to_owned();
    let (_, _, alice_before) = avatar_bytes(&app, "10.45.1.2", &alice_url).await;

    // Bob 上传时夹带 alice 的 user_id；字段必须被忽略，图片落在他自己名下。
    let bob_upload = upload(
        &app,
        "10.45.1.3",
        &bob,
        &[
            multipart_text_field("user_id", &alice_id.to_string()),
            multipart_field("avatar", "b.png", "image/png", &png_of(80, 80, [20, 20, 220, 255])),
        ],
    )
    .await;
    assert_eq!(bob_upload.status(), StatusCode::OK);
    let bob_url = json_body(&body_text(bob_upload).await)["avatar_url"]
        .as_str()
        .expect("必须返回地址")
        .to_owned();
    assert!(bob_url.contains(&bob_id.to_string()));

    let (_, _, alice_after) = avatar_bytes(&app, "10.45.1.4", &alice_url).await;
    assert_eq!(alice_before, alice_after, "他人上传不得覆盖 alice 的头像");
    let (_, _, bob_bytes_served) = avatar_bytes(&app, "10.45.1.5", &bob_url).await;
    assert_ne!(alice_after, bob_bytes_served, "两个账户的头像必须彼此独立");
}

/// Unicode/IDN 邮箱形式：同一 IDNA 域的不同规范化形式不得产生两个账户。
#[tokio::test]
async fn sec_account_16_unicode_idn_email_forms_create_duplicate_accounts() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    let nfc = "user@b\u{00fc}cher.example";
    let nfd = "user@bu\u{0308}cher.example";
    assert_ne!(nfc, nfd);

    let first = register_at(&app, "10.46.0.1", nfc, "idn1", PASSWORD).await;
    assert_eq!(first.status(), StatusCode::CREATED, "IDN 邮箱应可注册");

    // 两种形式 IDNA 到同一 A-label 域，指向同一邮箱；第二个形式不得再建账户。
    let second = register_at(&app, "10.46.0.2", nfd, "idn2", PASSWORD).await;
    assert_ne!(
        second.status(),
        StatusCode::CREATED,
        "同一 IDNA 域的不同 Unicode 规范化形式被当成不同邮箱，产生重复账户"
    );
}

// ---------------------------------------------------------------------------
// 缺陷 6：未验证账户可启用 TOTP，配合永久占用可把任意邮箱锁死
// ---------------------------------------------------------------------------

/// 以注入时钟的固定时刻计算六位 TOTP 码（密钥来自 setup 响应）。
fn totp_code_at(secret: &str, now: chrono::DateTime<chrono::Utc>) -> String {
    use nebula_server::model::totp::{TOTP_ALGORITHM, TOTP_DIGITS, TOTP_PERIOD, TOTP_SKEW};

    let parsed = totp_rs::Secret::try_from_base32(secret).expect("setup 密钥必须可解析");
    let totp = totp_rs::Builder::new()
        .with_algorithm(TOTP_ALGORITHM)
        .with_digits(TOTP_DIGITS)
        .with_skew(u16::from(TOTP_SKEW))
        .with_step_duration(TOTP_PERIOD)
        .with_secret(parsed)
        .build()
        .expect("参数合法");
    totp.generate(now.timestamp().cast_unsigned()).to_string()
}

/// 攻击场景：攻击者用受害者邮箱注册但从不验证，随即 setup + enable TOTP。
/// 受害者走重置拿回口令后，登录仍被 TOTP 拦住，而恢复码只在攻击者手里；
/// 未验证注册又永不失效，于是该邮箱在此服务上被永久锁死、无法回收。
#[tokio::test]
async fn sec_account_17_unverified_account_can_enable_totp() {
    let (app, state, _clock, _mailbox, _dir) = common::app().await;
    register_ok(&app, "10.47.0.1", "hostage@nebula.st", "hostage").await;
    let session = login_at(&app, "10.47.0.2", "hostage@nebula.st", PASSWORD).await;

    let setup = post_json_auth(
        &app,
        "10.47.0.3",
        "/api/user/totp/setup",
        &json!({ "password": PASSWORD }).to_string(),
        &session,
    )
    .await;
    assert_eq!(setup.status(), StatusCode::OK, "setup 目前不要求邮箱已验证");
    let secret = json_body(&body_text(setup).await)["secret"]
        .as_str()
        .expect("必须返回 TOTP 密钥")
        .to_owned();
    let code = totp_code_at(&secret, state.clock.now());
    let enabled = post_json_auth(
        &app,
        "10.47.0.4",
        "/api/user/totp/enable",
        &json!({ "password": PASSWORD, "code": code }).to_string(),
        &session,
    )
    .await;
    let status = enabled.status();
    let body = body_text(enabled).await;
    assert_eq!(
        status,
        StatusCode::CONFLICT,
        "未验证邮箱的账户不得启用 TOTP（可永久锁死他人邮箱，无管理员重置入口）: {body}"
    );
}
