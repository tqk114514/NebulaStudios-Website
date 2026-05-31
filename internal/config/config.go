/**
 * internal/config/config.go
 * 应用配置加载模块
 *
 * 功能：
 * - 从环境变量加载所有配置
 * - 提供默认值和类型转换
 * - 配置验证（必需项检查）
 *
 * 依赖：
 * - github.com/joho/godotenv (.env 文件加载)
 */

package config

import (
	"auth-system/internal/utils"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ====================  错误定义 ====================

var (
	// ErrMissingRequired 缺少必需的配置项
	ErrMissingRequired = errors.New("MISSING_REQUIRED_CONFIG")

	// ErrInvalidValue 配置值无效
	ErrInvalidValue = errors.New("INVALID_CONFIG_VALUE")
)

// ====================  配置结构 ====================

// Config 应用配置
// 包含所有服务运行所需的配置项
type Config struct {
	// 服务器配置
	Port    string // 服务端口，默认 3000
	BaseURL string // 基础 URL（用于重定向等）

	// CORS 配置
	CORSAllowOrigins string // 允许的跨域来源，逗号分隔

	// 数据库配置
	DatabaseURL string // PostgreSQL 连接字符串
	DBMaxConns  int    // 最大连接数，默认 10

	// JWT 配置
	JWTPrivateKey string        // ECDSA P-256 私钥（PEM 格式，必需）
	JWTExpiresIn  time.Duration // JWT 过期时间，默认 60 天
	JWTIssuer     string        // JWT 签发者（iss）
	JWTAudience   string        // JWT 受众（aud）

	// SMTP 配置
	SMTPHost     string // SMTP 服务器地址
	SMTPPort     int    // SMTP 端口，默认 465
	SMTPUser     string // SMTP 用户名
	SMTPPassword string // SMTP 密码
	SMTPFrom     string // 发件人地址

	// Turnstile 配置
	TurnstileSiteKey   string // Cloudflare Turnstile 站点密钥
	TurnstileSecretKey string // Cloudflare Turnstile 密钥

	// hCaptcha 配置
	HCaptchaSiteKey   string // hCaptcha 站点密钥
	HCaptchaSecretKey string // hCaptcha 密钥

	// Microsoft OAuth 配置
	MicrosoftClientID     string // Microsoft 应用 ID
	MicrosoftClientSecret string // Microsoft 应用密钥
	MicrosoftRedirectURI  string // OAuth 回调地址

	// QR 登录加密密钥
	QREncryptionKey     string // QR 登录数据加密密钥
	QRKeyDerivationSalt string // QR 密钥派生 Salt（必需，用于确定性派生 AES 密钥）

	// AI 配置
	AIAPIKey  string // AI API 密钥
	AIBaseURL string // AI API 地址
	AIModel   string // AI 模型名称

	// R2 配置
	R2URL       string // R2 公开访问 URL
	R2AccessKey string // R2 Access Key
	R2SecretKey string // R2 Secret Key
	R2Endpoint  string // R2 Endpoint
	R2Bucket    string // R2 Bucket 名称

	// 默认头像
	DefaultAvatarURL string // 默认头像 URL

	// 数据导出加密
	DataExportSalt string // HKDF 密钥派生的 Salt1（Base64）

	// 图片处理器 Unix Socket 路径
	ImageProcessorSocket string // 图片处理器的 Unix Socket 路径
}

// ====================  配置加载 ====================

// Load 加载配置
//
// 返回：
//   - *Config: 配置实例
//   - error: 加载错误
func Load() (*Config, error) {
	// 加载 .env 文件（优先从 /var/www/.env 加载，其次当前目录）
	envPaths := []string{"/var/www/.env", ".env"}
	envLoaded := false
	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			utils.LogInfo("CONFIG", fmt.Sprintf("Loaded .env from %s", path))
			envLoaded = true
			break
		}
	}
	if !envLoaded {
		utils.LogWarn("CONFIG", ".env file not found (this is OK if using system env vars)")
	}

	// 创建配置实例
	newCfg := &Config{}

	// 加载服务器配置
	newCfg.Port = getEnv("PORT", "3000")
	newCfg.BaseURL = getEnv("BASE_URL", "http://localhost:3000")
	newCfg.CORSAllowOrigins = getEnv("CORS_ALLOW_ORIGINS", "")

	// 加载数据库配置
	newCfg.DatabaseURL = getEnv("DATABASE_URL", "")
	dbMaxConns, err := getEnvInt("DB_MAX_CONNS", 10)
	if err != nil {
		utils.LogWarn("CONFIG", fmt.Sprintf("Invalid DB_MAX_CONNS, using default: %v", err))
	}
	newCfg.DBMaxConns = dbMaxConns

	// 加载 JWT 配置
	newCfg.JWTPrivateKey = getEnv("JWT_PRIVATE_KEY", "")
	newCfg.JWTIssuer = getEnv("JWT_ISSUER", "auth-system")
	newCfg.JWTAudience = getEnv("JWT_AUDIENCE", "auth-system-users")
	jwtExpires, err := getEnvDuration("JWT_EXPIRES_IN", 60*24*time.Hour)
	if err != nil {
		utils.LogWarn("CONFIG", fmt.Sprintf("Invalid JWT_EXPIRES_IN, using default (60 days): %v", err))
	}
	newCfg.JWTExpiresIn = jwtExpires

	// 加载 SMTP 配置（兼容旧版 EMAIL/EMAIL_KEY 变量名）
	newCfg.SMTPHost = getEnv("SMTP_HOST", "smtp.163.com")
	smtpPort, err := getEnvInt("SMTP_PORT", 465)
	if err != nil {
		utils.LogWarn("CONFIG", fmt.Sprintf("Invalid SMTP_PORT, using default (465): %v", err))
	}
	newCfg.SMTPPort = smtpPort
	newCfg.SMTPUser = getEnvWithFallback("SMTP_USER", "EMAIL", "")
	newCfg.SMTPPassword = getEnvWithFallback("SMTP_PASSWORD", "EMAIL_KEY", "")
	newCfg.SMTPFrom = getEnvWithFallback("SMTP_FROM", "EMAIL", "")

	// 加载 Turnstile 配置
	newCfg.TurnstileSiteKey = getEnv("TURNSTILE_SITE_KEY", "")
	newCfg.TurnstileSecretKey = getEnv("TURNSTILE_SECRET_KEY", "")

	// 加载 hCaptcha 配置
	newCfg.HCaptchaSiteKey = getEnv("HCAPTCHA_SITE_KEY", "")
	newCfg.HCaptchaSecretKey = getEnv("HCAPTCHA_SECRET_KEY", "")

	// 加载 Microsoft OAuth 配置
	newCfg.MicrosoftClientID = getEnv("MICROSOFT_CLIENT_ID", "")
	newCfg.MicrosoftClientSecret = getEnv("MICROSOFT_CLIENT_SECRET", "")
	newCfg.MicrosoftRedirectURI = getEnv("MICROSOFT_REDIRECT_URI", "")

	// 加载 QR 登录加密密钥
	newCfg.QREncryptionKey = getEnv("QR_ENCRYPTION_KEY", "")
	newCfg.QRKeyDerivationSalt = getEnv("QR_KEY_DERIVATION_SALT", "")

	// 加载 R2 配置
	newCfg.R2URL = getEnv("R2_URL", "")
	newCfg.R2AccessKey = getEnv("R2_ACCESS_KEY", "")
	newCfg.R2SecretKey = getEnv("R2_SECRET_KEY", "")
	newCfg.R2Endpoint = getEnv("R2_ENDPOINT", "")
	newCfg.R2Bucket = getEnv("R2_BUCKET", "")

	// 加载默认头像 URL
	newCfg.DefaultAvatarURL = getEnv("DEFAULT_AVATAR_URL", "https://cdn01.nebulastudios.top/images/default-avatar.svg")

	// 加载数据导出 Salt1
	newCfg.DataExportSalt = getEnv("DATA_EXPORT_SALT", "")

	// 加载图片处理器 Socket 路径
	newCfg.ImageProcessorSocket = getEnv("IMG_PROCESSOR_SOCKET", "/tmp/img-processor.sock")

	// 验证配置
	if err := validateConfig(newCfg); err != nil {
		return nil, err
	}

	// 记录配置加载成功（不记录敏感信息）
	utils.LogInfo("CONFIG", fmt.Sprintf("Configuration loaded: port=%s, db_max_conns=%d",
		newCfg.Port, newCfg.DBMaxConns))

	return newCfg, nil
}

