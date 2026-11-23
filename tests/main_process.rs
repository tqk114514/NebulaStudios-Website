//! 真实子进程启动测试：覆盖 `main.rs` 与 `telemetry.rs` 的装配路径。
//!
//! 每个用例用独立临时目录（库、JWT PEM、头像目录），配置只经 `Command::env`
//! 传给子进程。stdout/stderr 由读线程持续消费，避免管道写满阻塞子进程；
//! 就绪以日志中的 `listening` 行（实际监听地址）为信号。

// 断言失败即 panic 是测试的正确失败语义；unwrap/expect 禁令面向生产路径
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use std::ffi::OsStr;
use std::io::{BufRead, BufReader, Read};
use std::net::SocketAddr;
use std::path::Path;
use std::process::{Child, Command, ExitStatus, Stdio};
use std::sync::{Arc, Mutex, mpsc};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use base64::Engine as _;
use ed25519_dalek::pkcs8::EncodePublicKey as _;

/// 测试密钥：64 个十六进制字符即 32 字节，满足 AES-256 与 HMAC 的长度要求。
const TEST_KEY_A: &str = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";
/// 验证码 pepper，要求同上。
const TEST_KEY_B: &str = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100";

/// 启动就绪上限；完整装配含 Argon2 哈希与建表，留出余量。
const STARTUP_TIMEOUT: Duration = Duration::from_secs(10);

/// `try_wait` 轮询间隔。
const POLL_INTERVAL: Duration = Duration::from_millis(25);

/// 输出来源；stderr 单独留存，供「错误配置写到 stderr」的断言使用。
#[derive(Clone, Copy)]
enum Stream {
    /// 标准输出，`tracing` 默认写入这里。
    Stdout,
    /// 标准错误，`anyhow` 的 `Error: …` 写在这里。
    Stderr,
}

/// 子进程输出行快照；读线程逐行追加，断言时读取。
#[derive(Clone, Default)]
struct LogCapture {
    all: Arc<Mutex<Vec<String>>>,
    stderr: Arc<Mutex<Vec<String>>>,
}

impl LogCapture {
    /// 记录一行并标明来源流。
    fn push(&self, line: &str, origin: Stream) {
        self.all.lock().expect("日志锁不应中毒").push(line.to_owned());
        if matches!(origin, Stream::Stderr) {
            self.stderr.lock().expect("日志锁不应中毒").push(line.to_owned());
        }
    }

    /// 全部日志行（含 ANSI 码）；超时诊断用。
    fn dump(&self) -> String {
        self.all.lock().expect("日志锁不应中毒").join("\n")
    }

    /// 仅 stderr 行。
    fn stderr_text(&self) -> String {
        self.stderr.lock().expect("日志锁不应中毒").join("\n")
    }
}

/// 子进程守卫：Drop 时若仍在运行则 kill + wait，并 join 读线程，避免泄漏。
struct ChildGuard {
    child: Child,
    readers: Vec<JoinHandle<()>>,
}

impl ChildGuard {
    /// 启动二进制并接管两条输出管道；返回守卫、日志快照与转发通道。
    fn spawn(command: &mut Command) -> (Self, LogCapture, mpsc::Receiver<String>) {
        let mut child = command.spawn().expect("服务器二进制必须可启动");
        let stdout = child.stdout.take().expect("stdout 必须是管道");
        let stderr = child.stderr.take().expect("stderr 必须是管道");
        let capture = LogCapture::default();
        let (tx, rx) = mpsc::channel();
        let readers = vec![
            spawn_reader(stdout, tx.clone(), capture.clone(), Stream::Stdout),
            spawn_reader(stderr, tx, capture.clone(), Stream::Stderr),
        ];
        (Self { child, readers }, capture, rx)
    }

    /// 子进程 id，用于向它发送信号。
    #[cfg(unix)]
    fn id(&self) -> u32 {
        self.child.id()
    }

