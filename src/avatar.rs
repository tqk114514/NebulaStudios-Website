//! 头像处理：解码、居中裁剪缩放、WebP 编码，以及按用户 id 落盘读取。
//!
//! 图像处理是同步 CPU 工作，调用方必须放进 `spawn_blocking`（AGENTS §0、§9.5）。
//! 输入尺寸有上限，输出恒为 [`OUTPUT_SIZE`] 见方的 WebP。

use std::io::Cursor;
use std::path::{Path, PathBuf};

use image::ImageReader;
use uuid::Uuid;

use crate::model::user::UserId;

/// 单次上传的原始字节上限。
pub const MAX_UPLOAD_BYTES: usize = 5 * 1024 * 1024;
/// multipart 请求体上限；为边界与表单头留出余量。
pub const MAX_REQUEST_BYTES: usize = MAX_UPLOAD_BYTES + 64 * 1024;
/// 输入图像单边像素上限；超出即拒绝，不按比例缩小。
pub const MAX_DIMENSION: u32 = 4096;
/// 输出头像边长（正方形）。
pub const OUTPUT_SIZE: u32 = 512;
/// WebP 有损编码质量。
const WEBP_QUALITY: f32 = 85.0;
/// 解码期允许的最大内存分配，防止解压炸弹。
const MAX_DECODE_ALLOC: u64 = 256 * 1024 * 1024;

/// 头像解码或编码失败。
#[derive(Debug, thiserror::Error)]
pub enum AvatarError {
    /// 字节流不是受支持的图片格式。
    #[error("不支持的图片格式")]
    UnsupportedFormat,
    /// 输入图像超出单边像素上限。
    #[error("图片尺寸超过上限")]
    DimensionsTooLarge,
    /// 图片内容损坏或无法解码。
    #[error("图片内容无法解码")]
    InvalidImage,
    /// WebP 编码失败；底层错误不实现 `Error`，只保留调试表示。
    #[error("WebP 编码失败: {0:?}")]
    Encode(webp::WebPEncodingError),
}

/// 把任意受支持格式的图片渲染为 [`OUTPUT_SIZE`] 见方的 WebP。
///
/// 先按内容嗅探格式，再在尺寸与分配上限内解码；居中裁剪后缩放，最后有损编码。
/// 输入格式由内容决定，不信任调用方声明的 MIME 类型。
///
/// # Errors
/// 无法识别格式返回 [`AvatarError::UnsupportedFormat`]；尺寸或分配超限返回
/// [`AvatarError::DimensionsTooLarge`]；解码失败返回 [`AvatarError::InvalidImage`]；
/// 编码失败返回 [`AvatarError::Encode`]。
pub fn render_webp(bytes: &[u8]) -> Result<Vec<u8>, AvatarError> {
    let mut reader = ImageReader::new(Cursor::new(bytes))
        .with_guessed_format()
        .map_err(|_| AvatarError::InvalidImage)?;
    if reader.format().is_none() {
        return Err(AvatarError::UnsupportedFormat);
    }
    let mut limits = image::Limits::default();
    limits.max_image_width = Some(MAX_DIMENSION);
    limits.max_image_height = Some(MAX_DIMENSION);
    limits.max_alloc = Some(MAX_DECODE_ALLOC);
    reader.limits(limits);

    let decoded = reader.decode().map_err(|err| match err {
        image::ImageError::Unsupported(_) => AvatarError::UnsupportedFormat,
        image::ImageError::Limits(_) => AvatarError::DimensionsTooLarge,
        image::ImageError::IoError(_)
        | image::ImageError::Decoding(_)
        | image::ImageError::Encoding(_)
        | image::ImageError::Parameter(_) => AvatarError::InvalidImage,
    })?;

    let resized =
        decoded.resize_to_fill(OUTPUT_SIZE, OUTPUT_SIZE, image::imageops::FilterType::Lanczos3);
    let rgba = resized.to_rgba8();
    let (width, height) = rgba.dimensions();
    webp::Encoder::from_rgba(rgba.as_raw(), width, height)
        .encode_simple(false, WEBP_QUALITY)
        .map(|encoded| encoded.to_vec())
        .map_err(AvatarError::Encode)
}

/// 头像文件读写失败。
#[derive(Debug, thiserror::Error)]
pub enum AvatarStoreError {
    /// 目录创建、读取、写入或删除失败。
    #[error("头像文件读写失败: {0}")]
    Io(#[from] std::io::Error),
}

/// 按用户 id 存取 `{dir}/{user_id}.webp` 的文件存储。
///
/// 写入先落临时文件再原子替换，读者不会看到半张图片。
#[derive(Debug, Clone)]
pub struct AvatarStore {
    dir: PathBuf,
}

impl AvatarStore {
    /// 指定头像目录；目录在首次写入时创建。
    #[must_use]
    pub fn new(dir: impl Into<PathBuf>) -> Self {
        Self { dir: dir.into() }
    }

