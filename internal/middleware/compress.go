/**
 * internal/middleware/compress.go
 * Brotli 预压缩静态文件中间件
 *
 * 功能：
 * - 直接服务 .br 预压缩文件（dist 目录只有压缩文件）
 * - 零运行时压缩开销
 * - 支持 JS、CSS、HTML、JSON 文件类型
 * - 自动设置正确的 Content-Type 和缓存头
 *
 * 依赖：
 * - 构建系统生成的 .br 文件
 */

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

// ====================  错误定义 ====================

var (
	// ErrCompressEmptyBasePath 基础路径为空
	ErrCompressEmptyBasePath = errors.New("base path is empty")
	// ErrCompressEmptyHTMLFile HTML 文件名为空
	ErrCompressEmptyHTMLFile = errors.New("html file name is empty")
	// ErrCompressFileNotFound 文件未找到
	ErrCompressFileNotFound = errors.New("compressed file not found")
)

// ====================  常量定义 ====================

const (
	// contentEncodingBrotli Brotli 编码标识
	contentEncodingBrotli = "br"

	// brotliExtension Brotli 文件扩展名
	brotliExtension = ".br"

	// cacheControlImmutable 不可变资源缓存策略（1年）
	cacheControlImmutable = "public, max-age=31536000, immutable"

	// cacheControlNoCache 不缓存策略
	cacheControlNoCache = "no-cache"
)

// contentTypeMap 文件扩展名到 Content-Type 的映射
var contentTypeMap = map[string]string{
	".js":   "application/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".json": "application/json; charset=utf-8",
}

// ====================  公开函数 ====================

// PreCompressedStatic Brotli 预压缩静态文件中间件
// dist 目录只存放 .br 文件，直接服务预压缩内容
//
// 参数：
//   - basePath: 静态文件基础路径（通常是 "./dist"）
//
// 返回：
//   - gin.HandlerFunc: Gin 中间件函数
//
// 支持的路径：
//   - /shared/* - 共享资源（js, css, components）
//   - /account/assets/* - Account 模块资源
//   - /account/data/* - Account 模块数据
//   - /policy/assets/* - Policy 模块资源
//   - /policy/data/* - Policy 模块数据
//   - /admin/assets/* - Admin 模块资源
func PreCompressedStatic(basePath string) gin.HandlerFunc {
	// 参数验证
	if basePath == "" {
		utils.LogWarn("COMPRESS", "Empty base path, using default './dist'", "")
		basePath = "./dist"
	}

	// 检查基础路径是否存在
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "Base path does not exist", fmt.Sprintf("path=%s", basePath))
	}

	return func(c *gin.Context) {
		reqPath := c.Request.URL.Path

		// 规范化路径
		cleanPath := filepath.Clean(reqPath)

		// 安全检查：防止路径遍历攻击
		if strings.Contains(cleanPath, "..") {
			utils.LogWarn("COMPRESS", "Path traversal attempt detected", fmt.Sprintf("path=%s", reqPath))
			c.Next()
			return
		}

		// i18n JSON 已合并到 translations.js，生产环境不需要单独服务
		// 保留跳过逻辑以防万一有请求
		if strings.HasPrefix(reqPath, "/shared/i18n/") {
			c.Next()
			return
		}

		// 获取文件扩展名和 Content-Type
		ext := filepath.Ext(reqPath)
		contentType, ok := contentTypeMap[ext]
		if !ok {
			// 不支持的文件类型，交给下一个处理器
			c.Next()
			return
		}

		// 解析请求路径，获取 .br 文件路径
		brPath, err := resolveBrotliPath(basePath, reqPath)
		if err != nil {
			// 无法解析路径，交给下一个处理器
			c.Next()
			return
		}

		// 规范化最终路径
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

		// 确保最终路径在基础路径内
		if !strings.HasPrefix(absBrPath, absBasePath+string(os.PathSeparator)) && absBrPath != absBasePath {
			utils.LogWarn("COMPRESS", "Path traversal attempt blocked", fmt.Sprintf("path=%s, brPath=%s", reqPath, brPath))
			c.Next()
			return
		}

		// 检查文件是否存在
		if _, err := os.Stat(absBrPath); os.IsNotExist(err) {
			utils.LogDebug("COMPRESS", fmt.Sprintf("Brotli file not found: %s", absBrPath))
			c.Next()
			return
		}

		// 设置响应头
		setCompressedHeaders(c, contentType, cacheControlImmutable)

		// 发送文件
		c.File(absBrPath)
		c.Abort()
	}
}