    /// 在 `timeout` 内等待退出并排空管道；仍在运行返回 `None`。
    fn wait_exit(&mut self, timeout: Duration) -> Option<ExitStatus> {
        let deadline = Instant::now() + timeout;
        loop {
            match self.child.try_wait().expect("查询子进程状态失败") {
                Some(status) => {
                    // 进程退出后管道关闭；join 保证尾部输出已进入快照
                    self.join_readers();
                    return Some(status);
                }
                None if Instant::now() >= deadline => return None,
                None => thread::sleep(POLL_INTERVAL),
            }
        }
    }

    /// 等读线程读到 EOF；子进程及其管道关闭后才会返回。
    fn join_readers(&mut self) {
        for reader in self.readers.drain(..) {
            let _ = reader.join();
        }
    }
}

impl Drop for ChildGuard {
    fn drop(&mut self) {
        // 已退出则只回收；其余情况（仍在运行或状态查询失败）一律强杀后再回收
        if !matches!(self.child.try_wait(), Ok(Some(_))) {
            let _ = self.child.kill();
            let _ = self.child.wait();
        }
        self.join_readers();
    }
}

/// 起线程逐行读管道：转发到通道，同时写入日志快照。
fn spawn_reader<R: Read + Send + 'static>(
    stream: R,
    tx: mpsc::Sender<String>,
    capture: LogCapture,
    origin: Stream,
) -> JoinHandle<()> {
    thread::spawn(move || {
        for line in BufReader::new(stream).lines() {
            let Ok(line) = line else { break };
            // 先入库再转发：等待方拿到行时，快照里必定已有该行
            capture.push(&line, origin);
            let _ = tx.send(line);
        }
    })
}

/// 等待 `listening` 日志行并返回实际监听地址；超时或提前退出时打印已捕获日志。
fn await_listening(rx: &mpsc::Receiver<String>, capture: &LogCapture) -> SocketAddr {
    let deadline = Instant::now() + STARTUP_TIMEOUT;
    loop {
        let remaining = deadline.saturating_duration_since(Instant::now());
        match rx.recv_timeout(remaining) {
            Ok(line) => {
                if let Some(addr) = parse_listening(&line) {
                    return addr;
                }
            }
            Err(mpsc::RecvTimeoutError::Timeout | mpsc::RecvTimeoutError::Disconnected) => {
                panic!(
                    "子进程未在 {} 秒内输出 listening；已捕获日志:\n{}",
                    STARTUP_TIMEOUT.as_secs(),
                    capture.dump()
                );
            }
        }
    }
}

/// 从一行日志解析 `bind=` 后的地址；先去掉 ANSI 转义，再只取地址字符。
fn parse_listening(line: &str) -> Option<SocketAddr> {
    let plain = strip_ansi(line);
    if !plain.contains("listening") {
        return None;
    }
    let (_, tail) = plain.split_once("bind=")?;
    let candidate: String =
        tail.chars().take_while(|ch| ch.is_ascii_digit() || matches!(ch, '.' | ':')).collect();
    candidate.parse().ok()
}

/// 去掉 ANSI 转义序列；`tracing` 在管道下仍输出颜色码，字段名与 `=` 之间也夹着转义。
fn strip_ansi(text: &str) -> String {
    let mut plain = String::with_capacity(text.len());
    let mut chars = text.chars();
    while let Some(ch) = chars.next() {
        if ch == '\u{1b}' {
            // CSI 由 ESC '[' 引入，参数之后是 0x40..=0x7e 的终止字节；
            // '[' 自身也落在该区间，须先消费掉再扫描终止字节
            if let Some('[') = chars.next() {
                for next in chars.by_ref() {
                    if ('@'..='~').contains(&next) {
                        break;
                    }
                }
            }
        } else {
            plain.push(ch);
        }
    }
    plain
}

/// 写入一个配置项；名字即文档约定的 `NEBULA_<KEY>`。
fn set_config(command: &mut Command, key: &str, value: impl AsRef<OsStr>) {
    command.env(format!("NEBULA_{key}"), value.as_ref());
}

