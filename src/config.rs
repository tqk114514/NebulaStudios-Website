//! 环境变量到类型化配置的加载，值非法时启动即失败。

use std::net::SocketAddr;

use serde::Deserialize;

/// 服务配置；字段经 `NEBULA_` 前缀环境变量覆盖（如 `NEBULA_BIND`）。
#[derive(Debug, Clone, Deserialize)]
pub struct Config {
    /// HTTP 监听地址。
    pub bind: SocketAddr,
}

/// 配置加载或校验失败。
#[derive(Debug, thiserror::Error)]
#[error("配置加载失败: {0}")]
pub struct LoadConfigError(#[from] config::ConfigError);

impl Config {
    /// 从环境变量构建配置；解析与类型校验在此完成，调用方拿不到半成品配置。
    ///
    /// # Errors
    /// 环境变量值无法反序列化为字段类型（如 `bind` 不是合法 `ip:port`）时返回错误。
    pub fn load() -> Result<Self, LoadConfigError> {
        let settings = config::Config::builder()
            .set_default("bind", "127.0.0.1:8080")?
            .add_source(config::Environment::with_prefix("NEBULA"))
            .build()?;
        Ok(settings.try_deserialize()?)
    }
}

#[cfg(test)]
mod tests {
    // 断言失败即 panic 是测试的正确失败语义；expect 禁令面向生产路径
    #![allow(clippy::expect_used)]

    use super::Config;
    use std::net::SocketAddr;

    // 测试进程未设置 NEBULA_BIND，走默认值路径；不改环境变量（edition 2024 中
    // set_var 是 unsafe，本 crate forbid unsafe）
    #[test]
    fn default_bind_is_loopback() {
        let config = Config::load().expect("未设置环境变量时必须成功");
        assert_eq!(config.bind, SocketAddr::from(([127, 0, 0, 1], 8080)));
    }
}
