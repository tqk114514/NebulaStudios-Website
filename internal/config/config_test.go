package config

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupGlobals() {
	cfgMu.Lock()
	cfg = nil
	cfgOnce = sync.Once{}
	cfgMu.Unlock()
}

// ====================  getEnv ====================

func TestGetEnv_KeyExists(t *testing.T) {
	t.Setenv("TEST_KEY", "hello")
	got := getEnv("TEST_KEY", "default")
	if got != "hello" {
		t.Errorf("expected 'hello', got '%s'", got)
	}
}

func TestGetEnv_KeyNotExists(t *testing.T) {
	os.Unsetenv("TEST_MISSING_KEY")
	got := getEnv("TEST_MISSING_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("expected 'fallback', got '%s'", got)
	}
}

func TestGetEnv_KeyExistsButEmpty(t *testing.T) {
	t.Setenv("TEST_EMPTY", "")
	got := getEnv("TEST_EMPTY", "default-val")
	if got != "default-val" {
		t.Errorf("expected default for empty env, got '%s'", got)
	}
}

// ====================  getEnvInt ====================

func TestGetEnvInt_ValidValue(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	val, err := getEnvInt("TEST_INT", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

func TestGetEnvInt_NotSet(t *testing.T) {
	os.Unsetenv("TEST_INT_MISSING")
	val, err := getEnvInt("TEST_INT_MISSING", 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 99 {
		t.Errorf("expected 99, got %d", val)
	}
}

func TestGetEnvInt_NotANumber(t *testing.T) {
	t.Setenv("TEST_INT_BAD", "abc")
	val, err := getEnvInt("TEST_INT_BAD", 10)
	if err == nil {
		t.Error("expected error for non-integer value")
	}
	if val != 10 {
		t.Errorf("expected default 10 on error, got %d", val)
	}
}

func TestGetEnvInt_Zero(t *testing.T) {
	t.Setenv("TEST_INT_ZERO", "0")
	val, err := getEnvInt("TEST_INT_ZERO", 10)
	if err == nil {
		t.Error("expected error for zero value")
	}
	if val != 10 {
		t.Errorf("expected default 10 on error, got %d", val)
	}
}

func TestGetEnvInt_Negative(t *testing.T) {
	t.Setenv("TEST_INT_NEG", "-5")
	val, err := getEnvInt("TEST_INT_NEG", 10)
	if err == nil {
		t.Error("expected error for negative value")
	}
	if val != 10 {
		t.Errorf("expected default 10 on error, got %d", val)
	}
}

func TestGetEnvInt_LargePositive(t *testing.T) {
	t.Setenv("TEST_INT_LARGE", "99999")
	val, err := getEnvInt("TEST_INT_LARGE", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 99999 {
		t.Errorf("expected 99999, got %d", val)
	}
}

// ====================  getEnvDuration ====================

func TestGetEnvDuration_ValidGoFormat(t *testing.T) {
	tests := []struct {
		env      string
		expected time.Duration
	}{
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"90s", 90 * time.Second},
		{"24h", 24 * time.Hour},
		{"500ms", 500 * time.Millisecond},
		{"1h30m", 90 * time.Minute},
	}

	for _, tt := range tests {
		t.Setenv("TEST_DUR", tt.env)
		got, err := getEnvDuration("TEST_DUR", time.Second)
		if err != nil {
			t.Errorf("input=%q: unexpected error: %v", tt.env, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("input=%q: expected %v, got %v", tt.env, tt.expected, got)
		}
	}
}

func TestGetEnvDuration_PlainHours(t *testing.T) {
	t.Setenv("TEST_DUR_HOURS", "48")
	got, err := getEnvDuration("TEST_DUR_HOURS", time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 48*time.Hour {
		t.Errorf("expected 48h, got %v", got)
	}
}

func TestGetEnvDuration_NotSet(t *testing.T) {
	os.Unsetenv("TEST_DUR_MISSING")
	defaultVal := 30 * time.Minute
	got, err := getEnvDuration("TEST_DUR_MISSING", defaultVal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != defaultVal {
		t.Errorf("expected %v, got %v", defaultVal, got)
	}
}

func TestGetEnvDuration_Invalid(t *testing.T) {
	t.Setenv("TEST_DUR_BAD", "not-a-duration")
	got, err := getEnvDuration("TEST_DUR_BAD", 10*time.Minute)
	if err == nil {
		t.Error("expected error for invalid duration")
	}
	if got != 10*time.Minute {
		t.Errorf("expected default on error, got %v", got)
	}
}

func TestGetEnvDuration_ZeroValue(t *testing.T) {
	t.Setenv("TEST_DUR_ZERO", "0")
	got, err := getEnvDuration("TEST_DUR_ZERO", 5*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: '0' is a valid Go duration (0s)")
	}
	if got != 0 {
		t.Errorf("expected 0s, got %v", got)
	}
}

func TestGetEnvDuration_NegativeHours(t *testing.T) {
	t.Setenv("TEST_DUR_NEG", "-3")
	got, err := getEnvDuration("TEST_DUR_NEG", 5*time.Hour)
	if err == nil {
		t.Error("expected error for negative hours")
	}
	if got != 5*time.Hour {
		t.Errorf("expected default, got %v", got)
	}
}

// ====================  getEnvWithFallback ====================

func TestGetEnvWithFallback_PrimaryExists(t *testing.T) {
	t.Setenv("PRIMARY_KEY", "primary-value")
	os.Unsetenv("FALLBACK_KEY")
	got := getEnvWithFallback("PRIMARY_KEY", "FALLBACK_KEY", "default")
	if got != "primary-value" {
		t.Errorf("expected 'primary-value', got '%s'", got)
	}
}

func TestGetEnvWithFallback_FallbackUsed(t *testing.T) {
	os.Unsetenv("PRIMARY_KEY")
	t.Setenv("FALLBACK_KEY", "fallback-value")
	got := getEnvWithFallback("PRIMARY_KEY", "FALLBACK_KEY", "default")
	if got != "fallback-value" {
		t.Errorf("expected 'fallback-value', got '%s'", got)
	}
}

func TestGetEnvWithFallback_NeitherExists(t *testing.T) {
	os.Unsetenv("PRIMARY_KEY")
	os.Unsetenv("FALLBACK_KEY")
	got := getEnvWithFallback("PRIMARY_KEY", "FALLBACK_KEY", "default-value")
	if got != "default-value" {
		t.Errorf("expected 'default-value', got '%s'", got)
	}
}

func TestGetEnvWithFallback_PrimaryEmptyFallbackExists(t *testing.T) {
	t.Setenv("PRIMARY_KEY", "")
	t.Setenv("FALLBACK_KEY", "fallback")
	got := getEnvWithFallback("PRIMARY_KEY", "FALLBACK_KEY", "default")
	if got == "default" {
		t.Error("expected fallback or primary value, not default, when primary is empty")
	}
}

// ====================  getDefaultConfig ====================

func TestGetDefaultConfig_Values(t *testing.T) {
	c := getDefaultConfig()
	if c.Port != "3000" {
		t.Errorf("expected Port=3000, got %s", c.Port)
	}
	if c.DBMaxConns != 10 {
		t.Errorf("expected DBMaxConns=10, got %d", c.DBMaxConns)
	}
	if c.JWTExpiresIn != 60*24*time.Hour {
		t.Errorf("expected JWTExpiresIn=60 days, got %v", c.JWTExpiresIn)
	}
	if c.SMTPHost != "smtp.163.com" {
		t.Errorf("expected SMTPHost=smtp.163.com, got %s", c.SMTPHost)
	}
	if c.SMTPPort != 465 {
		t.Errorf("expected SMTPPort=465, got %d", c.SMTPPort)
	}
}

// ====================  validateConfig ====================

func TestValidateConfig_AllRequiredPresent(t *testing.T) {
	c := &Config{
		DatabaseURL:         "postgres://localhost/db",
		JWTSecret:           "test-secret-32-chars-long-minimum",
		QRKeyDerivationSalt: "test-salt",
	}
	err := validateConfig(c)
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateConfig_MissingDatabaseURL(t *testing.T) {
	c := &Config{
		JWTSecret:           "test-secret-32-chars-long-minimum",
		QRKeyDerivationSalt: "test-salt",
	}
	err := validateConfig(c)
	if err == nil {
		t.Error("expected error for missing DATABASE_URL")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error should mention DATABASE_URL, got: %v", err)
	}
}

func TestValidateConfig_MissingJWTSecret(t *testing.T) {
	c := &Config{
		DatabaseURL:         "postgres://localhost/db",
		QRKeyDerivationSalt: "test-salt",
	}
	err := validateConfig(c)
	if err == nil {
		t.Error("expected error for missing JWT_SECRET")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error should mention JWT_SECRET, got: %v", err)
	}
}

func TestValidateConfig_MissingQRSalt(t *testing.T) {
	c := &Config{
		DatabaseURL: "postgres://localhost/db",
		JWTSecret:   "test-secret-32-chars-long-minimum",
	}
	err := validateConfig(c)
	if err == nil {
		t.Error("expected error for missing QR_KEY_DERIVATION_SALT")
	}
	if !strings.Contains(err.Error(), "QR_KEY_DERIVATION_SALT") {
		t.Errorf("error should mention QR_KEY_DERIVATION_SALT, got: %v", err)
	}
}

func TestValidateConfig_MultipleMissing(t *testing.T) {
	c := &Config{}
	err := validateConfig(c)
	if err == nil {
		t.Error("expected error for multiple missing keys")
	}
	msg := err.Error()
	if !strings.Contains(msg, "DATABASE_URL") || !strings.Contains(msg, "JWT_SECRET") || !strings.Contains(msg, "QR_KEY_DERIVATION_SALT") {
		t.Errorf("error should mention all missing keys, got: %v", err)
	}
}

// ====================  Config Check Methods ====================

func TestIsEmailConfigured_Full(t *testing.T) {
	c := &Config{SMTPHost: "smtp.example.com", SMTPUser: "user", SMTPPassword: "pass"}
	if !c.IsEmailConfigured() {
		t.Error("expected email configured with all fields")
	}
}

func TestIsEmailConfigured_MissingHost(t *testing.T) {
	c := &Config{SMTPUser: "user", SMTPPassword: "pass"}
	if c.IsEmailConfigured() {
		t.Error("expected not configured without host")
	}
}

func TestIsEmailConfigured_MissingUser(t *testing.T) {
	c := &Config{SMTPHost: "host", SMTPPassword: "pass"}
	if c.IsEmailConfigured() {
		t.Error("expected not configured without user")
	}
}

func TestIsEmailConfigured_MissingPassword(t *testing.T) {
	c := &Config{SMTPHost: "host", SMTPUser: "user"}
	if c.IsEmailConfigured() {
		t.Error("expected not configured without password")
	}
}

func TestIsEmailConfigured_Empty(t *testing.T) {
	c := &Config{}
	if c.IsEmailConfigured() {
		t.Error("expected not configured for empty config")
	}
}

func TestIsTurnstileConfigured_Full(t *testing.T) {
	c := &Config{TurnstileSiteKey: "site", TurnstileSecretKey: "secret"}
	if !c.IsTurnstileConfigured() {
		t.Error("expected turnstile configured")
	}
}

func TestIsTurnstileConfigured_Partial(t *testing.T) {
	c := &Config{TurnstileSiteKey: "site"}
	if c.IsTurnstileConfigured() {
		t.Error("expected not configured with partial turnstile")
	}
}

func TestIsHCaptchaConfigured_Full(t *testing.T) {
	c := &Config{HCaptchaSiteKey: "site", HCaptchaSecretKey: "secret"}
	if !c.IsHCaptchaConfigured() {
		t.Error("expected hcaptcha configured")
	}
}

func TestIsHCaptchaConfigured_Partial(t *testing.T) {
	c := &Config{HCaptchaSecretKey: "secret"}
	if c.IsHCaptchaConfigured() {
		t.Error("expected not configured with partial hcaptcha")
	}
}

func TestIsMicrosoftOAuthConfigured_Full(t *testing.T) {
	c := &Config{
		MicrosoftClientID:     "id",
		MicrosoftClientSecret: "secret",
		MicrosoftRedirectURI:  "https://example.com/callback",
	}
	if !c.IsMicrosoftOAuthConfigured() {
		t.Error("expected microsoft oauth configured")
	}
}

func TestIsMicrosoftOAuthConfigured_MissingRedirect(t *testing.T) {
	c := &Config{MicrosoftClientID: "id", MicrosoftClientSecret: "secret"}
	if c.IsMicrosoftOAuthConfigured() {
		t.Error("expected not configured without redirect uri")
	}
}

func TestIsQRLoginConfigured_WithKey(t *testing.T) {
	c := &Config{QREncryptionKey: "some-key"}
	if !c.IsQRLoginConfigured() {
		t.Error("expected QR login configured")
	}
}

func TestIsQRLoginConfigured_WithoutKey(t *testing.T) {
	c := &Config{}
	if c.IsQRLoginConfigured() {
		t.Error("expected not configured without encryption key")
	}
}

// ====================  Load ====================

func TestLoad_ValidConfig(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
	t.Setenv("JWT_SECRET", "super-secret-key-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "my-salt")

	c, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if c.Port != "3000" {
		t.Errorf("expected default port=3000, got %s", c.Port)
	}
	if c.DatabaseURL != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("unexpected DatabaseURL: %s", c.DatabaseURL)
	}
	if c.JWTIssuer != "auth-system" {
		t.Errorf("expected default issuer=auth-system, got %s", c.JWTIssuer)
	}
	if c.JWTAudience != "auth-system-users" {
		t.Errorf("expected default audience=auth-system-users, got %s", c.JWTAudience)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "my-jwt-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")
	t.Setenv("PORT", "8080")
	t.Setenv("BASE_URL", "https://custom.example.com")
	t.Setenv("CORS_ALLOW_ORIGINS", "https://a.com,https://b.com")
	t.Setenv("JWT_ISSUER", "my-issuer")
	t.Setenv("JWT_EXPIRES_IN", "720h")
	t.Setenv("DB_MAX_CONNS", "25")

	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != "8080" {
		t.Errorf("expected port=8080, got %s", c.Port)
	}
	if c.BaseURL != "https://custom.example.com" {
		t.Errorf("expected BaseURL set, got %s", c.BaseURL)
	}
	if c.CORSAllowOrigins != "https://a.com,https://b.com" {
		t.Errorf("unexpected CORSAllowOrigins: %s", c.CORSAllowOrigins)
	}
	if c.JWTIssuer != "my-issuer" {
		t.Errorf("expected issuer=my-issuer, got %s", c.JWTIssuer)
	}
	if c.JWTExpiresIn != 720*time.Hour {
		t.Errorf("expected 720h, got %v", c.JWTExpiresIn)
	}
	if c.DBMaxConns != 25 {
		t.Errorf("expected DBMaxConns=25, got %d", c.DBMaxConns)
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "my-jwt-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")

	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Port != "3000" {
		t.Errorf("expected default port=3000, got %s", c.Port)
	}
	if c.DBMaxConns != 10 {
		t.Errorf("expected default DBMaxConns=10, got %d", c.DBMaxConns)
	}
	if c.JWTExpiresIn != 60*24*time.Hour {
		t.Errorf("expected default JWTExpiresIn, got %v", c.JWTExpiresIn)
	}
	if c.SMTPHost != "smtp.163.com" {
		t.Errorf("expected default SMTPHost, got %s", c.SMTPHost)
	}
	if c.SMTPPort != 465 {
		t.Errorf("expected default SMTPPort=465, got %d", c.SMTPPort)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	setupGlobals()

	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("QR_KEY_DERIVATION_SALT")

	_, err := Load()
	if err == nil {
		t.Error("expected error for missing required config")
	}
	if !errors.Is(err, ErrMissingRequired) {
		t.Errorf("expected ErrMissingRequired, got: %v", err)
	}
}

func TestLoad_InvalidDBMaxConns(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "my-jwt-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")
	t.Setenv("DB_MAX_CONNS", "not-a-number")

	c, err := Load()
	if err != nil {
		t.Fatalf("expected no error (invalid DB_MAX_CONNS uses default), got: %v", err)
	}
	if c.DBMaxConns != 10 {
		t.Errorf("expected fallback DBMaxConns=10, got %d", c.DBMaxConns)
	}
}

func TestLoad_InvalidJWTExpiresIn(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "my-jwt-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")
	t.Setenv("JWT_EXPIRES_IN", "bad-format")

	c, err := Load()
	if err != nil {
		t.Fatalf("expected no error (invalid JWT_EXPIRES_IN uses default), got: %v", err)
	}
	if c.JWTExpiresIn != 60*24*time.Hour {
		t.Errorf("expected fallback JWTExpiresIn, got %v", c.JWTExpiresIn)
	}
}

func TestLoad_SMTPFallbackKeys(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "my-jwt-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")
	t.Setenv("EMAIL", "old-email@example.com")
	t.Setenv("EMAIL_KEY", "old-password")

	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SMTPUser != "old-email@example.com" {
		t.Errorf("expected SMTPUser from EMAIL fallback, got %s", c.SMTPUser)
	}
	if c.SMTPPassword != "old-password" {
		t.Errorf("expected SMTPPassword from EMAIL_KEY fallback, got %s", c.SMTPPassword)
	}
}

func TestLoad_SMTPPrimaryOverridesFallback(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "my-jwt-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")
	t.Setenv("SMTP_USER", "new-user@example.com")
	t.Setenv("EMAIL", "old-user@example.com")

	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.SMTPUser != "new-user@example.com" {
		t.Errorf("expected SMTP_USER to override EMAIL, got %s", c.SMTPUser)
	}
}

func TestLoad_SyncOnce(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://first/db")
	t.Setenv("JWT_SECRET", "first-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt1")

	c1, err := Load()
	if err != nil {
		t.Fatalf("first Load failed: %v", err)
	}

	t.Setenv("DATABASE_URL", "postgres://second/db")
	t.Setenv("JWT_SECRET", "second-secret-at-least-32-chars")

	c2, err := Load()
	if err != nil {
		t.Fatalf("second Load failed: %v", err)
	}

	if c1.DatabaseURL != c2.DatabaseURL {
		t.Errorf("sync.Once should return same config: %s vs %s", c1.DatabaseURL, c2.DatabaseURL)
	}
}

// ====================  Get ====================

func TestGet_AfterLoad(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	t.Setenv("JWT_SECRET", "super-secret-key-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	c := Get()
	if c == nil {
		t.Fatal("Get returned nil")
	}
	if c.DatabaseURL != loaded.DatabaseURL {
		t.Errorf("Get returned different config than Load")
	}
}

func TestGet_AutoLoad(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	t.Setenv("JWT_SECRET", "super-secret-key-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")

	c := Get()
	if c == nil {
		t.Fatal("Get returned nil on auto-load")
	}
	if c.Port != "3000" {
		t.Errorf("expected auto-loaded config with port=3000, got %s", c.Port)
	}
}

func TestGet_FallbackOnError(t *testing.T) {
	setupGlobals()

	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("QR_KEY_DERIVATION_SALT")

	c := Get()
	if c == nil {
		t.Fatal("Get should never return nil (falls back to default config)")
	}
	if c.Port != "3000" {
		t.Errorf("expected fallback config port=3000, got %s", c.Port)
	}
}

// ====================  MustGet ====================

func TestMustGet_AfterLoad(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost/test")
	t.Setenv("JWT_SECRET", "super-secret-key-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")

	if _, err := Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	c := MustGet()
	if c == nil {
		t.Fatal("MustGet returned nil after Load")
	}
}

// ====================  Reload ====================

func TestReload_PicksUpChanges(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://first/db")
	t.Setenv("JWT_SECRET", "first-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt1")

	c1, err := Load()
	if err != nil {
		t.Fatalf("initial Load failed: %v", err)
	}

	t.Setenv("DATABASE_URL", "postgres://changed/db")
	t.Setenv("JWT_SECRET", "changed-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt2")
	t.Setenv("PORT", "9090")

	if err := Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	c2 := Get()
	if c2.DatabaseURL == c1.DatabaseURL {
		t.Errorf("Reload should pick up new DATABASE_URL: old=%s new=%s", c1.DatabaseURL, c2.DatabaseURL)
	}
	if c2.Port != "9090" {
		t.Errorf("expected port=9090 after reload, got %s", c2.Port)
	}
}

func TestReload_StaleConfigNotReturned(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://initial/db")
	t.Setenv("JWT_SECRET", "initial-secret-at-least-32-chars")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt1")
	t.Setenv("PORT", "1111")

	c1, err := Load()
	if err != nil {
		t.Fatalf("initial Load failed: %v", err)
	}
	if c1.Port != "1111" {
		t.Fatalf("expected initial port=1111, got %s", c1.Port)
	}

	t.Setenv("PORT", "2222")

	if err := Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	c2 := Get()
	if c2.Port == c1.Port {
		t.Errorf("Reload should produce fresh config: old port=%s, new port=%s", c1.Port, c2.Port)
	}
}

func TestReload_FailsOnMissingRequired(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://db")
	t.Setenv("JWT_SECRET", "secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "salt")

	if _, err := Load(); err != nil {
		t.Fatalf("initial Load failed: %v", err)
	}

	os.Unsetenv("DATABASE_URL")

	if err := Reload(); err == nil {
		t.Error("expected Reload to fail with missing required config")
	}
}

// ====================  Config Edge Cases ====================

func TestConfig_EmptyStructHasSaneDefaults(t *testing.T) {
	c := &Config{}
	if c.IsEmailConfigured() {
		t.Error("empty config should not claim email is configured")
	}
	if c.IsMicrosoftOAuthConfigured() {
		t.Error("empty config should not claim MS oauth is configured")
	}
	if c.IsQRLoginConfigured() {
		t.Error("empty config should not claim QR login is configured")
	}
	if c.IsTurnstileConfigured() {
		t.Error("empty config should not claim turnstile is configured")
	}
	if c.IsHCaptchaConfigured() {
		t.Error("empty config should not claim hcaptcha is configured")
	}
}

// ====================  Concurrent Access ====================

func TestConcurrent_GetAndReload(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://concurrent/db")
	t.Setenv("JWT_SECRET", "concurrent-secret-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "concurrent-salt")

	if _, err := Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := Get()
			if c == nil {
				t.Error("Get returned nil during concurrent access")
			}
		}()
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Reload(); err != nil {
				t.Logf("Reload error: %v", err)
			}
		}()
	}

	wg.Wait()
}

func TestConcurrent_GetBeforeLoad(t *testing.T) {
	setupGlobals()

	t.Setenv("DATABASE_URL", "postgres://auto/db")
	t.Setenv("JWT_SECRET", "auto-secret-key-at-least-32-chars-long")
	t.Setenv("QR_KEY_DERIVATION_SALT", "auto-salt")

	var wg sync.WaitGroup
	results := make([]*Config, 30)

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = Get()
		}(i)
	}
	wg.Wait()

	for i := 1; i < 30; i++ {
		if results[i] != results[0] {
			t.Error("all Get() calls should return the same config instance")
			break
		}
	}
}

// ====================  Benchmark ====================

func BenchmarkGetEnv(b *testing.B) {
	b.Setenv("BENCH_KEY", "value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getEnv("BENCH_KEY", "default")
	}
}

func BenchmarkGetEnvInt(b *testing.B) {
	b.Setenv("BENCH_INT", "42")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = getEnvInt("BENCH_INT", 10)
	}
}

func BenchmarkGetEnvDuration(b *testing.B) {
	b.Setenv("BENCH_DUR", "720h")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = getEnvDuration("BENCH_DUR", time.Hour)
	}
}

func BenchmarkIsConfigured(b *testing.B) {
	c := &Config{SMTPHost: "h", SMTPUser: "u", SMTPPassword: "p"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.IsEmailConfigured()
	}
}
