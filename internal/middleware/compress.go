package middleware

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

var (
	ErrCompressEmptyBasePath = errors.New("base path is empty")
	ErrCompressEmptyHTMLFile = errors.New("html file name is empty")
	ErrCompressFileNotFound  = errors.New("compressed file not found")
)

const (
	contentEncodingBrotli  = "br"
	brotliExtension        = ".br"
	cacheControlImmutable  = "public, max-age=31536000, immutable"
	cacheControlNoCache    = "no-cache"
)

var contentTypeMap = map[string]string{
	".js":   "application/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".md":   "text/markdown; charset=utf-8",
}

// PreCompressedStatic Brotli 预压缩静态文件中间件，dist 目录只存放 .br 文件，直接服务预压缩内容
func PreCompressedStatic(basePath string) gin.HandlerFunc {
	if basePath == "" {
		utils.LogWarn("COMPRESS", "Empty base path, using default './dist'", "")
		basePath = "./dist"
	}

	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "Base path does not exist", fmt.Sprintf("path=%s", basePath))
	}

	return func(c *gin.Context) {
		reqPath := c.Request.URL.Path

		cleanPath := filepath.Clean(reqPath)

		if strings.Contains(cleanPath, "..") {
			utils.LogWarn("COMPRESS", "Path traversal attempt detected", fmt.Sprintf("path=%s", reqPath))
			c.Next()
			return
		}

		// i18n JSON 已合并到 translations.js，生产环境不需要单独服务
		// 但 policy 目录下的 Markdown 文件需要单独服务
		if strings.HasPrefix(reqPath, "/shared/i18n/") && !strings.HasPrefix(reqPath, "/shared/i18n/policy/") {
			c.Next()
			return
		}

		ext := filepath.Ext(reqPath)
		contentType, ok := contentTypeMap[ext]
		if !ok {
			c.Next()
			return
		}

		brPath, err := resolveBrotliPath(basePath, reqPath)
		if err != nil {
			c.Next()
			return
		}

		cleanBrPath := filepath.Clean(brPath)
		absBasePath, err := filepath.Abs(basePath)
		if err != nil {
			utils.LogWarn("COMPRESS", "Failed to get absolute base path", fmt.Sprintf("error=%v", err))
			c.Next()
			return
		}
		absBrPath, err := filepath.Abs(cleanBrPath)
		if err != nil {
			utils.LogWarn("COMPRESS", "Failed to get absolute br path", fmt.Sprintf("error=%v", err))
			c.Next()
			return
		}

		// 确保最终路径在基础路径内，防止路径遍历
		if !strings.HasPrefix(absBrPath, absBasePath+string(os.PathSeparator)) && absBrPath != absBasePath {
			utils.LogWarn("COMPRESS", "Path traversal attempt blocked", fmt.Sprintf("path=%s, brPath=%s", reqPath, brPath))
			c.Next()
			return
		}

		if _, err := os.Stat(absBrPath); os.IsNotExist(err) {
			utils.LogDebug("COMPRESS", fmt.Sprintf("Brotli file not found: %s", absBrPath))
			c.Next()
			return
		}

		serveBrotliOrDecompressed(c, absBrPath, contentType, cacheControlImmutable)
	}
}

// ServeCompressedHTML 服务预压缩的 HTML 文件，用于页面路由
func ServeCompressedHTML(basePath, htmlFile string) func(*gin.Context) {
	if basePath == "" {
		utils.LogWarn("COMPRESS", "Empty base path for HTML, using default './dist'", "")
		basePath = "./dist"
	}
	if htmlFile == "" {
		utils.LogError("COMPRESS", "ServeCompressedHTML", fmt.Errorf("empty HTML file name"), "")
		return errorHandler("HTML file name is empty")
	}

	if strings.Contains(htmlFile, "..") || strings.Contains(htmlFile, "/") {
		utils.LogError("COMPRESS", "ServeCompressedHTML", fmt.Errorf("invalid HTML file name: %s", htmlFile), "")
		return errorHandler("Invalid HTML file name")
	}

	brPath := filepath.Join(basePath, "account/pages", htmlFile+".html"+brotliExtension)

	if _, err := os.Stat(brPath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "HTML file not found at startup", fmt.Sprintf("path=%s", brPath))
	}

	return func(c *gin.Context) {
		if _, err := os.Stat(brPath); os.IsNotExist(err) {
			utils.LogInfo("COMPRESS", fmt.Sprintf("HTML file not found: %s", brPath), "")
			c.String(404, "Page not found")
			return
		}

		serveBrotliOrDecompressed(c, brPath, contentTypeMap[".html"], cacheControlNoCache)
	}
}

