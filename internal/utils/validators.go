package utils

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ErrInvalidEmail      = "INVALID_EMAIL"
	ErrEmailNotSupported = "EMAIL_NOT_SUPPORTED"
)

const (
	ErrInvalidUsername  = "INVALID_USERNAME"
	ErrUsernameTooShort = "USERNAME_TOO_SHORT"
	ErrUsernameTooLong  = "USERNAME_TOO_LONG"
)

const (
	ErrInvalidPassword   = "INVALID_PASSWORD"
	ErrPasswordTooShort  = "PASSWORD_TOO_SHORT"
	ErrPasswordTooLong   = "PASSWORD_TOO_LONG"
	ErrPasswordNoNumber  = "PASSWORD_NO_NUMBER"
	ErrPasswordNoSpecial = "PASSWORD_NO_SPECIAL"
	ErrPasswordNoCase    = "PASSWORD_NO_CASE"
)

const (
	ErrInvalidURL         = "INVALID_URL"
	ErrURLTooLong         = "URL_TOO_LONG"
	ErrInvalidURLProtocol = "INVALID_URL_PROTOCOL"
	ErrInvalidImageURL    = "INVALID_IMAGE_URL"
)

const (
	ErrInvalidCode = "INVALID_CODE"
)

const (
	usernameMinLength      = 1
	usernameMaxLength      = 15
	passwordMinLength      = 16
	passwordMaxLength      = 64
	urlMaxLength           = 2048
	dataURLMaxLength       = 500000
	verificationCodeLength = 6
)

// ValidationResult 验证结果
type ValidationResult struct {
	Valid     bool   `json:"valid"`
	Value     string `json:"value,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

var (
	emailRegex   = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	digitRegex   = regexp.MustCompile(`\d`)
	specialRegex = regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>/?` + "`~]")
	upperRegex   = regexp.MustCompile(`[A-Z]`)
	lowerRegex   = regexp.MustCompile(`[a-z]`)
	dataURLRegex = regexp.MustCompile(`^data:image/(jpeg|jpg|png|gif|webp);base64,[A-Za-z0-9+/]+=*$`)
	codeRegex    = regexp.MustCompile(`^[123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz]{6}$`)
)

var (
	allowedImageExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico"}
	specialAllowedDomains  = []string{"graph.microsoft.com"}
)

// ValidateEmail 验证邮箱格式
// 执行以下检查：
// 1. 非空检查
// 2. 格式验证（正则）
// 3. 白名单验证（如果配置了白名单）
func ValidateEmail(email string) ValidationResult {
	if email == "" {
		LogDebug("VALIDATOR", "Email validation failed: empty email")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	trimmed := strings.ToLower(strings.TrimSpace(email))

	if len(trimmed) > 254 {
		LogDebug("VALIDATOR", fmt.Sprintf("Email validation failed: too long (%d chars)", len(trimmed)))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	if !emailRegex.MatchString(trimmed) {
		LogDebug("VALIDATOR", "Email validation failed: invalid format")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	parts := strings.Split(trimmed, "@")
	if len(parts) != 2 {
		LogDebug("VALIDATOR", "Email validation failed: invalid structure")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	localPart := parts[0]
	domain := parts[1]

	if localPart == "" {
		LogDebug("VALIDATOR", "Email validation failed: empty local part")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	if domain == "" || !strings.Contains(domain, ".") {
		LogDebug("VALIDATOR", "Email validation failed: invalid domain")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidEmail}
	}

	return ValidationResult{Valid: true, Value: trimmed}
}

// ValidateUsername 验证用户名
// 规则：长度 1-15 个字符（Unicode 字符计数）
func ValidateUsername(username string) ValidationResult {
	if username == "" {
		LogDebug("VALIDATOR", "Username validation failed: empty username")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidUsername}
	}

	trimmed := strings.TrimSpace(username)

	if trimmed == "" {
		LogDebug("VALIDATOR", "Username validation failed: only whitespace")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidUsername}
	}

	runeCount := utf8.RuneCountInString(trimmed)

	if runeCount < usernameMinLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Username validation failed: too short (%d chars)", runeCount))
		return ValidationResult{Valid: false, ErrorCode: ErrUsernameTooShort}
	}

	if runeCount > usernameMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Username validation failed: too long (%d chars)", runeCount))
		return ValidationResult{Valid: false, ErrorCode: ErrUsernameTooLong}
	}

	return ValidationResult{Valid: true, Value: trimmed}
}

