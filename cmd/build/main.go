// 前端资源构建工具，支持 JS/CSS/HTML 压缩、i18n 合并、Brotli 预压缩。
// 用法：go run ./cmd/build （生产）或 go run ./cmd/build -dev（开发模式）。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

const (
	distDir    = "dist"
	sharedDir  = "shared"
	modulesDir = "modules"
	dataDir    = "data"

	dirPerm  = 0755
	filePerm = 0644
)

var (
	isDev = flag.Bool("dev", false, "Development mode (no minification, with sourcemap)")

	homePageEntries = []string{
		"modules/home/assets/js/home.ts",
	}

	accountPageEntries = []string{
		"modules/account/assets/js/login.ts",
		"modules/account/assets/js/register.ts",
		"modules/account/assets/js/verify.ts",
		"modules/account/assets/js/forgot.ts",
		"modules/account/assets/js/dashboard.ts",
		"modules/account/assets/js/link.ts",
		"modules/account/assets/js/oauth.ts",
		"modules/account/assets/js/404.ts",
	}

	policyPageEntries = []string{
		"modules/policy/assets/js/policy.ts",
	}

	adminPageEntries = []string{
		"modules/admin/assets/js/admin.ts",
	}

	supportedLanguages = []string{
		"zh-CN", "zh-TW", "en", "ja", "ko",
	}
)

type BuildStats struct {
	FilesProcessed int64
	BytesRead      int64
	BytesWritten   int64
	Errors         int64
}

var stats BuildStats

func main() {
	flag.Parse()

	startTime := time.Now()
	mode := "production"
	if *isDev {
		mode = "development"
	}

	log.Printf("[BUILD] Starting build in %s mode...", mode)

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

func run() error {
	if err := setupDistDir(); err != nil {
		return fmt.Errorf("setup dist dir failed: %w", err)
	}

	if err := buildBackendData(); err != nil {
		return fmt.Errorf("backend data build failed: %w", err)
	}

	if err := buildTranslations(); err != nil {
		return fmt.Errorf("translations build failed: %w", err)
	}

	if err := buildCookieConsent(); err != nil {
		return fmt.Errorf("cookie-consent build failed: %w", err)
	}

	if err := buildJS(); err != nil {
		return fmt.Errorf("JS build failed: %w", err)
	}

	if err := buildCSS(); err != nil {
		return fmt.Errorf("CSS build failed: %w", err)
	}

	if err := buildHTML(); err != nil {
		return fmt.Errorf("HTML build failed: %w", err)
	}

	if err := buildPolicyMarkdown(); err != nil {
		return fmt.Errorf("policy markdown build failed: %w", err)
	}

	if err := saveAssetManifest(); err != nil {
		log.Printf("[BUILD] WARN: Failed to save asset manifest: %v", err)
	}

	// 生产模式下为所有静态文件生成 Brotli 预压缩版本，服务端可零运行时开销直接发送 .br 文件
	if !*isDev {
		if err := brotliCompressDir(distDir); err != nil {
			log.Printf("[BUILD] WARN: Brotli compression had errors: %v", err)
		}
	}

	return nil
}

func setupDistDir() error {
	log.Println("[BUILD] Setting up dist directory...")

	if err := os.RemoveAll(distDir); err != nil {
		log.Printf("[BUILD] WARN: Failed to remove old dist dir: %v", err)
	}

	dirs := []string{
		"dist/shared/js",
		"dist/shared/css",
		"dist/shared/components",
		"dist/shared/i18n/policy/privacy",
		"dist/shared/i18n/policy/terms",
		"dist/shared/i18n/policy/cookies",
		"dist/home/assets/js",
		"dist/home/assets/css",
		"dist/home/pages",
		"dist/account/assets/js",
		"dist/account/assets/css",
		"dist/account/pages",
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
