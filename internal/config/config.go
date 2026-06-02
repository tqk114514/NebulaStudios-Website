// Package config 从环境变量加载应用配置，提供默认值、类型转换和必需项验证。
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

var (
	ErrMissingRequired = errors.New("MISSING_REQUIRED_CONFIG")
	ErrInvalidValue    = errors.New("INVALID_CONFIG_VALUE")
)

// Config 应用配置，包含所有服务运行所需的配置项
type Config struct {
	Port             string
	BaseURL          string
	CORSAllowOrigins string

	DatabaseURL string
	DBMaxConns  int

	JWTPrivateKey string
	JWTExpiresIn  time.Duration
	JWTIssuer     string
	JWTAudience   string

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string

	TurnstileSiteKey   string
	TurnstileSecretKey string
	HCaptchaSiteKey    string
	HCaptchaSecretKey  string

	MicrosoftClientID     string
	MicrosoftClientSecret string
	MicrosoftRedirectURI  string

	QREncryptionKey     string
	QRKeyDerivationSalt string

	R2URL       string
	R2AccessKey string
	R2SecretKey string
	R2Endpoint  string
	R2Bucket    string

	DefaultAvatarURL string
	DataExportSalt   string

	ImageProcessorSocket string
}

// Load 从 .env 文件和系统环境变量加载配置，验证必需项后返回
func Load() (*Config, error) {
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

	newCfg := &Config{}

	newCfg.Port = getEnv("PORT", "3000")
	newCfg.BaseURL = getEnv("BASE_URL", "http://localhost:3000")
	newCfg.CORSAllowOrigins = getEnv("CORS_ALLOW_ORIGINS", "")

	newCfg.DatabaseURL = getEnv("DATABASE_URL", "")
	dbMaxConns, err := getEnvInt("DB_MAX_CONNS", 10)
	if err != nil {
		utils.LogWarn("CONFIG", fmt.Sprintf("Invalid DB_MAX_CONNS, using default: %v", err))
	}
	newCfg.DBMaxConns = dbMaxConns

	newCfg.JWTPrivateKey = getEnv("JWT_PRIVATE_KEY", "")
	newCfg.JWTIssuer = getEnv("JWT_ISSUER", "auth-system")
	newCfg.JWTAudience = getEnv("JWT_AUDIENCE", "auth-system-users")
	jwtExpires, err := getEnvDuration("JWT_EXPIRES_IN", 60*24*time.Hour)
	if err != nil {
		utils.LogWarn("CONFIG", fmt.Sprintf("Invalid JWT_EXPIRES_IN, using default (60 days): %v", err))
	}
	newCfg.JWTExpiresIn = jwtExpires

	newCfg.SMTPHost = getEnv("SMTP_HOST", "smtp.163.com")
	smtpPort, err := getEnvInt("SMTP_PORT", 465)
	if err != nil {
		utils.LogWarn("CONFIG", fmt.Sprintf("Invalid SMTP_PORT, using default (465): %v", err))
	}
	newCfg.SMTPPort = smtpPort
	newCfg.SMTPUser = getEnvWithFallback("SMTP_USER", "EMAIL", "")
	newCfg.SMTPPassword = getEnvWithFallback("SMTP_PASSWORD", "EMAIL_KEY", "")
	newCfg.SMTPFrom = getEnvWithFallback("SMTP_FROM", "EMAIL", "")

	newCfg.TurnstileSiteKey = getEnv("TURNSTILE_SITE_KEY", "")
	newCfg.TurnstileSecretKey = getEnv("TURNSTILE_SECRET_KEY", "")

	newCfg.HCaptchaSiteKey = getEnv("HCAPTCHA_SITE_KEY", "")
	newCfg.HCaptchaSecretKey = getEnv("HCAPTCHA_SECRET_KEY", "")

	newCfg.MicrosoftClientID = getEnv("MICROSOFT_CLIENT_ID", "")
	newCfg.MicrosoftClientSecret = getEnv("MICROSOFT_CLIENT_SECRET", "")
	newCfg.MicrosoftRedirectURI = getEnv("MICROSOFT_REDIRECT_URI", "")

	newCfg.QREncryptionKey = getEnv("QR_ENCRYPTION_KEY", "")
	newCfg.QRKeyDerivationSalt = getEnv("QR_KEY_DERIVATION_SALT", "")

	newCfg.R2URL = getEnv("R2_URL", "")
	newCfg.R2AccessKey = getEnv("R2_ACCESS_KEY", "")
	newCfg.R2SecretKey = getEnv("R2_SECRET_KEY", "")
	newCfg.R2Endpoint = getEnv("R2_ENDPOINT", "")
	newCfg.R2Bucket = getEnv("R2_BUCKET", "")

	newCfg.DefaultAvatarURL = getEnv("DEFAULT_AVATAR_URL", "https://cdn01.nebulastudios.top/images/default-avatar.svg")
	newCfg.DataExportSalt = getEnv("DATA_EXPORT_SALT", "")
	newCfg.ImageProcessorSocket = getEnv("IMG_PROCESSOR_SOCKET", "/tmp/img-processor.sock")

	if err := validateConfig(newCfg); err != nil {
		return nil, err
	}

	utils.LogInfo("CONFIG", fmt.Sprintf("Configuration loaded: port=%s, db_max_conns=%d",
		newCfg.Port, newCfg.DBMaxConns))

	return newCfg, nil
}

