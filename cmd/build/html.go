/**
 * cmd/build/html.go
 * HTML 构建模块
 *
 * 功能：
 * - HTML 压缩（去空白、注释）
 * - 支持 header 组件替换
 * - 支持多模块构建
 */

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
)

// HTML 压缩用正则（预编译，提高性能）
var (
	htmlCommentRe    = regexp.MustCompile(`<!--[\s\S]*?-->`)
	htmlWhitespaceRe = regexp.MustCompile(`\s+`)
	htmlTagSpaceRe   = regexp.MustCompile(`>\s+<`)
)

// ====================  HTML 构建 ====================

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

	// 构建 Admin 页面（独立，不使用 header 组件）
	if err := buildHTMLModuleSimple("modules/admin/pages/*.html", "dist/admin/pages", "admin"); err != nil {
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

// buildHTMLModule 构建单个 HTML 模块（支持 header 组件替换）
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

// buildHTMLModuleSimple 构建简单 HTML 模块（不替换 header 组件）
// 用于 Admin 等独立模块
func buildHTMLModuleSimple(pattern, outdir, moduleName string) error {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob %s HTML: %w", moduleName, err)
	}

	if len(files) == 0 {
		log.Printf("[BUILD] WARN: No HTML files found for %s", moduleName)
		return nil
	}

	for _, src := range files {
		if err := minifyHTMLFile(src, outdir); err != nil {
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
