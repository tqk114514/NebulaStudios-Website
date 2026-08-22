package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// vendorLibs 内嵌库清单：目录名前缀 → 版本无关的再导出语句模板（%s 为实际目录名）。
// 业务代码统一 import shared/js/lib/vendor.ts，升级内嵌库只需替换
// shared/js/lib/名字@新版本 目录并重新执行构建，无需改动任何业务代码
var vendorLibs = []struct {
	dirPrefix string
	exports   string
}{
	{"cure53-DOMPurify@", "export { default as DOMPurify } from './%s/src/purify.ts';"},
	{"markedjs-marked@", "export { marked } from './%s/src/marked.ts';"},
	{"faisalman-ua-parser-js@", "export { default as UAParser } from './%s/src/ua-parser.js';"},
	{"paulmillr-qr@", "export { QRCanvas, QRCamera, frameLoop } from './%s/src/dom.ts';"},
}

// syncVendorLibs 依据 shared/js/lib 目录中实际存在的版本生成 vendor.ts，
// 并为内嵌库源文件补齐 // @ts-nocheck 头（上游源码不受本项目严格 tsconfig 约束）。
// 内容未变化时不写文件，避免每次构建弄脏 git 工作区
func syncVendorLibs() error {
	log.Println("[BUILD] Syncing vendor library exports...")

	var lines []string
	for _, lib := range vendorLibs {
		dir, err := findVendorDir(lib.dirPrefix)
		if err != nil {
			return err
		}
		if err := ensureTSNocheck(filepath.Join(sharedDir, "js", "lib", dir)); err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf(lib.exports, dir))
	}

	content := "// 本文件由 cmd/build 依据 shared/js/lib 目录自动生成，请勿手动修改。\n" +
		"// 升级内嵌库：替换 shared/js/lib/名字@新版本 目录后重新执行 go run ./cmd/build\n" +
		strings.Join(lines, "\n") + "\n"

	vendorPath := filepath.Join(sharedDir, "js", "lib", "vendor.ts")
	if existing, err := os.ReadFile(vendorPath); err == nil && string(existing) == content {
		return nil
	}

	if err := os.WriteFile(vendorPath, []byte(content), filePerm); err != nil {
		return fmt.Errorf("failed to write vendor.ts: %w", err)
	}

	log.Printf("[BUILD] Updated %s", vendorPath)
	return nil
}

// findVendorDir 查找带版本目录名（前缀匹配），多于一个视为升级残留，报错提示清理
func findVendorDir(prefix string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(sharedDir, "js", "lib", prefix+"*"))
	if err != nil {
		return "", fmt.Errorf("failed to glob vendor dir %s: %w", prefix, err)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("vendored library %s* not found under shared/js/lib", prefix)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple versions of %s found (%s): remove the old one before building",
			prefix, strings.Join(matches, ", "))
	}
	return filepath.Base(matches[0]), nil
}

// ensureTSNocheck 为目录下所有 .ts 文件补齐 // @ts-nocheck 头（缺失时）
func ensureTSNocheck(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".ts" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) >= 14 && string(data[:14]) == "// @ts-nocheck" {
			return nil
		}
		log.Printf("[BUILD] Added ts-nocheck header to %s", path)
		return os.WriteFile(path, append([]byte("// @ts-nocheck\n"), data...), filePerm)
	})
}
