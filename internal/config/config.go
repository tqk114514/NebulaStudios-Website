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

	JWTPrivateKey      string
	JWTExpiresIn       time.Duration
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
	JWTIssuer          string
	JWTAudience        string

	SMTPHost     string
	SMTPFrom     string
	SMTPUser     string
	SMTPPassword string
	SMTPPort     int

	TurnstileSiteKey   string
	TurnstileSecretKey string

	MicrosoftClientID     string
	MicrosoftClientSecret string

	GoogleClientID     string
	GoogleClientSecret string
	GoogleProxyURL     string

	// 代理的 Cloudflare Access Service Token（必需；代理仅接受带此凭证的请求，边缘拦截不耗 Worker 配额）
	ProxyAccessClientID     string
	ProxyAccessClientSecret string

	// Google id_token 验证：代理 Worker 签名背书的 Ed25519 验签公钥（PEM 全文）
	WorkerSigningPublicKey string

	QREncryptionKey     string
	QRKeyDerivationSalt string

	AvatarDir        string
	DefaultAvatarURL string
	DataExportSalt   string

	CDNURL string

	EmailWhitelistDomains string
}

// Load 从 .env 文件和系统环境变量加载配置，验证必需项后返回
func Load() (*Config, error) {
	if err := godotenv.Load(".env"); err != nil {
		utils.LogWarn("CONFIG", ".env file not found (this is OK if using system env vars)")
	} else {
		utils.LogInfo("CONFIG", "Loaded .env")
	}

	newCfg := &Config{}

	newCfg.Port = getEnv("PORT", "3000")
	newCfg.BaseURL = getEnv("BASE_URL", "http://localhost:3000")
	newCfg.CORSAllowOrigins = getEnv("CORS_ALLOW_ORIGINS", "")

	newCfg.DatabaseURL = getEnv("DATABASE_URL", "")
	dbMaxConns, err := getEnvInt("DB_MAX_CONNS", 10)
	if err != nil {
		utils.LogWarn("CONFIG", "Invalid DB_MAX_CONNS, using default", "error", err)
	}
	newCfg.DBMaxConns = dbMaxConns

	newCfg.JWTPrivateKey = getEnv("JWT_PRIVATE_KEY", "")
	newCfg.JWTIssuer = getEnv("JWT_ISSUER", "")
	newCfg.JWTAudience = getEnv("JWT_AUDIENCE", "")
	jwtExpires, err := getEnvDuration("JWT_EXPIRES_IN", 60*24*time.Hour)
	if err != nil {
		utils.LogWarn("CONFIG", "Invalid JWT_EXPIRES_IN, using default (60 days)", "error", err)
	}
	newCfg.JWTExpiresIn = jwtExpires

	accessTokenExpiry, err := getEnvDuration("ACCESS_TOKEN_EXPIRY", 1*time.Hour)
	if err != nil {
		utils.LogWarn("CONFIG", "Invalid ACCESS_TOKEN_EXPIRY, using default (1h)", "error", err)
	}
	newCfg.AccessTokenExpiry = accessTokenExpiry

	refreshTokenExpiry, err := getEnvDuration("REFRESH_TOKEN_EXPIRY", 30*24*time.Hour)
	if err != nil {
		utils.LogWarn("CONFIG", "Invalid REFRESH_TOKEN_EXPIRY, using default (30d)", "error", err)
	}
	newCfg.RefreshTokenExpiry = refreshTokenExpiry

	newCfg.SMTPHost = getEnv("SMTP_HOST", "")
	newCfg.SMTPFrom = getEnv("SMTP_FROM", "")
	newCfg.SMTPUser = getEnv("SMTP_USER", "")
	newCfg.SMTPPassword = getEnv("SMTP_PASSWORD", "")
	smtpPort, err := getEnvInt("SMTP_PORT", 0)
	if err != nil {
		utils.LogWarn("CONFIG", "Invalid SMTP_PORT, using default", "error", err)
	}
	newCfg.SMTPPort = smtpPort

	newCfg.TurnstileSiteKey = getEnv("TURNSTILE_SITE_KEY", "")
	newCfg.TurnstileSecretKey = getEnv("TURNSTILE_SECRET_KEY", "")

	newCfg.MicrosoftClientID = getEnv("MICROSOFT_CLIENT_ID", "")
	newCfg.MicrosoftClientSecret = getEnv("MICROSOFT_CLIENT_SECRET", "")

	newCfg.GoogleClientID = getEnv("GOOGLE_CLIENT_ID", "")
	newCfg.GoogleClientSecret = getEnv("GOOGLE_CLIENT_SECRET", "")
	newCfg.GoogleProxyURL = getEnv("GOOGLE_PROXY_URL", "")
	newCfg.ProxyAccessClientID = getEnv("GOOGLE_PROXY_ACCESS_CLIENT_ID", "")
	newCfg.ProxyAccessClientSecret = getEnv("GOOGLE_PROXY_ACCESS_CLIENT_SECRET", "")
	newCfg.WorkerSigningPublicKey = getEnv("WORKER_SIGNING_PUBLIC_KEY", "")

	newCfg.QREncryptionKey = getEnv("QR_ENCRYPTION_KEY", "")
	newCfg.QRKeyDerivationSalt = getEnv("QR_KEY_DERIVATION_SALT", "")

	newCfg.AvatarDir = getEnv("AVATAR_DIR", "./data/avatars")
	newCfg.CDNURL = getEnv("CDN_URL", "")

	newCfg.DefaultAvatarURL = getEnv("DEFAULT_AVATAR_URL", "")
	newCfg.DataExportSalt = getEnv("DATA_EXPORT_SALT", "")
	newCfg.EmailWhitelistDomains = getEnv("EMAIL_WHITELIST_DOMAINS", "")

	if err := validateConfig(newCfg); err != nil {
		return nil, err
	}

	utils.LogInfo("CONFIG", "Configuration loaded", "port", newCfg.Port, "db_max_conns", newCfg.DBMaxConns)

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

	if c.EmailWhitelistDomains == "" {
		missingKeys = append(missingKeys, "EMAIL_WHITELIST_DOMAINS")
	}

	if c.TurnstileSecretKey == "" {
		warnings = append(warnings, "No captcha configured (TURNSTILE_SECRET_KEY is empty)")
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

func (c *Config) IsCaptchaConfigured() bool {
	return c.TurnstileSiteKey != "" && c.TurnstileSecretKey != ""
}

func (c *Config) IsMicrosoftOAuthConfigured() bool {
	return c.MicrosoftClientID != "" && c.MicrosoftClientSecret != ""
}

func (c *Config) IsGoogleOAuthConfigured() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != "" && len(c.GoogleProxyURLs()) > 0
}

// GoogleProxyURLs 解析 GOOGLE_PROXY_URL（逗号分隔），返回去空格后的代理地址列表。
// 支持单个或多个代理，用于故障转移：首个失败时自动切换到下一个。
func (c *Config) GoogleProxyURLs() []string {
	var urls []string
	for _, s := range strings.Split(c.GoogleProxyURL, ",") {
		if s = strings.TrimSpace(s); s != "" {
			urls = append(urls, s)
		}
	}
	return urls
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
