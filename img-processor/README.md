# img-processor

高性能图片处理服务，将图片转换为 WebP 格式。

## 编译

```bash
cd img-processor
cargo build --release
```

编译后的二进制文件在 `target/release/img-processor`

## 运行

```bash
./target/release/img-processor
```

服务会监听 `/tmp/img-processor.sock`

## 部署 (systemd)

创建 `/etc/systemd/system/img-processor.service`:

```ini
[Unit]
Description=Image Processor Service
After=network.target

[Service]
Type=simple
ExecStart=/path/to/img-processor
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

启动:
```bash
sudo systemctl enable img-processor
sudo systemctl start img-processor
```

## 协议

请求: `[4字节长度(大端)][图片数据]`
响应: `[1字节状态][4字节长度(大端)][数据]`

- 状态 0: 成功，数据为 WebP
- 状态 1: 失败，数据为错误信息
