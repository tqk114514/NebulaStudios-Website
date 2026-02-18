/**
 * cmd/build/js.go
 * JavaScript 构建模块
 *
 * 功能：
 * - 使用 esbuild 构建 TypeScript/JavaScript
 * - 支持数据注入（Policy 数据内嵌）
 * - 构建 i18n 翻译文件
 */

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

// ====================  JavaScript 构建 ====================

// buildJS 构建 JavaScript 文件
func buildJS() error {
	log.Println("[BUILD] Building JavaScript...")

	// 验证入口文件存在
	if err := validateEntryPoints(accountPageEntries); err != nil {
		return fmt.Errorf("account entries validation failed: %w", err)
	}

	if err := validateEntryPoints(policyPageEntries); err != nil {
		return fmt.Errorf("policy entries validation failed: %w", err)
	}

	if err := validateEntryPoints(adminPageEntries); err != nil {
		return fmt.Errorf("admin entries validation failed: %w", err)
	}

	// 构建 Account 模块
	if err := buildJSModule(accountPageEntries, "dist/account/assets/js", "account", ""); err != nil {
		return err
	}

	// 读取 Policy 数据用于注入
	policyDataJSON, err := loadPolicyDataJSON()
	if err != nil {
		log.Printf("[BUILD] WARN: Failed to load policy data: %v", err)
		policyDataJSON = "{}"
	}

	// 构建 Policy 模块（注入数据）
	if err := buildJSModule(policyPageEntries, "dist/policy/assets/js", "policy", policyDataJSON); err != nil {
		return err
	}

	// 构建 Admin 模块（独立，无数据注入）
	if err := buildJSModule(adminPageEntries, "dist/admin/assets/js", "admin", ""); err != nil {
		return err
	}

	log.Println("[BUILD] JavaScript build completed")
	return nil
}

// loadPolicyDataJSON 读取并压缩 Policy 数据
func loadPolicyDataJSON() (string, error) {
	src := filepath.Join(sharedDir, "i18n/policy/policy.json")
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}

	// 验证并压缩 JSON
	var jsonData interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		return "", err
	}

	minified, err := json.Marshal(jsonData)
	if err != nil {
		return "", err
	}

	return string(minified), nil
}

// validateEntryPoints 验证入口文件是否存在
func validateEntryPoints(entries []string) error {
	for _, entry := range entries {
		if _, err := os.Stat(entry); os.IsNotExist(err) {
			return fmt.Errorf("entry point not found: %s", entry)
		}
	}
	return nil
}

