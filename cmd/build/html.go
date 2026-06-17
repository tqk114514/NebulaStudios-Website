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

var (
	htmlCommentRe    = regexp.MustCompile(`<!--[\s\S]*?-->`)
	htmlWhitespaceRe = regexp.MustCompile(`\s+`)
	htmlTagSpaceRe   = regexp.MustCompile(`>\s+<`)
	scriptBlockRe    = regexp.MustCompile(`(?i)<script\b[^>]*>[\s\S]*?</script>`)
	styleBlockRe     = regexp.MustCompile(`(?i)<style\b[^>]*>[\s\S]*?</style>`)
)

func loadInitLangScript() (string, error) {
	scriptPath := filepath.Join(sharedDir, "js", "init-lang-inline.js")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read init-lang-inline.js: %w", err)
	}
	return fmt.Sprintf("<script nonce=\"{{CSP_NONCE}}\">%s</script>", string(data)), nil
}

func buildHTML() error {
	log.Println("[BUILD] Building HTML...")

	initLangScript, err := loadInitLangScript()
	if err != nil {
		log.Printf("[BUILD] WARN: Failed to load init-lang script: %v", err)
		initLangScript = ""
	}

	if err := buildHTMLModule("modules/home/pages/*.html", "dist/home/pages", "home", initLangScript); err != nil {
		return err
	}

	if err := buildHTMLModule("modules/account/pages/*.html", "dist/account/pages", "account", initLangScript); err != nil {
		return err
	}

	if err := buildHTMLModule("modules/policy/pages/*.html", "dist/policy/pages", "policy", initLangScript); err != nil {
		return err
	}

	if err := buildHTMLModuleSimple("modules/admin/pages/*.html", "dist/admin/pages", "admin"); err != nil {
		return err
	}

	if err := buildHTMLModule("shared/components/*.html", "dist/shared/components", "components", initLangScript); err != nil {
		return err
	}

	log.Println("[BUILD] HTML build completed")
	return nil
}

func loadHeaderComponent() (string, error) {
	headerPath := filepath.Join(sharedDir, "components", "header.html")
	data, err := os.ReadFile(headerPath)
	if err != nil {
		return "", fmt.Errorf("failed to read header.html: %w", err)
	}
	return string(data), nil
}

func buildHTMLModule(pattern, outdir, moduleName, initLangScript string) error {
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob %s HTML: %w", moduleName, err)
	}

	if len(files) == 0 {
		log.Printf("[BUILD] WARN: No HTML files found for %s", moduleName)
		return nil
	}

	headerContent, err := loadHeaderComponent()
	if err != nil {
		log.Printf("[BUILD] WARN: Failed to load header component: %v", err)
		headerContent = ""
	}

	for _, src := range files {
		if filepath.Base(src) == "header.html" {
			if err := minifyHTMLFile(src, outdir); err != nil {
				return fmt.Errorf("failed to minify %s: %w", src, err)
			}
			continue
		}

		if err := minifyHTMLFileWithHeader(src, outdir, headerContent, initLangScript); err != nil {
			return fmt.Errorf("failed to minify %s: %w", src, err)
		}
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(files)))
	log.Printf("[BUILD] Built %d HTML files for %s", len(files), moduleName)
	return nil
}

// buildHTMLModuleSimple 构建简单 HTML 模块，不替换 header 组件（用于 Admin 等独立模块）
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

func minifyHTMLFile(src, outDir string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	content := string(data)
	content = replaceAssetRefs(content)
	content = replaceCDNURL(content)
	minified := minifyHTML(content)
	filename := filepath.Base(src)
	dst := filepath.Join(outDir, filename)

	if err := os.MkdirAll(outDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	if err := os.WriteFile(dst, []byte(minified), filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

func minifyHTMLFileWithHeader(src, outDir, headerContent, initLangScript string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	content := string(data)
	if headerContent != "" {
		content = strings.ReplaceAll(content, "{{HEADER}}", headerContent)
	}

	if initLangScript != "" {
		content = strings.ReplaceAll(content, "</head>", initLangScript+"</head>")
	}

	content = replaceAssetRefs(content)
	content = replaceCDNURL(content)

	minified := minifyHTML(content)
	filename := filepath.Base(src)
	dst := filepath.Join(outDir, filename)

	if err := os.MkdirAll(outDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	if err := os.WriteFile(dst, []byte(minified), filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

func minifyHTMLFileTo(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	content := string(data)
	content = replaceAssetRefs(content)
	content = replaceCDNURL(content)
	minified := minifyHTML(content)

	if err := os.WriteFile(dst, []byte(minified), filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

func minifyHTML(html string) string {
	if html == "" {
		return ""
	}

	var preservedBlocks []string

	placeholder := func(block string) string {
		idx := len(preservedBlocks)
		preservedBlocks = append(preservedBlocks, block)
		return fmt.Sprintf("\x00PRESERVE%d\x00", idx)
	}

	html = scriptBlockRe.ReplaceAllStringFunc(html, func(match string) string {
		return placeholder(match)
	})
	html = styleBlockRe.ReplaceAllStringFunc(html, func(match string) string {
		return placeholder(match)
	})

	html = htmlCommentRe.ReplaceAllString(html, "")
	html = htmlWhitespaceRe.ReplaceAllString(html, " ")
	html = htmlTagSpaceRe.ReplaceAllString(html, "><")
	html = strings.TrimSpace(html)

	for i, block := range preservedBlocks {
		html = strings.Replace(html, fmt.Sprintf("\x00PRESERVE%d\x00", i), block, 1)
	}

	return html
}

func replaceAssetRefs(html string) string {
	re := regexp.MustCompile(`(href|src)=["']([^"']+\.(css|js))["']`)

	return re.ReplaceAllStringFunc(html, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}

		attr := parts[1]
		original := parts[2]
		_ = parts[3] // css 或 js

		for originalPath, hashedName := range assetManifest {
			originalBase := filepath.Base(originalPath)
			if strings.HasSuffix(original, originalBase) {
				newPath := strings.Replace(original, filepath.Base(originalPath), hashedName, 1)
				return fmt.Sprintf(`%s="%s"`, attr, newPath)
			}
		}

		return match
	})
}
