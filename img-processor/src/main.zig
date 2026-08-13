const std = @import("std");
const builtin = @import("builtin");
const net = std.Io.net;

const c = @cImport({
    @cInclude("stb_image/stb_image.h");
    @cInclude("src/webp/encode.h");
});

const SOCKET_PATH_DEFAULT = "/tmp/img-processor.sock";
const MAX_IMAGE_SIZE: usize = 10 * 1024 * 1024;
/// 读取客户端数据的超时（秒），与 Go 端 ReadWriteTimeout 一致
const READ_TIMEOUT_SECONDS: i64 = 30;
/// 解码前限制的图像总像素数上限（4096x4096≈16.7M 像素，RGBA 约 67MB），防超大图内存 DoS
const MAX_PIXELS: i64 = 4096 * 4096;

/// 同时处理的图片请求上限。每连接一个线程，用信号量限制并发数：
/// 防止连接洪泛导致线程无限增长（DoS），同时保证多请求真正并行处理。
const MAX_CONCURRENT: usize = 2;
var sem: std.Io.Semaphore = .{ .permits = MAX_CONCURRENT };

pub fn main(init: std.process.Init) !void {
    const io = init.io;

    // socket 路径由 Go 端通过环境变量 IMG_PROCESSOR_SOCKET 传入
    // （未设置时回退默认路径，便于手动运行调试）
    const socket_path = init.environ_map.get("IMG_PROCESSOR_SOCKET") orelse SOCKET_PATH_DEFAULT;

    std.Io.Dir.deleteFileAbsolute(io, socket_path) catch {};

    const unix_addr = try net.UnixAddress.init(socket_path);
    var server = try unix_addr.listen(io, .{ .kernel_backlog = 128 });
    defer server.deinit(io);

    if (@import("builtin").os.tag != .windows) {
        const file = std.Io.Dir.openFileAbsolute(io, socket_path, .{ .mode = .write_only }) catch null;
        if (file) |f| {
            defer f.close(io);
            // 0600：仅属主可访问，防止其他本地用户连接（配合 Go 端 0700 私有目录）
            std.Io.File.setPermissions(f, io, .fromMode(0o600)) catch {};
        }
    }

    std.debug.print("[img-processor] Listening on {s}\n", .{socket_path});

    while (true) {
        // 先获取 permit 再 accept：限流发生在 accept 之前，超出的连接留在内核
        // backlog（上限 128）排队，不占用用户态 fd（防本地连接洪泛耗尽 fd）。
        sem.wait(io) catch {
            continue;
        };

        const client = server.accept(io) catch |err| {
            sem.post(io); // accept 失败未获得连接，归还 permit
            std.debug.print("[img-processor] Accept error: {}\n", .{err});
            continue;
        };

        const t = std.Thread.spawn(.{}, handleConnection, .{ client, io, init.gpa }) catch |err| {
            std.debug.print("[img-processor] Spawn error: {}\n", .{err});
            sem.post(io);
            client.close(io);
            continue;
        };
        t.detach();
    }
}

// worker 线程：处理单个连接，结束后释放信号量与连接
fn handleConnection(client: net.Stream, io: std.Io, allocator: std.mem.Allocator) void {
    defer sem.post(io);
    defer client.close(io);

    handleConnectionImpl(client, io, allocator) catch |err| {
        std.debug.print("[img-processor] Handle error: {}\n", .{err});
    };
}

