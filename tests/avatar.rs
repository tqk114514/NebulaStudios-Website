//! 头像上传与公开读取集成测试：缩放输出、版本化地址、格式/尺寸/大小上限、
//! 鉴权门禁与注销清理。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use axum::http::StatusCode;
use common::{
    Mailbox, OneshotPost, Session, body_text, cookie_header, extract_code, login_session,
    register_user, register_verified_user,
};
use image::{DynamicImage, ImageFormat, Rgba, RgbaImage};
use nebula_server::avatar::MAX_UPLOAD_BYTES;

const BOUNDARY: &str = "nebula-avatar-test";

fn png_of(width: u32, height: u32, color: [u8; 4]) -> Vec<u8> {
    let image = RgbaImage::from_pixel(width, height, Rgba(color));
    let mut bytes = std::io::Cursor::new(Vec::new());
    DynamicImage::ImageRgba8(image)
        .write_to(&mut bytes, ImageFormat::Png)
        .expect("测试图片必须可编码");
    bytes.into_inner()
}

fn multipart(name: &str, filename: &str, content_type: &str, data: &[u8]) -> Vec<u8> {
    let mut body = Vec::new();
    body.extend_from_slice(
        format!(
            "--{BOUNDARY}\r\nContent-Disposition: form-data; name=\"{name}\"; filename=\"{filename}\"\r\nContent-Type: {content_type}\r\n\r\n"
        )
        .as_bytes(),
    );
    body.extend_from_slice(data);
    body.extend_from_slice(format!("\r\n--{BOUNDARY}--\r\n").as_bytes());
    body
}

async fn upload(app: &axum::Router, session: &Session, data: &[u8]) -> axum::response::Response {
    app.clone()
        .oneshot_post_bytes(
            "/api/user/avatar",
            &format!("multipart/form-data; boundary={BOUNDARY}"),
            multipart("avatar", "avatar.png", "image/png", data),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await
}

async fn profile_json(app: &axum::Router, session: &Session) -> serde_json::Value {
    let cookie = cookie_header(Some(&session.access), None, Some(&session.csrf));
    let response = app.clone().oneshot_get_with_cookie("/api/user", "", &cookie).await;
    assert_eq!(response.status(), StatusCode::OK);
    serde_json::from_str(&body_text(response).await).expect("资料响应必须是 JSON")
}

#[tokio::test]
async fn upload_serves_webp_and_profile_url_changes_per_version() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026").await;
    let session = login_session(&app).await;

    let before = profile_json(&app, &session).await;
    assert!(before["avatar_url"].is_null(), "未上传时不得给出头像地址");

    let first = upload(&app, &session, &png_of(800, 600, [180, 40, 90, 255])).await;
    assert_eq!(first.status(), StatusCode::OK);
    let first: serde_json::Value =
        serde_json::from_str(&body_text(first).await).expect("上传响应必须是 JSON");
    let first_url = first["avatar_url"].as_str().expect("必须返回头像地址").to_owned();
    assert!(first_url.starts_with("/api/users/"), "地址必须指向用户头像路由");
    assert!(first_url.ends_with("?v=1"), "首次上传版本必须为 1");

    let image = app.clone().oneshot_get_with_cookie(&first_url, "", "").await;
    assert_eq!(image.status(), StatusCode::OK);
    assert_eq!(
        image.headers().get("content-type").and_then(|value| value.to_str().ok()),
        Some("image/webp")
    );
    assert!(
        image
            .headers()
            .get("cache-control")
            .and_then(|value| value.to_str().ok())
            .is_some_and(|value| value.contains("immutable")),
        "版本化地址必须可长期缓存"
    );
    let first_bytes =
        http_body_util::BodyExt::collect(image.into_body()).await.expect("读体必须成功").to_bytes();
    assert_eq!(&first_bytes[..4], b"RIFF");
    assert_eq!(&first_bytes[8..12], b"WEBP");
    let decoded = image::load_from_memory(&first_bytes).expect("输出必须是可解码的 WebP");
    assert_eq!((decoded.width(), decoded.height()), (512, 512));

    let after = profile_json(&app, &session).await;
    assert_eq!(after["avatar_url"], first_url);
    assert!(after.get("avatar_version").is_none(), "内部版本号不得外泄");

    let second = upload(&app, &session, &png_of(400, 400, [10, 120, 220, 255])).await;
    assert_eq!(second.status(), StatusCode::OK);
    let second: serde_json::Value =
        serde_json::from_str(&body_text(second).await).expect("上传响应必须是 JSON");
    let second_url = second["avatar_url"].as_str().expect("必须返回头像地址");
    assert!(second_url.ends_with("?v=2"), "再次上传必须换新版本");
    assert_ne!(first_url, second_url);

    let replaced = app.clone().oneshot_get_with_cookie(second_url, "", "").await;
    let replaced_bytes = http_body_util::BodyExt::collect(replaced.into_body())
        .await
        .expect("读体必须成功")
        .to_bytes();
    assert_ne!(first_bytes, replaced_bytes, "新版本必须返回新图片");
}

