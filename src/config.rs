//! 环境变量到类型化配置的加载，值非法时启动即失败。

use std::collections::HashSet;
use std::fmt;
use std::net::SocketAddr;
use std::path::PathBuf;

use serde::Deserialize;

use crate::model::policy::{Policy, PolicyCatalog};

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
    /// 社交登录子配置（ADR-0012）。
    pub social: SocialConfig,
    /// OAuth Provider 客户端子配置。
    pub oauth: OAuthConfig,
    /// 头像存储子配置。
    pub avatar: AvatarConfig,
    /// 政策中心子配置。
    pub policy: PolicyConfig,
    /// 注册策略子配置。
    pub registration: RegistrationConfig,
    /// 人机验证子配置。
    pub turnstile: TurnstileConfig,
}

/// 人机验证配置；启用时注册、重发验证码与重置发码要求 Turnstile token。
#[derive(Clone, Deserialize)]
pub struct TurnstileConfig {
    /// 是否启用。
    pub enabled: bool,
    /// 前端渲染组件用的公开 site key。
    pub site_key: String,
    /// 服务端 `siteverify` 用的 secret key。
    pub secret_key: String,
    /// 校验地址；仅允许 https。
    pub verify_url: String,
}

impl std::fmt::Debug for TurnstileConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("TurnstileConfig")
            .field("enabled", &self.enabled)
            .field("site_key", &self.site_key)
            .field("secret_key", &"<redacted>")
            .field("verify_url", &self.verify_url)
            .finish()
    }
}

/// 注册策略配置；白名单只作用于新注册，不影响既有账号登录。
#[derive(Debug, Clone, Default, Deserialize)]
pub struct RegistrationConfig {
    /// 允许注册的邮箱域名，逗号分隔；空串表示不限制。
    pub allowed_email_domains: String,
    /// 已归一化并校验的域名集合；不参与环境变量反序列化。
    #[serde(skip)]
    pub allowed_domains: HashSet<String>,
}

/// 政策中心配置；manifest 只在启动时读取，修改后需重启。
#[derive(Debug, Clone, Default, Deserialize)]
pub struct PolicyConfig {
    /// 政策 manifest TOML 路径；空串表示不启用政策中心。
    pub manifest_file: String,
    /// 已校验并排序的政策目录；不参与环境变量反序列化。
    #[serde(skip)]
    pub catalog: PolicyCatalog,
}

#[derive(Debug, Deserialize)]
struct PolicyManifestFile {
    #[serde(default)]
    policies: Vec<Policy>,
}

/// 头像文件存储配置；路径须可写，启动时创建。
#[derive(Debug, Clone, Deserialize)]
pub struct AvatarConfig {
    /// 头像文件目录；每个用户一个 `{user_id}.webp`。
    pub dir: PathBuf,
}

/// OAuth Provider 客户端配置；客户端只在部署配置中登记，不提供管理端点。
#[derive(Debug, Clone, Default, Deserialize)]
pub struct OAuthConfig {
    /// 客户端 TOML 文件路径；空串表示没有客户端。
    pub clients_file: String,
    /// 已从文件加载的客户端；不参与环境变量反序列化。
    #[serde(skip)]
    pub clients: Vec<OAuthClientSeed>,
}

/// 配置文件中的单个 OAuth 客户端；secret 为明文，启动时哈希后入库。
#[derive(Clone, Deserialize)]
pub struct OAuthClientSeed {
    /// 公开 `client_id`。
    pub client_id: String,
    /// 展示名。
    pub name: String,
    /// 精确匹配的回跳地址。
    pub redirect_uri: String,
    /// 客户端密钥明文；只在启动装配时出现。
    pub secret: String,
    /// 是否启用；缺省 true。
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

fn default_enabled() -> bool {
    true
}

impl std::fmt::Debug for OAuthClientSeed {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("OAuthClientSeed")
            .field("client_id", &self.client_id)
            .field("name", &self.name)
            .field("redirect_uri", &self.redirect_uri)
            .field("secret", &"<redacted>")
            .field("enabled", &self.enabled)
            .finish()
    }
}

