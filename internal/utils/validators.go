/**
 * internal/utils/validators.go
 * 数据验证模块
 *
 * 功能：
 * - 邮箱格式验证
 * - 用户名长度验证
 * - 密码强度验证
 * - 头像 URL 验证
 * - 验证码格式验证
 *
 * 安全特性：
 * - SSRF 防护（禁止内网地址）
 * - URL 协议限制（仅 http/https）
 * - 图片格式白名单
 */

package utils

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ====================  错误码定义 ====================

// 邮箱验证错误码
const (
	ErrInvalidEmail      = "INVALID_EMAIL"
	ErrEmailNotSupported = "EMAIL_NOT_SUPPORTED"
)

// 用户名验证错误码
const (
	ErrInvalidUsername  = "INVALID_USERNAME"
	ErrUsernameTooShort = "USERNAME_TOO_SHORT"
	ErrUsernameTooLong  = "USERNAME_TOO_LONG"
)

// 密码验证错误码
const (
	ErrInvalidPassword   = "INVALID_PASSWORD"
	ErrPasswordTooShort  = "PASSWORD_TOO_SHORT"
	ErrPasswordTooLong   = "PASSWORD_TOO_LONG"
	ErrPasswordNoNumber  = "PASSWORD_NO_NUMBER"
	ErrPasswordNoSpecial = "PASSWORD_NO_SPECIAL"
	ErrPasswordNoCase    = "PASSWORD_NO_CASE"
)

// URL 验证错误码
const (
	ErrInvalidURL         = "INVALID_URL"
	ErrURLTooLong         = "URL_TOO_LONG"
	ErrInvalidURLProtocol = "INVALID_URL_PROTOCOL"
	ErrInvalidImageURL    = "INVALID_IMAGE_URL"
)

// 验证码错误码
const (
	ErrInvalidCode = "INVALID_CODE"
)

// ====================  常量定义 ====================

// 验证参数
const (
	// 用户名长度限制
	usernameMinLength = 1
	usernameMaxLength = 15

	// 密码长度限制
	passwordMinLength = 16
	passwordMaxLength = 64

	// URL 长度限制
	urlMaxLength     = 2048
	dataURLMaxLength = 500000 // 约 500KB

	// 验证码长度
	verificationCodeLength = 6
)

// ====================  数据结构 ====================

// ValidationResult 验证结果
type ValidationResult struct {
	Valid     bool   `json:"valid"`
	Value     string `json:"value,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// ====================  正则表达式（编译一次，复用）====================

var (
	// 邮箱格式正则
	emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

	// 密码强度正则
	digitRegex   = regexp.MustCompile(`\d`)
	specialRegex = regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>/?` + "`~]")
	upperRegex   = regexp.MustCompile(`[A-Z]`)
	lowerRegex   = regexp.MustCompile(`[a-z]`)

	// Data URL 正则（支持常见图片格式）
	dataURLRegex = regexp.MustCompile(`^data:image/(jpeg|jpg|png|gif|webp);base64,[A-Za-z0-9+/]+=*$`)

	// 验证码正则（6位字母数字，排除易混淆字符）
	codeRegex = regexp.MustCompile(`^[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz]{6}$`)
)

// ====================  允许列表 ====================

var (
	// 允许的图片扩展名
	allowedImageExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico"}

	// 特殊允许的域名（不需要图片扩展名）
	specialAllowedDomains = []string{"graph.microsoft.com"}
)

// ====================  邮箱验证 ====================