fn handleConnectionImpl(client: net.Stream, io: std.Io, allocator: std.mem.Allocator) !void {
    var write_buf: [4096]u8 = undefined;
    var stream_writer = client.writer(io, &write_buf);
    const writer = &stream_writer.interface;

    // 与 Go 端 ReadWriteTimeout(30s) 一致：客户端不发送数据时释放 permit，防 slowloris 占满并发。
    // 转成绝对 deadline：整体 30s 内必须发完数据，防止"每 29s 发 1 字节"无限续期。
    const timeout: std.Io.Timeout = .{ .duration = .{
        .raw = std.Io.Duration.fromSeconds(READ_TIMEOUT_SECONDS),
        .clock = .awake,
    } };
    const deadline = timeout.toDeadline(io);

    var len_buf: [4]u8 = undefined;
    try readFullTimeout(&client.socket, io, &len_buf, deadline);
    const len = std.mem.readInt(u32, &len_buf, .big);

    if (len == 0 or len > MAX_IMAGE_SIZE) {
        try sendError(writer, "Invalid size");
        return;
    }

    const data = try allocator.alloc(u8, len);
    defer allocator.free(data);
    try readFullTimeout(&client.socket, io, data, deadline);

    const result = processImage(data, allocator) catch |err| {
        try sendError(writer, @errorName(err));
        return;
    };
    defer allocator.free(result);

    // 写侧无超时 API（Zig 0.16 Io 只有 receiveTimeout）。写永久阻塞需要"对端持连接永不读"：
    // 客户端固定为 Go 进程（socket 0600 + 0700 目录已封死其他本地用户），Go 端 ToWebP 总是
    // 读响应且 30s 读超时后关闭连接（届时本端写返回 EPIPE 错误，permit 正常归还）——因此
    // 写阻塞不会无限期占用 permit。
    try sendResponse(writer, result);
}

/// 带超时的完整读取（对 stream socket 用 recvmsg，超时返回 error.Timeout）
fn readFullTimeout(socket: *const net.Socket, io: std.Io, buf: []u8, timeout: std.Io.Timeout) !void {
    var off: usize = 0;
    while (off < buf.len) {
        const msg = try socket.receiveTimeout(io, buf[off..], timeout);
        if (msg.data.len == 0) return error.EndOfStream;
        off += msg.data.len;
    }
}

fn processImage(data: []const u8, allocator: std.mem.Allocator) ![]u8 {
    var width: c_int = 0;
    var height: c_int = 0;
    var channels: c_int = 0;

    // 解码前检查图像尺寸：超大图像（如巨大 BMP）解码出 RGBA 会耗尽内存，并发下放大 DoS。
    // info 不支持的格式（如 ICO，stbi_info 无 ICO 分支但 load 支持）跳过预检，交给 load 决定。
    var info_w: c_int = 0;
    var info_h: c_int = 0;
    var info_c: c_int = 0;
    if (c.stbi_info_from_memory(data.ptr, @intCast(data.len), &info_w, &info_h, &info_c) != 0) {
        if (@as(i64, info_w) * info_h > MAX_PIXELS) {
            return error.ImageTooLarge;
        }
    }

    const rgba = c.stbi_load_from_memory(
        data.ptr,
        @intCast(data.len),
        &width,
        &height,
        &channels,
        4,
    );
    if (rgba == null) {
        return error.DecodeError;
    }
    defer c.stbi_image_free(rgba);

    var config: c.WebPConfig = undefined;
    if (c.WebPConfigPreset(&config, c.WEBP_PRESET_DEFAULT, 85.0) == 0) {
        return error.ConfigError;
    }
    config.method = 6;

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

    var webp_writer: c.WebPMemoryWriter = undefined;
    c.WebPMemoryWriterInit(&webp_writer);
    picture.writer = c.WebPMemoryWrite;
    picture.custom_ptr = &webp_writer;

    if (c.WebPEncode(&config, &picture) == 0) {
        if (webp_writer.mem != null) c.WebPFree(webp_writer.mem);
        return error.EncodeError;
    }
    defer c.WebPFree(webp_writer.mem);

    const result = try allocator.alloc(u8, webp_writer.size);
    @memcpy(result, webp_writer.mem[0..webp_writer.size]);
    return result;
}