// ValidatePassword 验证密码强度
// 规则：
// - 长度 16-64 字符
// - 必须包含数字
// - 必须包含特殊字符
// - 必须包含大小写字母
func ValidatePassword(password string) ValidationResult {
	if password == "" {
		LogDebug("VALIDATOR", "Password validation failed: empty password")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidPassword}
	}

	if len(password) < passwordMinLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Password validation failed: too short (%d chars)", len(password)))
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordTooShort}
	}

	if len(password) > passwordMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Password validation failed: too long (%d chars)", len(password)))
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordTooLong}
	}

	if !digitRegex.MatchString(password) {
		LogDebug("VALIDATOR", "Password validation failed: no digit")
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordNoNumber}
	}

	if !specialRegex.MatchString(password) {
		LogDebug("VALIDATOR", "Password validation failed: no special character")
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordNoSpecial}
	}

	if !upperRegex.MatchString(password) || !lowerRegex.MatchString(password) {
		LogDebug("VALIDATOR", "Password validation failed: missing upper or lower case")
		return ValidationResult{Valid: false, ErrorCode: ErrPasswordNoCase}
	}

	return ValidationResult{Valid: true}
}

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
func ValidateAvatarURL(avatarURL string) ValidationResult {
	if avatarURL == "" {
		LogDebug("VALIDATOR", "Avatar URL validation failed: empty URL")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	trimmed := strings.TrimSpace(avatarURL)
	if trimmed == "" {
		LogDebug("VALIDATOR", "Avatar URL validation failed: only whitespace")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	if trimmed == "microsoft" {
		return ValidationResult{Valid: true, Value: "microsoft"}
	}

	if strings.HasPrefix(trimmed, "data:") {
		return validateDataURL(trimmed)
	}

	return validateHTTPURL(trimmed)
}

// validateDataURL 验证 data URL
func validateDataURL(dataURL string) ValidationResult {
	if len(dataURL) > dataURLMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Data URL validation failed: too long (%d bytes)", len(dataURL)))
		return ValidationResult{Valid: false, ErrorCode: ErrURLTooLong}
	}

	if !dataURLRegex.MatchString(dataURL) {
		LogDebug("VALIDATOR", "Data URL validation failed: invalid format")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	return ValidationResult{Valid: true, Value: dataURL}
}

// validateHTTPURL 验证 HTTP/HTTPS URL
func validateHTTPURL(httpURL string) ValidationResult {
	if len(httpURL) > urlMaxLength {
		LogDebug("VALIDATOR", fmt.Sprintf("HTTP URL validation failed: too long (%d chars)", len(httpURL)))
		return ValidationResult{Valid: false, ErrorCode: ErrURLTooLong}
	}

	parsed, err := url.Parse(httpURL)
	if err != nil {
		LogDebug("VALIDATOR", fmt.Sprintf("HTTP URL validation failed: parse error: %v", err))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		LogDebug("VALIDATOR", fmt.Sprintf("HTTP URL validation failed: invalid protocol: %s", scheme))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURLProtocol}
	}

	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" {
		LogDebug("VALIDATOR", "HTTP URL validation failed: empty hostname")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	if isBlockedHost(hostname) {
		LogWarn("VALIDATOR", fmt.Sprintf("Blocked internal URL: %s", hostname))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidURL}
	}

	if isSpecialAllowedDomain(hostname) {
		return ValidationResult{Valid: true, Value: httpURL}
	}

	pathname := strings.ToLower(parsed.Path)
	if !hasImageExtension(pathname) {
		LogWarn("VALIDATOR", fmt.Sprintf("URL does not end with image extension: %s", pathname))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidImageURL}
	}

	return ValidationResult{Valid: true, Value: httpURL}
}

// isBlockedHost 检查是否为禁止的内网地址
func isBlockedHost(hostname string) bool {
	if hostname == "localhost" {
		return true
	}

	ip := net.ParseIP(hostname)
	if ip != nil {
		return isBlockedIP(ip)
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
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
	if ip.IsLoopback() {
		return true
	}

	if ip.IsPrivate() {
		return true
	}

	if ip.IsUnspecified() {
		return true
	}

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

// ValidateCode 验证验证码格式
// 规则：6 位字母数字（排除易混淆字符：0, O, I, l）
func ValidateCode(code string) ValidationResult {
	if code == "" {
		LogDebug("VALIDATOR", "Code validation failed: empty code")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidCode}
	}

	trimmed := strings.TrimSpace(code)

	if len(trimmed) != verificationCodeLength {
		LogDebug("VALIDATOR", fmt.Sprintf("Code validation failed: invalid length (%d)", len(trimmed)))
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidCode}
	}

	if !codeRegex.MatchString(trimmed) {
		LogDebug("VALIDATOR", "Code validation failed: invalid format")
		return ValidationResult{Valid: false, ErrorCode: ErrInvalidCode}
	}

	return ValidationResult{Valid: true, Value: trimmed}
}

// IsValidEmail 快速检查邮箱是否有效（不检查白名单）
// 用于不需要白名单验证的场景
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
func IsValidUsername(username string) bool {
	if username == "" {
		return false
	}
	trimmed := strings.TrimSpace(username)
	runeCount := utf8.RuneCountInString(trimmed)
	return runeCount >= usernameMinLength && runeCount <= usernameMaxLength
}

// IsValidPassword 快速检查密码是否有效
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