// ValidateEmail 验证邮箱格式
// 执行以下检查：
// 1. 非空检查
// 2. 格式验证（正则）
// 3. 白名单验证（如果配置了白名单）
//
// 参数：
//   - email: 要验证的邮箱地址
//
// 返回：
//   - ValidationResult: 验证结果，包含处理后的邮箱（小写、去空格）
func ValidateEmail(email string) ValidationResult {
	// 空值检查
	if email == "" {
		LogDebug("VALIDATOR", "Email validation failed: empty email")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	// 去空格并转小写
	trimmed := strings.ToLower(strings.TrimSpace(email))

	// 长度检查（防止过长的输入）
	if len(trimmed) > 254 { // RFC 5321 限制
		LogDebug("VALIDATOR", fmt.Sprintf("Email validation failed: too long (%d chars)", len(trimmed)))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	// 格式验证
	if !emailRegex.MatchString(trimmed) {
		LogDebug("VALIDATOR", "Email validation failed: invalid format")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	// 提取域名
	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 {
		LogDebug("VALIDATOR", "Email validation failed: invalid structure")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	localPart := parts[0]
	domain := parts[1]

	// 验证本地部分不为空
	if localPart == "" {
		LogDebug("VALIDATOR", "Email validation failed: empty local part")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	// 验证域名不为空且包含点
	if domain == "" || !strings.Contains(domain, ".") {
		LogDebug("VALIDATOR", "Email validation failed: invalid domain")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	// 注意：白名单验证由 emailWhitelistRepo 处理，不在这里验证

	return ValidationResult{Valid: true, Value: trimmed}
}

// ====================  用户名验证 ====================

// ValidateUsername 验证用户名
// 规则：长度 1-15 个字符（Unicode 字符计数）
//
// 参数：
//   - username: 要验证的用户名
//
// 返回：
//   - ValidationResult: 验证结果，包含处理后的用户名（去空格）
func ValidateUsername(username string) ValidationResult {
	// 空值检查
	if username == "" {
		LogDebug("VALIDATOR", "Username validation failed: empty username")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidUsername}
	}

	// 去空格
	trimmed := strings.TrimSpace(username)

	// 验证去空格后不为空
	if trimmed == "" {
		LogDebug("VALIDATOR", "Username validation failed: only whitespace")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidUsername}
	}

	// 使用 Unicode 字符计数（支持中文等多字节字符）
	runeCount := utf8.RuneCountInString(trimmed)

	// 最小长度检查
	if runeCount < usernameMinLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Username validation failed: too short (%d chars)", runeCount))
		return ValidationResult{Valid: false, ErrorCode: ErrUsernameTooShort}
	}

	// 最大长度检查
	if runeCount > usernameMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Username validation failed: too long (%d chars)", runeCount))
		return ValidationResult{Valid: false, ErrorCode: ErrUsernameTooLong}
	}

	return ValidationResult{Valid: true, Value: trimmed}
}

// ====================  密码验证 ====================

// ValidatePassword 验证密码强度
// 规则：
// - 长度 16-64 字符
// - 必须包含数字
// - 必须包含特殊字符
// - 必须包含大小写字母
//
// 参数：
//   - password: 要验证的密码
//
// 返回：
//   - ValidationResult: 验证结果
func ValidatePassword(password string) ValidationResult {
	// 空值检查
	if password == "" {
		LogDebug("VALIDATOR", "Password validation failed: empty password")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidPassword}
	}

	// 长度检查（使用字节长度，因为密码通常是 ASCII）
	if len(password) < passwordMinLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Password validation failed: too short (%d chars)", len(password)))
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordTooShort}
	}

	if len(password) > passwordMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Password validation failed: too long (%d chars)", len(password)))
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordTooLong}
	}

	// 必须包含数字
	if !digitRegex.MatchString(password) {
		LogDebug("VALIDATOR", "Password validation failed: no digit")
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordNoNumber}
	}

	// 必须包含特殊字符
	if !specialRegex.MatchString(password) {
		LogDebug("VALIDATOR", "Password validation failed: no special character")
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordNoSpecial}
	}

	// 必须包含大小写字母
	if !upperRegex.MatchString(password) || !lowerRegex.MatchString(password) {
		LogDebug("VALIDATOR", "Password validation failed: missing upper or lower case")
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordNoCase}
	}

	return ValidationResult{Valid: true}
}

