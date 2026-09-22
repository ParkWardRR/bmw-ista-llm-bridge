const std = @import("std");
const Io = std.Io;

/// Default location of the VIN ranges data file, relative to the current
/// working directory. Overridable with `--data`.
const default_data_path = "test_data/vinranges.json";

/// One row of the VIN ranges table, as parsed straight out of the input
/// JSON file.
const VinRange = struct {
    from: []const u8,
    to: []const u8,
    type_key: []const u8,
    vin_4_7: []const u8,
    year: []const u8,
    month: []const u8,
    gearbox: []const u8,
};

/// A single decoded match, using the output field names expected on stdout.
const Match = struct {
    type_key: []const u8,
    vin_4_7: []const u8,
    production_year: []const u8,
    production_month: []const u8,
    gearbox_type: []const u8,
};

fn lessThanFrom(_: void, a: VinRange, b: VinRange) bool {
    return std.mem.lessThan(u8, a.from, b.from);
}

/// Ordering used to binary search the sorted range table: a query is `.eq`
/// to a range when it falls within `[from, to]` (inclusive), `.lt` when it
/// is smaller than the whole range, and `.gt` when it is larger.
fn orderQuery(query: []const u8, item: VinRange) std.math.Order {
    if (std.mem.order(u8, query, item.from) == .lt) return .lt;
    if (std.mem.order(u8, query, item.to) == .gt) return .gt;
    return .eq;
}

fn printUsage(prog: []const u8) void {
    std.debug.print(
        \\Usage: {s} [OPTIONS] <VIN>
        \\
        \\Decode a VIN (or a 7-character VIN suffix) against a table of VIN
        \\ranges using a sorted binary search.
        \\
        \\Arguments:
        \\  <VIN>              Full VIN. Only the last 7 characters are used for lookup.
        \\
        \\Options:
        \\  --last7 <SUFFIX>   Decode using a 7-character VIN suffix directly.
        \\  --data <PATH>      Path to the vinranges.json data file.
        \\                     (default: {s})
        \\  -h, --help         Show this help message.
        \\
        \\Examples:
        \\  {s} WBAPH5C50BA123456
        \\  {s} --last7 A123456
        \\  {s} --data path/to/vinranges.json WBAPH5C50BA123456
        \\
    , .{ prog, default_data_path, prog, prog, prog });
}

fn fail(comptime fmt: []const u8, args: anytype) noreturn {
    std.debug.print("error: " ++ fmt ++ "\n", args);
    std.process.exit(1);
}

pub fn main(init: std.process.Init) !void {
    const allocator = init.arena.allocator();
    const io = init.io;

    const args = try init.minimal.args.toSlice(allocator);
    const prog = if (args.len > 0) args[0] else "ista-vinlookup";

    var data_path: []const u8 = default_data_path;
    var last7_opt: ?[]const u8 = null;
    var vin_opt: ?[]const u8 = null;

    var i: usize = 1;
    while (i < args.len) : (i += 1) {
        const arg = args[i];
        if (std.mem.eql(u8, arg, "--help") or std.mem.eql(u8, arg, "-h")) {
            printUsage(prog);
            return;
        } else if (std.mem.eql(u8, arg, "--data")) {
            i += 1;
            if (i >= args.len) fail("--data requires a path argument", .{});
            data_path = args[i];
        } else if (std.mem.startsWith(u8, arg, "--data=")) {
            data_path = arg["--data=".len..];
        } else if (std.mem.eql(u8, arg, "--last7")) {
            i += 1;
            if (i >= args.len) fail("--last7 requires a value argument", .{});
            last7_opt = args[i];
        } else if (std.mem.startsWith(u8, arg, "--last7=")) {
            last7_opt = arg["--last7=".len..];
        } else if (std.mem.startsWith(u8, arg, "-")) {
            fail("unrecognized option '{s}'", .{arg});
        } else if (vin_opt == null) {
            vin_opt = arg;
        } else {
            fail("unexpected extra argument '{s}'", .{arg});
        }
    }

    var vin_display: []const u8 = "";
    var last7: []const u8 = "";

    if (last7_opt) |suffix| {
        if (suffix.len != 7) {
            fail("--last7 value must be exactly 7 characters, got {d}", .{suffix.len});
        }
        last7 = suffix;
        vin_display = suffix;
    } else if (vin_opt) |vin| {
        if (vin.len < 7) {
            fail("VIN must be at least 7 characters, got {d}", .{vin.len});
        }
        vin_display = vin;
        last7 = vin[vin.len - 7 ..];
    } else {
        printUsage(prog);
        std.process.exit(1);
    }

    const data_slice = Io.Dir.cwd().readFileAlloc(io, data_path, allocator, .unlimited) catch |err| {
        fail("failed to read data file '{s}': {s}", .{ data_path, @errorName(err) });
    };

    var parsed = std.json.parseFromSlice([]VinRange, allocator, data_slice, .{}) catch |err| {
        fail("failed to parse JSON data file '{s}': {s}", .{ data_path, @errorName(err) });
    };
    defer parsed.deinit();

    const ranges = parsed.value;
    std.mem.sort(VinRange, ranges, {}, lessThanFrom);

    var matches: std.ArrayList(Match) = .empty;
    defer matches.deinit(allocator);

    if (std.sort.binarySearch(VinRange, ranges, last7, orderQuery)) |found_idx| {
        // The binary search may land on any one of several adjacent rows
        // that share an overlapping/identical range (e.g. distinct
        // attributes recorded for the same numeric window). Expand outward
        // while neighbors still match the query.
        var lo = found_idx;
        while (lo > 0 and orderQuery(last7, ranges[lo - 1]) == .eq) : (lo -= 1) {}
        var hi = found_idx;
        while (hi + 1 < ranges.len and orderQuery(last7, ranges[hi + 1]) == .eq) : (hi += 1) {}

        var idx = lo;
        while (idx <= hi) : (idx += 1) {
            try matches.append(allocator, .{
                .type_key = ranges[idx].type_key,
                .vin_4_7 = ranges[idx].vin_4_7,
                .production_year = ranges[idx].year,
                .production_month = ranges[idx].month,
                .gearbox_type = ranges[idx].gearbox,
            });
        }
    }

    var stdout_buffer: [8192]u8 = undefined;
    var stdout_file_writer: Io.File.Writer = .init(.stdout(), io, &stdout_buffer);
    const w = &stdout_file_writer.interface;

    var stringify: std.json.Stringify = .{ .writer = w, .options = .{} };
    try stringify.beginObject();

    try stringify.objectField("vin");
    try stringify.write(vin_display);

    try stringify.objectField("last7");
    try stringify.write(last7);

    try stringify.objectField("matches");
    try stringify.beginArray();
    for (matches.items) |m| {
        try stringify.beginObject();

        try stringify.objectField("type_key");
        try stringify.write(m.type_key);

        try stringify.objectField("vin_4_7");
        try stringify.write(m.vin_4_7);

        try stringify.objectField("production_year");
        try stringify.write(m.production_year);

        try stringify.objectField("production_month");
        try stringify.write(m.production_month);

        try stringify.objectField("gearbox_type");
        try stringify.write(m.gearbox_type);

        try stringify.endObject();
    }
    try stringify.endArray();

    try stringify.endObject();
    try w.writeByte('\n');
    try w.flush();
}
