package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/evanw/esbuild/pkg/api"
)

func buildCSS() error {
	log.Println("[BUILD] Building CSS...")

	if err := buildCSSModule(filepath.Join(sharedDir, "css/*.css"), "dist/shared/css", "shared"); err != nil {
		return err
	}

	if err := buildCSSModule("modules/home/assets/css/*.css", "dist/home/assets/css", "home"); err != nil {
		return err
	}

	if err := buildCSSModule("modules/account/assets/css/*.css", "dist/account/assets/css", "account"); err != nil {
		return err
	}

	if err := buildCSSModule("modules/policy/assets/css/*.css", "dist/policy/assets/css", "policy"); err != nil {
		return err
	}

	if err := buildAdminCSS(); err != nil {
		return err
	}

	log.Println("[BUILD] CSS build completed")
	return nil
}

func buildAdminCSS() error {
	cssFiles := []string{
		"modules/admin/assets/css/common.css",
		"modules/admin/assets/css/admin.css",
	}

	var combined []byte
	for _, file := range cssFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}
		combined = append(combined, data...)
		combined = append(combined, '\n')
	}

	opts := api.TransformOptions{
		Loader: api.LoaderCSS,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifySyntax = true
	}

	result := api.Transform(string(combined), opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: admin CSS: %s", err.Text)
		}
		atomic.AddInt64(&stats.Errors, int64(len(result.Errors)))
		return fmt.Errorf("admin CSS build failed")
	}

	outFile := "dist/admin/assets/css/admin.css"
	if err := os.WriteFile(outFile, result.Code, filePerm); err != nil {
		return fmt.Errorf("failed to write %s: %w", outFile, err)
	}

	_, _ = addToManifest(outFile)

	atomic.AddInt64(&stats.FilesProcessed, int64(len(cssFiles)))
	atomic.AddInt64(&stats.BytesWritten, int64(len(result.Code)))
	log.Printf("[BUILD] Built admin CSS (merged %d files)", len(cssFiles))
	return nil
}

func buildCSSModule(pattern, outdir, moduleName string) error {
	entries, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob %s CSS: %w", moduleName, err)
	}

	if len(entries) == 0 {
		log.Printf("[BUILD] WARN: No CSS files found for %s module", moduleName)
		return nil
	}

	opts := api.BuildOptions{
		EntryPoints: entries,
		Outdir:      outdir,
		Write:       true,
		LogLevel:    api.LogLevelWarning,
	}

	if !*isDev {
		opts.MinifyWhitespace = true
		opts.MinifySyntax = true
	}

	result := api.Build(opts)

	if len(result.Errors) > 0 {
		for _, err := range result.Errors {
			log.Printf("[BUILD] ERROR: %s CSS: %s", moduleName, err.Text)
		}
		atomic.AddInt64(&stats.Errors, int64(len(result.Errors)))
		return fmt.Errorf("%s CSS build failed", moduleName)
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
		if strings.HasSuffix(name, ".css") {
			originalPath := filepath.Join(outdir, name)
			_, err = addToManifest(originalPath)
			if err != nil {
				log.Printf("[BUILD] WARN: Failed to hash %s: %v", name, err)
			}
		}
	}

	atomic.AddInt64(&stats.FilesProcessed, int64(len(entries)))
	log.Printf("[BUILD] Built %d CSS files for %s module", len(entries), moduleName)
	return nil
}