// buildJSModule 构建单个 JS 模块
// injectData 为空时不注入数据，非空时作为 __POLICY_DATA__ 注入
func buildJSModule(entries []string, outdir, moduleName, injectData string) error {
	if len(entries) == 0 {
		log.Printf("[BUILD] WARN: No JS entries for %s module", moduleName)
		return nil
	}

	sourcemap := api.SourceMapNone
	if *isDev {
		sourcemap = api.SourceMapLinked
	}

	// 如果需要注入数据，创建临时文件（保持在原目录，以便 import 能正确解析）
	actualEntries := entries
	var tmpFiles []string
	if injectData != "" {
		for _, entry := range entries {
			// 读取原文件
			data, err := os.ReadFile(entry)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", entry, err)
			}

			// 在文件开头注入数据
			injectedCode := fmt.Sprintf("const __POLICY_DATA__ = %s;\n\n", injectData)
			output := injectedCode + string(data)

			// 写入临时文件到原目录（保持相对 import 路径可用）
			tmpFile := strings.TrimSuffix(entry, ".ts") + ".tmp.ts"
			if err := os.WriteFile(tmpFile, []byte(output), filePerm); err != nil {
				return fmt.Errorf("failed to write temp file: %w", err)
			}
			tmpFiles = append(tmpFiles, tmpFile)
		}
		actualEntries = tmpFiles
		defer func() {
			for _, f := range tmpFiles {
				os.Remove(f)
			}
		}()
	}

	opts := api.BuildOptions{
		EntryPoints: actualEntries,
		Bundle:      true,
		Outdir:      outdir,
		Sourcemap:   sourcemap,
		Target:      api.ES2020,
		Format:      api.FormatIIFE,
		TreeShaking: api.TreeShakingTrue,
		KeepNames:   *isDev,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	// 处理错误
	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: %s: %s", moduleName, err.Text)
			if err.Location != nil {
				log.Printf("[BUILD]   at %s:%d:%d", err.Location.File, err.Location.Line, err.Location.Column)
			}
		}
		atomic.AddInt64(&stats.Errors, int64(len(result.Errors)))
		return fmt.Errorf("%s JS build failed with %d errors", moduleName, len(result.Errors))
	}

	// 如果使用了临时文件，需要重命名输出文件
	if injectData != "" {
		for _, entry := range entries {
			baseName := strings.TrimSuffix(filepath.Base(entry), ".ts")
			oldName := filepath.Join(outdir, baseName+".tmp.js")
			newName := filepath.Join(outdir, baseName+".js")
			if err := os.Rename(oldName, newName); err != nil {
				log.Printf("[BUILD] WARN: Failed to rename %s: %v", oldName, err)
			}
		}
	}

	// 哈希化所有输出的 JS 文件
	files, err := os.ReadDir(outdir)
	if err != nil {
		return fmt.Errorf("failed to read output dir: %w", err)
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if strings.HasSuffix(name, ".js") {
			originalPath := filepath.Join(outdir, name)
			_, err := addToManifest(originalPath)
			if err != nil {
				log.Printf("[BUILD] WARN: Failed to hash %s: %v", name, err)
			}
		}
	}

	// 处理警告
	for _, warn := range result.Warnings {
		log.Printf("[BUILD] WARN: %s: %s", moduleName, warn.Text)
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(entries)))
	log.Printf("[BUILD] Built %d JS files for %s module", len(entries), moduleName)
	return nil
}

// ====================  翻译文件构建 ====================

