//! SMTP 真实 TCP 路径测试：进程内最小 SMTP 服务器覆盖 [`SmtpMailer`] 的连接与投递。
//!
//! 服务器只实现 lettre 投递路径用到的命令；监听任务在测试结束随 [`TestSmtpServer`]
//! 析构而 abort。每个用例自建服务器，避免进程内状态共享。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::sync::{Arc, Mutex};
use std::time::Duration;

use nebula_server::mailer::{EmailMessage, MailError, Mailer, SmtpMailer};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::tcp::{OwnedReadHalf, OwnedWriteHalf};
use tokio::net::{TcpListener, TcpStream};
use tokio::task::JoinHandle;

/// 单次 send 的上限；超出即视为挂死。
const SEND_TIMEOUT: Duration = Duration::from_secs(10);
/// 固定发件人，与生产配置格式一致（带显示名）。
const SENDER: &str = "Nebula <no-reply@nebula.st>";

/// 服务器捕获的客户端行为。
#[derive(Debug, Default)]
struct Captured {
    /// 命令序列（含 AUTH LOGIN 的续行），保持原始大小写。
    commands: Vec<String>,
    /// 每封 DATA 的整段内容。
    data: Vec<String>,
}

/// 进程内最小 SMTP 服务器；析构时 abort 监听任务。
struct TestSmtpServer {
    port: u16,
    captured: Arc<Mutex<Captured>>,
    accept_task: JoinHandle<()>,
}

impl TestSmtpServer {
    /// 绑定 127.0.0.1:0 并启动监听；`reject_rcpt` 为真时 RCPT TO 回 550。
    async fn start(reject_rcpt: bool) -> Self {
        let listener = TcpListener::bind("127.0.0.1:0").await.expect("测试 SMTP 服务器必须能绑定");
        let port = listener.local_addr().expect("必须能取到本地端口").port();
        let captured = Arc::new(Mutex::new(Captured::default()));
        let captured_for_accept = Arc::clone(&captured);
        let accept_task = tokio::spawn(async move {
            loop {
                let Ok((stream, _)) = listener.accept().await else {
                    return;
                };
                let captured = Arc::clone(&captured_for_accept);
                tokio::spawn(handle_connection(stream, captured, reject_rcpt));
            }
        });
        Self { port, captured, accept_task }
    }

    fn commands(&self) -> Vec<String> {
        self.captured.lock().unwrap().commands.clone()
    }

    fn data(&self) -> Vec<String> {
        self.captured.lock().unwrap().data.clone()
    }
}

impl Drop for TestSmtpServer {
    fn drop(&mut self) {
        self.accept_task.abort();
    }
}

/// 逐行读取并以 CRLF 之外的空白结尾裁剪；EOF 或 I/O 错误返回 `None`。
async fn read_line(reader: &mut BufReader<OwnedReadHalf>) -> Option<String> {
    let mut line = String::new();
    match reader.read_line(&mut line).await {
        Ok(0) | Err(_) => None,
        Ok(_) => Some(line.trim_end_matches(['\r', '\n']).to_owned()),
    }
}

/// 写一条响应；连接已断开时返回 false。
async fn write_response(writer: &mut OwnedWriteHalf, response: &str) -> bool {
    writer.write_all(response.as_bytes()).await.is_ok()
}

/// EHLO 多行能力；lettre 从 `AUTH` 行选择机制。
const EHLO_RESPONSE: &str =
    "250-localhost\r\n250-SIZE 10485760\r\n250-AUTH PLAIN LOGIN\r\n250 8BITMIME\r\n";

async fn handle_connection(stream: TcpStream, captured: Arc<Mutex<Captured>>, reject_rcpt: bool) {
    let (read_half, mut writer) = stream.into_split();
    let mut reader = BufReader::new(read_half);
    if !write_response(&mut writer, "220 localhost ESMTP\r\n").await {
        return;
    }
    loop {
        let Some(command) = read_line(&mut reader).await else {
            return;
        };
        if command.is_empty() {
            continue;
        }
        captured.lock().unwrap().commands.push(command.clone());
        if !respond(&command, &mut reader, &mut writer, &captured, reject_rcpt).await {
            return;
        }
    }
}

