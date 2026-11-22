//! 邮件端口测试：日志实现、错误显示与 SMTP 发送前的地址校验。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use nebula_server::mailer::{EmailMessage, LogMailer, MailError, Mailer, SmtpMailer};

fn message(to: &str) -> EmailMessage {
    EmailMessage { to: to.to_owned(), subject: "Subject".to_owned(), body: "Body".to_owned() }
}

#[tokio::test]
async fn log_mailer_accepts_message() {
    LogMailer.send(message("nova@nebula.st")).await.expect("日志邮件必须成功");
}

#[test]
fn mail_error_exposes_display_and_source() {
    let error = MailError::wrap(std::io::Error::other("boom"));
    assert_eq!(error.to_string(), "邮件发送失败: boom");
    assert!(std::error::Error::source(&error).is_some());
}

#[tokio::test]
async fn smtp_mailer_rejects_invalid_recipient_before_network() {
    let mailer =
        SmtpMailer::connect("localhost", 25, "none", "", "", "Nebula <no-reply@nebula.st>")
            .expect("SMTP 配置必须可构建");

    let error = mailer.send(message("not-an-email")).await.expect_err("非法收件人必须失败");

    assert!(error.to_string().contains("邮件发送失败"));
}