#[derive(Debug, Deserialize)]
struct OAuthClientsFile {
    #[serde(default)]
    clients: Vec<OAuthClientSeed>,
}

/// 社交登录配置（ADR-0012）；某 provider 的 client id 为空即不启用该 provider。
///
/// Google 取数经 Cloudflare Worker 代理，启用时须同时提供代理地址、Access
/// service token 与 Worker 的 Ed25519 签名公钥。
#[derive(Clone, Deserialize)]
pub struct SocialConfig {
    /// 站点对外基地址；回跳地址由它派生，启用任一 provider 时必填。
    pub base_url: String,
    /// Google OAuth 客户端 id；空串表示不启用 Google。
    pub google_client_id: String,
    /// Google OAuth 客户端密钥。
    pub google_client_secret: String,
    /// Google 取数代理基地址（Cloudflare Worker）。
    pub google_proxy_url: String,
    /// 访问 Worker 的 Cloudflare Access service token client id。
    pub google_proxy_access_client_id: String,
    /// 访问 Worker 的 Cloudflare Access service token secret。
    pub google_proxy_access_client_secret: String,
    /// Worker 对 Google 响应背书所用的 Ed25519 公钥（PKIX PEM，单行或多行）。
    pub google_worker_signing_public_key: String,
    /// Microsoft OAuth 客户端 id；空串表示不启用 Microsoft。
    pub microsoft_client_id: String,
    /// Microsoft OAuth 客户端密钥。
    pub microsoft_client_secret: String,
}

// 三个 secret 都不得出现在 Debug 输出
impl fmt::Debug for SocialConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("SocialConfig")
            .field("base_url", &self.base_url)
            .field("google_client_id", &self.google_client_id)
            .field("google_client_secret", &"<redacted>")
            .field("google_proxy_url", &self.google_proxy_url)
            .field("google_proxy_access_client_id", &self.google_proxy_access_client_id)
            .field("google_proxy_access_client_secret", &"<redacted>")
            .field("google_worker_signing_public_key", &self.google_worker_signing_public_key)
            .field("microsoft_client_id", &self.microsoft_client_id)
            .field("microsoft_client_secret", &"<redacted>")
            .finish()
    }
}

impl SocialConfig {
    /// Google 是否启用；判定只看 client id。
    #[must_use]
    pub fn google_enabled(&self) -> bool {
        !self.google_client_id.is_empty()
    }

    /// Microsoft 是否启用；判定只看 client id。
    #[must_use]
    pub fn microsoft_enabled(&self) -> bool {
        !self.microsoft_client_id.is_empty()
    }

    /// 派生 provider 的授权回跳地址 `{base_url}/api/auth/{provider}/callback`。
    #[must_use]
    pub fn redirect_uri(&self, provider: &str) -> String {
        format!("{}/api/auth/{provider}/callback", self.base_url.trim_end_matches('/'))
    }
}

/// 加密配置；TOTP 密钥必须由部署方提供 32 字节十六进制密钥。
#[derive(Clone, Deserialize)]
pub struct CryptoConfig {
    /// AES-256 密钥，64 个十六进制字符。
    pub totp_secret_key: String,
    /// 验证码 HMAC pepper，64 个十六进制字符。
    pub code_pepper: String,
}

// 两个字段都是长期密钥；任何 `?config` 都不应把它们写进日志
impl fmt::Debug for CryptoConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("CryptoConfig")
            .field("totp_secret_key", &"<redacted>")
            .field("code_pepper", &"<redacted>")
            .finish()
    }
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
#[derive(Clone, Deserialize)]
pub struct MailConfig {
    /// SMTP 主机；空串启用 [`crate::mailer::LogMailer`]。
    pub smtp_host: String,
    /// SMTP 端口。
    pub smtp_port: u16,
    /// TLS 模式：`starttls`（默认，587）、`implicit`（465）或 `none`。
    pub smtp_tls: String,
    /// SMTP 用户名；空串不带凭据。
    pub smtp_username: String,
    /// SMTP 密码。
    pub smtp_password: String,
    /// 发件人，`名字 <地址>` 形式。
    pub from: String,
}