#[tokio::test]
async fn upload_rejects_missing_invalid_oversized_and_over_dimension_images() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026").await;
    let session = login_session(&app).await;

    let missing = app
        .clone()
        .oneshot_post_bytes(
            "/api/user/avatar",
            &format!("multipart/form-data; boundary={BOUNDARY}"),
            format!("--{BOUNDARY}--\r\n").into_bytes(),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(missing.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(missing).await.contains("avatar_missing"));

    let invalid = upload(&app, &session, b"not an image").await;
    assert_eq!(invalid.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(invalid).await.contains("avatar_unsupported_format"));

    let too_wide = upload(&app, &session, &png_of(4097, 8, [0, 0, 0, 255])).await;
    assert_eq!(too_wide.status(), StatusCode::UNPROCESSABLE_ENTITY);
    assert!(body_text(too_wide).await.contains("avatar_dimensions_too_large"));

    let too_big = upload(&app, &session, &vec![0u8; MAX_UPLOAD_BYTES + 1]).await;
    assert_eq!(too_big.status(), StatusCode::PAYLOAD_TOO_LARGE);
    assert!(body_text(too_big).await.contains("payload_too_large"));
}

#[tokio::test]
async fn upload_requires_authentication_and_csrf() {
    let (app, _state, _clock, _mailbox, _dir) = common::app().await;
    register_user(&app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026").await;
    let session = login_session(&app).await;
    let body = multipart("avatar", "avatar.png", "image/png", &png_of(64, 64, [0, 0, 0, 255]));

    let anonymous = app
        .clone()
        .oneshot_post_bytes(
            "/api/user/avatar",
            &format!("multipart/form-data; boundary={BOUNDARY}"),
            body.clone(),
            "",
            None,
        )
        .await;
    assert_eq!(anonymous.status(), StatusCode::UNAUTHORIZED);

    let without_csrf = app
        .clone()
        .oneshot_post_bytes(
            "/api/user/avatar",
            &format!("multipart/form-data; boundary={BOUNDARY}"),
            body,
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            None,
        )
        .await;
    assert_eq!(without_csrf.status(), StatusCode::FORBIDDEN);
    assert!(body_text(without_csrf).await.contains("csrf_invalid"));
}

#[tokio::test]
async fn account_deletion_removes_avatar_file() {
    let (app, state, _clock, mailbox, _dir) = common::app().await;
    register_verified_user(&app, "nova@nebula.st", "nova", "Nebula!Passw0rd-2026", &mailbox).await;
    let session = login_session(&app).await;
    let uploaded = upload(&app, &session, &png_of(256, 256, [30, 160, 90, 255])).await;
    assert_eq!(uploaded.status(), StatusCode::OK);
    let user = state
        .user_repo
        .find_by_email("nova@nebula.st")
        .await
        .expect("查询必须成功")
        .expect("用户必须存在");
    assert!(state.avatar_store.load(&user.id).await.expect("读取必须成功").is_some());

    request_and_confirm_deletion(&app, &session, &mailbox).await;

    assert!(
        state.avatar_store.load(&user.id).await.expect("读取必须成功").is_none(),
        "注销后头像文件必须删除"
    );
    let response = app
        .clone()
        .oneshot_get_with_cookie(&format!("/api/users/{}/avatar", user.id), "", "")
        .await;
    assert_eq!(response.status(), StatusCode::NOT_FOUND);
}

/// 走完注销发码与确认；只用于需要真实删除路径的用例。
async fn request_and_confirm_deletion(app: &axum::Router, session: &Session, mailbox: &Mailbox) {
    let requested = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/request",
            "",
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(requested.status(), StatusCode::NO_CONTENT);

    let mail = common::await_mail_subject(mailbox, "账户注销确认码").await;
    let code = extract_code(&mail);
    let confirmed = app
        .clone()
        .oneshot_post_with(
            "/api/auth/account/delete/confirm",
            &format!(r#"{{"code":"{code}"}}"#),
            &cookie_header(Some(&session.access), None, Some(&session.csrf)),
            Some(&session.csrf),
        )
        .await;
    assert_eq!(confirmed.status(), StatusCode::NO_CONTENT);
}
