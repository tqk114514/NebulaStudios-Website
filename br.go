package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/andybalholm/brotli"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "用法: go run br.go <文件名> [压缩级别]\n")
		fmt.Fprintf(os.Stderr, "将指定文件压缩为同目录下的 .br 文件\n")
		fmt.Fprintf(os.Stderr, "压缩级别: 0-11 (默认 11, 0=最快, 11=最小)\n")
		os.Exit(1)
	}

	src := os.Args[1]

	level := brotli.BestCompression
	if len(os.Args) >= 3 {
		l, err := strconv.Atoi(os.Args[2])
		if err != nil || l < 0 || l > 11 {
			fmt.Fprintf(os.Stderr, "错误: 压缩级别必须为 0-11 的整数\n")
			os.Exit(1)
		}
		level = l
	}

	info, err := os.Stat(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}
	if info.IsDir() {
		fmt.Fprintf(os.Stderr, "错误: %s 是目录，不是文件\n", src)
		os.Exit(1)
	}

	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 读取文件失败: %v\n", err)
		os.Exit(1)
	}

	if len(data) == 0 {
		fmt.Fprintf(os.Stderr, "错误: 文件为空\n")
		os.Exit(1)
	}

	brPath := src + ".br"
	brFile, err := os.Create(brPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 创建 .br 文件失败: %v\n", err)
		os.Exit(1)
	}

	brWriter := brotli.NewWriterLevel(brFile, level)
	if _, err := brWriter.Write(data); err != nil {
		_ = brFile.Close()
		_ = os.Remove(brPath)
		fmt.Fprintf(os.Stderr, "错误: 压缩写入失败: %v\n", err)
		os.Exit(1)
	}

	if err := brWriter.Close(); err != nil {
		_ = brFile.Close()
		_ = os.Remove(brPath)
		fmt.Fprintf(os.Stderr, "错误: 关闭压缩器失败: %v\n", err)
		os.Exit(1)
	}

	if err := brFile.Close(); err != nil {
		_ = os.Remove(brPath)
		fmt.Fprintf(os.Stderr, "错误: 关闭文件失败: %v\n", err)
		os.Exit(1)
	}

	brInfo, _ := os.Stat(brPath)
	fmt.Printf("完成: %s (%d bytes) -> %s (%d bytes, 级别 %d, 压缩率 %.1f%%)\n",
		src, len(data), brPath, brInfo.Size(), level,
		float64(brInfo.Size())/float64(len(data))*100)
}
