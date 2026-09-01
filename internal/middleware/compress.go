package middleware

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

var (
	ErrCompressEmptyBasePath = errors.New("base path is empty")
	ErrCompressFileNotFound  = errors.New("compressed file not found")
)

const (
	contentEncodingBrotli = "br"
	brotliExtension       = ".br"
	cacheControlImmutable = "public, max-age=31536000, immutable"
)

var contentTypeMap = map[string]string{
	".js":    "application/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml; charset=utf-8",
	".md":    "text/plain; charset=utf-8",
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

// PreCompressedStatic Brotli 预压缩静态文件中间件。SPA 静态资源均位于 /assets/ 下，
// 基于 server 二进制旁的 dist 产物目录服务。dist 目录只存放 .br 文件时可直接服务压缩内容，
// 浏览器不支持 Brotli 时回退到原文件。
func PreCompressedStatic() gin.HandlerFunc {
	basePath := "dist"

	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "Base path does not exist", "path", basePath)
	}

	return func(c *gin.Context) {
		reqPath := c.Request.URL.Path

		cleanPath := filepath.Clean(reqPath)

		if strings.Contains(cleanPath, "..") {
			utils.LogWarnCtx(c.Request.Context(), "COMPRESS", "Path traversal attempt detected", "path", reqPath)
			c.Next()
			return
		}

		ext := filepath.Ext(reqPath)
		contentType, ok := contentTypeMap[ext]
		if !ok {
			c.Next()
			return
		}

		brPath, err := resolveStaticPath(basePath, reqPath)
		if err != nil {
			c.Next()
			return
		}

		cleanBrPath := filepath.Clean(brPath)
		absBasePath, err := filepath.Abs(basePath)
		if err != nil {
			utils.LogWarnCtx(c.Request.Context(), "COMPRESS", "Failed to get absolute base path", "error", err)
			c.Next()
			return
		}
		absBrPath, err := filepath.Abs(cleanBrPath)
		if err != nil {
			utils.LogWarnCtx(c.Request.Context(), "COMPRESS", "Failed to get absolute br path", "error", err)
			c.Next()
			return
		}

		// 确保最终路径在基础路径内，防止路径遍历
		if !strings.HasPrefix(absBrPath, absBasePath+string(os.PathSeparator)) && absBrPath != absBasePath {
			utils.LogWarnCtx(c.Request.Context(), "COMPRESS", "Path traversal attempt blocked", "path", reqPath, "br_path", brPath)
			c.Next()
			return
		}

		// .br 缺失时由 serveBrotliOrDecompressed 回退到原文件；两者都不存在才放行（→ SPA fallback/404）
		if _, err := os.Stat(absBrPath); os.IsNotExist(err) {
			origPath := strings.TrimSuffix(absBrPath, brotliExtension)
			if _, oerr := os.Stat(origPath); oerr != nil {
				utils.LogDebugCtx(c.Request.Context(), "COMPRESS", "Static file not found", "path", origPath)
				c.Next()
				return
			}
		}

		serveBrotliOrDecompressed(c, absBrPath, contentType, cacheControlImmutable)
	}
}

// resolveStaticPath 解析 SPA 静态资源请求路径为对应 .br 文件路径。
// - /assets/*　　→ dist/assets/*（Vite 产物）
// - /policy-content/* → dist/policy/*（政策 Markdown 正文）
func resolveStaticPath(basePath, reqPath string) (string, error) {
	var relPath string
	switch {
	case strings.HasPrefix(reqPath, "/assets/"):
		relPath = strings.TrimPrefix(reqPath, "/assets")
		return filepath.Join(basePath, "assets", relPath+brotliExtension), nil
	case strings.HasPrefix(reqPath, "/policy-content/"):
		relPath = strings.TrimPrefix(reqPath, "/policy-content")
		return filepath.Join(basePath, "policy", relPath+brotliExtension), nil
	default:
		return "", errors.New("unsupported path prefix")
	}
}

// AcceptsBrotli 检查浏览器是否支持 Brotli 压缩
func AcceptsBrotli(c *gin.Context) bool {
	acceptEncoding := c.GetHeader("Accept-Encoding")
	return strings.Contains(acceptEncoding, "br")
}

// setCompressedHeaders 设置压缩文件的响应头
func setCompressedHeaders(c *gin.Context, contentType, cacheControl string) {
	c.Header("Content-Encoding", contentEncodingBrotli)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", cacheControl)
	c.Header("Vary", "Accept-Encoding")
}

// setUncompressedHeaders 设置未压缩文件的响应头
func setUncompressedHeaders(c *gin.Context, contentType, cacheControl string) {
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", cacheControl)
	c.Header("Vary", "Accept-Encoding")
}

// serveBrotliOrDecompressed 根据浏览器支持发送 .br 压缩文件或原文件
func serveBrotliOrDecompressed(c *gin.Context, brPath, contentType, cacheControl string) {
	if AcceptsBrotli(c) {
		if _, err := os.Stat(brPath); err == nil {
			setCompressedHeaders(c, contentType, cacheControl)
			c.File(brPath)
			c.Abort()
			return
		}
	}

	origPath := strings.TrimSuffix(brPath, ".br")
	if _, err := os.Stat(origPath); err == nil {
		setUncompressedHeaders(c, contentType, cacheControl)
		c.File(origPath)
		c.Abort()
		return
	}

	utils.LogErrorCtx(c.Request.Context(), "COMPRESS", "serveBrotliOrDecompressed", nil, "br_path", brPath)
	c.String(500, "Internal server error")
	c.Abort()
}