// ====================  配置验证 ====================

// validateConfig 验证配置
// 检查必需的配置项是否存在
//
// 参数：
//   - c: 配置实例
//
// 返回：
//   - error: 验证错误
func validateConfig(c *Config) error {
	var missingKeys []string
	var warnings []string

	// 必需配置
	if c.DatabaseURL == "" {
		missingKeys = append(missingKeys, "DATABASE_URL")
	}

	if c.JWTPrivateKey == "" {
		missingKeys = append(missingKeys, "JWT_PRIVATE_KEY")
	}

	if c.QRKeyDerivationSalt == "" {
		missingKeys = append(missingKeys, "QR_KEY_DERIVATION_SALT")
	}

	// 可选但建议配置
	if c.TurnstileSecretKey == "" && c.HCaptchaSecretKey == "" {
		warnings = append(warnings, "No captcha configured (both TURNSTILE and HCAPTCHA are empty)")
	}

	if c.SMTPUser == "" || c.SMTPPassword == "" {
		warnings = append(warnings, "SMTP credentials incomplete (email sending will fail)")
	}

	if c.QREncryptionKey == "" {
		warnings = append(warnings, "QR_ENCRYPTION_KEY is empty (QR login will fail)")
	}

	// 记录警告
	for _, w := range warnings {
		utils.LogWarn("CONFIG", w)
	}

	// 生产环境缺少必需配置时返回错误
	if len(missingKeys) > 0 {
		errMsg := fmt.Sprintf("missing required config: %s", strings.Join(missingKeys, ", "))
		utils.LogError("CONFIG", "Validate", ErrMissingRequired, errMsg)
		return fmt.Errorf("%w: %s", ErrMissingRequired, errMsg)
	}

	return nil
}

