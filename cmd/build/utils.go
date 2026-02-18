/**
 * cmd/build/utils.go
 * 辅助函数模块
 *
 * 功能：
 * - 文件复制
 * - 字节格式化
 */

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

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

// AssetManifest 资源清单，用于哈希化文件名映射
type AssetManifest map[string]string

var assetManifest AssetManifest = make(AssetManifest)

// hashFile 计算文件的 SHA256 哈希前8位
func hashFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)[:8], nil
}

// addToManifest 将文件添加到清单并返回哈希化后的路径
func addToManifest(originalPath string) (string, error) {
	hash, err := hashFile(originalPath)
	if err != nil {
		return "", err
	}

	ext := filepath.Ext(originalPath)
	base := strings.TrimSuffix(filepath.Base(originalPath), ext)
	dir := filepath.Dir(originalPath)

	hashedName := fmt.Sprintf("%s.%s%s", base, hash, ext)
	hashedPath := filepath.Join(dir, hashedName)

	if err := copyFile(originalPath, hashedPath); err != nil {
		return "", err
	}

	// 存储完整路径映射
	assetManifest[originalPath] = hashedName

	// 同时存储相对路径映射（去掉 dist/ 前缀）
	relPath := strings.TrimPrefix(originalPath, "dist/")
	assetManifest[relPath] = hashedName

	return hashedName, nil
}

// saveAssetManifest 保存资源清单到 JSON 文件
func saveAssetManifest() error {
	if len(assetManifest) == 0 {
		log.Println("[BUILD] No assets to manifest")
		return nil
	}

	data, err := json.MarshalIndent(assetManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(distDir, "manifest.json")
	if err := os.WriteFile(manifestPath, data, filePerm); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	log.Printf("[BUILD] Saved asset manifest with %d entries", len(assetManifest))
	return nil
}
