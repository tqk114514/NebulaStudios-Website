/**
 * cmd/build/brotli.go
 * Brotli 压缩模块
 *
 * 功能：
 * - 使用 Brotli 预压缩静态文件
 * - 并行压缩提高效率
 * - 压缩后删除原文件
 */

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/andybalholm/brotli"
)

// Brotli 压缩级别
const brotliLevel = brotli.BestCompression

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
