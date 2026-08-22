package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

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
		// data 下的子目录（如运行时本地存储 data/avatars）不复制进 dist
		info, err := os.Stat(src)
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", src, err)
		}
		if info.IsDir() {
			continue
		}

		filename := filepath.Base(src)
		dst := filepath.Join(distDir, dataDir, filename)
		ext := strings.ToLower(filepath.Ext(src))

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

func minifyJSONFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(data)))

	// json.Compact 做纯词法压缩（去空白），不经过 any round-trip：
	// Unmarshal→Marshal 会把所有数字变 float64（大整数丢精度、1.0 改写成 1）
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	minified := buf.Bytes()

	if err := os.WriteFile(dst, minified, filePerm); err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, int64(len(minified)))
	return nil
}

func buildPolicyMarkdown() error {
	log.Println("[BUILD] Building policy markdown files...")

	policyTypes := []string{"privacy", "terms", "cookies"}
	supportedLanguages := []string{"zh-CN", "zh-TW", "en", "ja", "ko"}
	var processedCount int

	for _, policyType := range policyTypes {
		for _, lang := range supportedLanguages {
			srcDir := filepath.Join(sharedDir, "i18n/policy", policyType, lang)
			dstDir := filepath.Join(distDir, "shared/i18n/policy", policyType, lang)

			if err := os.MkdirAll(dstDir, dirPerm); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstDir, err)
			}

			entries, err := os.ReadDir(srcDir)
			if err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("failed to read directory %s: %w", srcDir, err)
				}
				continue
			}

			for _, entry := range entries {
				if !entry.IsDir() && filepath.Ext(entry.Name()) == ".md" {
					src := filepath.Join(srcDir, entry.Name())
					dst := filepath.Join(dstDir, entry.Name())

					if err := copyFile(src, dst); err != nil {
						return fmt.Errorf("failed to copy %s: %w", src, err)
					}
					processedCount++
				}
			}
		}
	}

	// 复制 manifest.json（政策版本清单，后端读取以获取各版本的公示/生效日期）
	manifestSrc := filepath.Join(sharedDir, "i18n/policy/manifest.json")
	manifestDst := filepath.Join(distDir, "shared/i18n/policy/manifest.json")
	if err := copyFile(manifestSrc, manifestDst); err != nil {
		return fmt.Errorf("failed to copy policy manifest: %w", err)
	}
	processedCount++

	atomic.AddInt64(&stats.FilesProcessed, int64(processedCount))
	log.Printf("[BUILD] Processed %d policy markdown files (incl. manifest)", processedCount)
	return nil
}