    /// 头像目录。
    #[must_use]
    pub fn dir(&self) -> &Path {
        &self.dir
    }

    /// 读取用户头像；文件不存在返回 `None`。
    ///
    /// # Errors
    /// 路径存在但读取失败时返回 [`AvatarStoreError::Io`]。
    pub async fn load(&self, user_id: &UserId) -> Result<Option<Vec<u8>>, AvatarStoreError> {
        match tokio::fs::read(self.path_for(user_id)).await {
            Ok(bytes) => Ok(Some(bytes)),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
            Err(err) => Err(AvatarStoreError::Io(err)),
        }
    }

    /// 写入用户头像；已存在时原子替换。
    ///
    /// # Errors
    /// 目录创建、临时文件写入或替换失败时返回 [`AvatarStoreError::Io`]。
    pub async fn store(&self, user_id: &UserId, bytes: &[u8]) -> Result<(), AvatarStoreError> {
        tokio::fs::create_dir_all(&self.dir).await?;
        let target = self.path_for(user_id);
        let temp = self.dir.join(format!("{user_id}.webp.tmp-{}", Uuid::now_v7()));
        tokio::fs::write(&temp, bytes).await?;
        if let Err(err) = tokio::fs::rename(&temp, &target).await {
            // 清理失败不改变主错误；残留临时文件不影响读取
            let _ = tokio::fs::remove_file(&temp).await;
            return Err(AvatarStoreError::Io(err));
        }
        Ok(())
    }

    /// 删除用户头像；文件本来不存在也视为成功（幂等）。
    ///
    /// # Errors
    /// 删除失败时返回 [`AvatarStoreError::Io`]。
    pub async fn remove(&self, user_id: &UserId) -> Result<(), AvatarStoreError> {
        match tokio::fs::remove_file(self.path_for(user_id)).await {
            Ok(()) => Ok(()),
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(()),
            Err(err) => Err(AvatarStoreError::Io(err)),
        }
    }

    fn path_for(&self, user_id: &UserId) -> PathBuf {
        self.dir.join(format!("{user_id}.webp"))
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
    #![allow(clippy::expect_used, clippy::unwrap_used)]

    use image::{DynamicImage, ImageFormat, Rgba, RgbaImage};

    use super::{AvatarError, AvatarStore, MAX_DIMENSION, OUTPUT_SIZE, render_webp};
    use crate::model::user::UserId;

    fn png_of(width: u32, height: u32) -> Vec<u8> {
        let image = RgbaImage::from_pixel(width, height, Rgba([180, 40, 90, 255]));
        let mut bytes = std::io::Cursor::new(Vec::new());
        DynamicImage::ImageRgba8(image)
            .write_to(&mut bytes, ImageFormat::Png)
            .expect("测试图片必须可编码");
        bytes.into_inner()
    }

    #[test]
    fn render_webp_scales_to_square_output() {
        let encoded = render_webp(&png_of(800, 600)).expect("合法 PNG 必须可转换");
        assert_eq!(&encoded[..4], b"RIFF");
        assert_eq!(&encoded[8..12], b"WEBP");

        let decoded = image::load_from_memory(&encoded).expect("输出必须是可解码的 WebP");
        assert_eq!(decoded.width(), OUTPUT_SIZE);
        assert_eq!(decoded.height(), OUTPUT_SIZE);
    }

    #[test]
    fn render_webp_rejects_image_over_dimension_limit() {
        let err = render_webp(&png_of(MAX_DIMENSION + 1, 8)).expect_err("超宽图片必须被拒绝");
        assert!(matches!(err, AvatarError::DimensionsTooLarge));
    }

    #[test]
    fn render_webp_rejects_non_image_bytes() {
        let err = render_webp(b"not an image").expect_err("非图片字节必须被拒绝");
        assert!(matches!(err, AvatarError::UnsupportedFormat));
    }

    #[tokio::test]
    async fn store_load_and_remove_round_trip() {
        let dir = tempfile::tempdir().expect("临时目录必须可创建");
        let store = AvatarStore::new(dir.path().join("avatars"));
        let user_id = UserId::new();

        assert!(store.load(&user_id).await.expect("读取必须成功").is_none());
        store.store(&user_id, b"webp-bytes").await.expect("写入必须成功");
        assert_eq!(
            store.load(&user_id).await.expect("读取必须成功").expect("文件必须存在"),
            b"webp-bytes"
        );
        assert!(store.dir().join(format!("{user_id}.webp")).exists());

        store.remove(&user_id).await.expect("删除必须成功");
        assert!(store.load(&user_id).await.expect("读取必须成功").is_none());
        // 幂等：再次删除不报错
        store.remove(&user_id).await.expect("重复删除必须成功");
    }
}
