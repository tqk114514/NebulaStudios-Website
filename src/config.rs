//! 环境变量到类型化配置的加载，值非法时启动即失败。

use std::net::SocketAddr;
use std::path::PathBuf;

use serde::Deserialize;

/// 服务配置；字段经 `NEBULA_` 前缀环境变量覆盖（如 `NEBULA_BIND`、
/// `NEBULA_DATABASE__PATH`，双下划线表层级）。
#[derive(Debug, Clone, Deserialize)]
pub struct Config {
    /// HTTP 监听地址。
    pub bind: SocketAddr,
    /// 数据库子配置。
    pub database: DatabaseConfig,
    /// JWT 子配置。
    pub jwt: JwtConfig,
    /// 邮件子配置。
    pub mail: MailConfig,
    /// 后台任务子配置。
    pub jobs: JobsConfig,
    /// CORS 子配置。
    pub cors: CorsConfig,
    /// 管理员子配置。
    pub admin: AdminConfig,
    /// 加密子配置。
    pub crypto: CryptoConfig,
}

/// 加密配置；TOTP 密钥必须由部署方提供 32 字节十六进制密钥。
#[derive(Debug, Clone, Deserialize)]
pub struct CryptoConfig {
    /// AES-256 密钥，64 个十六进制字符。
    pub totp_secret_key: String,
}

/// 管理员配置；邮箱列表逗号分隔，启动时 trim + 小写。
#[derive(Debug, Clone, Deserialize)]
pub struct AdminConfig {
    /// 管理员邮箱列表；空串表示没有管理员，admin 端点不可用。
    pub emails: String,
}

/// 后台任务配置。
#[derive(Debug, Clone, Deserialize)]
pub struct JobsConfig {
    /// 过期数据清理任务运行间隔（秒）。
    pub cleanup_interval_secs: u64,
}

/// CORS 配置；`allowed_origin` 为空表示同源部署、不启用 CORS。
#[derive(Debug, Clone, Deserialize)]
pub struct CorsConfig {
    /// 允许的前端来源，如 `http://localhost:3000`。
    pub allowed_origin: String,
}

/// JWT 会话令牌配置。
#[derive(Debug, Clone, Deserialize)]
pub struct JwtConfig {
    /// ES256 私钥路径（PKCS#8 PEM）。
    pub private_key_path: PathBuf,
    /// 对应公钥路径（PEM），用于校验。
    pub public_key_path: PathBuf,
    /// access token 有效秒数。
    pub access_ttl_secs: u64,
    /// refresh token 有效秒数。
    pub refresh_ttl_secs: u64,
}

/// 邮件配置；`smtp_host` 为空表示不投递（日志模式，开发用）。
#[derive(Debug, Clone, Deserialize)]
pub struct MailConfig {
    /// SMTP 主机；空串启用 [`crate::mailer::LogMailer`]。
    pub smtp_host: String,
    /// SMTP 端口。
    pub smtp_port: u16,
    /// SMTP 用户名；空串不带凭据。
    pub smtp_username: String,
    /// SMTP 密码。
    pub smtp_password: String,
    /// 发件人，`名字 <地址>` 形式。
    pub from: String,
}

/// SQLite 数据库配置（ADR-0003）。
#[derive(Debug, Clone, Deserialize)]
pub struct DatabaseConfig {
    /// 数据库文件路径；须先经 `sqlx migrate` 迁移，启动不建表。
    pub path: PathBuf,
}

/// 配置加载或校验失败。
#[derive(Debug, thiserror::Error)]
pub enum LoadConfigError {
    /// 环境变量读取或反序列化失败。
    #[error("配置加载失败: {0}")]
    Load(#[from] config::ConfigError),
    /// 值通过类型检查但不满足业务约束。
    #[error("配置值非法: {0}")]
    Invalid(String),
}

impl Config {
    /// 从环境变量构建配置；解析与类型校验在此完成，调用方拿不到半成品配置。
    ///
    /// # Errors
    /// 环境变量值无法反序列化为字段类型（如 `bind` 不是合法 `ip:port`）时返回错误。
    pub fn load() -> Result<Self, LoadConfigError> {
        let settings = config::Config::builder()
            .set_default("bind", "127.0.0.1:8080")?
            .set_default("database.path", "nebula.db")?
            .set_default("jwt.private_key_path", "jwt-private.pem")?
            .set_default("jwt.public_key_path", "jwt-public.pem")?
            .set_default("jwt.access_ttl_secs", 3_600)?
            .set_default("jwt.refresh_ttl_secs", 2_592_000)?
            .set_default("mail.smtp_host", "")?
            .set_default("mail.smtp_port", 587)?
            .set_default("mail.smtp_username", "")?
            .set_default("mail.smtp_password", "")?
            .set_default("mail.from", "Nebula <no-reply@nebula.st>")?
            .set_default("jobs.cleanup_interval_secs", 300)?
            .set_default("cors.allowed_origin", "")?
            .set_default("admin.emails", "")?
            .set_default("crypto.totp_secret_key", "")?
            .add_source(config::Environment::with_prefix("NEBULA").separator("__"))
            .build()?;
        let config: Self = settings.try_deserialize()?;
        // 红线 #7：配置必须存在即生效——TTL 在此校验并由会话签发实际使用
        if config.jwt.access_ttl_secs == 0 || config.jwt.refresh_ttl_secs == 0 {
            return Err(LoadConfigError::Invalid("jwt ttl 必须为正秒数".to_owned()));
        }
        if config.jobs.cleanup_interval_secs == 0 {
            return Err(LoadConfigError::Invalid(
                "jobs.cleanup_interval_secs 必须为正秒数".to_owned(),
            ));
        }
        Ok(config)
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use super::Config;
    use std::net::SocketAddr;

    // 测试进程未设置 NEBULA_*，走默认值路径；不改环境变量（edition 2024 中
    // set_var 是 unsafe，本 crate forbid unsafe）
    #[test]
    fn defaults_are_loopback_and_local_file() {
        let config = Config::load().expect("未设置环境变量时必须成功");
        assert_eq!(config.bind, SocketAddr::from(([127, 0, 0, 1], 8080)));
        assert_eq!(config.database.path, std::path::Path::new("nebula.db"));
        assert_eq!(config.jwt.access_ttl_secs, 3_600);
        assert_eq!(config.jwt.refresh_ttl_secs, 2_592_000);
        assert_eq!(config.jobs.cleanup_interval_secs, 300);
        assert_eq!(config.cors.allowed_origin, "");
        assert_eq!(config.admin.emails, "");
        assert_eq!(config.crypto.totp_secret_key, "");
    }
}
