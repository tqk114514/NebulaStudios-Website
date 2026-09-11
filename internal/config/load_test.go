package config

import (
	"strings"
	"testing"
	"time"
)

// setRequiredEnv 设置能通过必需项校验的最小环境变量集
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	t.Setenv("JWT_PRIVATE_KEY", "-----BEGIN EC PRIVATE KEY-----")
	t.Setenv("EMAIL_WHITELIST_DOMAINS", "example.com")
	t.Setenv("CAPTCHA_ENABLED", "false")
}

func TestLoadMissingRequiredConfig(t *testing.T) {
	tests := []struct {
		name    string
		unset   string // 需要清掉的环境变量
		wantIn  string
		wantErr bool
	}{
		{"database url", "DATABASE_URL", "DATABASE_URL", true},
		{"jwt key", "JWT_PRIVATE_KEY", "JWT_PRIVATE_KEY", true},
		{"email whitelist", "EMAIL_WHITELIST_DOMAINS", "EMAIL_WHITELIST_DOMAINS", true},
		{"captcha switch", "CAPTCHA_ENABLED", "CAPTCHA_ENABLED", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv(tt.unset, "")

			cfg, err := Load()
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Load() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantIn) {
				t.Errorf("error %q should mention %q", err.Error(), tt.wantIn)
			}
			if cfg != nil {
				t.Error("Load() should return nil config on validation failure")
			}
		})
	}
}

// CAPTCHA_ENABLED=true 时必须同时提供 Turnstile 密钥
func TestLoadCaptchaEnabledRequiresKeys(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CAPTCHA_ENABLED", "true")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TURNSTILE") {
		t.Fatalf("Load() error = %v, want missing TURNSTILE keys error", err)
	}

	t.Setenv("TURNSTILE_SITE_KEY", "site")
	t.Setenv("TURNSTILE_SECRET_KEY", "secret")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() with keys error = %v, want nil", err)
	}
}

// 非法值应回退到默认值而不是让服务启动失败
func TestLoadInvalidValuesFallBack(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DB_MAX_CONNS", "not-a-number")
	t.Setenv("JWT_EXPIRES_IN", "not-a-duration")
	t.Setenv("SMTP_PORT", "not-a-number")
	t.Setenv("SCHEMA_ALLOW_DESTRUCTIVE", "maybe")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DBMaxConns != 10 {
		t.Errorf("DBMaxConns = %d, want default 10", cfg.DBMaxConns)
	}
	if cfg.JWTExpiresIn != 60*24*time.Hour {
		t.Errorf("JWTExpiresIn = %v, want default 1440h", cfg.JWTExpiresIn)
	}
	if cfg.SMTPPort != 0 {
		t.Errorf("SMTPPort = %d, want default 0", cfg.SMTPPort)
	}
	if cfg.SchemaAllowDestructive {
		t.Error("SchemaAllowDestructive should default to false on invalid input")
	}
}

func TestLoadParsesConfiguredValues(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORT", "8080")
	t.Setenv("BASE_URL", "https://example.com")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://example.com")
	t.Setenv("DB_MAX_CONNS", "25")
	t.Setenv("SCHEMA_ALLOW_DESTRUCTIVE", "true")
	t.Setenv("ACCESS_TOKEN_EXPIRY", "30m")
	t.Setenv("REFRESH_TOKEN_EXPIRY", "12h")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_PORT", "465")
	t.Setenv("SMTP_FROM", "noreply@example.com")
	t.Setenv("SMTP_FROM_NAME", "Nebula Studios")
	t.Setenv("AVATAR_DIR", "./tmp-avatars")
	t.Setenv("CDN_URL", "https://cdn.example.com")
	t.Setenv("DEFAULT_AVATAR_URL", "https://cdn.example.com/default.svg")
	t.Setenv("DATA_EXPORT_SALT", "salt")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.BaseURL != "https://example.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.DBMaxConns != 25 {
		t.Errorf("DBMaxConns = %d, want 25", cfg.DBMaxConns)
	}
	if !cfg.SchemaAllowDestructive {
		t.Error("SchemaAllowDestructive should be true")
	}
	if cfg.AccessTokenExpiry != 30*time.Minute {
		t.Errorf("AccessTokenExpiry = %v, want 30m", cfg.AccessTokenExpiry)
	}
	if cfg.RefreshTokenExpiry != 12*time.Hour {
		t.Errorf("RefreshTokenExpiry = %v, want 12h", cfg.RefreshTokenExpiry)
	}
	if cfg.SMTPPort != 465 || cfg.SMTPHost != "smtp.example.com" || cfg.SMTPFromName != "Nebula Studios" {
		t.Errorf("SMTP parsed incorrectly: %+v", cfg)
	}
	if cfg.AvatarDir != "./tmp-avatars" || cfg.CDNURL != "https://cdn.example.com" {
		t.Errorf("AvatarDir/CDNURL parsed incorrectly: %q / %q", cfg.AvatarDir, cfg.CDNURL)
	}
	if cfg.DataExportSalt != "salt" {
		t.Errorf("DataExportSalt = %q", cfg.DataExportSalt)
	}
}