func validateConfig(c *Config) error {
	var missingKeys []string
	var warnings []string

	if c.DatabaseURL == "" {
		missingKeys = append(missingKeys, "DATABASE_URL")
	}

	if c.JWTPrivateKey == "" {
		missingKeys = append(missingKeys, "JWT_PRIVATE_KEY")
	}

	if c.QRKeyDerivationSalt == "" {
		missingKeys = append(missingKeys, "QR_KEY_DERIVATION_SALT")
	}

	if c.TurnstileSecretKey == "" && c.HCaptchaSecretKey == "" {
		warnings = append(warnings, "No captcha configured (both TURNSTILE and HCAPTCHA are empty)")
	}

	if c.SMTPUser == "" || c.SMTPPassword == "" {
		warnings = append(warnings, "SMTP credentials incomplete (email sending will fail)")
	}

	if c.QREncryptionKey == "" {
		warnings = append(warnings, "QR_ENCRYPTION_KEY is empty (QR login will fail)")
	}

	for _, w := range warnings {
		utils.LogWarn("CONFIG", w)
	}

	if len(missingKeys) > 0 {
		errMsg := fmt.Sprintf("missing required config: %s", strings.Join(missingKeys, ", "))
		utils.LogError("CONFIG", "Validate", ErrMissingRequired, errMsg)
		return fmt.Errorf("%w: %s", ErrMissingRequired, errMsg)
	}

	return nil
}

func (c *Config) IsEmailConfigured() bool {
	return c.SMTPHost != "" && c.SMTPUser != "" && c.SMTPPassword != ""
}

func (c *Config) IsTurnstileConfigured() bool {
	return c.TurnstileSiteKey != "" && c.TurnstileSecretKey != ""
}

func (c *Config) IsHCaptchaConfigured() bool {
	return c.HCaptchaSiteKey != "" && c.HCaptchaSecretKey != ""
}

func (c *Config) IsMicrosoftOAuthConfigured() bool {
	return c.MicrosoftClientID != "" && c.MicrosoftClientSecret != "" && c.MicrosoftRedirectURI != ""
}

func (c *Config) IsQRLoginConfigured() bool {
	return c.QREncryptionKey != ""
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}

	intVal, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Errorf("%w: %s=%s is not a valid integer", ErrInvalidValue, key, value)
	}

	if intVal <= 0 {
		return defaultValue, fmt.Errorf("%w: %s=%d must be positive", ErrInvalidValue, key, intVal)
	}

	return intVal, nil
}

// getEnvDuration 解析时间间隔环境变量，支持 Go duration 格式（1h, 30m）和纯数字（视为小时）
func getEnvDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue, nil
	}

	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration, nil
	}

	hours, err := strconv.Atoi(value)
	if err == nil && hours > 0 {
		return time.Duration(hours) * time.Hour, nil
	}

	return defaultValue, fmt.Errorf("%w: %s=%s is not a valid duration", ErrInvalidValue, key, value)
}

// getEnvWithFallback 获取环境变量，优先使用主键名，为空时回退到兼容旧版变量名
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
