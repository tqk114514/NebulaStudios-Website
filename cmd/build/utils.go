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

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, srcInfo.Size())

	if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}

	written, err := io.Copy(dstFile, srcFile)
	if err != nil {
		_ = dstFile.Close()
		// 移除半成品文件，避免损坏的产物混入 dist 被部署
		_ = os.Remove(dst)
		return fmt.Errorf("failed to copy: %w", err)
	}

	// 先显式 Close 再检查错误：Close 时的落盘失败（如 ENOSPC）不能报成功
	if err := dstFile.Close(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("failed to close destination: %w", err)
	}

	atomic.AddInt64(&stats.BytesWritten, written)
	return nil
}

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

// replaceCDNURL 将内容中的构建期占位符替换为对应常量
func replaceCDNURL(content string) string {
	content = strings.ReplaceAll(content, "{{CDN_URL}}", cdnURL)
	content = strings.ReplaceAll(content, "{{TURNSTILE_SDK_URL}}", turnstileSDKURL)
	return content
}

// replaceCDNURLInFile 替换文件中的构建期占位符，无占位符时跳过
func replaceCDNURLInFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(data)
	if !strings.Contains(s, "{{CDN_URL}}") && !strings.Contains(s, "{{TURNSTILE_SDK_URL}}") {
		return nil
	}
	return os.WriteFile(path, []byte(replaceCDNURL(s)), filePerm)
}

func hashFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash)[:8], nil
}

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

	if err := os.Rename(originalPath, hashedPath); err != nil {
		return "", err
	}

	assetManifest[originalPath] = hashedName

	// 归一化为 / 分隔的相对路径键（Windows 下 filepath.Join 产生 \），供 HTML 资源引用改写精确匹配
	relPath := strings.TrimPrefix(filepath.ToSlash(originalPath), distDir+"/")
	assetManifest[relPath] = hashedName

	return hashedName, nil
}

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
	// 原子写：先写临时文件再 rename，避免进程中断留下截断的 manifest 被部署
	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, filePerm); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename manifest: %w", err)
	}

	log.Printf("[BUILD] Saved asset manifest with %d entries", len(assetManifest))
	return nil
}
