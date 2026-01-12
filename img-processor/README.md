# img-processor

高性能图片处理服务，将图片转换为 WebP 格式。

## 架构

此服务**嵌入到 Go 主程序中**，无需单独部署：

1. GitHub Actions 编译 Rust 二进制
2. Go 通过 `//go:embed` 嵌入二进制
3. Go 启动时自动释放到 `/tmp/img-processor` 并启动
4. 通过 Unix Socket (`/tmp/img-processor.sock`) 通信
5. Go 关闭时自动清理

## 本地开发

```bash
cd img-processor
cargo build --release
```

编译后的二进制文件在 `target/release/img-processor`

## 协议

请求: `[4字节长度(大端)][图片数据]`
响应: `[1字节状态][4字节长度(大端)][数据]`

- 状态 0: 成功，数据为 WebP
- 状态 1: 失败，数据为错误信息