// SMTP 口令只在连接中继时使用；Debug 输出不得携带
impl fmt::Debug for MailConfig {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("MailConfig")
            .field("smtp_host", &self.smtp_host)
            .field("smtp_port", &self.smtp_port)
            .field("smtp_tls", &self.smtp_tls)
            .field("smtp_username", &self.smtp_username)
            .field("smtp_password", &"<redacted>")
            .field("from", &self.from)
            .finish()
    }
}

/// SQLite 数据库配置（ADR-0003）。
#[derive(Debug, Clone, Deserialize)]
pub struct DatabaseConfig {
    /// 数据库文件路径；启动时自动创建并初始化 schema。
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
            .set_default("mail.smtp_tls", "starttls")?
            .set_default("mail.smtp_username", "")?
            .set_default("mail.smtp_password", "")?
            .set_default("mail.from", "Nebula <no-reply@nebula.st>")?
            .set_default("jobs.cleanup_interval_secs", 300)?
            .set_default("cors.allowed_origin", "")?
            .set_default("admin.emails", "")?
            .set_default("crypto.totp_secret_key", "")?
            .set_default("crypto.code_pepper", "")?
            .set_default("social.base_url", "")?
            .set_default("social.google_client_id", "")?
            .set_default("social.google_client_secret", "")?
            .set_default("social.google_proxy_url", "")?
            .set_default("social.google_proxy_access_client_id", "")?
            .set_default("social.google_proxy_access_client_secret", "")?
            .set_default("social.google_worker_signing_public_key", "")?
            .set_default("social.microsoft_client_id", "")?
            .set_default("social.microsoft_client_secret", "")?
            .set_default("oauth.clients_file", "")?
            .set_default("avatar.dir", "avatars")?
            .set_default("policy.manifest_file", "")?
            .set_default("registration.allowed_email_domains", "")?
            .set_default("turnstile.enabled", false)?
            .set_default("turnstile.site_key", "")?
            .set_default("turnstile.secret_key", "")?
            .set_default(
                "turnstile.verify_url",
                "https://challenges.cloudflare.com/turnstile/v0/siteverify",
            )?
            // prefix_separator 默认等于 separator；必须显式设为单下划线，
            // 否则 NEBULA_BIND 会被当成 NEBULA__BIND，文档约定全部失效
            .add_source(
                config::Environment::with_prefix("NEBULA").prefix_separator("_").separator("__"),
            )
            .build()?;
        let mut config: Self = settings.try_deserialize()?;
        config.oauth.clients = load_oauth_clients(&config.oauth.clients_file)?;
        config.policy.catalog = load_policies(&config.policy.manifest_file)?;
        config.registration.allowed_domains =
            parse_email_domains(&config.registration.allowed_email_domains)?;
        validate_turnstile(&config.turnstile)?;
        // 红线 #7：配置必须存在即生效——TTL 在此校验并由会话签发实际使用
        if config.jwt.access_ttl_secs == 0 || config.jwt.refresh_ttl_secs == 0 {
            return Err(LoadConfigError::Invalid("jwt ttl 必须为正秒数".to_owned()));
        }
        if config.jobs.cleanup_interval_secs == 0 {
            return Err(LoadConfigError::Invalid(
                "jobs.cleanup_interval_secs 必须为正秒数".to_owned(),
            ));
        }
        if config.avatar.dir.as_os_str().is_empty() {
            return Err(LoadConfigError::Invalid("avatar.dir 不得为空".to_owned()));
        }
        if !matches!(config.mail.smtp_tls.as_str(), "starttls" | "implicit" | "none") {
            return Err(LoadConfigError::Invalid(
                "mail.smtp_tls 必须为 starttls/implicit/none".to_owned(),
            ));
        }
        validate_cors_origin(&config.cors.allowed_origin)?;
        // 社交登录：启用任一 provider 才校验其配套项，全空即不启用（ADR-0012）
        let social = &config.social;
        if (social.google_enabled() || social.microsoft_enabled()) && social.base_url.is_empty() {
            return Err(LoadConfigError::Invalid(
                "启用社交登录时 social.base_url 必须配置".to_owned(),
            ));
        }
        if social.google_enabled()
            && (social.google_proxy_url.is_empty()
                || social.google_proxy_access_client_id.is_empty()
                || social.google_proxy_access_client_secret.is_empty()
                || social.google_worker_signing_public_key.is_empty())
        {
            return Err(LoadConfigError::Invalid(
                "配置 google_client_id 时，google 代理地址、Access token 与签名公钥必须齐全"
                    .to_owned(),
            ));
        }
        if social.microsoft_enabled() && social.microsoft_client_secret.is_empty() {
            return Err(LoadConfigError::Invalid(
                "配置 microsoft_client_id 时 microsoft_client_secret 必须配置".to_owned(),
            ));
        }
        Ok(config)
    }
}

