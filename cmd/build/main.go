/**
 * cmd/build/main.go
 * 前端资源构建工具
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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/evanw/esbuild/pkg/api"
)

// ====================  常量定义 ====================

const (
	// 目录路径
	distDir          = "dist"
	sharedDir        = "shared"
	modulesDir       = "modules"
	dataDir          = "data"
	accountModule    = "account"
	policyModule     = "policy"

	// 文件权限
	dirPerm  = 0755
	filePerm = 0644

	// Brotli 压缩级别
	brotliLevel = brotli.BestCompression
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

	// 5. 构建 Policy 数据文件
	if err := buildPolicyData(); err != nil {
		return fmt.Errorf("policy data build failed: %w", err)
	}

	// 6. 构建 JavaScript
	if err := buildJS(); err != nil {
		return fmt.Errorf("JS build failed: %w", err)
	}

	// 7. 构建 CSS
	if err := buildCSS(); err != nil {
		return fmt.Errorf("CSS build failed: %w", err)
	}

	// 8. 构建 HTML
	if err := buildHTML(); err != nil {
		return fmt.Errorf("HTML build failed: %w", err)
	}

	// 9. 生产模式下生成 Brotli 预压缩文件
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
		"dist/policy/data",
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

// ====================  JavaScript 构建 ====================

// buildJS 构建 JavaScript 文件
func buildJS() error {
	log.Println("[BUILD] Building JavaScript...")

	// 验证入口文件存在
	if err := validateEntryPoints(accountPageEntries); err != nil {
		return fmt.Errorf("account entries validation failed: %w", err)
	}

	if err := validateEntryPoints(policyPageEntries); err != nil {
		return fmt.Errorf("policy entries validation failed: %w", err)
	}

	// 构建 Shared 模块（Turbo 初始化等）
	sharedJSEntries := []string{"shared/js/turbo-init.ts"}
	if err := buildJSModule(sharedJSEntries, "dist/shared/js", "shared"); err != nil {
		return err
	}

	// 构建 Account 模块
	if err := buildJSModule(accountPageEntries, "dist/account/assets/js", "account"); err != nil {
		return err
	}

	// 构建 Policy 模块
	if err := buildJSModule(policyPageEntries, "dist/policy/assets/js", "policy"); err != nil {
		return err
	}

	log.Println("[BUILD] JavaScript build completed")
	return nil
}

// validateEntryPoints 验证入口文件是否存在
func validateEntryPoints(entries []string) error {
	for _, entry := range entries {
		if _, err := os.Stat(entry); os.IsNotExist(err) {
			return fmt.Errorf("entry point not found: %s", entry)
		}
	}
	return nil
}

// buildJSModule 构建单个 JS 模块
func buildJSModule(entries []string, outdir, moduleName string) error {
	if len(entries) == 0 {
		log.Printf("[BUILD] WARN: No JS entries for %s module", moduleName)
		return nil
	}

	sourcemap := api.SourceMapNone
	if *isDev {
		sourcemap = api.SourceMapLinked
	}

	opts := api.BuildOptions{
		EntryPoints: entries,
		Bundle:      true,
		Outdir:      outdir,
		Sourcemap:   sourcemap,
		Target:      api.ES2020,
		Format:      api.FormatIIFE,
		TreeShaking: api.TreeShakingTrue,
		KeepNames:   *isDev,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	// 处理错误
	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: %s: %s", moduleName, err.Text)
			if err.Location != nil {
				log.Printf("[BUILD]   at %s:%d:%d", err.Location.File, err.Location.Line, err.Location.Column)
			}
		}
		atomic.AddInt64(&stats.Errors, int64(len(result.Errors)))
		return fmt.Errorf("%s JS build failed with %d errors", moduleName, len(result.Errors))
	}

	// 处理警告
	for _, warn := range result.Warnings {
		log.Printf("[BUILD] WARN: %s: %s", moduleName, warn.Text)
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(entries)))
	log.Printf("[BUILD] Built %d JS files for %s module", len(entries), moduleName)
	return nil
}

// ====================  翻译文件构建 ====================

// buildTranslations 合并所有 i18n 文件并生成 translations.js
func buildTranslations() error {
	log.Println("[BUILD] Building translations...")

	// 读取所有语言文件
	allTranslations := make(map[string]map[string]string)
	var totalBytes int64

	for _, lang := range supportedLanguages {
		filePath := filepath.Join(sharedDir, "i18n", lang+".json")

		data, err := os.ReadFile(filePath)
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("[BUILD] WARN: Language file not found: %s", filePath)
				continue
			}
			return fmt.Errorf("failed to read %s: %w", filePath, err)
		}

		totalBytes += int64(len(data))

		var langData map[string]string
		if err := json.Unmarshal(data, &langData); err != nil {
			return fmt.Errorf("failed to parse %s: %w", filePath, err)
		}

		if len(langData) == 0 {
			log.Printf("[BUILD] WARN: Empty language file: %s", filePath)
		}

		allTranslations[lang] = langData
	}

	atomic.AddInt64(&stats.BytesRead, totalBytes)

	if len(allTranslations) == 0 {
		return errors.New("no translation files found")
	}

	// 序列化为 JSON
	translationsJSON, err := json.Marshal(allTranslations)
	if err != nil {
		return fmt.Errorf("failed to marshal translations: %w", err)
	}

	// 读取 translations.ts 模板
	templatePath := filepath.Join(sharedDir, "js", "translations.ts")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read translations.ts template: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(templateData)))

	// 在文件开头注入所有翻译数据
	injectedCode := fmt.Sprintf("const __ALL_TRANSLATIONS__ = %s;\n\n", string(translationsJSON))
	output := injectedCode + string(templateData)

	// 写入临时文件
	tmpFile := filepath.Join(distDir, "shared/js/translations.tmp.ts")
	if err := os.WriteFile(tmpFile, []byte(output), filePerm); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	defer func() {
		if err := os.Remove(tmpFile); err != nil {
			log.Printf("[BUILD] WARN: Failed to remove temp file: %v", err)
		}
	}() // 确保清理临时文件

	// 使用 esbuild 压缩
	sourcemap := api.SourceMapNone
	if *isDev {
		sourcemap = api.SourceMapLinked
	}

	opts := api.BuildOptions{
		EntryPoints: []string{tmpFile},
		Outfile:     filepath.Join(distDir, "shared/js/translations.js"),
		Sourcemap:   sourcemap,
		Target:      api.ES2020,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: translations: %s", err.Text)
		}
		return errors.New("translations.js build failed")
	}

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Printf("[BUILD] Built translations.js with %d languages", len(allTranslations))
	return nil
}


// ====================  数据文件构建 ====================

// buildBackendData 构建后端数据文件（压缩但不生成 br）
func buildBackendData() error {
	log.Println("[BUILD] Building backend data...")

	files, err := filepath.Glob(filepath.Join(dataDir, "*"))
	if err != nil {
		return fmt.Errorf("failed to glob data files: %w", err)
	}

	if len(files) == 0 {
		log.Println("[BUILD] WARN: No backend data files found")
		return nil
	}

	var processedCount int
	for _, src := range files {
		filename := filepath.Base(src)
		dst := filepath.Join(distDir, dataDir, filename)
		ext := strings.ToLower(filepath.Ext(src))

		var err error
		switch ext {
		case ".json":
			err = minifyJSONFile(src, dst)
		case ".html":
			err = minifyHTMLFileTo(src, dst)
		default:
			err = copyFile(src, dst)
		}

		if err != nil {
			return fmt.Errorf("failed to process %s: %w", src, err)
		}
		processedCount++
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(processedCount))
	log.Printf("[BUILD] Processed %d backend data files", processedCount)
	return nil
}

// buildAccountData 构建 Account 数据文件
func buildAccountData() error {
	log.Println("[BUILD] Building account data...")

	src := filepath.Join(dataDir, "email.json")
	dst := filepath.Join(distDir, "account/data/email.json")

	if _, err := os.Stat(src); os.IsNotExist(err) {
		log.Printf("[BUILD] WARN: Account data file not found: %s", src)
		return nil
	}

	if err := minifyJSONFile(src, dst); err != nil {
		return fmt.Errorf("failed to build account data: %w", err)
	}

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Println("[BUILD] Account data built")
	return nil
}

// buildPolicyData 构建 Policy 数据文件
func buildPolicyData() error {
	log.Println("[BUILD] Building policy data...")

	src := filepath.Join(modulesDir, "policy/data/i18n-policy.json")
	dst := filepath.Join(distDir, "policy/data/i18n-policy.json")

	if _, err := os.Stat(src); os.IsNotExist(err) {
		log.Printf("[BUILD] WARN: Policy data file not found: %s", src)
		return nil
	}

	if err := minifyJSONFile(src, dst); err != nil {
		return fmt.Errorf("failed to build policy data: %w", err)
	}

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Println("[BUILD] Policy data built")
	return nil
}

// minifyJSONFile 压缩 JSON 文件
func minifyJSONFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	// 验证 JSON 格式
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// 压缩输出（无缩进）
	minified, err := json.Marshal(jsonData)
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	if err := os.WriteFile(dst, minified, filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

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

	log.Println("[BUILD] CSS build completed")
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

// ====================  HTML 构建 ====================

// HTML 压缩用正则（预编译，提高性能）
var (
	htmlCommentRe    = regexp.MustCompile(`<!--[\s\S]*?-->`)
	htmlWhitespaceRe = regexp.MustCompile(`\s+`)
	htmlTagSpaceRe   = regexp.MustCompile(`>\s+<`)
)

// buildHTML 构建 HTML 文件
func buildHTML() error {
	log.Println("[BUILD] Building HTML...")

	// 构建 Account 页面
	if err := buildHTMLModule("modules/account/pages/*.html", "dist/account/pages", "account"); err != nil {
		return err
	}

	// 构建 Policy 页面
	if err := buildHTMLModule("modules/policy/pages/*.html", "dist/policy/pages", "policy"); err != nil {
		return err
	}

	// 构建 shared/components
	if err := buildHTMLModule("shared/components/*.html", "dist/shared/components", "components"); err != nil {
		return err
	}

	log.Println("[BUILD] HTML build completed")
	return nil
}

// loadHeaderComponent 加载 header.html 组件内容
func loadHeaderComponent() (string, error) {
	headerPath := filepath.Join(sharedDir, "components", "header.html")
	data, err := os.ReadFile(headerPath)
	if err != nil {
		return "", fmt.Errorf("failed to read header.html: %w", err)
	}
	return string(data), nil
}

// buildHTMLModule 构建单个 HTML 模块
func buildHTMLModule(pattern, outdir, moduleName string) error {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob %s HTML: %w", moduleName, err)
	}

	if len(files) == 0 {
		log.Printf("[BUILD] WARN: No HTML files found for %s", moduleName)
		return nil
	}

	// 加载 header 组件（用于替换 {{HEADER}}）
	headerContent, err := loadHeaderComponent()
	if err != nil {
		log.Printf("[BUILD] WARN: Failed to load header component: %v", err)
		headerContent = "" // 继续构建，但不替换
	}

	for _, src := range files {
		// 跳过 header.html 本身（不需要替换）
		if filepath.Base(src) == "header.html" {
			if err := minifyHTMLFile(src, outdir); err != nil {
				return fmt.Errorf("failed to minify %s: %w", src, err)
			}
			continue
		}

		if err := minifyHTMLFileWithHeader(src, outdir, headerContent); err != nil {
			return fmt.Errorf("failed to minify %s: %w", src, err)
		}
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(files)))
	log.Printf("[BUILD] Built %d HTML files for %s", len(files), moduleName)
	return nil
}

// minifyHTMLFile 压缩单个 HTML 文件到指定目录
func minifyHTMLFile(src, outDir string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	minified := minifyHTML(string(data))
	filename := filepath.Base(src)
	dst := filepath.Join(outDir, filename)

	// 确保目标目录存在
	if err := os.MkdirAll(outDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	if err := os.WriteFile(dst, []byte(minified), filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

// minifyHTMLFileWithHeader 压缩 HTML 文件并替换 {{HEADER}} 占位符
func minifyHTMLFileWithHeader(src, outDir, headerContent string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	// 替换 {{HEADER}} 占位符
	content := string(data)
	if headerContent != "" {
		content = strings.ReplaceAll(content, "{{HEADER}}", headerContent)
	}

	// 注入 Turbo Drive 脚本（在 </head> 前插入）
	turboScript := `<script type="module" src="https://unpkg.com/@hotwired/turbo@8.0.12/dist/turbo.es2017-esm.js"></script><script type="module" src="/shared/js/turbo-init.js"></script>`
	content = strings.Replace(content, "</head>", turboScript+"</head>", 1)

	minified := minifyHTML(content)
	filename := filepath.Base(src)
	dst := filepath.Join(outDir, filename)

	// 确保目标目录存在
	if err := os.MkdirAll(outDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	if err := os.WriteFile(dst, []byte(minified), filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

// minifyHTMLFileTo 压缩单个 HTML 文件到指定路径
func minifyHTMLFileTo(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	minified := minifyHTML(string(data))

	if err := os.WriteFile(dst, []byte(minified), filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

// minifyHTML 压缩 HTML（去空白、注释）
func minifyHTML(html string) string {
	if html == "" {
		return ""
	}

	// 移除 HTML 注释
	html = htmlCommentRe.ReplaceAllString(html, "")

	// 移除多余空白（保留单个空格）
	html = htmlWhitespaceRe.ReplaceAllString(html, " ")

	// 移除标签间的空白
	html = htmlTagSpaceRe.ReplaceAllString(html, "><")

	// 移除首尾空白
	html = strings.TrimSpace(html)

	return html
}


// ====================  Brotli 压缩 ====================

// brotliCompressDir 使用 Brotli 预压缩目录中的文件
func brotliCompressDir(dir string) error {
	log.Println("[BUILD] Compressing with Brotli...")

	var (
		compressedCount int64
		totalOriginal   int64
		totalCompressed int64
		mu              sync.Mutex
		wg              sync.WaitGroup
		errChan         = make(chan error, 100)
	)

	// 收集需要压缩的文件
	var filesToCompress []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("[BUILD] WARN: Walk error for %s: %v", path, err)
			return nil // 继续处理其他文件
		}

		if info.IsDir() {
			return nil
		}

		// 跳过已压缩的文件
		if strings.HasSuffix(path, ".br") {
			return nil
		}

		// 跳过 dist/data 目录（后端数据不压缩）
		if strings.HasPrefix(filepath.ToSlash(path), "dist/data/") {
			return nil
		}

		// 只压缩 js, css, html, json 文件
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".js" || ext == ".css" || ext == ".html" || ext == ".json" {
			filesToCompress = append(filesToCompress, path)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	if len(filesToCompress) == 0 {
		log.Println("[BUILD] WARN: No files to compress")
		return nil
	}

	// 并行压缩（限制并发数）
	semaphore := make(chan struct{}, 4) // 最多 4 个并发

	for _, path := range filesToCompress {
		wg.Add(1)
		go func(filePath string) {
			defer wg.Done()

			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			original, compressed, err := brotliFile(filePath)
			if err != nil {
				errChan <- fmt.Errorf("%s: %w", filePath, err)
				return
			}

			mu.Lock()
			compressedCount++
			totalOriginal += original
			totalCompressed += compressed
			mu.Unlock()
		}(path)
	}

	wg.Wait()
	close(errChan)

	// 收集错误
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
		log.Printf("[BUILD] WARN: Brotli compression failed: %v", err)
	}

	// 计算压缩率
	var ratio float64
	if totalOriginal > 0 {
		ratio = float64(totalCompressed) / float64(totalOriginal) * 100
	}

	log.Printf("[BUILD] Brotli: compressed %d files, %s -> %s (%.1f%%)",
		compressedCount,
		formatBytes(totalOriginal),
		formatBytes(totalCompressed),
		ratio)

	if len(errs) > 0 {
		return fmt.Errorf("%d files failed to compress", len(errs))
	}

	return nil
}

// brotliFile 使用 Brotli 压缩单个文件，压缩后删除原文件
// 返回原始大小和压缩后大小
func brotliFile(src string) (int64, int64, error) {
	// 读取原文件
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read: %w", err)
	}

	originalSize := int64(len(data))

	// 跳过空文件
	if originalSize == 0 {
		log.Printf("[BUILD] WARN: Skipping empty file: %s", src)
		return 0, 0, nil
	}

	// 创建 .br 文件
	brPath := src + ".br"
	brFile, err := os.Create(brPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create .br file: %w", err)
	}

	// 使用 Brotli 压缩
	brWriter := brotli.NewWriterLevel(brFile, brotliLevel)
	_, err = brWriter.Write(data)
	if err != nil {
		_ = brFile.Close()
		_ = os.Remove(brPath) // 清理失败的文件
		return 0, 0, fmt.Errorf("failed to write compressed data: %w", err)
	}

	if err := brWriter.Close(); err != nil {
		_ = brFile.Close()
		_ = os.Remove(brPath)
		return 0, 0, fmt.Errorf("failed to close brotli writer: %w", err)
	}

	if err := brFile.Close(); err != nil {
		_ = os.Remove(brPath)
		return 0, 0, fmt.Errorf("failed to close file: %w", err)
	}

	// 获取压缩后大小
	brInfo, err := os.Stat(brPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to stat .br file: %w", err)
	}
	compressedSize := brInfo.Size()

	// 删除原文件，只保留 .br
	if err := os.Remove(src); err != nil {
		log.Printf("[BUILD] WARN: Failed to remove original file %s: %v", src, err)
	}

	return originalSize, compressedSize, nil
}

// ====================  辅助函数 ====================

// copyFile 复制文件
func copyFile(src, dst string) error {
	// 打开源文件
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	// 获取源文件信息
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, srcInfo.Size())

	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// 创建目标文件
	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	// 复制内容
	written, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, written)
	return nil
}

// formatBytes 格式化字节数为人类可读格式
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
	)

	switch {
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
