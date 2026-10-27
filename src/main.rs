//! 装配与监听，无业务逻辑（AGENTS.md §2）。

use std::collections::HashSet;
use std::sync::Arc;
use std::time::Duration;

use anyhow::Context as _;
use axum_prometheus::PrometheusMetricLayer;
use chrono::TimeDelta;
use nebula_server::api::{self, AppState, JwtKeys, SessionPolicy};
use nebula_server::config::Config;
use nebula_server::context::SystemClock;
use nebula_server::crypto::{AesGcmCipher, SecretCipher};
use nebula_server::storage::sqlite::{
    self, SqliteAdminRepo, SqliteMaintenanceRepo, SqliteRefreshRepo, SqliteUserRepo,
};
use nebula_server::telemetry;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    telemetry::init();
    let config = Config::load().context("读取配置")?;

    let pool = sqlite::connect_pool(&config.database.path)
        .await
        .with_context(|| format!("打开数据库 {}", config.database.path.display()))?;

    let private_pem = std::fs::read(&config.jwt.private_key_path)
        .with_context(|| format!("读取 JWT 私钥 {}", config.jwt.private_key_path.display()))?;
    let public_pem = std::fs::read(&config.jwt.public_key_path)
        .with_context(|| format!("读取 JWT 公钥 {}", config.jwt.public_key_path.display()))?;
    let jwt = JwtKeys::from_pem(&private_pem, &public_pem).context("解析 JWT 密钥")?;

    let mailer: Arc<dyn nebula_server::mailer::Mailer> = if config.mail.smtp_host.is_empty() {
        tracing::warn!("未配置 SMTP 主机，邮件仅记日志（开发模式）");
        Arc::new(nebula_server::mailer::LogMailer)
    } else {
        Arc::new(nebula_server::mailer::SmtpMailer::connect(
            &config.mail.smtp_host,
            config.mail.smtp_port,
            &config.mail.smtp_username,
            &config.mail.smtp_password,
            &config.mail.from,
        )?)
    };

    let cors_allowed_origin: Option<axum::http::HeaderValue> =
        if config.cors.allowed_origin.is_empty() {
            None
        } else {
            Some(config.cors.allowed_origin.parse().context("解析 CORS allowed_origin")?)
        };
    let (metrics_layer, metrics_handle) = PrometheusMetricLayer::pair();
    let admin_emails: Arc<HashSet<String>> = Arc::new(
        config
            .admin
            .emails
            .split(',')
            .map(str::trim)
            .filter(|email| !email.is_empty())
            .map(str::to_lowercase)
            .collect(),
    );
    if admin_emails.is_empty() {
        tracing::warn!("未配置管理员邮箱，/api/admin/* 不可用");
    }
    if config.crypto.totp_secret_key.is_empty() {
        anyhow::bail!("NEBULA_CRYPTO__TOTP_SECRET_KEY 未配置");
    }
    let secret_cipher: Arc<dyn SecretCipher> = Arc::new(
        AesGcmCipher::from_hex(&config.crypto.totp_secret_key).context("解析 TOTP 加密密钥")?,
    );

    let state = AppState {
        user_repo: Arc::new(SqliteUserRepo::from_pool(pool.clone())),
        refresh_repo: Arc::new(SqliteRefreshRepo::from_pool(pool.clone())),
        clock: Arc::new(SystemClock),
        jwt,
        verification_repo: Arc::new(
            nebula_server::storage::sqlite::SqliteEmailVerificationRepo::from_pool(pool.clone()),
        ),
        account_repo: Arc::new(nebula_server::storage::sqlite::SqliteAccountRepo::from_pool(
            pool.clone(),
        )),
        totp_repo: Arc::new(nebula_server::storage::sqlite::SqliteTotpRepo::from_pool(
            pool.clone(),
        )),
        oauth_repo: Arc::new(nebula_server::storage::sqlite::SqliteOAuthRepo::from_pool(
            pool.clone(),
        )),
        maintenance_repo: Arc::new(SqliteMaintenanceRepo::from_pool(pool.clone())),
        admin_repo: Arc::new(SqliteAdminRepo::from_pool(pool)),
        admin_emails,
        secret_cipher,
        mailer,
        metrics: Some(Arc::new(metrics_handle)),
        cors_allowed_origin,
        session: SessionPolicy {
            access_ttl: TimeDelta::try_seconds(i64::try_from(config.jwt.access_ttl_secs)?)
                .context("access ttl 超出可表示范围")?,
            refresh_ttl: TimeDelta::try_seconds(i64::try_from(config.jwt.refresh_ttl_secs)?)
                .context("refresh ttl 超出可表示范围")?,
        },
    };

    let listener = tokio::net::TcpListener::bind(config.bind)
        .await
        .with_context(|| format!("监听 {}", config.bind))?;
    tracing::info!(bind = %config.bind, "listening");
    // 监听成功后再启动后台任务，避免端口占用时留下无意义的清理循环
    let cleanup = nebula_server::jobs::spawn_cleanup(
        state.clone(),
        Duration::from_secs(config.jobs.cleanup_interval_secs),
    );
    // 限额层以对端 IP 为键（tower_governor PeerIpKeyExtractor），须带连接信息
    let result = axum::serve(
        listener,
        api::router(state)
            .layer(metrics_layer)
            .into_make_service_with_connect_info::<std::net::SocketAddr>(),
    )
    .with_graceful_shutdown(shutdown_signal())
    .await
    .context("服务退出");
    cleanup.abort();
    result?;
    Ok(())
}

/// 等待 Ctrl-C，收到后进入优雅停机。
async fn shutdown_signal() {
    // Err 意味着该平台无法注册信号处理器，等待本身已无意义
    let _ = tokio::signal::ctrl_c().await;
}