/// 读取并校验 OAuth 客户端 TOML；空路径表示没有客户端。
fn load_oauth_clients(path: &str) -> Result<Vec<OAuthClientSeed>, LoadConfigError> {
    if path.is_empty() {
        return Ok(Vec::new());
    }
    let settings = config::Config::builder()
        .add_source(config::File::new(path, config::FileFormat::Toml))
        .build()?;
    let file: OAuthClientsFile = settings.try_deserialize()?;

    let mut seen = std::collections::HashSet::new();
    for client in &file.clients {
        if client.client_id.trim().is_empty()
            || client.name.trim().is_empty()
            || client.redirect_uri.trim().is_empty()
            || client.secret.is_empty()
        {
            return Err(LoadConfigError::Invalid(
                "oauth 客户端字段 client_id/name/redirect_uri/secret 不得为空".to_owned(),
            ));
        }
        if !(client.redirect_uri.starts_with("https://")
            || client.redirect_uri.starts_with("http://"))
        {
            return Err(LoadConfigError::Invalid(format!(
                "oauth 客户端 {} 的 redirect_uri 必须是 http(s) 地址",
                client.client_id
            )));
        }
        if !seen.insert(client.client_id.as_str()) {
            return Err(LoadConfigError::Invalid(format!(
                "oauth 客户端 id 重复: {}",
                client.client_id
            )));
        }
    }
    Ok(file.clients)
}

/// 读取并校验政策 manifest；空路径表示政策中心未配置。
fn load_policies(path: &str) -> Result<PolicyCatalog, LoadConfigError> {
    if path.is_empty() {
        return Ok(PolicyCatalog::default());
    }
    let settings = config::Config::builder()
        .add_source(config::File::new(path, config::FileFormat::Toml))
        .build()?;
    let file: PolicyManifestFile = settings.try_deserialize()?;
    PolicyCatalog::new(file.policies).map_err(|err| LoadConfigError::Invalid(err.to_string()))
}

/// 解析逗号分隔的邮箱域名白名单；空串表示不限制，非空配置不得解析为空集。
fn parse_email_domains(raw: &str) -> Result<HashSet<String>, LoadConfigError> {
    let mut domains = HashSet::new();
    for part in raw.split(',') {
        let domain = part.trim().to_ascii_lowercase();
        if domain.is_empty() {
            continue;
        }
        if !valid_email_domain(&domain) {
            return Err(LoadConfigError::Invalid(format!("邮箱域名非法: {domain}")));
        }
        domains.insert(domain);
    }
    if !raw.is_empty() && domains.is_empty() {
        return Err(LoadConfigError::Invalid(
            "registration.allowed_email_domains 非空但未解析出域名".to_owned(),
        ));
    }
    Ok(domains)
}

