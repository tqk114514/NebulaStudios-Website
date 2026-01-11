/**
 * cmd/build/main.go
 * 前端资源构建工具 - 入口文件
 *
 * 功能：
 * - JS 压缩优化（使用 esbuild）
 * - CSS 压缩
 * - HTML 压缩（去空白、注释）
 * - i18n 合并（所有语言打包到 translations.js）
 * - Brotli 预压缩
 * - 输出到 dist/ 目录
 *
 * 用法：
 *   go run ./cmd/build        # 生产构建（压缩 + Brotli）
 *   go run ./cmd/build -dev   # 开发模式（不压缩，保留 sourcemap）
 *
 * 依赖：
 * - github.com/evanw/esbuild/pkg/api
 * - github.com/andybalholm/brotli
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

// ====================  常量定义 ====================

const (
	// 目录路径
	distDir    = "dist"
	sharedDir  = "shared"
	modulesDir = "modules"
	dataDir    = "data"

	// 文件权限
	dirPerm  = 0755
	filePerm = 0644
)

// ====================  配置 ====================

var (
	// 命令行参数
	isDev = flag.Bool("dev", false, "Development mode (no minification, with sourcemap)")

	// Account 模块页面入口文件
	accountPageEntries = []string{
		"modules/account/assets/js/login.ts",
		"modules/account/assets/js/register.ts",
		"modules/account/assets/js/verify.ts",
		"modules/account/assets/js/forgot.ts",
		"modules/account/assets/js/dashboard.ts",
		"modules/account/assets/js/link.ts",
		"modules/account/assets/js/404.ts",
	}

	// Policy 模块页面入口文件
	policyPageEntries = []string{
		"modules/policy/assets/js/policy.ts",
	}

	// Admin 模块页面入口文件（完全独立，不依赖 shared）
	adminPageEntries = []string{
		"modules/admin/assets/js/admin.ts",
	}

	// 支持的语言列表
	supportedLanguages = []string{
		"zh-CN", "zh-TW", "en", "ja", "ko",
	}
)

// ====================  构建统计 ====================

// BuildStats 构建统计信息
type BuildStats struct {
	FilesProcessed int64
	BytesRead      int64
	BytesWritten   int64
	Errors         int64
}

var stats BuildStats

// ====================  主函数 ====================

func main() {
	flag.Parse()

	startTime := time.Now()
	mode := "production"
	if *isDev {
		mode = "development"
	}

	log.Printf("[BUILD] Starting build in %s mode...", mode)

	// 运行构建
	if err := run(); err != nil {
		log.Fatalf("[BUILD] FATAL: Build failed: %v", err)
	}

	elapsed := time.Since(startTime)
	log.Printf("[BUILD] Completed successfully in %dms", elapsed.Milliseconds())
	log.Printf("[BUILD] Stats: files=%d, read=%s, written=%s",
		stats.FilesProcessed,
		formatBytes(stats.BytesRead),
		formatBytes(stats.BytesWritten))
}

// run 执行构建流程
func run() error {
	// 1. 清理并创建 dist 目录
	if err := setupDistDir(); err != nil {
		return fmt.Errorf("setup dist dir failed: %w", err)
	}

	// 2. 构建后端数据文件
	if err := buildBackendData(); err != nil {
		return fmt.Errorf("backend data build failed: %w", err)
	}

	// 3. 合并 i18n 并构建 translations.js
	if err := buildTranslations(); err != nil {
		return fmt.Errorf("translations build failed: %w", err)
	}

	// 4. 构建 Account 数据文件
	if err := buildAccountData(); err != nil {
		return fmt.Errorf("account data build failed: %w", err)
	}

	// 5. 构建 JavaScript（包含 Policy 数据内嵌）
	if err := buildJS(); err != nil {
		return fmt.Errorf("JS build failed: %w", err)
	}

	// 6. 构建 CSS
	if err := buildCSS(); err != nil {
		return fmt.Errorf("CSS build failed: %w", err)
	}

	// 7. 构建 HTML
	if err := buildHTML(); err != nil {
		return fmt.Errorf("HTML build failed: %w", err)
	}

	// 8. 生产模式下生成 Brotli 预压缩文件
	if !*isDev {
		if err := brotliCompressDir(distDir); err != nil {
			log.Printf("[BUILD] WARN: Brotli compression had errors: %v", err)
		}
	}

	return nil
}

// ====================  目录设置 ====================

// setupDistDir 清理并创建 dist 目录结构
func setupDistDir() error {
	log.Println("[BUILD] Setting up dist directory...")

	// 清理旧的 dist 目录
	if err := os.RemoveAll(distDir); err != nil {
		log.Printf("[BUILD] WARN: Failed to remove old dist dir: %v", err)
	}

	// 创建目录结构
	dirs := []string{
		"dist/shared/js",
		"dist/shared/css",
		"dist/shared/components",
		"dist/account/assets/js",
		"dist/account/assets/css",
		"dist/account/pages",
		"dist/account/data",
		"dist/policy/assets/js",
		"dist/policy/assets/css",
		"dist/policy/pages",
		"dist/admin/assets/js",
		"dist/admin/assets/css",
		"dist/admin/pages",
		"dist/data",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	log.Printf("[BUILD] Created %d directories", len(dirs))
	return nil
}