/// 处理一条命令；返回 false 表示关闭连接。
async fn respond(
    command: &str,
    reader: &mut BufReader<OwnedReadHalf>,
    writer: &mut OwnedWriteHalf,
    captured: &Mutex<Captured>,
    reject_rcpt: bool,
) -> bool {
    let (verb, rest) = command.split_once(' ').unwrap_or((command, ""));
    match verb.to_ascii_uppercase().as_str() {
        "EHLO" => write_response(writer, EHLO_RESPONSE).await,
        "HELO" => write_response(writer, "250 localhost\r\n").await,
        "MAIL" => write_response(writer, "250 2.1.0 Ok\r\n").await,
        "RCPT" => {
            let response =
                if reject_rcpt { "550 5.1.1 No such user\r\n" } else { "250 2.1.5 Ok\r\n" };
            write_response(writer, response).await
        }
        "DATA" => receive_data(reader, writer, captured).await,
        "AUTH" => receive_auth(rest, reader, writer, captured).await,
        "QUIT" => {
            let _ = write_response(writer, "221 2.0.0 Bye\r\n").await;
            false
        }
        "RSET" | "NOOP" => write_response(writer, "250 2.0.0 Ok\r\n").await,
        _ => write_response(writer, "500 5.5.2 Unrecognized command\r\n").await,
    }
}

/// 354 之后读到单独一行 "." 为止；整段内容按 CRLF 还原后入库。
async fn receive_data(
    reader: &mut BufReader<OwnedReadHalf>,
    writer: &mut OwnedWriteHalf,
    captured: &Mutex<Captured>,
) -> bool {
    if !write_response(writer, "354 End data with <CR><LF>.<CR><LF>\r\n").await {
        return false;
    }
    let mut lines = Vec::new();
    loop {
        let Some(line) = read_line(reader).await else {
            return false;
        };
        if line == "." {
            break;
        }
        lines.push(line);
    }
    captured.lock().unwrap().data.push(lines.join("\r\n"));
    write_response(writer, "250 2.0.0 Ok: queued\r\n").await
}

/// 同时支持 PLAIN（initial response 或空挑战）与 LOGIN 的两步挑战。
async fn receive_auth(
    rest: &str,
    reader: &mut BufReader<OwnedReadHalf>,
    writer: &mut OwnedWriteHalf,
    captured: &Mutex<Captured>,
) -> bool {
    let mut parts = rest.split_whitespace();
    let mechanism = parts.next().unwrap_or_default().to_ascii_uppercase();
    match mechanism.as_str() {
        "PLAIN" => {
            // 无 initial response 时 lettre 需要空挑战再发凭据。
            if parts.next().is_none() {
                if !write_response(writer, "334 \r\n").await {
                    return false;
                }
                let Some(line) = read_line(reader).await else {
                    return false;
                };
                captured.lock().unwrap().commands.push(line);
            }
            write_response(writer, "235 2.7.0 Authentication successful\r\n").await
        }
        "LOGIN" => {
            if !write_response(writer, "334 VXNlcm5hbWU6\r\n").await {
                return false;
            }
            let Some(username) = read_line(reader).await else {
                return false;
            };
            captured.lock().unwrap().commands.push(username);
            if !write_response(writer, "334 UGFzc3dvcmQ6\r\n").await {
                return false;
            }
            let Some(password) = read_line(reader).await else {
                return false;
            };
            captured.lock().unwrap().commands.push(password);
            write_response(writer, "235 2.7.0 Authentication successful\r\n").await
        }
        _ => write_response(writer, "504 5.5.4 Unrecognized authentication type\r\n").await,
    }
}

/// 构造消息并发送，10 秒未完成视为挂死。
async fn send(mailer: &SmtpMailer, to: &str, subject: &str, body: &str) -> Result<(), MailError> {
    let message =
        EmailMessage { to: to.to_owned(), subject: subject.to_owned(), body: body.to_owned() };
    tokio::time::timeout(SEND_TIMEOUT, mailer.send(message)).await.expect("SMTP 发送不得挂死")
}

