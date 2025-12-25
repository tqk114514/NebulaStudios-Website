/**
 * internal/config/config.go
 * 应用配置加载模块
 *
 * 功能：
 * - 从环境变量加载所有配置
 * - 提供默认值和类型转换
 * - 配置验证（必需项检查）
 * - 安全的配置访问（防止 nil panic）
 *
 * 依赖：
 * - github.com/joho/godotenv (.env 文件加载)
 */

package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// ====================  错误定义 ====================

var (
	// ErrConfigNotLoaded 配置未加载
	ErrConfigNotLoaded = errors.New("CONFIG_NOT_LOADED")

	// ErrMissingRequired 缺少必需的配置项
	ErrMissingRequired = errors.New("MISSING_REQUIRED_CONFIG")

	// ErrInvalidValue 配置值无效
	ErrInvalidValue = errors.New("INVALID_CONFIG_VALUE")

	// ErrEnvFileNotFound .env 文件未找到（仅警告，不阻止启动）
	ErrEnvFileNotFound = errors.New("ENV_FILE_NOT_FOUND")
)

// ====================  配置结构 ====================

// Config 应用配置
// 包含所有服务运行所需的配置项
type Config struct {
	// 服务器配置
	Port         string // 服务端口，默认 3001
	Environment  string // 运行环境：development/production
	IsProduction bool   // 是否为生产环境

	// 数据库配置
	DatabaseURL string // PostgreSQL 连接字符串
	DBMaxConns  int    // 最大连接数，默认 10

	// JWT 配置
	JWTSecret    string        // JWT 签名密钥（必需）
	JWTExpiresIn time.Duration // JWT 过期时间，默认 60 天

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
	QREncryptionKey string // QR 登录数据加密密钥
}

// ====================  全局配置实例 ====================

var (
	cfg     *Config      // 全局配置实例
	cfgOnce sync.Once    // 确保只加载一次
	cfgMu   sync.RWMutex // 配置读写锁
)

// ====================  配置加载 ====================

// Load 加载配置
// 从环境变量加载所有配置项，支持 .env 文件
//
// 返回：
//   - *Config: 配置实例
//   - error: 错误信息
//     - ErrMissingRequired: 缺少必需的配置项（仅生产环境）
//     - ErrInvalidValue: 配置值无效
//
// 注意：
//   - .env 文件不存在时会记录警告但不会返回错误
//   - 开发环境下缺少配置只会警告，生产环境会返回错误
func Load() (*Config, error) {
	var loadErr error

	cfgOnce.Do(func() {
		loadErr = loadConfig()
	})

	if loadErr != nil {
		return nil, loadErr
	}

	cfgMu.RLock()
	defer cfgMu.RUnlock()

	return cfg, nil
}