// ====================  头像 URL 验证 ====================

// ValidateAvatarURL 验证头像 URL
// 支持：
// - http/https URL（必须以图片扩展名结尾，除特殊域名外）
// - data URL（base64 编码的图片）
//
// 安全检查：
// - 禁止内网地址（防止 SSRF）
// - 限制 URL 长度
// - 限制允许的图片格式
// - 支持 "microsoft" 特殊值（使用微软头像）
//
// 参数：
//   - avatarURL: 要验证的头像 URL
//
// 返回：
//   - ValidationResult: 验证结果，包含处理后的 URL（去空格）
func ValidateAvatarURL(avatarURL string) ValidationResult {
	// 空值检查
	if avatarURL == "" {
		LogDebug("VALIDATOR", "Avatar URL validation failed: empty URL")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	// 去空格
	trimmed := strings.TrimSpace(avatarURL)
	if trimmed == "" {
		LogDebug("VALIDATOR", "Avatar URL validation failed: only whitespace")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	// 支持 "microsoft" 特殊值（使用微软头像）
	if trimmed == "microsoft" {
		return ValidationResult{Valid: true, Value: "microsoft"}
	}

	// 处理 data URL（base64 图片）
	if strings.HasPrefix(trimmed, "data:") {
		return validateDataURL(trimmed)
	}

	// 处理 http/https URL
	return validateHTTPURL(trimmed)
}

// validateDataURL 验证 data URL
func validateDataURL(dataURL string) ValidationResult {
	// 大小限制（约 500KB）
	if len(dataURL) > dataURLMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Data URL validation failed: too long (%d bytes)", len(dataURL)))
		return ValidationResult{Valid: false, ErrorCode: ErrURLTooLong}
	}

	// 格式验证（只允许安全的图片格式）
	if !dataURLRegex.MatchString(dataURL) {
		LogDebug("VALIDATOR", "Data URL validation failed: invalid format")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	return ValidationResult{Valid: true, Value: dataURL}
}

// validateHTTPURL 验证 HTTP/HTTPS URL
func validateHTTPURL(httpURL string) ValidationResult {
	// 长度限制
	if len(httpURL) > urlMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("HTTP URL validation failed: too long (%d chars)", len(httpURL)))
		return ValidationResult{Valid: false, ErrorCode: ErrURLTooLong}
	}

	// URL 格式验证
	parsed, err := url.Parse(httpURL)
	if err != nil {
		LogDebug("VALIDATOR", fmt.Sprintf("HTTP URL validation failed: parse error: %v", err))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	// 协议检查（只允许 http 和 https）
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		LogDebug("VALIDATOR", fmt.Sprintf("HTTP URL validation failed: invalid protocol: %s", scheme))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURLProtocol}
	}

	// 主机名检查
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		LogDebug("VALIDATOR", "HTTP URL validation failed: empty hostname")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	// 禁止内网地址（防止 SSRF 攻击）
	if isBlockedHost(hostname) {
		LogWarn("VALIDATOR", fmt.Sprintf("Blocked internal URL: %s", hostname))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	// 检查是否为特殊允许的域名
	if isSpecialAllowedDomain(hostname) {
		return ValidationResult{Valid: true, Value: httpURL}
	}

	// 普通 URL 必须以图片后缀结尾
	pathname := strings.ToLower(parsed.Path)
	if !hasImageExtension(pathname) {
		LogWarn("VALIDATOR", fmt.Sprintf("URL does not end with image extension: %s", pathname))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidImageURL}
	}

	return ValidationResult{Valid: true, Value: httpURL}
}

