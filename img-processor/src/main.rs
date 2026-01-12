//! img-processor - 图片处理服务
//! 
//! 通过 Unix Socket 接收图片，转换为 WebP 格式后返回
//! 协议: [4字节长度(大端)][数据]

use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::UnixListener;

const SOCKET_PATH: &str = "/tmp/img-processor.sock";
const MAX_IMAGE_SIZE: usize = 10 * 1024 * 1024; // 10MB

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // 删除旧的 socket 文件
    let _ = std::fs::remove_file(SOCKET_PATH);
    
    let listener = UnixListener::bind(SOCKET_PATH)?;
    println!("[img-processor] Listening on {}", SOCKET_PATH);
    
    // 设置权限
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(SOCKET_PATH, std::fs::Permissions::from_mode(0o666))?;
    }
    
    // 优雅关闭
    let shutdown = async {
        tokio::signal::ctrl_c().await.ok();
        println!("\n[img-processor] Shutting down...");
    };
    
    tokio::select! {
        _ = accept_loop(&listener) => {}
        _ = shutdown => {}
    }
    
    let _ = std::fs::remove_file(SOCKET_PATH);
    Ok(())
}

async fn accept_loop(listener: &UnixListener) {
    loop {
        match listener.accept().await {
            Ok((stream, _)) => {
                tokio::spawn(handle_connection(stream));
            }
            Err(e) => {
                eprintln!("[img-processor] Accept error: {}", e);
            }
        }
    }
}

async fn handle_connection(mut stream: tokio::net::UnixStream) {
    // 读取长度 (4 字节大端)
    let mut len_buf = [0u8; 4];
    if stream.read_exact(&mut len_buf).await.is_err() {
        return;
    }
    let len = u32::from_be_bytes(len_buf) as usize;
    
    // 验证大小
    if len == 0 || len > MAX_IMAGE_SIZE {
        let _ = send_error(&mut stream, "Invalid size").await;
        return;
    }
    
    // 读取图片数据
    let mut data = vec![0u8; len];
    if stream.read_exact(&mut data).await.is_err() {
        let _ = send_error(&mut stream, "Read failed").await;
        return;
    }
    
    // 处理图片
    match process_image(&data) {
        Ok(webp_data) => {
            let _ = send_response(&mut stream, &webp_data).await;
        }
        Err(e) => {
            let _ = send_error(&mut stream, &e).await;
        }
    }
}

fn process_image(data: &[u8]) -> Result<Vec<u8>, String> {
    // 解码图片
    let img = image::load_from_memory(data)
        .map_err(|e| format!("Decode failed: {}", e))?;
    
    // 转换为 RGBA8
    let rgba = img.to_rgba8();
    let (width, height) = rgba.dimensions();
    
    // 编码为 WebP (有损，质量 85)
    let encoder = webp::Encoder::from_rgba(&rgba, width, height);
    let webp_data = encoder.encode(85.0);
    
    Ok(webp_data.to_vec())
}

async fn send_response(stream: &mut tokio::net::UnixStream, data: &[u8]) -> std::io::Result<()> {
    // 状态码 0 = 成功
    stream.write_all(&[0u8]).await?;
    // 长度
    stream.write_all(&(data.len() as u32).to_be_bytes()).await?;
    // 数据
    stream.write_all(data).await?;
    stream.flush().await
}

async fn send_error(stream: &mut tokio::net::UnixStream, msg: &str) -> std::io::Result<()> {
    // 状态码 1 = 错误
    stream.write_all(&[1u8]).await?;
    let bytes = msg.as_bytes();
    stream.write_all(&(bytes.len() as u32).to_be_bytes()).await?;
    stream.write_all(bytes).await?;
    stream.flush().await
}
