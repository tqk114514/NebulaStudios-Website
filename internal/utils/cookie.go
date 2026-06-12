package utils

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	StateExpiryDuration = 10 * time.Minute
)

const (
	TokenCookieName        = "token"
	RefreshTokenCookieName = "refresh_token"
	LanguageCookieName     = "selectedLanguage"
	LinkTokenCookieName    = "link_token"
	CSRFTokenName          = "csrf_token"
)

const (
	DefaultCookieMaxAge = int(60 * 24 * time.Hour / time.Second)
	DefaultCookiePath   = "/"
	DefaultCookieDomain = "www.nebulastudios.top"
)

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

// SetTokenCookie 设置认证 Token Cookie
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

// GetTokenCookie 从 Gin Context 获取 Token Cookie
func GetTokenCookie(c *gin.Context) (string, error) {
	return c.Cookie(TokenCookieName)
}

// GetLanguageCookie 从 Gin Context 获取语言偏好 Cookie
func GetLanguageCookie(c *gin.Context) string {
	lang, _ := c.Cookie(LanguageCookieName)
	return lang
}

// SetTokenCookieGin 设置认证 Token Cookie（GIN 版本）
func SetTokenCookieGin(c *gin.Context, token string) {
	SetTokenCookie(c.Writer, token)
}

// ClearTokenCookieGin 清除认证 Token Cookie（GIN 版本）
func ClearTokenCookieGin(c *gin.Context) {
	ClearTokenCookie(c.Writer)
}

// SetRefreshTokenCookie 设置 Refresh Token Cookie
// path=/api/auth/refresh 限制 Cookie 只在刷新端点发送
func SetRefreshTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    token,
		Path:     "/api/auth/refresh",
		Domain:   DefaultCookieDomain,
		MaxAge:   30 * 24 * 60 * 60,
		Secure:   IsSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearRefreshTokenCookie 清除 Refresh Token Cookie
func ClearRefreshTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    "",
		Path:     "/api/auth/refresh",
		Domain:   DefaultCookieDomain,
		MaxAge:   -1,
		Secure:   IsSecure(),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// GetRefreshTokenCookie 从 Gin Context 获取 Refresh Token Cookie
func GetRefreshTokenCookie(c *gin.Context) (string, error) {
	return c.Cookie(RefreshTokenCookieName)
}

// SetRefreshTokenCookieGin 设置 Refresh Token Cookie（GIN 版本）
func SetRefreshTokenCookieGin(c *gin.Context, token string) {
	SetRefreshTokenCookie(c.Writer, token)
}

// ClearRefreshTokenCookieGin 清除 Refresh Token Cookie（GIN 版本）
func ClearRefreshTokenCookieGin(c *gin.Context) {
	ClearRefreshTokenCookie(c.Writer)
}

// SetLanguageCookieGin 设置语言偏好 Cookie（GIN 版本）
func SetLanguageCookieGin(c *gin.Context, language string) {
	SetLanguageCookie(c.Writer, language)
}

// SetLinkTokenCookie 设置微软账户绑定确认 Token Cookie
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
func GetLinkTokenCookie(c *gin.Context) (string, error) {
	return c.Cookie(LinkTokenCookieName)
}

// SetLinkTokenCookieGin 设置微软账户绑定确认 Token Cookie（GIN 版本）
func SetLinkTokenCookieGin(c *gin.Context, token string) {
	SetLinkTokenCookie(c.Writer, token)
}

// ClearLinkTokenCookieGin 清除微软账户绑定确认 Token Cookie（GIN 版本）
func ClearLinkTokenCookieGin(c *gin.Context) {
	ClearLinkTokenCookie(c.Writer)
}

const (
	CSRFTokenMaxAge = 86400
)

// SetCSRFCookie 设置 CSRF Token Cookie
// HttpOnly=false 是因为前端 JS 需要读取并放入请求头/表单
func SetCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFTokenName,
		Value:    token,
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   CSRFTokenMaxAge,
		Secure:   IsSecure(),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCSRFCookie 清除 CSRF Token Cookie
func ClearCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFTokenName,
		Value:    "",
		Path:     DefaultCookiePath,
		Domain:   DefaultCookieDomain,
		MaxAge:   -1,
		Secure:   IsSecure(),
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})
}

// GetCSRFCookie 从 Gin Context 获取 CSRF Token Cookie
func GetCSRFCookie(c *gin.Context) (string, error) {
	return c.Cookie(CSRFTokenName)
}

// SetCSRFCookieGin 设置 CSRF Token Cookie（GIN 版本）
func SetCSRFCookieGin(c *gin.Context, token string) {
	SetCSRFCookie(c.Writer, token)
}

// ClearCSRFCookieGin 清除 CSRF Token Cookie（GIN 版本）
func ClearCSRFCookieGin(c *gin.Context) {
	ClearCSRFCookie(c.Writer)
}
