package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"auth-system/internal/config"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	corsMaxAge        = "86400"
	corsAllowMethods  = "GET, POST, PUT, DELETE, OPTIONS, PATCH"
	corsAllowHeaders  = "Content-Type, Authorization, X-Requested-With, Accept, Origin, Cache-Control, X-CSRF-Token"
	corsExposeHeaders = "Content-Length, Content-Type"
)

// CORSConfig CORS 配置
type CORSConfig struct {
	AllowOrigins     []string // 允许的来源列表，为空则允许所有
	AllowCredentials bool     // 是否允许携带凭证（为 true 时不能使用 "*" 作为 Allow-Origin）
	AllowMethods     string   // 允许的 HTTP 方法
	AllowHeaders     string   // 允许的请求头
	ExposeHeaders    string   // 允许客户端访问的响应头
	MaxAge           string   // 预检请求缓存时间（秒）
}

// CORS 跨域中间件（使用默认配置），从配置读取允许的来源，支持 credentials
func CORS(cfg *config.Config) gin.HandlerFunc {
	allowOrigins := parseAllowOrigins(cfg.CORSAllowOrigins)

	corsConfig := CORSConfig{
		AllowOrigins:     allowOrigins,
		AllowCredentials: true,
		AllowMethods:     corsAllowMethods,
		AllowHeaders:     corsAllowHeaders,
		ExposeHeaders:    corsExposeHeaders,
		MaxAge:           corsMaxAge,
	}

	// 当 AllowCredentials=true 时必须配置白名单，否则存在凭证泄露安全风险
	if corsConfig.AllowCredentials && len(corsConfig.AllowOrigins) == 0 {
		utils.LogError("CORS", "CORS", nil, "FATAL: CORS_ALLOW_ORIGINS is empty but AllowCredentials=true. This is insecure.")
		utils.LogError("CORS", "CORS", nil, "Please configure a comma-separated list of allowed origins in CORS_ALLOW_ORIGINS.")
		panic("CORS configuration error: CORS_ALLOW_ORIGINS must be configured when credentials are enabled")
	}

	return CORSWithConfig(corsConfig)
}

// parseAllowOrigins 解析逗号分隔的允许来源列表
func parseAllowOrigins(originsStr string) []string {
	if originsStr == "" {
		return nil
	}
	var origins []string
	for origin := range strings.SplitSeq(originsStr, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	return origins
}

// CORSWithConfig 使用自定义配置的跨域中间件
func CORSWithConfig(config CORSConfig) gin.HandlerFunc {
	if config.AllowMethods == "" {
		config.AllowMethods = corsAllowMethods
	}
	if config.AllowHeaders == "" {
		config.AllowHeaders = corsAllowHeaders
	}
	if config.MaxAge == "" {
		config.MaxAge = corsMaxAge
	}

	allowOriginsMap := make(map[string]bool)
	for _, origin := range config.AllowOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowOriginsMap[origin] = true
		}
	}
	allowAllOrigins := len(allowOriginsMap) == 0

	// 强制要求当 AllowCredentials=true 时必须配置白名单，防止凭证泄露
	if config.AllowCredentials && allowAllOrigins {
		utils.LogError("CORS", "CORSWithConfig", nil, "FATAL: CORS configuration is insecure - AllowCredentials=true but no origin whitelist.")
		utils.LogError("CORS", "CORSWithConfig", nil, "CORS requests will be blocked until a proper origin whitelist is configured.")
		panic("CORS configuration error: origin whitelist required when credentials are enabled")
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		allowOrigin := determineAllowOrigin(origin, allowOriginsMap, allowAllOrigins, config.AllowCredentials)

		setCORSHeaders(c, allowOrigin, config)

		if c.Request.Method == http.MethodOptions {
			utils.LogDebug("CORS", fmt.Sprintf("Preflight request: origin=%s, path=%s", origin, c.Request.URL.Path))
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// determineAllowOrigin 确定响应的 Access-Control-Allow-Origin 值
func determineAllowOrigin(origin string, allowOriginsMap map[string]bool, allowAllOrigins bool, allowCredentials bool) string {
	if origin == "" {
		return ""
	}

	// 如果允许凭证，则绝对不能允许所有来源——这是 CORS 凭证泄露漏洞的关键防护
	if allowCredentials && allowAllOrigins {
		utils.LogError("CORS", "determineAllowOrigin", nil, "BLOCKED: CORS request blocked - AllowCredentials=true but no origin whitelist")
		utils.LogError("CORS", "determineAllowOrigin", nil, fmt.Sprintf("Origin: %s", origin))
		return ""
	}

	if allowAllOrigins && !allowCredentials {
		return "*"
	}

	if allowOriginsMap[origin] {
		return origin
	}

	utils.LogWarn("CORS", "Origin not allowed", fmt.Sprintf("origin=%s", origin))
	return ""
}

// setCORSHeaders 设置 CORS 响应头
func setCORSHeaders(c *gin.Context, allowOrigin string, config CORSConfig) {
	if allowOrigin == "" {
		return
	}

	c.Header("Access-Control-Allow-Origin", allowOrigin)

	if config.AllowCredentials {
		c.Header("Access-Control-Allow-Credentials", "true")
	}

	if config.AllowMethods != "" {
		c.Header("Access-Control-Allow-Methods", config.AllowMethods)
	}

	if config.AllowHeaders != "" {
		c.Header("Access-Control-Allow-Headers", config.AllowHeaders)
	}

	if config.ExposeHeaders != "" {
		c.Header("Access-Control-Expose-Headers", config.ExposeHeaders)
	}

	if config.MaxAge != "" && c.Request.Method == http.MethodOptions {
		c.Header("Access-Control-Max-Age", config.MaxAge)
	}

	c.Header("Vary", "Origin")
}
