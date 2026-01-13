const std = @import("std");
const net = std.net;
const posix = std.posix;

const c = @cImport({
    @cInclude("stb_image.h");
    @cInclude("src/webp/encode.h");
});

const SOCKET_PATH = "/tmp/img-processor.sock";
const MAX_IMAGE_SIZE: usize = 10 * 1024 * 1024; // 10MB

pub fn main() !void {
    // 删除旧 socket
    std.fs.cwd().deleteFile(SOCKET_PATH) catch {};

    // 创建 Unix Socket
    const addr = net.Address.initUnix(SOCKET_PATH) catch unreachable;
    const server = try posix.socket(posix.AF.UNIX, posix.SOCK.STREAM, 0);
    defer posix.close(server);

    try posix.bind(server, &addr.any, addr.getOsSockLen());
    try posix.listen(server, 128);

    // 设置权限 0666 (仅 Linux)
    if (@import("builtin").os.tag != .windows) {
        const file = std.fs.cwd().openFile(SOCKET_PATH, .{ .mode = .write_only }) catch null;
        if (file) |f| {
            defer f.close();
            f.chmod(0o666) catch {};
        }
    }

    std.debug.print("[img-processor] Listening on {s}\n", .{SOCKET_PATH});

    // Accept loop
    while (true) {
        const client = posix.accept(server, null, null, 0) catch |err| {
            std.debug.print("[img-processor] Accept error: {}\n", .{err});
            continue;
        };

        // 单线程处理（简单起见，可以后续改成线程池）
        handleConnection(client) catch |err| {
            std.debug.print("[img-processor] Handle error: {}\n", .{err});
        };
        posix.close(client);
    }
}

fn handleConnection(client: posix.socket_t) !void {
    var allocator = std.heap.page_allocator;

    // 读取长度 (4 字节大端)
    var len_buf: [4]u8 = undefined;
    _ = try readExact(client, &len_buf);
    const len = std.mem.readInt(u32, &len_buf, .big);

    if (len == 0 or len > MAX_IMAGE_SIZE) {
        try sendError(client, "Invalid size");
        return;
    }

    // 读取图片数据
    const data = try allocator.alloc(u8, len);
    defer allocator.free(data);
    _ = try readExact(client, data);

    // 处理图片
    const result = processImage(data, allocator) catch |err| {
        try sendError(client, @errorName(err));
        return;
    };
    defer allocator.free(result);

    // 发送成功响应
    try sendResponse(client, result);
}

fn processImage(data: []const u8, allocator: std.mem.Allocator) ![]u8 {
    var width: c_int = 0;
    var height: c_int = 0;
    var channels: c_int = 0;

    // 用 stb_image 解码
    const rgba = c.stbi_load_from_memory(
        data.ptr,
        @intCast(data.len),
        &width,
        &height,
        &channels,
        4, // 强制 RGBA
    );
    if (rgba == null) {
        return error.DecodeError;
    }
    defer c.stbi_image_free(rgba);

    // 配置 WebP 编码参数
    var config: c.WebPConfig = undefined;
    if (c.WebPConfigPreset(&config, c.WEBP_PRESET_DEFAULT, 85.0) == 0) {
        return error.ConfigError;
    }
    config.method = 6; // 最高压缩质量 (0=快, 6=慢但更小)

    // 设置图片数据
    var picture: c.WebPPicture = undefined;
    if (c.WebPPictureInit(&picture) == 0) {
        return error.PictureInitError;
    }
    picture.width = width;
    picture.height = height;
    picture.use_argb = 1;

    if (c.WebPPictureImportRGBA(&picture, rgba, width * 4) == 0) {
        return error.ImportError;
    }
    defer c.WebPPictureFree(&picture);

    // 使用内存写入器
    var writer: c.WebPMemoryWriter = undefined;
    c.WebPMemoryWriterInit(&writer);
    picture.writer = c.WebPMemoryWrite;
    picture.custom_ptr = &writer;

    // 编码
    if (c.WebPEncode(&config, &picture) == 0) {
        if (writer.mem != null) c.WebPFree(writer.mem);
        return error.EncodeError;
    }
    defer c.WebPFree(writer.mem);

    // 复制到 Zig 管理的内存
    const result = try allocator.alloc(u8, writer.size);
    @memcpy(result, writer.mem[0..writer.size]);
    return result;
}

fn readExact(fd: posix.socket_t, buf: []u8) !void {
    var total: usize = 0;
    while (total < buf.len) {
        const n = try posix.read(fd, buf[total..]);
        if (n == 0) return error.ConnectionClosed;
        total += n;
    }
}

fn sendResponse(fd: posix.socket_t, data: []const u8) !void {
    // 状态码 0 = 成功
    _ = try posix.write(fd, &[_]u8{0});
    // 长度
    var len_buf: [4]u8 = undefined;
    std.mem.writeInt(u32, &len_buf, @intCast(data.len), .big);
    _ = try posix.write(fd, &len_buf);
    // 数据
    _ = try posix.write(fd, data);
}

fn sendError(fd: posix.socket_t, msg: []const u8) !void {
    // 状态码 1 = 错误
    _ = try posix.write(fd, &[_]u8{1});
    // 长度
    var len_buf: [4]u8 = undefined;
    std.mem.writeInt(u32, &len_buf, @intCast(msg.len), .big);
    _ = try posix.write(fd, &len_buf);
    // 消息
    _ = try posix.write(fd, msg);
}