fn sendResponse(writer: *std.Io.Writer, data: []const u8) !void {
    try std.Io.Writer.writeByte(writer, 0);
    var len_buf: [4]u8 = undefined;
    std.mem.writeInt(u32, &len_buf, @intCast(data.len), .big);
    try std.Io.Writer.writeAll(writer, &len_buf);
    try std.Io.Writer.writeAll(writer, data);
}

fn sendError(writer: *std.Io.Writer, msg: []const u8) !void {
    try std.Io.Writer.writeByte(writer, 1);
    var len_buf: [4]u8 = undefined;
    std.mem.writeInt(u32, &len_buf, @intCast(msg.len), .big);
    try std.Io.Writer.writeAll(writer, &len_buf);
    try std.Io.Writer.writeAll(writer, msg);
}

const minimal_bmp = [_]u8{
    0x42, 0x4D, 0x3A, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x36, 0x00, 0x00, 0x00,
    0x28, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x00, 0x00,
    0x01, 0x00,
    0x18, 0x00,
    0x00, 0x00, 0x00, 0x00,
    0x04, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0xFF, 0x00,
};

test "processImage - valid BMP returns WebP data" {
    const result = try processImage(&minimal_bmp, std.testing.allocator);
    defer std.testing.allocator.free(result);

    try std.testing.expect(result.len > 0);
    try std.testing.expect(result[0] == 'R');
    try std.testing.expect(result[1] == 'I');
    try std.testing.expect(result[2] == 'F');
    try std.testing.expect(result[3] == 'F');
    try std.testing.expect(result[8] == 'W');
    try std.testing.expect(result[9] == 'E');
    try std.testing.expect(result[10] == 'B');
    try std.testing.expect(result[11] == 'P');
}

test "processImage - invalid data returns DecodeError" {
    const invalid_data = [_]u8{ 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07 };
    const result = processImage(&invalid_data, std.testing.allocator);
    try std.testing.expectError(error.DecodeError, result);
}

test "processImage - empty data returns DecodeError" {
    const empty_data = [_]u8{};
    const result = processImage(&empty_data, std.testing.allocator);
    try std.testing.expectError(error.DecodeError, result);
}

test "processImage - truncated BMP returns DecodeError" {
    const truncated = minimal_bmp[0..20];
    const result = processImage(truncated, std.testing.allocator);
    try std.testing.expectError(error.DecodeError, result);
}

test "processImage - oversized dimensions rejected before decode" {
    var big_bmp = minimal_bmp;
    // BMP 头：宽在 offset 18-21、高在 22-25（小端），改写成超大尺寸
    std.mem.writeInt(u32, big_bmp[18..22], 5000, .little);
    std.mem.writeInt(u32, big_bmp[22..26], 5000, .little);

    const result = processImage(&big_bmp, std.testing.allocator);
    try std.testing.expectError(error.ImageTooLarge, result);
}

test "processImage - WebP output size is reasonable" {
    const result = try processImage(&minimal_bmp, std.testing.allocator);
    defer std.testing.allocator.free(result);

    try std.testing.expect(result.len < minimal_bmp.len * 100);
    try std.testing.expect(result.len >= 20);
}

test "protocol - sendResponse format matches Go client" {
    var allocating = std.Io.Writer.Allocating.init(std.testing.allocator);
    defer {
        var list = allocating.toArrayList();
        list.deinit(std.testing.allocator);
    }
    const writer = &allocating.writer;

    const test_data = "hello webp";
    try sendResponse(writer, test_data);

    const written = allocating.written();
    try std.testing.expect(written[0] == 0);
    const len = std.mem.readInt(u32, written[1..5], .big);
    try std.testing.expect(len == test_data.len);
    try std.testing.expectEqualSlices(u8, written[5..], test_data);
}

test "protocol - sendError format matches Go client" {
    var allocating = std.Io.Writer.Allocating.init(std.testing.allocator);
    defer {
        var list = allocating.toArrayList();
        list.deinit(std.testing.allocator);
    }
    const writer = &allocating.writer;

    const test_msg = "DecodeError";
    try sendError(writer, test_msg);

    const written = allocating.written();
    try std.testing.expect(written[0] == 1);
    const len = std.mem.readInt(u32, written[1..5], .big);
    try std.testing.expect(len == test_msg.len);
    try std.testing.expectEqualSlices(u8, written[5..], test_msg);
}