// ====================  配置检查方法 ====================

// IsEmailConfigured 检查邮件配置是否完整
// 返回 SMTP 配置是否可用
func (c *Config) IsEmailConfigured() bool {
	return c.SMTPHost != "" && c.SMTPUser != "" && c.SMTPPassword != ""
}

// IsTurnstileConfigured 检查 Turnstile 配置是否完整
// 返回 Turnstile 验证是否可用
func (c *Config) IsTurnstileConfigured() bool {
	return c.TurnstileSiteKey != "" && c.TurnstileSecretKey != ""
}

// IsHCaptchaConfigured 检查 hCaptcha 配置是否完整
// 返回 hCaptcha 验证是否可用
func (c *Config) IsHCaptchaConfigured() bool {
	return c.HCaptchaSiteKey != "" && c.HCaptchaSecretKey != ""
}

// IsMicrosoftOAuthConfigured 检查 Microsoft OAuth 配置是否完整
// 返回 Microsoft 登录是否可用
func (c *Config) IsMicrosoftOAuthConfigured() bool {
	return c.MicrosoftClientID != "" && c.MicrosoftClientSecret != "" && c.MicrosoftRedirectURI != ""
}

// IsQRLoginConfigured 检查 QR 登录配置是否完整
// 返回 QR 登录是否可用
func (c *Config) IsQRLoginConfigured() bool {
	return c.QREncryptionKey != ""
}

// ====================  辅助函数 ====================

// getEnv 获取环境变量，支持默认值
//
// 参数：
//   - key: 环境变量名
//   - defaultValue: 默认值
//
// 返回：
//   - string: 环境变量值或默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt 获取整数环境变量
//
// 参数：
//   - key: 环境变量名
//   - defaultValue: 默认值
//
// 返回：
//   - int: 环境变量值或默认值
//   - error: 解析错误（如果值存在但无法解析）
func getEnvInt(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}

	intVal, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Errorf("%w: %s=%s is not a valid integer", ErrInvalidValue, key, value)
	}

	// 验证范围
	if intVal <= 0 {
		return defaultValue, fmt.Errorf("%w: %s=%d must be positive", ErrInvalidValue, key, intVal)
	}

	return intVal, nil
}

// getEnvDuration 获取时间间隔环境变量
//
// 参数：
//   - key: 环境变量名
//   - defaultValue: 默认值
//
// 返回：
//   - time.Duration: 环境变量值或默认值
//   - error: 解析错误（如果值存在但无法解析）
//
// 支持的格式：
//   - Go duration 格式：1h, 30m, 24h, 60s
//   - 纯数字：解析为小时数
func getEnvDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}

	// 尝试解析为 Go duration 格式
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration, nil
	}

	// 尝试解析为纯数字（小时）
	hours, err := strconv.Atoi(value)
	if err == nil && hours > 0 {
		return time.Duration(hours) * time.Hour, nil
	}

	return defaultValue, fmt.Errorf("%w: %s=%s is not a valid duration", ErrInvalidValue, key, value)
}

// getEnvWithFallback 获取环境变量，支持备用键名
// 优先使用主键名，如果为空则尝试备用键名
//
// 参数：
//   - primaryKey: 主键名
//   - fallbackKey: 备用键名
//   - defaultValue: 默认值
//
// 返回：
//   - string: 环境变量值或默认值
func getEnvWithFallback(primaryKey, fallbackKey, defaultValue string) string {
	if value := os.Getenv(primaryKey); value != "" {
		return value
	}
	if value := os.Getenv(fallbackKey); value != "" {
		utils.LogInfo("CONFIG", fmt.Sprintf("Using fallback key %s instead of %s", fallbackKey, primaryKey))
		return value
	}
	return defaultValue
}