/// 构造启动必需项齐全的命令；临时目录中的库与 PEM 文件在此写入。
fn base_command(dir: &Path) -> Command {
    let private_key = dir.join("jwt-private.pem");
    let public_key = dir.join("jwt-public.pem");
    std::fs::write(&private_key, common::TEST_PRIVATE_PEM).expect("JWT 私钥必须可写");
    std::fs::write(&public_key, common::TEST_PUBLIC_PEM).expect("JWT 公钥必须可写");

    let mut command = Command::new(env!("CARGO_BIN_EXE_nebula-server"));
    command.stdin(Stdio::null()).stdout(Stdio::piped()).stderr(Stdio::piped());
    set_config(&mut command, "BIND", "127.0.0.1:0");
    set_config(&mut command, "DATABASE__PATH", dir.join("nebula.db"));
    set_config(&mut command, "JWT__PRIVATE_KEY_PATH", &private_key);
    set_config(&mut command, "JWT__PUBLIC_KEY_PATH", &public_key);
    set_config(&mut command, "CRYPTO__TOTP_SECRET_KEY", TEST_KEY_A);
    set_config(&mut command, "CRYPTO__CODE_PEPPER", TEST_KEY_B);
    set_config(&mut command, "AVATAR__DIR", dir.join("avatars"));
    set_config(&mut command, "JOBS__CLEANUP_INTERVAL_SECS", "1");
    command.env("RUST_LOG", "info");
    command
}

/// 生成 PKIX 公钥 PEM，供完整装配的 Google 客户端配置使用；本用例不校验签名。
fn google_signing_public_key_pem() -> String {
    let signing = ed25519_dalek::SigningKey::from_bytes(&[7u8; 32]);
    let der = signing.verifying_key().to_public_key_der().expect("Ed25519 公钥 DER 必须可编码");
    let encoded = base64::engine::general_purpose::STANDARD.encode(der.as_bytes());
    let mut body = String::new();
    for chunk in encoded.as_bytes().chunks(64) {
        body.push_str(std::str::from_utf8(chunk).expect("base64 是 ASCII"));
        body.push('\n');
    }
    format!("-----BEGIN PUBLIC KEY-----\n{body}-----END PUBLIC KEY-----\n")
}

#[tokio::test]
async fn minimal_config_serves_healthz_and_metrics() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let mut command = base_command(dir.path());
    let (_guard, logs, rx) = ChildGuard::spawn(&mut command);
    let addr = await_listening(&rx, &logs);
    let base = format!("http://{addr}");
    let client = reqwest::Client::new();

    // NEBULA_ 前缀确实生效：库与头像目录落在本次临时目录，而非 cwd 下默认路径
    assert!(dir.path().join("nebula.db").exists(), "NEBULA_DATABASE__PATH 未生效");
    assert!(dir.path().join("avatars").is_dir(), "NEBULA_AVATAR__DIR 未生效");

    // given: 仅配置启动必需项；when: 请求存活探针；then: 200 且空体
    let response =
        client.get(format!("{base}/healthz")).send().await.expect("healthz 请求必须成功");
    assert_eq!(response.status().as_u16(), 200, "healthz 必须返回 200");
    assert_eq!(response.text().await.expect("读体必须成功"), "", "healthz 响应体必须为空");

    // 先由 /healthz 让指标层记录一次，再读导出端点：自身请求尚未进入快照
    let response =
        client.get(format!("{base}/metrics")).send().await.expect("metrics 请求必须成功");
    assert_eq!(response.status().as_u16(), 200, "metrics 必须返回 200");
    let metrics = response.text().await.expect("读体必须成功");
    assert!(!metrics.is_empty(), "metrics 响应体不得为空");
    assert!(
        metrics.contains("axum_http_requests_total"),
        "metrics 缺少真实指标名 axum_http_requests_total:\n{metrics}"
    );

    assert!(logs.dump().contains("listening"), "启动日志必须包含 listening:\n{}", logs.dump());
}