test "protocol - length encoding uses big-endian (Go binary.BigEndian)" {
    var allocating = std.Io.Writer.Allocating.init(std.testing.allocator);
    defer {
        var list = allocating.toArrayList();
        list.deinit(std.testing.allocator);
    }
    const writer = &allocating.writer;

    const test_data = "abc";
    try sendResponse(writer, test_data);

    const written = allocating.written();
    try std.testing.expect(written[1] == 0);
    try std.testing.expect(written[2] == 0);
    try std.testing.expect(written[3] == 0);
    try std.testing.expect(written[4] == 3);
}

test "semaphore limits concurrent workers to MAX_CONCURRENT" {
    if (builtin.single_threaded) return error.SkipZigTest;
    const io = std.testing.io;

    const Context = struct {
        sem: *std.Io.Semaphore,
        active: *std.atomic.Value(usize),
        max_active: *std.atomic.Value(usize),
        fn worker(ctx: *@This()) void {
            ctx.sem.wait(io) catch return;
            defer ctx.sem.post(io);

            const cur = ctx.active.fetchAdd(1, .monotonic) + 1;
            var seen = ctx.max_active.load(.monotonic);
            while (cur > seen) {
                seen = ctx.max_active.cmpxchgWeak(seen, cur, .monotonic, .monotonic) orelse break;
            }
            // 模拟图片处理耗时，确保并发窗口有重叠
            std.Io.Clock.Duration.sleep(.{
                .raw = std.Io.Duration.fromMilliseconds(50),
                .clock = .awake,
            }, io) catch {};
            _ = ctx.active.fetchSub(1, .monotonic);
        }
    };

    var my_sem: std.Io.Semaphore = .{ .permits = MAX_CONCURRENT };
    var active = std.atomic.Value(usize).init(0);
    var max_active = std.atomic.Value(usize).init(0);

    const num_threads = 4;
    var threads: [num_threads]std.Thread = undefined;
    var ctxs: [num_threads]Context = undefined;
    for (0..num_threads) |i| {
        ctxs[i] = .{ .sem = &my_sem, .active = &active, .max_active = &max_active };
    }
    for (0..num_threads) |i| {
        threads[i] = try std.Thread.spawn(.{}, Context.worker, .{&ctxs[i]});
    }
    for (threads) |t| t.join();

    // 并发峰值恰为 MAX_CONCURRENT，且结束后活跃数为 0（无信号量泄露）
    try std.testing.expectEqual(MAX_CONCURRENT, max_active.load(.monotonic));
    try std.testing.expectEqual(@as(usize, 0), active.load(.monotonic));
}

test "processImage - concurrent calls are safe" {
    if (builtin.single_threaded) return error.SkipZigTest;

    const num_threads = 4;
    const Context = struct {
        input: []const u8,
        ok: *bool,
        fn worker(ctx: *@This()) void {
            const result = processImage(ctx.input, std.heap.page_allocator) catch {
                ctx.ok.* = false;
                return;
            };
            defer std.heap.page_allocator.free(result);
            ctx.ok.* = result.len > 0;
        }
    };

    var oks = [_]bool{false} ** num_threads;
    var ctxs: [num_threads]Context = undefined;
    for (0..num_threads) |i| {
        ctxs[i] = .{ .input = &minimal_bmp, .ok = &oks[i] };
    }

    var threads: [num_threads]std.Thread = undefined;
    for (0..num_threads) |i| {
        threads[i] = try std.Thread.spawn(.{}, Context.worker, .{&ctxs[i]});
    }
    for (threads) |t| t.join();

    for (oks) |ok| try std.testing.expect(ok);
}