/// CORS 来源必须是精确的 `http(s)://host[:port]`；通配符与带路径的值会让
/// `AllowOrigin::exact` 与浏览器实际发送的 Origin 永不匹配，属配置陷阱。
fn validate_cors_origin(origin: &str) -> Result<(), LoadConfigError> {
    if origin.is_empty() {
        return Ok(());
    }
    let rest =
        origin.strip_prefix("https://").or_else(|| origin.strip_prefix("http://")).ok_or_else(
            || LoadConfigError::Invalid("cors.allowed_origin 必须是 http(s) 来源".to_owned()),
        )?;
    if rest.is_empty()
        || rest.contains('@')
        || rest.chars().any(|ch| matches!(ch, '/' | '?' | '#') || ch.is_whitespace())
    {
        return Err(LoadConfigError::Invalid(
            "cors.allowed_origin 必须是精确来源（不含路径、查询、用户信息或通配符）".to_owned(),
        ));
    }
    Ok(())
}

/// 域名形状校验：ASCII 字母数字、`.`、`-`，至少两段，标签不以连字符开头或结尾。
fn valid_email_domain(domain: &str) -> bool {
    !domain.is_empty()
        && domain.len() <= 253
        && domain.contains('.')
        && domain.bytes().all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'-'))
        && domain
            .split('.')
            .all(|label| !label.is_empty() && !label.starts_with('-') && !label.ends_with('-'))
}