// ServeCompressedHTML 服务预压缩的 HTML 文件
// 用于页面路由，返回预压缩的 HTML 内容
//
// 参数：
//   - basePath: 静态文件基础路径
//   - htmlFile: HTML 文件名（不含扩展名）
//
// 返回：
//   - func(*gin.Context): Gin 处理函数
//
// 示例：
//
//	ServeCompressedHTML("./dist", "login") -> 服务 ./dist/account/pages/login.html.br
func ServeCompressedHTML(basePath, htmlFile string) func(*gin.Context) {
	// 参数验证
	if basePath == "" {
		utils.LogWarn("COMPRESS", "Empty base path for HTML, using default './dist'", "")
		basePath = "./dist"
	}
	if htmlFile == "" {
		utils.LogError("COMPRESS", "ServeCompressedHTML", fmt.Errorf("empty HTML file name"), "")
		return errorHandler("HTML file name is empty")
	}

	// 安全检查：防止路径遍历
	if strings.Contains(htmlFile, "..") || strings.Contains(htmlFile, "/") {
		utils.LogError("COMPRESS", "ServeCompressedHTML", fmt.Errorf("invalid HTML file name: %s", htmlFile), "")
		return errorHandler("Invalid HTML file name")
	}

	// 构建 .br 文件路径
	brPath := filepath.Join(basePath, "account/pages", htmlFile+".html"+brotliExtension)

	// 检查文件是否存在（启动时检查）
	if _, err := os.Stat(brPath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "HTML file not found at startup", fmt.Sprintf("path=%s", brPath))
	}

	return func(c *gin.Context) {
		// 运行时再次检查文件是否存在
		if _, err := os.Stat(brPath); os.IsNotExist(err) {
			utils.LogInfo("COMPRESS", fmt.Sprintf("HTML file not found: %s", brPath), "")
			c.String(404, "Page not found")
			return
		}

		// 设置响应头（HTML 不缓存，确保用户获取最新内容）
		setCompressedHeaders(c, contentTypeMap[".html"], cacheControlNoCache)

		// 发送文件
		c.File(brPath)
	}
}

// ServeCompressedPolicyHTML 服务预压缩的 Policy 模块 HTML 文件
// 用于 Policy 页面路由
//
// 参数：
//   - basePath: 静态文件基础路径
//   - htmlFile: HTML 文件名（不含扩展名）
//
// 返回：
//   - func(*gin.Context): Gin 处理函数
func ServeCompressedPolicyHTML(basePath, htmlFile string) func(*gin.Context) {
	// 参数验证
	if basePath == "" {
		utils.LogWarn("COMPRESS", "Empty base path for Policy HTML, using default './dist'", "")
		basePath = "./dist"
	}
	if htmlFile == "" {
		utils.LogError("COMPRESS", "ServeCompressedPolicyHTML", fmt.Errorf("empty Policy HTML file name"), "")
		return errorHandler("Policy HTML file name is empty")
	}

	// 安全检查
	if strings.Contains(htmlFile, "..") || strings.Contains(htmlFile, "/") {
		utils.LogError("COMPRESS", "ServeCompressedPolicyHTML", fmt.Errorf("invalid Policy HTML file name: %s", htmlFile), "")
		return errorHandler("Invalid Policy HTML file name")
	}

	// 构建 .br 文件路径
	brPath := filepath.Join(basePath, "policy/pages", htmlFile+".html"+brotliExtension)

	// 检查文件是否存在（启动时检查）
	if _, err := os.Stat(brPath); os.IsNotExist(err) {
		utils.LogWarn("COMPRESS", "Policy HTML file not found at startup", fmt.Sprintf("path=%s", brPath))
	}

	return func(c *gin.Context) {
		// 运行时再次检查文件是否存在
		if _, err := os.Stat(brPath); os.IsNotExist(err) {
			utils.LogInfo("COMPRESS", fmt.Sprintf("Policy HTML file not found: %s", brPath), "")
			c.String(404, "Page not found")
			return
		}

		// 设置响应头
		setCompressedHeaders(c, contentTypeMap[".html"], cacheControlNoCache)

		// 发送文件
		c.File(brPath)
	}
}

