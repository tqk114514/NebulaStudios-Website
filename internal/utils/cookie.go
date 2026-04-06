/**
 * internal/utils/cookie.go
 * Cookie 工具模块
 *
 * 功能：
 * - 统一的 Cookie 配置常量
 * - Cookie 读取和写入辅助函数
 * - 简化 Gin Context 的 Cookie 操作
 *
 * 依赖：
 * - net/http: HTTP Cookie
 * - time: 时间处理
 */

package utils

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ====================  临时常量（与 oauth 包保持一致） ====================

const (
	// StateExpiryDuration 临时定义，与 oauth 包保持一致（10 分钟）
	StateExpiryDuration = 10 * time.Minute
)

// ====================  Cookie 名称常量 ====================

const (
	// TokenCookieName 认证 Token Cookie 名称
	TokenCookieName = "token"

	// LanguageCookieName 语言偏好 Cookie 名称
	LanguageCookieName = "selectedLanguage"

	// LinkTokenCookieName 微软账户绑定确认 Token Cookie 名称
	LinkTokenCookieName = "link_token"
)

// ====================  Cookie 配置常量 ====================

const (
	// DefaultCookieMaxAge 默认 Cookie 有效期（60 天，转换为秒）
	DefaultCookieMaxAge = int(60 * 24 * time.Hour / time.Second)

	// DefaultCookiePath 默认 Cookie 路径
	DefaultCookiePath = "/"

	// DefaultCookieDomain 默认 Cookie 域名（仅对 www 子域名有效，隔离其他子域名）
	DefaultCookieDomain = "www.nebulastudios.top"
)

// secureFlag Cookie Secure 标志（由 InitSecure 在启动时设置）
var secureFlag bool

// InitSecure 初始化 Cookie Secure 标志
// 应在应用启动时调用一次，根据 BaseURL 判断是否为 HTTPS 环境
func InitSecure(secure bool) {
	secureFlag = secure
}

// IsSecure 返回当前 Cookie Secure 标志状态
func IsSecure() bool {
	return secureFlag
}

// ====================  Cookie 写入函数 ====================

// SetTokenCookie 设置认证 Token Cookie
// 参数：
//   - w: HTTP 响应写入器
//   - token: Token 值
func SetTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     TokenCookieName,
		Value:    token,
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   DefaultCookieMaxAge,
		Secure:   IsSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearTokenCookie 清除认证 Token Cookie
// 通过设置 MaxAge 为 -1 使 Cookie 立即失效
// 参数：
//   - w: HTTP 响应写入器
func ClearTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     TokenCookieName,
		Value:    "",
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   -1,
		Secure:   IsSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// SetLanguageCookie 设置语言偏好 Cookie
// 参数：
//   - w: HTTP 响应写入器
//   - language: 语言代码（如 "zh-CN", "en"）
func SetLanguageCookie(w http.ResponseWriter, language string) {
	http.SetCookie(w, &http.Cookie{
		Name:     LanguageCookieName,
		Value:    language,
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   int(365 * 24 * time.Hour / time.Second),
		Secure:   IsSecure(),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearLanguageCookie 清除语言偏好 Cookie
// 参数：
//   - w: HTTP 响应写入器
func ClearLanguageCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     LanguageCookieName,
		Value:    "",
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   -1,
		Secure:   IsSecure(),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

// ====================  Gin Context 辅助函数 ====================

// GetTokenCookie 从 Gin Context 获取 Token Cookie
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: Token 值（如果存在）
//   - error: 错误（如果 Cookie 不存在或解析失败）
func GetTokenCookie(c *gin.Context) (string, error) {
	return c.Cookie(TokenCookieName)
}

// GetLanguageCookie 从 Gin Context 获取语言偏好 Cookie
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: 语言代码（如果存在）
func GetLanguageCookie(c *gin.Context) string {
	lang, _ := c.Cookie(LanguageCookieName)
	return lang
}

// SetTokenCookieGin 设置认证 Token Cookie（GIN 版本）
// 参数：
//   - c: Gin Context
//   - token: Token 值
func SetTokenCookieGin(c *gin.Context, token string) {
	SetTokenCookie(c.Writer, token)
}

// ClearTokenCookieGin 清除认证 Token Cookie（GIN 版本）
// 参数：
//   - c: Gin Context
func ClearTokenCookieGin(c *gin.Context) {
	ClearTokenCookie(c.Writer)
}

// SetLanguageCookieGin 设置语言偏好 Cookie（GIN 版本）
// 参数：
//   - c: Gin Context
//   - language: 语言代码
func SetLanguageCookieGin(c *gin.Context, language string) {
	SetLanguageCookie(c.Writer, language)
}

// SetLinkTokenCookie 设置微软账户绑定确认 Token Cookie
// 参数：
//   - w: HTTP 响应写入器
//   - token: Token 值
func SetLinkTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     LinkTokenCookieName,
		Value:    token,
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   int(StateExpiryDuration.Seconds()),
		Secure:   IsSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearLinkTokenCookie 清除微软账户绑定确认 Token Cookie
// 参数：
//   - w: HTTP 响应写入器
func ClearLinkTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     LinkTokenCookieName,
		Value:    "",
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   -1,
		Secure:   IsSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// GetLinkTokenCookie 从 Gin Context 获取微软账户绑定确认 Token Cookie
// 参数：
//   - c: Gin Context
//
// 返回：
//   - string: Token 值（如果存在）
//   - error: 错误（如果 Cookie 不存在或解析失败）
func GetLinkTokenCookie(c *gin.Context) (string, error) {
	return c.Cookie(LinkTokenCookieName)
}

// SetLinkTokenCookieGin 设置微软账户绑定确认 Token Cookie（GIN 版本）
// 参数：
//   - c: Gin Context
//   - token: Token 值
func SetLinkTokenCookieGin(c *gin.Context, token string) {
	SetLinkTokenCookie(c.Writer, token)
}

// ClearLinkTokenCookieGin 清除微软账户绑定确认 Token Cookie（GIN 版本）
// 参数：
//   - c: Gin Context
func ClearLinkTokenCookieGin(c *gin.Context) {
	ClearLinkTokenCookie(c.Writer)
}