// isBlockedHost 检查是否为禁止的内网地址
func isBlockedHost(hostname string) bool {
	// localhost 检查
	if hostname == "localhost" {
		return true
	}

	// 尝试直接解析为 IP 地址
	ip := net.ParseIP(hostname)
	if ip != nil {
		return isBlockedIP(ip)
	}

	// 如果是域名，执行 DNS 解析并检查所有解析出的 IP
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// DNS 解析失败，拒绝请求（防御深度原则）
		LogWarn("VALIDATOR", fmt.Sprintf("DNS resolution failed for hostname: %s", hostname))
		return true
	}

	for _, resolvedIP := range ips {
		if isBlockedIP(resolvedIP) {
			LogWarn("VALIDATOR", fmt.Sprintf("Blocked domain pointing to internal IP: %s (IP: %s)", hostname, resolvedIP.String()))
			return true
		}
	}

	return false
}

// isBlockedIP 检查单个 IP 是否为受限的内网或本地地址
func isBlockedIP(ip net.IP) bool {
	// 检查是否为回环地址
	if ip.IsLoopback() {
		return true
	}

	// 检查是否为私有地址
	if ip.IsPrivate() {
		return true
	}

	// 检查是否为未指定地址
	if ip.IsUnspecified() {
		return true
	}

	// 检查链路本地地址
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	return false
}

// isSpecialAllowedDomain 检查是否为特殊允许的域名
func isSpecialAllowedDomain(hostname string) bool {
	for _, domain := range specialAllowedDomains {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}
	return false
}

// hasImageExtension 检查路径是否以图片扩展名结尾
func hasImageExtension(pathname string) bool {
	for _, ext := range allowedImageExtensions {
		if strings.HasSuffix(pathname, ext) {
			return true
		}
	}
	return false
}

// ====================  验证码验证 ====================

// ValidateCode 验证验证码格式
// 规则：6 位字母数字（排除易混淆字符：0, O, I, l）
//
// 参数：
//   - code: 要验证的验证码
//
// 返回：
//   - ValidationResult: 验证结果
func ValidateCode(code string) ValidationResult {
	// 空值检查
	if code == "" {
		LogDebug("VALIDATOR", "Code validation failed: empty code")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidCode}
	}

	// 去空格
	trimmed := strings.TrimSpace(code)

	// 长度检查
	if len(trimmed) != verificationCodeLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Code validation failed: invalid length (%d)", len(trimmed)))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidCode}
	}

	// 格式验证
	if !codeRegex.MatchString(trimmed) {
		LogDebug("VALIDATOR", "Code validation failed: invalid format")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidCode}
	}

	return ValidationResult{Valid: true, Value: trimmed}
}

// ====================  辅助函数 ====================

// IsValidEmail 快速检查邮箱是否有效（不检查白名单）
// 用于不需要白名单验证的场景
//
// 参数：
//   - email: 要验证的邮箱地址
//
// 返回：
//   - bool: 邮箱格式是否有效
func IsValidEmail(email string) bool {
	if email == "" {
		return false
	}
	trimmed := strings.ToLower(strings.TrimSpace(email))
	if len(trimmed) > 254 {
		return false
	}
	return emailRegex.MatchString(trimmed)
}

// IsValidUsername 快速检查用户名是否有效
//
// 参数：
//   - username: 要验证的用户名
//
// 返回：
//   - bool: 用户名是否有效
func IsValidUsername(username string) bool {
	if username == "" {
		return false
	}
	trimmed := strings.TrimSpace(username)
	runeCount := utf8.RuneCountInString(trimmed)
	return runeCount >= usernameMinLength && runeCount <= usernameMaxLength
}

// IsValidPassword 快速检查密码是否有效
//
// 参数：
//   - password: 要验证的密码
//
// 返回：
//   - bool: 密码是否有效
func IsValidPassword(password string) bool {
	if password == "" {
		return false
	}
	if len(password) < passwordMinLength || len(password) > passwordMaxLength {
		return false
	}
	return digitRegex.MatchString(password) &&
		specialRegex.MatchString(password) &&
		upperRegex.MatchString(password) &&
		lowerRegex.MatchString(password)
}