#[tokio::test]
async fn smtp_send_delivers_message_over_tcp() {
    // given：在线服务器与无凭据的明文 SMTP 通道
    let server = TestSmtpServer::start(false).await;
    let mailer = SmtpMailer::connect("127.0.0.1", server.port, "none", "", "", SENDER)
        .expect("SMTP 配置必须可构建");

    // when
    send(&mailer, "nova@nebula.st", "Nebula SMTP test", "Hello from the SMTP integration test.")
        .await
        .expect("投递必须成功");

    // then：命令序列覆盖信封，DATA 覆盖头与正文
    let commands = server.commands();
    assert!(
        commands.iter().any(|command| command.to_ascii_uppercase().starts_with("MAIL FROM:")),
        "命令序列缺少 MAIL FROM: {commands:?}"
    );
    assert!(
        commands.iter().any(|command| command.to_ascii_uppercase().starts_with("RCPT TO:")),
        "命令序列缺少 RCPT TO: {commands:?}"
    );
    let data = server.data();
    assert_eq!(data.len(), 1, "应恰好捕获一封 DATA: {data:?}");
    assert!(data[0].contains("From: Nebula <no-reply@nebula.st>"), "DATA 缺少 From: {}", data[0]);
    assert!(data[0].contains("To: nova@nebula.st"), "DATA 缺少 To: {}", data[0]);
    assert!(data[0].contains("Subject: Nebula SMTP test"), "DATA 缺少 Subject: {}", data[0]);
    assert!(
        data[0].contains("Hello from the SMTP integration test."),
        "DATA 缺少正文: {}",
        data[0]
    );
}

#[tokio::test]
async fn smtp_transport_with_credentials_authenticates_over_plaintext() {
    // given：服务器声明 AUTH PLAIN LOGIN，客户端带凭据走 builder_dangerous 明文通道
    let server = TestSmtpServer::start(false).await;
    let mailer = SmtpMailer::connect("127.0.0.1", server.port, "none", "user", "pass", SENDER)
        .expect("带凭据的 SMTP 配置必须可构建");

    // when
    let result =
        send(&mailer, "nova@nebula.st", "Nebula credentials test", "Credentialed body.").await;

    // then：lettre 不在明文通道上拒绝 AUTH，投递成功且确实发过 AUTH 命令
    result.expect("明文通道上带凭据投递必须成功");
    let commands = server.commands();
    assert!(
        commands.iter().any(|command| command.to_ascii_uppercase().starts_with("AUTH ")),
        "命令序列缺少 AUTH: {commands:?}"
    );
}

#[tokio::test]
async fn smtp_send_fails_when_recipient_rejected() {
    // given：RCPT TO 一律 550
    let server = TestSmtpServer::start(true).await;
    let mailer = SmtpMailer::connect("127.0.0.1", server.port, "none", "", "", SENDER)
        .expect("SMTP 配置必须可构建");

    // when
    let result = send(&mailer, "nova@nebula.st", "Rejected", "Body.").await;

    // then
    let error = result.expect_err("RCPT 被拒时投递必须失败");
    assert!(error.to_string().contains("邮件发送失败"), "错误应来自邮件端口: {error}");
}

#[tokio::test]
async fn smtp_send_fails_when_connection_refused() {
    // given：无人监听的端口；connect 只建配置，不做 I/O
    let mailer =
        SmtpMailer::connect("127.0.0.1", 1, "none", "", "", SENDER).expect("SMTP 配置必须可构建");

    // when
    let result = send(&mailer, "nova@nebula.st", "Refused", "Body.").await;

    // then
    result.expect_err("连接被拒时投递必须失败");
}

#[tokio::test]
async fn invalid_recipient_fails_before_connecting() {
    // given：在线服务器与非法收件人地址
    let server = TestSmtpServer::start(false).await;
    let mailer = SmtpMailer::connect("127.0.0.1", server.port, "none", "", "", SENDER)
        .expect("SMTP 配置必须可构建");

    // when
    let result = send(&mailer, "not-an-address", "Subject", "Body.").await;

    // then：地址解析在联网前失败，服务器未收到任何命令
    result.expect_err("非法收件人必须失败");
    assert!(server.commands().is_empty(), "联网前失败不应产生命令: {:?}", server.commands());
}

#[test]
fn connect_rejects_invalid_sender() {
    // given/when
    let result = SmtpMailer::connect("127.0.0.1", 25, "none", "", "", "not-an-address");

    // then
    let error = result.expect_err("非法发件人必须失败");
    assert!(error.to_string().contains("邮件发送失败"), "错误应来自邮件端口: {error}");
}