// loadConfig 内部配置加载函数
func loadConfig() error {
	// 加载 .env 文件
	if err := godotenv.Load(); err != nil {
		// .env 文件不存在不是致命错误，生产环境通常使用系统环境变量
		log.Printf("[CONFIG] WARN: .env file not found: %v (this is OK in production)", err)
	} else {
		log.Println("[CONFIG] Loaded .env file")
	}

	// 创建配置实例
	newCfg := &Config{}

	// 加载服务器配置
	newCfg.Port = getEnv("PORT", "3000")
	newCfg.Environment = getEnv("NODE_ENV", "development")
	newCfg.IsProduction = newCfg.Environment == "production"

	// 加载数据库配置
	newCfg.DatabaseURL = getEnv("DATABASE_URL", "")
	dbMaxConns, err := getEnvInt("DB_MAX_CONNS", 10)
	if err != nil {
		log.Printf("[CONFIG] WARN: Invalid DB_MAX_CONNS, using default: %v", err)
	}
	newCfg.DBMaxConns = dbMaxConns

	// 加载 JWT 配置
	newCfg.JWTSecret = getEnv("JWT_SECRET", "")
	jwtExpires, err := getEnvDuration("JWT_EXPIRES_IN", 60*24*time.Hour)
	if err != nil {
		log.Printf("[CONFIG] WARN: Invalid JWT_EXPIRES_IN, using default (60 days): %v", err)
	}
	newCfg.JWTExpiresIn = jwtExpires

	// 加载 SMTP 配置（兼容旧版 EMAIL/EMAIL_KEY 变量名）
	newCfg.SMTPHost = getEnv("SMTP_HOST", "smtp.163.com")
	smtpPort, err := getEnvInt("SMTP_PORT", 465)
	if err != nil {
		log.Printf("[CONFIG] WARN: Invalid SMTP_PORT, using default (465): %v", err)
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

	// 加载 QR 登录加密密钥（兼容旧版 QR_ENCRYPT_KEY 变量名）
	newCfg.QREncryptionKey = getEnvWithFallback("QR_ENCRYPTION_KEY", "QR_ENCRYPT_KEY", "")

	// 验证配置
	if err := validateConfig(newCfg); err != nil {
		return err
	}

	// 保存配置
	cfgMu.Lock()
	cfg = newCfg
	cfgMu.Unlock()

	// 记录配置加载成功（不记录敏感信息）
	log.Printf("[CONFIG] Configuration loaded: env=%s, port=%s, db_max_conns=%d",
		newCfg.Environment, newCfg.Port, newCfg.DBMaxConns)

	return nil
}

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

	// 必需配置（生产环境必须有）
	if c.DatabaseURL == "" {
		if c.IsProduction {
			missingKeys = append(missingKeys, "DATABASE_URL")
		} else {
			warnings = append(warnings, "DATABASE_URL is empty")
		}
	}

	if c.JWTSecret == "" {
		if c.IsProduction {
			missingKeys = append(missingKeys, "JWT_SECRET")
		} else {
			warnings = append(warnings, "JWT_SECRET is empty (using empty string)")
		}
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
		log.Printf("[CONFIG] WARN: %s", w)
	}

	// 生产环境缺少必需配置时返回错误
	if len(missingKeys) > 0 {
		errMsg := fmt.Sprintf("missing required config: %s", strings.Join(missingKeys, ", "))
		log.Printf("[CONFIG] ERROR: %s", errMsg)
		return fmt.Errorf("%w: %s", ErrMissingRequired, errMsg)
	}

	return nil
}

// ====================  配置访问 ====================

// Get 获取全局配置实例
// 如果配置未加载，会自动加载
//
// 返回：
//   - *Config: 配置实例（永不为 nil）
//
// 注意：
//   - 此方法会在配置未加载时自动调用 Load()
//   - 如果加载失败，会返回一个带有默认值的配置
func Get() *Config {
	cfgMu.RLock()
	if cfg != nil {
		defer cfgMu.RUnlock()
		return cfg
	}
	cfgMu.RUnlock()

	// 配置未加载，尝试加载
	loadedCfg, err := Load()
	if err != nil {
		log.Printf("[CONFIG] ERROR: Failed to load config: %v, using defaults", err)
		// 返回默认配置，避免 nil panic
		return getDefaultConfig()
	}

	return loadedCfg
}

// MustGet 获取全局配置实例（必须成功）
// 如果配置未加载或加载失败，会 panic
//
// 返回：
//   - *Config: 配置实例
//
// 注意：
//   - 仅在程序启动时使用，确保配置正确加载
//   - 运行时应使用 Get() 方法
func MustGet() *Config {
	cfgMu.RLock()
	if cfg != nil {
		defer cfgMu.RUnlock()
		return cfg
	}
	cfgMu.RUnlock()

	loadedCfg, err := Load()
	if err != nil {
		log.Fatalf("[CONFIG] FATAL: Failed to load config: %v", err)
	}

	return loadedCfg
}

// getDefaultConfig 获取默认配置
// 用于配置加载失败时的降级处理
func getDefaultConfig() *Config {
	return &Config{
		Port:         "3001",
		Environment:  "development",
		IsProduction: false,
		DBMaxConns:   10,
		JWTExpiresIn: 60 * 24 * time.Hour,
		SMTPHost:     "smtp.163.com",
		SMTPPort:     465,
	}
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
		log.Printf("[CONFIG] Using fallback key %s instead of %s", fallbackKey, primaryKey)
		return value
	}
	return defaultValue
}

// ====================  重新加载配置 ====================

// Reload 重新加载配置
// 用于运行时更新配置（如热重载）
//
// 返回：
//   - error: 加载错误
//
// 注意：
//   - 此方法会重置 sync.Once，允许重新加载
//   - 生产环境慎用，可能导致配置不一致
func Reload() error {
	log.Println("[CONFIG] Reloading configuration...")

	// 重置 once（允许重新加载）
	cfgOnce = sync.Once{}

	_, err := Load()
	if err != nil {
		log.Printf("[CONFIG] ERROR: Failed to reload config: %v", err)
		return err
	}

	log.Println("[CONFIG] Configuration reloaded successfully")
	return nil
}