// ServeCompressedPolicyHTML 服务预压缩的 Policy 模块 HTML 文件
func ServeCompressedPolicyHTML(basePath, htmlFile string) func(*gin.Context) {
	if basePath == "" {
		utils.LogWarn("COMPRESS", "Empty base path for Policy HTML, using default './dist'", "")
		basePath = "./dist"
	}
	if htmlFile == "" {
		utils.LogError("COMPRESS", "ServeCompressedPolicyHTML", fmt.Errorf("empty Policy HTML file name"), "")
		return errorHandler("Policy HTML file name is empty")
	}

	if strings.Contains(htmlFile, "..") || strings.Contains(htmlFile, "/") {
		utils.LogError("COMPRESS", "ServeCompressedPolicyHTML", fmt.Errorf("invalid Policy HTML file name: %s", htmlFile), "")
		return errorHandler("Invalid Policy HTML file name")
	}

	brPath := filepath.Join(basePath, "policy/pages", htmlFile+".html"+brotliExtension)

	if _, err := os.Stat(brPath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "Policy HTML file not found at startup", fmt.Sprintf("path=%s", brPath))
	}

	return func(c *gin.Context) {
		if _, err := os.Stat(brPath); os.IsNotExist(err) {
			utils.LogInfo("COMPRESS", fmt.Sprintf("Policy HTML file not found: %s", brPath), "")
			c.String(404, "Page not found")
			return
		}

		serveBrotliOrDecompressed(c, brPath, contentTypeMap[".html"], cacheControlNoCache)
	}
}

// ====================  私有函数 ====================

// resolveBrotliPath 解析请求路径，返回对应的 .br 文件路径
func resolveBrotliPath(basePath, reqPath string) (string, error) {
	var brPath string
	var relPath string

	cleanReqPath := filepath.Clean(reqPath)

	switch {
	case strings.HasPrefix(cleanReqPath, "/shared/i18n/policy/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/shared")
		brPath = filepath.Join(basePath, "shared", relPath+brotliExtension)
	case strings.HasPrefix(cleanReqPath, "/shared/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/shared")
		brPath = filepath.Join(basePath, "shared", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/home/assets/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/home/assets")
		brPath = filepath.Join(basePath, "home/assets", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/account/assets/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/account/assets")
		brPath = filepath.Join(basePath, "account/assets", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/account/data/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/account/data")
		brPath = filepath.Join(basePath, "account/data", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/policy/assets/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/policy/assets")
		brPath = filepath.Join(basePath, "policy/assets", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/policy/data/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/policy/data")
		brPath = filepath.Join(basePath, "policy/data", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/admin/assets/"):
		relPath = strings.TrimPrefix(cleanReqPath, "/admin/assets")
		brPath = filepath.Join(basePath, "admin/assets", relPath+brotliExtension)

	default:
		return "", errors.New("unsupported path prefix")
	}

	if strings.Contains(relPath, "..") {
		return "", errors.New("invalid path contains traversal")
	}

	return brPath, nil
}

// AcceptsBrotli 检查浏览器是否支持 Brotli 压缩
func AcceptsBrotli(c *gin.Context) bool {
	acceptEncoding := c.GetHeader("Accept-Encoding")
	return strings.Contains(acceptEncoding, "br")
}

// decompressBrotli 解压 Brotli 压缩数据
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

// serveBrotliOrDecompressed 根据浏览器支持发送 .br 压缩文件或原文件，构建时同时输出原文件和 .br 文件，运行时无需解压
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

	utils.LogError("COMPRESS", "serveBrotliOrDecompressed", nil, fmt.Sprintf("Neither .br nor original file found: brPath=%s", brPath))
	c.String(500, "Internal server error")
	c.Abort()
}

// errorHandler 返回 500 错误处理函数
func errorHandler(message string) func(*gin.Context) {
	return func(c *gin.Context) {
		utils.LogError("COMPRESS", "errorHandler", fmt.Errorf("%s", message), "")
		c.String(500, "Internal server error")
	}
}
