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

const brotliLevel = brotli.BestCompression

// brotliCompressDir 使用 Brotli 预压缩 dist 目录中的静态文件，运行时根据浏览器支持选择发送原文件或 .br 压缩版本
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

	var filesToCompress []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("[BUILD] WARN: Walk error for %s: %v", path, err)
			return nil
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, ".br") {
			return nil
		}

		// dist/data 是后端数据，运行时可能被读取，不预压缩
		if strings.HasPrefix(filepath.ToSlash(path), "dist/data/") {
			return nil
		}

		// HTML 需要运行时替换 CSP nonce，不预压缩
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".js" || ext == ".css" || ext == ".json" || ext == ".md" {
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

	semaphore := make(chan struct{}, 4)

	for _, path := range filesToCompress {
		wg.Add(1)
		go func(filePath string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

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

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
		log.Printf("[BUILD] WARN: Brotli compression failed: %v", err)
	}

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

func brotliFile(src string) (int64, int64, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read: %w", err)
	}

	originalSize := int64(len(data))

	if originalSize == 0 {
		log.Printf("[BUILD] WARN: Skipping empty file: %s", src)
		return 0, 0, nil
	}

	brPath := src + ".br"
	brFile, err := os.Create(brPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create .br file: %w", err)
	}

	brWriter := brotli.NewWriterLevel(brFile, brotliLevel)
	_, err = brWriter.Write(data)
	if err != nil {
		_ = brFile.Close()
		_ = os.Remove(brPath)
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

	brInfo, err := os.Stat(brPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to stat .br file: %w", err)
	}
	compressedSize := brInfo.Size()

	return originalSize, compressedSize, nil
}
