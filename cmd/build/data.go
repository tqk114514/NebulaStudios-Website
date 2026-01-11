/**
 * cmd/build/data.go
 * 数据文件构建模块
 *
 * 功能：
 * - 构建后端数据文件（JSON、HTML 等）
 * - JSON 压缩
 * - 文件复制
 */

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

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
