const std = @import("std");
const net = std.Io.net;

const c = @cImport({
    @cInclude("stb_image/stb_image.h");
    @cInclude("src/webp/encode.h");
});

const SOCKET_PATH = "/tmp/img-processor.sock";
const MAX_IMAGE_SIZE: usize = 10 * 1024 * 1024;

pub fn main(init: std.process.Init) !void {
    const io = init.io;

    std.Io.Dir.deleteFileAbsolute(io, SOCKET_PATH) catch {};

    const unix_addr = try net.UnixAddress.init(SOCKET_PATH);
    var server = try unix_addr.listen(io, .{ .kernel_backlog = 128 });
    defer server.deinit(io);

    if (@import("builtin").os.tag != .windows) {
        const file = std.Io.Dir.openFileAbsolute(io, SOCKET_PATH, .{ .mode = .write_only }) catch null;
        if (file) |f| {
            defer f.close(io);
            std.Io.File.setPermissions(f, io, .fromMode(0o666)) catch {};
        }
    }

    std.debug.print("[img-processor] Listening on {s}\n", .{SOCKET_PATH});

    while (true) {
        const client = server.accept(io) catch |err| {
            std.debug.print("[img-processor] Accept error: {}\n", .{err});
            continue;
        };

        handleConnection(client, io, init.gpa) catch |err| {
            std.debug.print("[img-processor] Handle error: {}\n", .{err});
        };
        client.close(io);
    }
}

fn handleConnection(client: net.Stream, io: std.Io, allocator: std.mem.Allocator) !void {
    var read_buf: [4096]u8 = undefined;
    var write_buf: [4096]u8 = undefined;
    var stream_reader = client.reader(io, &read_buf);
    var stream_writer = client.writer(io, &write_buf);
    const reader = &stream_reader.interface;
    const writer = &stream_writer.interface;

    var len_buf: [4]u8 = undefined;
    try std.Io.Reader.readSliceAll(reader, &len_buf);
    const len = std.mem.readInt(u32, &len_buf, .big);

    if (len == 0 or len > MAX_IMAGE_SIZE) {
        try sendError(writer, "Invalid size");
        return;
    }

    const data = try allocator.alloc(u8, len);
    defer allocator.free(data);
    try std.Io.Reader.readSliceAll(reader, data);

    const result = processImage(data, allocator) catch |err| {
        try sendError(writer, @errorName(err));
        return;
    };
    defer allocator.free(result);

    try sendResponse(writer, result);
}

fn processImage(data: []const u8, allocator: std.mem.Allocator) ![]u8 {
    var width: c_int = 0;
    var height: c_int = 0;
    var channels: c_int = 0;

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