#[tokio::test]
async fn full_config_serves_policies_and_cors_preflight() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let clients_file = dir.path().join("clients.toml");
    std::fs::write(
        &clients_file,
        r#"
[[clients]]
client_id = "client-1"
name = "Example"
redirect_uri = "https://app.example/cb"
secret = "client-secret"
"#,
    )
    .expect("OAuth 客户端配置必须可写");
    let manifest_file = dir.path().join("policies.toml");
    std::fs::write(
        &manifest_file,
        r#"
[[policies]]
slug = "privacy"
version = "2026-01-01"
effective_at = "2026-01-01T00:00:00Z"

[[policies.documents]]
locale = "zh-CN"
title = "隐私政策"
path = "/policies/privacy/2026-01-01/zh-CN"

[[policies.documents]]
locale = "en"
title = "Privacy Policy"
path = "/policies/privacy/2026-01-01/en"
"#,
    )
    .expect("政策 manifest 必须可写");

    let mut command = base_command(dir.path());
    set_config(&mut command, "CORS__ALLOWED_ORIGIN", "http://localhost:3000");
    set_config(&mut command, "ADMIN__EMAILS", "admin@nebula.st");
    set_config(&mut command, "REGISTRATION__ALLOWED_EMAIL_DOMAINS", "nebula.st");
    set_config(&mut command, "OAUTH__CLIENTS_FILE", &clients_file);
    set_config(&mut command, "POLICY__MANIFEST_FILE", &manifest_file);
    set_config(&mut command, "TURNSTILE__ENABLED", "true");
    set_config(&mut command, "TURNSTILE__SITE_KEY", "turnstile-site-key");
    set_config(&mut command, "TURNSTILE__SECRET_KEY", "turnstile-secret-key");
    set_config(
        &mut command,
        "TURNSTILE__VERIFY_URL",
        "https://challenges.cloudflare.com/turnstile/v0/siteverify",
    );
    set_config(&mut command, "SOCIAL__BASE_URL", "https://accounts.nebula.test");
    set_config(&mut command, "SOCIAL__GOOGLE_CLIENT_ID", "google-client-id");
    set_config(&mut command, "SOCIAL__GOOGLE_CLIENT_SECRET", "google-client-secret");
    set_config(&mut command, "SOCIAL__GOOGLE_PROXY_URL", "https://proxy.example");
    set_config(&mut command, "SOCIAL__GOOGLE_PROXY_ACCESS_CLIENT_ID", "cf-access-id");
    set_config(&mut command, "SOCIAL__GOOGLE_PROXY_ACCESS_CLIENT_SECRET", "cf-access-secret");
    set_config(
        &mut command,
        "SOCIAL__GOOGLE_WORKER_SIGNING_PUBLIC_KEY",
        google_signing_public_key_pem(),
    );
    set_config(&mut command, "SOCIAL__MICROSOFT_CLIENT_ID", "microsoft-client-id");
    set_config(&mut command, "SOCIAL__MICROSOFT_CLIENT_SECRET", "microsoft-client-secret");

    let (_guard, logs, rx) = ChildGuard::spawn(&mut command);
    let addr = await_listening(&rx, &logs);
    let base = format!("http://{addr}");
    let client = reqwest::Client::new();

    // 政策 manifest 已加载：响应含 manifest 中的 slug
    let response =
        client.get(format!("{base}/api/policies")).send().await.expect("policies 请求必须成功");
    assert_eq!(response.status().as_u16(), 200, "政策列表必须返回 200");
    let body = response.text().await.expect("读体必须成功");
    assert!(body.contains("privacy"), "政策响应缺少 manifest 中的 slug privacy:\n{body}");

    // CORS 已按配置装配：预检成功并回显 Origin
    let response = client
        .request(reqwest::Method::OPTIONS, format!("{base}/api/auth/login"))
        .header("origin", "http://localhost:3000")
        .header("access-control-request-method", "POST")
        .send()
        .await
        .expect("预检请求必须成功");
    assert!(response.status().is_success(), "CORS 预检必须成功，实际 {}", response.status());
    assert_eq!(
        response.headers().get("access-control-allow-origin").and_then(|value| value.to_str().ok()),
        Some("http://localhost:3000"),
        "预检必须回显配置的 Origin"
    );
}

