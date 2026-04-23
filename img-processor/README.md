# img-processor (Zig)

图片处理服务 - 通过 Unix Socket 接收图片，转换为 WebP 格式

## 依赖

1. stb_image (header-only)
2. libwebp

## 编译

```bash
zig build -Doptimize=ReleaseFast
```

输出: `zig-out/bin/img-processor`

## 协议

与 Go 端通信协议：

**请求**: `[4字节长度(大端)][图片数据]`

**响应**: `[1字节状态][4字节长度(大端)][数据]`
- 状态 0 = 成功，数据为 WebP
- 状态 1 = 错误，数据为错误消息