// ====================  私有函数 ====================

// resolveBrotliPath 解析请求路径，返回对应的 .br 文件路径
// 参数：
//   - basePath: 基础路径
//   - reqPath: 请求路径
//
// 返回：
//   - string: .br 文件完整路径
//   - error: 无法解析时返回错误
func resolveBrotliPath(basePath, reqPath string) (string, error) {
	var brPath string
	var relPath string

	// 规范化请求路径
	cleanReqPath := filepath.Clean(reqPath)

	switch {
	case strings.HasPrefix(cleanReqPath, "/shared/"):
		// 处理 /shared/ 路径（js, css, components）
		relPath = strings.TrimPrefix(cleanReqPath, "/shared")
		brPath = filepath.Join(basePath, "shared", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/account/assets/"):
		// 处理 /account/assets/ 路径
		relPath = strings.TrimPrefix(cleanReqPath, "/account/assets")
		brPath = filepath.Join(basePath, "account/assets", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/account/data/"):
		// 处理 /account/data/ 路径（email.json）
		relPath = strings.TrimPrefix(cleanReqPath, "/account/data")
		brPath = filepath.Join(basePath, "account/data", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/policy/assets/"):
		// 处理 /policy/assets/ 路径
		relPath = strings.TrimPrefix(cleanReqPath, "/policy/assets")
		brPath = filepath.Join(basePath, "policy/assets", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/policy/data/"):
		// 处理 /policy/data/ 路径（i18n-policy.json）
		relPath = strings.TrimPrefix(cleanReqPath, "/policy/data")
		brPath = filepath.Join(basePath, "policy/data", relPath+brotliExtension)

	case strings.HasPrefix(cleanReqPath, "/admin/assets/"):
		// 处理 /admin/assets/ 路径（js, css）
		relPath = strings.TrimPrefix(cleanReqPath, "/admin/assets")
		brPath = filepath.Join(basePath, "admin/assets", relPath+brotliExtension)

	default:
		return "", errors.New("unsupported path prefix")
	}

	// 检查相对路径是否包含路径遍历
	if strings.Contains(relPath, "..") {
		return "", errors.New("invalid path contains traversal")
	}

	return brPath, nil
}

// setCompressedHeaders 设置压缩文件的响应头
// 参数：
//   - c: Gin Context
//   - contentType: Content-Type 值
//   - cacheControl: Cache-Control 值
func setCompressedHeaders(c *gin.Context, contentType, cacheControl string) {
	c.Header("Content-Encoding", contentEncodingBrotli)
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", cacheControl)
	// 添加 Vary 头，告诉缓存服务器根据 Accept-Encoding 区分缓存
	c.Header("Vary", "Accept-Encoding")
}

// errorHandler 返回错误处理函数
// 参数：
//   - message: 错误消息
//
// 返回：
//   - func(*gin.Context): 返回 500 错误的处理函数
func errorHandler(message string) func(*gin.Context) {
	return func(c *gin.Context) {
		utils.LogError("COMPRESS", "errorHandler", fmt.Errorf("%s", message), "")
		c.String(500, "Internal server error")
	}
}
