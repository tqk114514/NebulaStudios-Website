/**
 * cmd/build/css.go
 * CSS 构建模块
 *
 * 功能：
 * - 使用 esbuild 压缩 CSS
 * - 支持多模块构建
 */

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

// ====================  CSS 构建 ====================

// buildCSS 构建 CSS 文件
func buildCSS() error {
	log.Println("[BUILD] Building CSS...")

	// 构建共享 CSS
	if err := buildCSSModule(filepath.Join(sharedDir, "css/*.css"), "dist/shared/css", "shared"); err != nil {
		return err
	}

	// 构建 Account 模块 CSS
	if err := buildCSSModule("modules/account/assets/css/*.css", "dist/account/assets/css", "account"); err != nil {
		return err
	}

	// 构建 Policy 模块 CSS
	if err := buildCSSModule("modules/policy/assets/css/*.css", "dist/policy/assets/css", "policy"); err != nil {
		return err
	}

	// 构建 Admin 模块 CSS（合并为单个文件）
	if err := buildAdminCSS(); err != nil {
		return err
	}

	log.Println("[BUILD] CSS build completed")
	return nil
}

// buildAdminCSS 构建 Admin 模块 CSS（合并多个文件为一个）
func buildAdminCSS() error {
	// Admin CSS 按顺序合并：common.css -> admin.css
	cssFiles := []string{
		"modules/admin/assets/css/common.css",
		"modules/admin/assets/css/admin.css",
	}

	// 读取并合并所有 CSS 文件
	var combined []byte
	for _, file := range cssFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}
		combined = append(combined, data...)
		combined = append(combined, '\n')
	}

	// 使用 esbuild 压缩合并后的 CSS
	opts := api.TransformOptions{
		Loader: api.LoaderCSS,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifySyntax = true
	}

	result := api.Transform(string(combined), opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: admin CSS: %s", err.Text)
		}
		atomic.AddInt64(&stats.Errors, int64(len(result.Errors)))
		return fmt.Errorf("admin CSS build failed")
	}

	// 写入输出文件
	outFile := "dist/admin/assets/css/admin.css"
	if err := os.WriteFile(outFile, result.Code, filePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", outFile, err)
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(cssFiles)))
	atomic.AddInt64(&stats.BytesWritten, int64(len(result.Code)))
	log.Printf("[BUILD] Built admin CSS (merged %d files)", len(cssFiles))
	return nil
}

// buildCSSModule 构建单个 CSS 模块
func buildCSSModule(pattern, outdir, moduleName string) error {
	entries, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob %s CSS: %w", moduleName, err)
	}

	if len(entries) == 0 {
		log.Printf("[BUILD] WARN: No CSS files found for %s module", moduleName)
		return nil
	}

	opts := api.BuildOptions{
		EntryPoints: entries,
		Outdir:      outdir,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: %s CSS: %s", moduleName, err.Text)
		}
		atomic.AddInt64(&stats.Errors, int64(len(result.Errors)))
		return fmt.Errorf("%s CSS build failed", moduleName)
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(entries)))
	log.Printf("[BUILD] Built %d CSS files for %s module", len(entries), moduleName)
	return nil
}
