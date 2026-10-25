//! 进程级遥测初始化。

/// 安装全局 tracing 订阅者；每进程调用一次，级别经 `RUST_LOG` 覆盖，默认 `info`。
pub fn init() {
    let filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));
    tracing_subscriber::fmt().with_env_filter(filter).init();
}
