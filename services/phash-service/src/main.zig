const std = @import("std");
const net = std.net;

const GRID: usize = 32;
const INPUT_BYTES: usize = GRID * GRID;
const HASH_SIZE: usize = 8;

fn dct1d(input: [GRID]f64) [GRID]f64 {
    var out: [GRID]f64 = undefined;
    var k: usize = 0;
    while (k < GRID) : (k += 1) {
        var sum: f64 = 0;
        var n: usize = 0;
        while (n < GRID) : (n += 1) {
            const angle = std.math.pi / @as(f64, @floatFromInt(GRID)) * (@as(f64, @floatFromInt(n)) + 0.5) * @as(f64, @floatFromInt(k));
            sum += input[n] * @cos(angle);
        }
        out[k] = sum;
    }
    return out;
}

fn computePHash(pixels: [INPUT_BYTES]u8) u64 {
    var matrix: [GRID][GRID]f64 = undefined;
    var y: usize = 0;
    while (y < GRID) : (y += 1) {
        var x: usize = 0;
        while (x < GRID) : (x += 1) {
            matrix[y][x] = @floatFromInt(pixels[y * GRID + x]);
        }
    }

    var rowDct: [GRID][GRID]f64 = undefined;
    y = 0;
    while (y < GRID) : (y += 1) {
        rowDct[y] = dct1d(matrix[y]);
    }

    var colInput: [GRID]f64 = undefined;
    var full: [GRID][GRID]f64 = undefined;
    var x: usize = 0;
    while (x < GRID) : (x += 1) {
        y = 0;
        while (y < GRID) : (y += 1) {
            colInput[y] = rowDct[y][x];
        }
        const colOut = dct1d(colInput);
        y = 0;
        while (y < GRID) : (y += 1) {
            full[y][x] = colOut[y];
        }
    }

    var low: [HASH_SIZE * HASH_SIZE]f64 = undefined;
    var idx: usize = 0;
    y = 0;
    while (y < HASH_SIZE) : (y += 1) {
        x = 0;
        while (x < HASH_SIZE) : (x += 1) {
            low[idx] = full[y][x];
            idx += 1;
        }
    }

    var sorted = low;
    std.mem.sort(f64, &sorted, {}, std.sort.asc(f64));
    const median = (sorted[low.len / 2 - 1] + sorted[low.len / 2]) / 2.0;

    var hash: u64 = 0;
    idx = 0;
    while (idx < low.len) : (idx += 1) {
        if (low[idx] > median) {
            hash |= @as(u64, 1) << @intCast(idx);
        }
    }
    return hash;
}

fn hexEncode(buf: []u8, value: u64) []u8 {
    const digits = "0123456789abcdef";
    var i: usize = 0;
    while (i < 16) : (i += 1) {
        const shift: u6 = @intCast((15 - i) * 4);
        const nibble: usize = @intCast((value >> shift) & 0xf);
        buf[i] = digits[nibble];
    }
    return buf[0..16];
}

fn writeJson(stream: net.Stream, status_line: []const u8, body: []const u8) !void {
    var header_buf: [256]u8 = undefined;
    const header = try std.fmt.bufPrint(&header_buf, "HTTP/1.1 {s}\r\nContent-Type: application/json; charset=utf-8\r\nContent-Length: {d}\r\nConnection: close\r\n\r\n", .{ status_line, body.len });
    try stream.writeAll(header);
    try stream.writeAll(body);
}

fn eqlIgnoreCasePrefix(haystack: []const u8, prefix: []const u8) bool {
    if (haystack.len < prefix.len) return false;
    return std.ascii.eqlIgnoreCase(haystack[0..prefix.len], prefix);
}

fn findContentLength(headBlock: []const u8) ?usize {
    var it = std.mem.splitSequence(u8, headBlock, "\r\n");
    while (it.next()) |line| {
        if (eqlIgnoreCasePrefix(line, "content-length:")) {
            const value = std.mem.trim(u8, line[16..], " ");
            return std.fmt.parseInt(usize, value, 10) catch null;
        }
    }
    return null;
}