#[tokio::test]
async fn missing_totp_key_exits_with_stderr_message() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let mut command = base_command(dir.path());
    command.env_remove("NEBULA_CRYPTO__TOTP_SECRET_KEY");

    let (mut guard, logs, _rx) = ChildGuard::spawn(&mut command);
    let status = guard.wait_exit(STARTUP_TIMEOUT).expect("缺少 TOTP 密钥时必须及时退出");

    assert!(!status.success(), "缺少 TOTP 密钥时必须以非零码退出");
    assert!(
        logs.stderr_text().contains("NEBULA_CRYPTO__TOTP_SECRET_KEY"),
        "stderr 必须指出缺失的配置项，实际:\n{}",
        logs.stderr_text()
    );
}

#[tokio::test]
async fn occupied_port_exits_after_full_assembly() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let clients_file = dir.path().join("clients.toml");
    std::fs::write(
        &clients_file,
        r#"
[[clients]]
client_id = "client-1"
name = "Example"
redirect_uri = "https://app.example/cb"
secret = "client-secret"
"#,
    )
    .expect("OAuth 客户端配置必须可写");
    // 占住端口，让子进程走完装配后停在 bind；错误退出会正常刷新覆盖率 profile
    let occupied = std::net::TcpListener::bind("127.0.0.1:0").expect("占位端口必须可绑定");
    let addr = occupied.local_addr().expect("占位地址必须可读");
    let mut command = base_command(dir.path());
    set_config(&mut command, "BIND", addr.to_string());
    set_config(&mut command, "OAUTH__CLIENTS_FILE", &clients_file);

    let (mut guard, logs, _rx) = ChildGuard::spawn(&mut command);
    let status = guard.wait_exit(STARTUP_TIMEOUT).expect("端口占用时必须及时退出");

    assert!(!status.success(), "端口占用时必须以非零码退出");
    assert!(
        logs.stderr_text().contains("监听"),
        "stderr 必须指出监听失败，实际:\n{}",
        logs.stderr_text()
    );

    // seed 在 bind 之前完成：库中必须已有配置的客户端
    let pool = sqlx::SqlitePool::connect_with(
        sqlx::sqlite::SqliteConnectOptions::new()
            .filename(dir.path().join("nebula.db"))
            .read_only(true),
    )
    .await
    .expect("退出后的库必须可读");
    let seeded: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM oauth_clients")
        .fetch_one(&pool)
        .await
        .expect("客户端计数必须可查询");
    assert_eq!(seeded, 1, "配置的 OAuth 客户端必须在 bind 前入库");
}

#[tokio::test]
async fn malformed_totp_key_exits_with_error() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let mut command = base_command(dir.path());
    set_config(&mut command, "CRYPTO__TOTP_SECRET_KEY", "not-a-64-char-hex-key");

    let (mut guard, _logs, _rx) = ChildGuard::spawn(&mut command);
    let status = guard.wait_exit(STARTUP_TIMEOUT).expect("非法 TOTP 密钥时必须及时退出");

    assert!(!status.success(), "非法 TOTP 密钥时必须以非零码退出");
}

// SIGINT 在 Windows 上无法按此方式投递，编译期跳过
#[cfg(unix)]
#[tokio::test]
async fn sigint_triggers_graceful_shutdown() {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let mut command = base_command(dir.path());
    let (mut guard, logs, rx) = ChildGuard::spawn(&mut command);
    await_listening(&rx, &logs);

    let pid = guard.id().to_string();
    let signal =
        Command::new("kill").args(["-INT", pid.as_str()]).status().expect("kill 必须可执行");
    assert!(signal.success(), "发送 SIGINT 失败");

    let status = guard.wait_exit(STARTUP_TIMEOUT).expect("SIGINT 后必须及时退出");
    assert!(status.success(), "优雅停机必须以 0 退出，实际码 {:?}", status.code());
}
