const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});

    const exe = b.addExecutable(.{
        .name = "img-processor",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .target = target,
            .optimize = optimize,
        }),
    });

    exe.linkLibC();

    // stb_image
    exe.addCSourceFile(.{
        .file = b.path("src/stb_impl.c"),
        .flags = &.{"-O2"},
    });
    exe.addIncludePath(b.path("vendor"));

    // libwebp - 编译源码
    const webp_flags = &[_][]const u8{
        "-O2",
        "-DWEBP_USE_THREAD",
    };

    // sharpyuv (编码器依赖)
    const sharpyuv_sources = &[_][]const u8{
        "vendor/libwebp/sharpyuv/sharpyuv.c",
        "vendor/libwebp/sharpyuv/sharpyuv_cpu.c",
        "vendor/libwebp/sharpyuv/sharpyuv_csp.c",
        "vendor/libwebp/sharpyuv/sharpyuv_dsp.c",
        "vendor/libwebp/sharpyuv/sharpyuv_gamma.c",
        "vendor/libwebp/sharpyuv/sharpyuv_sse2.c",
    };

    // utils
    const utils_sources = &[_][]const u8{
        "vendor/libwebp/src/utils/bit_reader_utils.c",
        "vendor/libwebp/src/utils/bit_writer_utils.c",
        "vendor/libwebp/src/utils/color_cache_utils.c",
        "vendor/libwebp/src/utils/filters_utils.c",
        "vendor/libwebp/src/utils/huffman_encode_utils.c",
        "vendor/libwebp/src/utils/huffman_utils.c",
        "vendor/libwebp/src/utils/palette.c",
        "vendor/libwebp/src/utils/quant_levels_dec_utils.c",
        "vendor/libwebp/src/utils/quant_levels_utils.c",
        "vendor/libwebp/src/utils/random_utils.c",
        "vendor/libwebp/src/utils/rescaler_utils.c",
        "vendor/libwebp/src/utils/thread_utils.c",
        "vendor/libwebp/src/utils/utils.c",
    };

    // dsp (通用 + SSE2)
    const dsp_sources = &[_][]const u8{
        "vendor/libwebp/src/dsp/alpha_processing.c",
        "vendor/libwebp/src/dsp/alpha_processing_sse2.c",
        "vendor/libwebp/src/dsp/cost.c",
        "vendor/libwebp/src/dsp/cost_sse2.c",
        "vendor/libwebp/src/dsp/cpu.c",
        "vendor/libwebp/src/dsp/dec.c",
        "vendor/libwebp/src/dsp/dec_sse2.c",
        "vendor/libwebp/src/dsp/dec_clip_tables.c",
        "vendor/libwebp/src/dsp/enc.c",
        "vendor/libwebp/src/dsp/enc_sse2.c",
        "vendor/libwebp/src/dsp/filters.c",
        "vendor/libwebp/src/dsp/filters_sse2.c",
        "vendor/libwebp/src/dsp/lossless.c",
        "vendor/libwebp/src/dsp/lossless_sse2.c",
        "vendor/libwebp/src/dsp/lossless_enc.c",
        "vendor/libwebp/src/dsp/lossless_enc_sse2.c",
        "vendor/libwebp/src/dsp/rescaler.c",
        "vendor/libwebp/src/dsp/rescaler_sse2.c",
        "vendor/libwebp/src/dsp/ssim.c",
        "vendor/libwebp/src/dsp/ssim_sse2.c",
        "vendor/libwebp/src/dsp/upsampling.c",
        "vendor/libwebp/src/dsp/upsampling_sse2.c",
        "vendor/libwebp/src/dsp/yuv.c",
        "vendor/libwebp/src/dsp/yuv_sse2.c",
    };

    // dsp SSE4.1 (需要单独的编译标志)
    const dsp_sse41_sources = &[_][]const u8{
        "vendor/libwebp/src/dsp/alpha_processing_sse41.c",
        "vendor/libwebp/src/dsp/dec_sse41.c",
        "vendor/libwebp/src/dsp/enc_sse41.c",
        "vendor/libwebp/src/dsp/lossless_sse41.c",
        "vendor/libwebp/src/dsp/lossless_enc_sse41.c",
        "vendor/libwebp/src/dsp/upsampling_sse41.c",
        "vendor/libwebp/src/dsp/yuv_sse41.c",
    };

    // dsp AVX2 (需要单独的编译标志)
    const dsp_avx2_sources = &[_][]const u8{
        "vendor/libwebp/src/dsp/lossless_avx2.c",
        "vendor/libwebp/src/dsp/lossless_enc_avx2.c",
    };

    // enc (编码器)
    const enc_sources = &[_][]const u8{
        "vendor/libwebp/src/enc/alpha_enc.c",
        "vendor/libwebp/src/enc/analysis_enc.c",
        "vendor/libwebp/src/enc/backward_references_cost_enc.c",
        "vendor/libwebp/src/enc/backward_references_enc.c",
        "vendor/libwebp/src/enc/config_enc.c",
        "vendor/libwebp/src/enc/cost_enc.c",
        "vendor/libwebp/src/enc/filter_enc.c",
        "vendor/libwebp/src/enc/frame_enc.c",
        "vendor/libwebp/src/enc/histogram_enc.c",
        "vendor/libwebp/src/enc/iterator_enc.c",
        "vendor/libwebp/src/enc/near_lossless_enc.c",
        "vendor/libwebp/src/enc/picture_csp_enc.c",
        "vendor/libwebp/src/enc/picture_enc.c",
        "vendor/libwebp/src/enc/picture_psnr_enc.c",
        "vendor/libwebp/src/enc/picture_rescale_enc.c",
        "vendor/libwebp/src/enc/picture_tools_enc.c",
        "vendor/libwebp/src/enc/predictor_enc.c",
        "vendor/libwebp/src/enc/quant_enc.c",
        "vendor/libwebp/src/enc/syntax_enc.c",
        "vendor/libwebp/src/enc/token_enc.c",
        "vendor/libwebp/src/enc/tree_enc.c",
        "vendor/libwebp/src/enc/vp8l_enc.c",
        "vendor/libwebp/src/enc/webp_enc.c",
    };

    // dec (解码器 - 编码器内部也用到)
    const dec_sources = &[_][]const u8{
        "vendor/libwebp/src/dec/alpha_dec.c",
        "vendor/libwebp/src/dec/buffer_dec.c",
        "vendor/libwebp/src/dec/frame_dec.c",
        "vendor/libwebp/src/dec/idec_dec.c",
        "vendor/libwebp/src/dec/io_dec.c",
        "vendor/libwebp/src/dec/quant_dec.c",
        "vendor/libwebp/src/dec/tree_dec.c",
        "vendor/libwebp/src/dec/vp8_dec.c",
        "vendor/libwebp/src/dec/vp8l_dec.c",
        "vendor/libwebp/src/dec/webp_dec.c",
    };

    // 添加所有源文件
    inline for (sharpyuv_sources) |src| {
        exe.addCSourceFile(.{ .file = b.path(src), .flags = webp_flags });
    }
    inline for (utils_sources) |src| {
        exe.addCSourceFile(.{ .file = b.path(src), .flags = webp_flags });
    }
    inline for (dsp_sources) |src| {
        exe.addCSourceFile(.{ .file = b.path(src), .flags = webp_flags });
    }
    inline for (dsp_sse41_sources) |src| {
        // SSE4.1 文件需要 -msse4.1 标志
        exe.addCSourceFile(.{ .file = b.path(src), .flags = &.{ "-O2", "-DWEBP_USE_THREAD", "-msse4.1" } });
    }
    inline for (dsp_avx2_sources) |src| {
        // AVX2 文件需要 -mavx2 标志
        exe.addCSourceFile(.{ .file = b.path(src), .flags = &.{ "-O2", "-DWEBP_USE_THREAD", "-mavx2" } });
    }
    inline for (enc_sources) |src| {
        exe.addCSourceFile(.{ .file = b.path(src), .flags = webp_flags });
    }
    inline for (dec_sources) |src| {
        exe.addCSourceFile(.{ .file = b.path(src), .flags = webp_flags });
    }

    // Include paths
    exe.addIncludePath(b.path("vendor/libwebp"));
    exe.addIncludePath(b.path("vendor/libwebp/src"));

    b.installArtifact(exe);

    const run_cmd = b.addRunArtifact(exe);
    run_cmd.step.dependOn(b.getInstallStep());
    const run_step = b.step("run", "Run the image processor");
    run_step.dependOn(&run_cmd.step);
}
