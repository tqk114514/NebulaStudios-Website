//! 邮件端口：trait 与 SMTP / 日志两种实现（§1：lettre 异步传输绑定当前 tokio
//! runtime，须在 runtime 内创建）。

use std::error::Error as StdError;
use std::fmt;
use std::future::Future;
use std::pin::Pin;

use lettre::message::Mailbox;
use lettre::transport::smtp::authentication::Credentials;
use lettre::{AsyncSmtpTransport, AsyncTransport, Message, Tokio1Executor};

/// 待发送邮件；纯文本，模板与多语言随后续切片引入。
#[derive(Debug, Clone)]
pub struct EmailMessage {
    /// 收件人地址（`名字 <addr>` 或裸地址）。
    pub to: String,
    /// 主题。
    pub subject: String,
    /// 正文。
    pub body: String,
}

/// 邮件发送失败；底层错误不透明封装。
#[derive(Debug)]
pub struct MailError(Box<dyn StdError + Send + Sync>);

impl MailError {
    /// 封装任意底层错误。
    pub fn wrap<E>(source: E) -> Self
    where
        E: StdError + Send + Sync + 'static,
    {
        Self(Box::new(source))
    }
}

impl fmt::Display for MailError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "邮件发送失败: {}", self.0)
    }
}

impl StdError for MailError {
    fn source(&self) -> Option<&(dyn StdError + 'static)> {
        Some(self.0.as_ref())
    }
}

/// 邮件端口；handler 只见本 trait。装箱 future 以支持 trait 对象（运行时切换
/// SMTP / 日志实现；仓储层同样经 `async_trait` 装箱）。
pub trait Mailer: Send + Sync {
    /// 发送一封邮件。
    ///
    /// # Errors
    /// 构造或投递失败时返回 [`MailError`]。
    fn send(
        &self,
        message: EmailMessage,
    ) -> Pin<Box<dyn Future<Output = Result<(), MailError>> + Send + '_>>;
}

/// SMTP 实现（lettre 0.11，rustls TLS，STARTTLS 587 端口惯例）。
#[derive(Debug)]
pub struct SmtpMailer {
    transport: AsyncSmtpTransport<Tokio1Executor>,
    from: Mailbox,
}

impl SmtpMailer {
    /// 连接 SMTP 中继；用户名/密码为空串时不带凭据。
    ///
    /// # Errors
    /// 中继地址或发件人格式非法、TLS 协商失败时返回 [`MailError`]。
    pub fn connect(
        host: &str,
        port: u16,
        username: &str,
        password: &str,
        from: &str,
    ) -> Result<Self, MailError> {
        let mut builder =
            AsyncSmtpTransport::<Tokio1Executor>::relay(host).map_err(MailError::wrap)?.port(port);
        if !username.is_empty() {
            builder =
                builder.credentials(Credentials::new(username.to_owned(), password.to_owned()));
        }
        let from = from.parse().map_err(MailError::wrap)?;
        Ok(Self { transport: builder.build(), from })
    }
}

impl Mailer for SmtpMailer {
    fn send(
        &self,
        message: EmailMessage,
    ) -> Pin<Box<dyn Future<Output = Result<(), MailError>> + Send + '_>> {
        Box::pin(async move {
            let to: Mailbox = message.to.parse().map_err(MailError::wrap)?;
            let letter = Message::builder()
                .from(self.from.clone())
                .to(to)
                .subject(&message.subject)
                .body(message.body)
                .map_err(MailError::wrap)?;
            self.transport.send(letter).await.map_err(MailError::wrap)?;
            Ok(())
        })
    }
}

/// 开发用：不发送，只记日志；配置未给 SMTP 主机时启用。
#[derive(Debug, Clone, Copy, Default)]
pub struct LogMailer;

impl Mailer for LogMailer {
    fn send(
        &self,
        message: EmailMessage,
    ) -> Pin<Box<dyn Future<Output = Result<(), MailError>> + Send + '_>> {
        Box::pin(async move {
            tracing::info!(to = %message.to, subject = %message.subject, body = %message.body, "邮件（日志模式）");
            Ok(())
        })
    }
}