fn handleConnection(conn: net.Server.Connection, allowedOrigin: []const u8) void {
    _ = allowedOrigin;
    defer conn.stream.close();

    var head_buf: [4096]u8 = undefined;
    var head_len: usize = 0;
    var header_end: usize = 0;
    var found = false;

    while (head_len < head_buf.len) {
        const n = conn.stream.read(head_buf[head_len..]) catch return;
        if (n == 0) break;
        head_len += n;
        if (std.mem.indexOf(u8, head_buf[0..head_len], "\r\n\r\n")) |pos| {
            header_end = pos + 4;
            found = true;
            break;
        }
    }
    if (!found) {
        writeJson(conn.stream, "400 Bad Request", "{\"success\":false,\"error\":\"header too large\"}") catch {};
        return;
    }

    const first_line_end = std.mem.indexOf(u8, head_buf[0..header_end], "\r\n") orelse header_end;
    const first_line = head_buf[0..first_line_end];

    var parts = std.mem.splitScalar(u8, first_line, ' ');
    const method = parts.next() orelse "";
    const path = parts.next() orelse "";

    if (std.mem.eql(u8, method, "GET") and std.mem.eql(u8, path, "/health")) {
        writeJson(conn.stream, "200 OK", "{\"status\":\"ok\"}") catch {};
        return;
    }

    if (!(std.mem.eql(u8, method, "POST") and std.mem.eql(u8, path, "/hash"))) {
        writeJson(conn.stream, "404 Not Found", "{\"success\":false,\"error\":\"not found\"}") catch {};
        return;
    }

    const content_length = findContentLength(head_buf[0..header_end]) orelse 0;
    if (content_length != INPUT_BYTES) {
        writeJson(conn.stream, "400 Bad Request", "{\"success\":false,\"error\":\"body must be 1024 raw grayscale bytes (32x32)\"}") catch {};
        return;
    }

    var body: [INPUT_BYTES]u8 = undefined;
    const already = head_len - header_end;
    const copy_len = @min(already, INPUT_BYTES);
    @memcpy(body[0..copy_len], head_buf[header_end .. header_end + copy_len]);

    var got: usize = copy_len;
    while (got < INPUT_BYTES) {
        const n = conn.stream.read(body[got..INPUT_BYTES]) catch return;
        if (n == 0) break;
        got += n;
    }
    if (got != INPUT_BYTES) {
        writeJson(conn.stream, "400 Bad Request", "{\"success\":false,\"error\":\"incomplete body\"}") catch {};
        return;
    }

    const hash = computePHash(body);
    var hex_buf: [16]u8 = undefined;
    const hex = hexEncode(&hex_buf, hash);

    var body_buf: [128]u8 = undefined;
    const resp = std.fmt.bufPrint(&body_buf, "{{\"success\":true,\"hash\":\"{s}\"}}", .{hex}) catch return;
    writeJson(conn.stream, "200 OK", resp) catch {};
}

pub fn main() !void {
    var arena_state = std.heap.ArenaAllocator.init(std.heap.page_allocator);
    defer arena_state.deinit();
    const allocator = arena_state.allocator();

    const port_str = std.process.getEnvVarOwned(allocator, "PHASH_SERVICE_PORT") catch try allocator.dupe(u8, "8096");
    const port = std.fmt.parseInt(u16, std.mem.trim(u8, port_str, " "), 10) catch 8096;

    const address = try net.Address.parseIp4("127.0.0.1", port);
    var server = try address.listen(.{ .reuse_address = true });
    defer server.deinit();

    std.debug.print("phash-service 기동 완료: 127.0.0.1:{d}\n", .{port});

    while (true) {
        const conn = server.accept() catch continue;
        const thread = std.Thread.spawn(.{}, handleConnection, .{ conn, "" }) catch {
            conn.stream.close();
            continue;
        };
        thread.detach();
    }
}