func TestLoadPprofSettings(t *testing.T) {
	setRequiredEnv(t)

	t.Run("defaults", func(t *testing.T) {
		t.Setenv("PPROF_ENABLED", "")
		t.Setenv("PPROF_ADDR", "")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.PprofEnabled {
			t.Error("PprofEnabled should default to false")
		}
		if cfg.PprofAddr != "127.0.0.1:6060" {
			t.Errorf("PprofAddr = %q, want 127.0.0.1:6060", cfg.PprofAddr)
		}
		if cfg.PprofAllowRemote {
			t.Error("PprofAllowRemote should default to false")
		}
	})

	t.Run("custom loopback addr", func(t *testing.T) {
		t.Setenv("PPROF_ENABLED", "true")
		t.Setenv("PPROF_ADDR", "127.0.0.1:7000")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !cfg.PprofEnabled || cfg.PprofAddr != "127.0.0.1:7000" {
			t.Errorf("pprof config = %+v, want enabled with 127.0.0.1:7000", cfg)
		}
	})

	t.Run("invalid bool falls back to false", func(t *testing.T) {
		t.Setenv("PPROF_ENABLED", "yes-please")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.PprofEnabled {
			t.Error("invalid PPROF_ENABLED should fall back to false")
		}
	})
}

func TestGoogleProxyURLs(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"https://a.workers.dev", 1},
		{"https://a.workers.dev, https://b.workers.dev", 2},
		{" , , ", 0},
	}

	for _, tt := range tests {
		cfg := &Config{GoogleProxyURL: tt.raw}
		if got := len(cfg.GoogleProxyURLs()); got != tt.want {
			t.Errorf("GoogleProxyURLs(%q) len = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestConfiguredHelpers(t *testing.T) {
	tests := []struct {
		name  string
		cfg   Config
		check func(*Config) bool
		want  bool
	}{
		{"email configured", Config{SMTPHost: "h", SMTPUser: "u", SMTPPassword: "p"}, (*Config).IsEmailConfigured, true},
		{"email missing password", Config{SMTPHost: "h", SMTPUser: "u"}, (*Config).IsEmailConfigured, false},
		{"captcha configured", Config{TurnstileSiteKey: "s", TurnstileSecretKey: "k"}, (*Config).IsCaptchaConfigured, true},
		{"captcha missing key", Config{TurnstileSiteKey: "s"}, (*Config).IsCaptchaConfigured, false},
		{"microsoft configured", Config{MicrosoftClientID: "id", MicrosoftClientSecret: "secret"}, (*Config).IsMicrosoftOAuthConfigured, true},
		{"microsoft missing secret", Config{MicrosoftClientID: "id"}, (*Config).IsMicrosoftOAuthConfigured, false},
		{"google configured", Config{GoogleClientID: "id", GoogleClientSecret: "secret", GoogleProxyURL: "https://p"}, (*Config).IsGoogleOAuthConfigured, true},
		{"google missing proxy", Config{GoogleClientID: "id", GoogleClientSecret: "secret"}, (*Config).IsGoogleOAuthConfigured, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			if got := tt.check(&cfg); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// 纯数字的时间配置应按小时解释（历史约定）
func TestGetEnvDurationAcceptsHours(t *testing.T) {
	t.Setenv("NB_TEST_DURATION_HOURS", "36")

	got, err := getEnvDuration("NB_TEST_DURATION_HOURS", time.Hour)
	if err != nil {
		t.Fatalf("getEnvDuration error = %v", err)
	}
	if got != 36*time.Hour {
		t.Errorf("getEnvDuration = %v, want 36h", got)
	}
}