/// 启用 Turnstile 时三项配置必须齐全；关闭时不读取 key，也不产生校验流量。
fn validate_turnstile(config: &TurnstileConfig) -> Result<(), LoadConfigError> {
    if !config.enabled {
        return Ok(());
    }
    if config.site_key.trim().is_empty() || config.secret_key.trim().is_empty() {
        return Err(LoadConfigError::Invalid(
            "启用 turnstile 时 site_key 与 secret_key 必须配置".to_owned(),
        ));
    }
    if !config.verify_url.starts_with("https://") {
        return Err(LoadConfigError::Invalid("turnstile.verify_url 必须是 https 地址".to_owned()));
    }
    Ok(())
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
        assert_eq!(config.crypto.code_pepper, "");
        assert_eq!(config.mail.smtp_tls, "starttls");
        // 社交登录默认全空：两个 provider 都不启用
        assert!(!config.social.google_enabled());
        assert!(!config.social.microsoft_enabled());
        assert_eq!(config.social.base_url, "");
        // 没有客户端配置文件时，OAuth 客户端列表为空
        assert_eq!(config.oauth.clients_file, "");
        assert!(config.oauth.clients.is_empty());
        assert_eq!(config.avatar.dir, std::path::Path::new("avatars"));
        assert_eq!(config.policy.manifest_file, "");
        assert!(config.policy.catalog.is_empty());
        assert_eq!(config.registration.allowed_email_domains, "");
        assert!(config.registration.allowed_domains.is_empty());
        assert!(!config.turnstile.enabled);
        assert_eq!(
            config.turnstile.verify_url,
            "https://challenges.cloudflare.com/turnstile/v0/siteverify"
        );
    }

    #[test]
    fn turnstile_requires_keys_only_when_enabled() {
        let mut config = super::TurnstileConfig {
            enabled: false,
            site_key: String::new(),
            secret_key: String::new(),
            verify_url: "http://insecure.example".to_owned(),
        };
        assert!(super::validate_turnstile(&config).is_ok());

        config.enabled = true;
        assert!(super::validate_turnstile(&config).is_err());

        config.site_key = "site-key".to_owned();
        config.secret_key = "secret-key".to_owned();
        assert!(super::validate_turnstile(&config).is_err(), "verify_url 必须是 https");

        config.verify_url = "https://challenges.cloudflare.com/turnstile/v0/siteverify".to_owned();
        assert!(super::validate_turnstile(&config).is_ok());
    }

    #[test]
    fn registration_email_domains_are_normalized_and_validated() {
        let domains = super::parse_email_domains("Nebula.st, example.com, ,nebula.st")
            .expect("合法域名必须可解析");
        assert_eq!(domains.len(), 2);
        assert!(domains.contains("nebula.st"));
        assert!(domains.contains("example.com"));

        assert!(super::parse_email_domains("").expect("空串不限制").is_empty());
        assert!(super::parse_email_domains(",").is_err());
        assert!(super::parse_email_domains("not a domain").is_err());
        assert!(super::parse_email_domains("@example.com").is_err());
        assert!(super::parse_email_domains("-example.com").is_err());
        assert!(super::parse_email_domains("localhost").is_err());
    }

    #[test]
    fn policy_manifest_file_parses_and_validates() {
        let dir = tempfile::tempdir().expect("临时目录必须可创建");
        let path = dir.path().join("policies.toml");
        std::fs::write(
            &path,
            r#"
[[policies]]
slug = "privacy"
version = "2026-11-01"
effective_at = "2026-11-01T00:00:00Z"

[[policies.documents]]
locale = "zh-CN"
title = "隐私政策"
path = "/policies/privacy/2026-11-01/zh-CN"

[[policies.documents]]
locale = "en"
title = "Privacy Policy"
path = "/policies/privacy/2026-11-01/en"
"#,
        )
        .expect("政策 manifest 必须可写");

        let catalog = super::load_policies(path.to_str().expect("路径必须是 UTF-8"))
            .expect("manifest 必须可解析");
        assert_eq!(catalog.policies().len(), 1);
        assert_eq!(catalog.policies()[0].slug, "privacy");
        assert!(
            catalog
                .current(
                    "privacy",
                    chrono::DateTime::parse_from_rfc3339("2026-11-02T00:00:00Z")
                        .expect("固定字面量必须可解析")
                        .with_timezone(&chrono::Utc)
                )
                .is_some()
        );

        std::fs::write(
            &path,
            r#"
[[policies]]
slug = "privacy"
version = "2026-11-01"
effective_at = "2026-11-01T00:00:00Z"

[[policies.documents]]
locale = "zh-CN"
title = "隐私政策"
path = "/policies/privacy/2026-11-01/zh-CN"
"#,
        )
        .expect("缺语言 manifest 必须可写");
        assert!(matches!(
            super::load_policies(path.to_str().expect("路径必须是 UTF-8")),
            Err(super::LoadConfigError::Invalid(_))
        ));
    }

    #[test]
    fn oauth_clients_file_parses_and_rejects_duplicates() {
        let dir = tempfile::tempdir().expect("临时目录必须可创建");
        let path = dir.path().join("clients.toml");
        std::fs::write(
            &path,
            r#"
[[clients]]
client_id = "client-1"
name = "Example"
redirect_uri = "https://app.example/cb"
secret = "secret"
"#,
        )
        .expect("客户端配置必须可写");

        let clients = super::load_oauth_clients(path.to_str().expect("路径必须是 UTF-8"))
            .expect("客户端配置必须可解析");
        assert_eq!(clients.len(), 1);
        assert_eq!(clients[0].client_id, "client-1");
        assert!(clients[0].enabled);

        std::fs::write(
            &path,
            r#"
[[clients]]
client_id = "client-1"
name = "One"
redirect_uri = "https://one.example/cb"
secret = "secret"
[[clients]]
client_id = "client-1"
name = "Two"
redirect_uri = "https://two.example/cb"
secret = "secret"
"#,
        )
        .expect("重复客户端配置必须可写");
        assert!(matches!(
            super::load_oauth_clients(path.to_str().expect("路径必须是 UTF-8")),
            Err(super::LoadConfigError::Invalid(_))
        ));
    }

    #[test]
    fn social_redirect_uri_derives_from_base_url() {
        let social = super::SocialConfig {
            base_url: "https://accounts.nebula.st/".to_owned(),
            google_client_id: "g".to_owned(),
            google_client_secret: String::new(),
            google_proxy_url: "https://proxy.example".to_owned(),
            google_proxy_access_client_id: "id".to_owned(),
            google_proxy_access_client_secret: "secret".to_owned(),
            google_worker_signing_public_key: "pem".to_owned(),
            microsoft_client_id: String::new(),
            microsoft_client_secret: String::new(),
        };
        assert_eq!(
            social.redirect_uri("google"),
            "https://accounts.nebula.st/api/auth/google/callback"
        );
        assert!(social.google_enabled());
        assert!(!social.microsoft_enabled());
    }
}