// buildTranslations 合并所有 i18n 文件并生成 translations.js
func buildTranslations() error {
	log.Println("[BUILD] Building translations...")

	// i18n 子目录列表
	i18nModules := []string{"general", "account", "policy"}

	// 读取所有语言文件
	allTranslations := make(map[string]map[string]string)
	var totalBytes int64

	for _, lang := range supportedLanguages {
		langData := make(map[string]string)

		// 从每个子目录读取并合并
		for _, module := range i18nModules {
			filePath := filepath.Join(sharedDir, "i18n", module, lang+".json")

			data, err := os.ReadFile(filePath)
			if err != nil {
				if os.IsNotExist(err) {
					log.Printf("[BUILD] WARN: Language file not found: %s", filePath)
					continue
				}
				return fmt.Errorf("failed to read %s: %w", filePath, err)
			}

			totalBytes += int64(len(data))

			var moduleData map[string]string
			if err := json.Unmarshal(data, &moduleData); err != nil {
				return fmt.Errorf("failed to parse %s: %w", filePath, err)
			}

			// 合并到语言数据
			for k, v := range moduleData {
				langData[k] = v
			}
		}

		if len(langData) == 0 {
			log.Printf("[BUILD] WARN: No translation data for language: %s", lang)
			continue
		}

		allTranslations[lang] = langData
	}

	atomic.AddInt64(&stats.BytesRead, totalBytes)

	if len(allTranslations) == 0 {
		return errors.New("no translation files found")
	}

	// 序列化为 JSON
	translationsJSON, err := json.Marshal(allTranslations)
	if err != nil {
		return fmt.Errorf("failed to marshal translations: %w", err)
	}

	// 读取 translations.ts 模板
	templatePath := filepath.Join(sharedDir, "js", "translations.ts")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read translations.ts template: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(templateData)))

	// 在文件开头注入所有翻译数据
	injectedCode := fmt.Sprintf("const __ALL_TRANSLATIONS__ = %s;\n\n", string(translationsJSON))
	output := injectedCode + string(templateData)

	// 写入临时文件
	tmpFile := filepath.Join(distDir, "shared/js/translations.tmp.ts")
	if err := os.WriteFile(tmpFile, []byte(output), filePerm); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	defer func() {
		if err := os.Remove(tmpFile); err != nil {
			log.Printf("[BUILD] WARN: Failed to remove temp file: %v", err)
		}
	}()

	// 使用 esbuild 压缩到临时文件
	sourcemap := api.SourceMapNone
	if *isDev {
		sourcemap = api.SourceMapLinked
	}

	tmpOutFile := filepath.Join(distDir, "shared/js/translations.tmp.js")
	opts := api.BuildOptions{
		EntryPoints: []string{tmpFile},
		Outfile:     tmpOutFile,
		Sourcemap:   sourcemap,
		Target:      api.ES2020,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: translations: %s", err.Text)
		}
		return errors.New("translations.js build failed")
	}

	// 重命名为正式文件名
	finalPath := filepath.Join(distDir, "shared/js/translations.js")
	if err := os.Rename(tmpOutFile, finalPath); err != nil {
		return fmt.Errorf("failed to rename translations.js: %w", err)
	}

	hashedName, err := addToManifest(finalPath)
	if err != nil {
		return fmt.Errorf("failed to hash translations.js: %w", err)
	}

	// 同时存储不带 .tmp 的映射
	assetManifest[finalPath] = hashedName
	assetManifest["shared/js/translations.js"] = hashedName

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Printf("[BUILD] Built translations.js with %d languages -> %s", len(allTranslations), hashedName)
	return nil
}

// buildCookieConsent 构建 cookie-consent.js
func buildCookieConsent() error {
	log.Println("[BUILD] Building cookie-consent.js...")

	cookieConsentPath := filepath.Join(sharedDir, "js", "cookie-consent.ts")
	cookieConsentData, err := os.ReadFile(cookieConsentPath)
	if err != nil {
		return fmt.Errorf("failed to read cookie-consent.ts: %w", err)
	}

	tmpFile := filepath.Join(distDir, "shared/js/cookie-consent.tmp.ts")
	if err := os.WriteFile(tmpFile, cookieConsentData, filePerm); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	defer func() {
		if err := os.Remove(tmpFile); err != nil {
			log.Printf("[BUILD] WARN: Failed to remove temp file: %v", err)
		}
	}()

	sourcemap := api.SourceMapNone
	if *isDev {
		sourcemap = api.SourceMapLinked
	}

	tmpOutFile := filepath.Join(distDir, "shared/js/cookie-consent.tmp.js")
	opts := api.BuildOptions{
		EntryPoints: []string{tmpFile},
		Outfile:     tmpOutFile,
		Sourcemap:   sourcemap,
		Target:      api.ES2020,
		Format:      api.FormatIIFE,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifyIdentifiers = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: cookie-consent: %s", err.Text)
		}
		return errors.New("cookie-consent.js build failed")
	}

	// 重命名为正式文件名
	finalPath := filepath.Join(distDir, "shared/js/cookie-consent.js")
	if err := os.Rename(tmpOutFile, finalPath); err != nil {
		return fmt.Errorf("failed to rename cookie-consent.js: %w", err)
	}

	hashedName, err := addToManifest(finalPath)
	if err != nil {
		return fmt.Errorf("failed to hash cookie-consent.js: %w", err)
	}

	// 同时存储不带 .tmp 的映射
	assetManifest[finalPath] = hashedName
	assetManifest["shared/js/cookie-consent.js"] = hashedName

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Printf("[BUILD] Built cookie-consent.js -> %s", hashedName)
	return nil
}
