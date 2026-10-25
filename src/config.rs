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
            .add_source(config::Environment::with_prefix("NEBULA").separator("__"))
            .build()?;
        let config: Self = settings.try_deserialize()?;
        // 红线 #7：配置必须存在即生效——TTL 在此校验并由会话签发实际使用
        if config.jwt.access_ttl_secs == 0 || config.jwt.refresh_ttl_secs == 0 {
            return Err(LoadConfigError::Invalid("jwt ttl 必须为正秒数".to_owned()));
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
    }
}
