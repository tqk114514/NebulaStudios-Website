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

func buildJS() error {
	log.Println("[BUILD] Building JavaScript...")

	if err := validateEntryPoints(homePageEntries); err != nil {
		return fmt.Errorf("home entries validation failed: %w", err)
	}

	if err := validateEntryPoints(accountPageEntries); err != nil {
		return fmt.Errorf("account entries validation failed: %w", err)
	}

	if err := validateEntryPoints(policyPageEntries); err != nil {
		return fmt.Errorf("policy entries validation failed: %w", err)
	}

	if err := validateEntryPoints(adminPageEntries); err != nil {
		return fmt.Errorf("admin entries validation failed: %w", err)
	}

	if err := buildJSModule(homePageEntries, "dist/home/assets/js", "home", ""); err != nil {
		return err
	}

	if err := buildJSModule(accountPageEntries, "dist/account/assets/js", "account", ""); err != nil {
		return err
	}

	if err := buildJSModule(policyPageEntries, "dist/policy/assets/js", "policy", ""); err != nil {
		return err
	}

	if err := buildJSModule(adminPageEntries, "dist/admin/assets/js", "admin", ""); err != nil {
		return err
	}

	log.Println("[BUILD] JavaScript build completed")
	return nil
}

func validateEntryPoints(entries []string) error {
	for _, entry := range entries {
		if _, err := os.Stat(entry); os.IsNotExist(err) {
			return fmt.Errorf("entry point not found: %s", entry)
		}
	}
	return nil
}

// buildJSModule 构建单个 JS 模块，injectData 为空时不注入，非空时作为 __POLICY_DATA__ 注入
func buildJSModule(entries []string, outdir, moduleName, injectData string) error {
	if len(entries) == 0 {
		log.Printf("[BUILD] WARN: No JS entries for %s module", moduleName)
		return nil
	}

	sourcemap := api.SourceMapNone
	if *isDev {
		sourcemap = api.SourceMapLinked
	}

	actualEntries := entries
	var tmpFiles []string
	if injectData != "" {
		for _, entry := range entries {
			data, err := os.ReadFile(entry)
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", entry, err)
			}

			injectedCode := fmt.Sprintf("const __POLICY_DATA__ = %s;\n\n", injectData)
			output := injectedCode + string(data)

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
		Format:      api.FormatESModule,
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
			if err := replaceCDNURLInFile(originalPath); err != nil {
				log.Printf("[BUILD] WARN: Failed to replace {{CDN_URL}} in %s: %v", name, err)
			}
			_, err := addToManifest(originalPath)
			if err != nil {
				log.Printf("[BUILD] WARN: Failed to hash %s: %v", name, err)
			}
		}
	}

	for _, warn := range result.Warnings {
		log.Printf("[BUILD] WARN: %s: %s", moduleName, warn.Text)
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(entries)))
	log.Printf("[BUILD] Built %d JS files for %s module", len(entries), moduleName)
	return nil
}

func buildTranslations() error {
	log.Println("[BUILD] Building translations...")

	i18nModules := []string{"general", "account", "policy", "home"}

	allTranslations := make(map[string]map[string]string)
	var totalBytes int64

	for _, lang := range supportedLanguages {
		langData := make(map[string]string)

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

			for key, newValue := range moduleData {
				if existingValue, exists := langData[key]; exists {
					if existingValue != newValue {
						return fmt.Errorf("translation conflict: key '%s' in language '%s' has conflicting values (module '%s' has '%s', previous value was '%s')",
							key, lang, module, newValue, existingValue)
					}
				}
				langData[key] = newValue
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

	translationsJSON, err := json.Marshal(allTranslations)
	if err != nil {
		return fmt.Errorf("failed to marshal translations: %w", err)
	}

	templatePath := filepath.Join(sharedDir, "js", "translations.ts")
	templateData, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("failed to read translations.ts template: %w", err)
	}

	atomic.AddInt64(&stats.BytesRead, int64(len(templateData)))

	injectedCode := fmt.Sprintf("const __ALL_TRANSLATIONS__ = %s;\n\n", string(translationsJSON))
	output := injectedCode + string(templateData)

	tmpFile := filepath.Join(distDir, "shared/js/translations.tmp.ts")
	if err := os.WriteFile(tmpFile, []byte(output), filePerm); err != nil {
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

	finalPath := filepath.Join(distDir, "shared/js/translations.js")
	if err := os.Rename(tmpOutFile, finalPath); err != nil {
		return fmt.Errorf("failed to rename translations.js: %w", err)
	}

	hashedName, err := addToManifest(finalPath)
	if err != nil {
		return fmt.Errorf("failed to hash translations.js: %w", err)
	}

	assetManifest[finalPath] = hashedName
	assetManifest["shared/js/translations.js"] = hashedName

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Printf("[BUILD] Built translations.js with %d languages -> %s", len(allTranslations), hashedName)
	return nil
}

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

	finalPath := filepath.Join(distDir, "shared/js/cookie-consent.js")
	if err := os.Rename(tmpOutFile, finalPath); err != nil {
		return fmt.Errorf("failed to rename cookie-consent.js: %w", err)
	}

	hashedName, err := addToManifest(finalPath)
	if err != nil {
		return fmt.Errorf("failed to hash cookie-consent.js: %w", err)
	}

	assetManifest[finalPath] = hashedName
	assetManifest["shared/js/cookie-consent.js"] = hashedName

	atomic.AddInt64(&stats.FilesProcessed, 1)
	log.Printf("[BUILD] Built cookie-consent.js -> %s", hashedName)
	return nil
}
