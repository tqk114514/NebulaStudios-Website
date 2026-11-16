//! POST /api/user/avatar：上传并缩放头像，转为 WebP 后落盘。

use axum::Json;
use axum::extract::{Multipart, State};
use serde::Serialize;

use crate::api::AppState;
use crate::avatar::{self, AvatarError, MAX_UPLOAD_BYTES};
use crate::error::{ApiError, FieldInvalid};
use crate::extractors::FirstPartyUser;

/// 头像上传成功响应。
#[derive(Debug, Serialize)]
pub struct AvatarResponse {
    /// 带版本参数的公开读取地址；上传成功后立即指向新图。
    pub avatar_url: String,
}

/// 接收 multipart 字段 `avatar`，解码、居中裁剪缩放为 512 见方 WebP 后落盘。
///
/// 版本先递增再替换文件：即使替换失败，旧地址仍对应旧图，新地址不会命中旧缓存。
/// 单文件上限 [`MAX_UPLOAD_BYTES`]，输入单边上限 [`avatar::MAX_DIMENSION`]。
///
/// # Errors
/// 未认证 401、CSRF 失败 403；缺字段 422、图片不可解码或超限 422；
/// multipart 非法 400；文件写入或存储故障 500。
pub async fn upload(
    State(state): State<AppState>,
    user: FirstPartyUser,
    mut multipart: Multipart,
) -> Result<Json<AvatarResponse>, ApiError> {
    let mut payload = None;
    while let Some(field) =
        multipart.next_field().await.map_err(|err| ApiError::MalformedMultipart(err.to_string()))?
    {
        if field.name() != Some("avatar") {
            continue;
        }
        if payload.is_some() {
            return Err(invalid("avatar_duplicate"));
        }
        let bytes =
            field.bytes().await.map_err(|err| ApiError::MalformedMultipart(err.to_string()))?;
        if bytes.len() > MAX_UPLOAD_BYTES {
            return Err(ApiError::PayloadTooLarge);
        }
        payload = Some(bytes);
    }
    let Some(payload) = payload else {
        return Err(invalid("avatar_missing"));
    };

    let rendered = tokio::task::spawn_blocking(move || avatar::render_webp(&payload))
        .await
        .map_err(anyhow::Error::from)?;
    let webp = match rendered {
        Ok(webp) => webp,
        Err(err) => return Err(map_avatar_error(&err)),
    };

    // 版本先于文件递增：文件替换失败时只会让新地址暂时返回旧图，
    // 不会出现同一地址永久缓存旧图
    let Some(version) =
        state.user_repo.bump_avatar_version(&user.user_id).await.map_err(anyhow::Error::new)?
    else {
        // 提取器刚确认用户存在；这里缺失只可能是并发删除
        return Err(ApiError::Unauthorized);
    };

    state.avatar_store.store(&user.user_id, &webp).await.map_err(anyhow::Error::new)?;

    let avatar_url = crate::api::avatar_url(&user.user_id, version)
        .ok_or_else(|| ApiError::Internal(anyhow::anyhow!("头像版本非法: {version}")))?;
    Ok(Json(AvatarResponse { avatar_url }))
}

/// 把图像处理错误映射为字段级 422；编码失败属于服务端问题。
fn map_avatar_error(err: &AvatarError) -> ApiError {
    match err {
        AvatarError::UnsupportedFormat => invalid("avatar_unsupported_format"),
        AvatarError::DimensionsTooLarge => invalid("avatar_dimensions_too_large"),
        AvatarError::InvalidImage => invalid("avatar_invalid_image"),
        AvatarError::Encode(inner) => {
            ApiError::Internal(anyhow::anyhow!("WebP 编码失败: {inner:?}"))
        }
    }
}

fn invalid(code: &'static str) -> ApiError {
    ApiError::Validation(vec![FieldInvalid { field: "avatar", code: code.to_owned() }])
}
