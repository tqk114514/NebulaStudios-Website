package services

import (
	"auth-system/internal/utils"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"net/http"
	"strings"
	"time"

	"auth-system/internal/config"
)

var (
	ErrCaptchaNilConfig       = errors.New("captcha config is nil")
	ErrCaptchaEmptySecret     = errors.New("captcha secret key is empty")
	ErrCaptchaEmptyToken      = errors.New("captcha token is empty")
	ErrCaptchaFailed          = errors.New("CAPTCHA_FAILED")
	ErrCaptchaNetworkErr      = errors.New("CAPTCHA_NETWORK_ERROR")
	ErrCaptchaTimeout         = errors.New("CAPTCHA_TIMEOUT")
	ErrCaptchaInvalidResponse = errors.New("CAPTCHA_INVALID_RESPONSE")
	ErrCaptchaUnsupportedType = errors.New("CAPTCHA_UNSUPPORTED_TYPE")
)

const (
	CaptchaTypeTurnstile = "turnstile"
	CaptchaTypeHCaptcha  = "hcaptcha"
	CaptchaTypeRecaptcha = "recaptcha"

	turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	hcaptchaVerifyURL  = "https://api.hcaptcha.com/siteverify"

	captchaDefaultTimeout  = 10 * time.Second
	captchaMaxResponseSize = 1024 * 1024
	captchaContentTypeJSON = "application/json"
)

var turnstileErrorMessages = map[string]string{
	"missing-input-secret":   "Secret key is missing",
	"invalid-input-secret":   "Secret key is invalid",
	"missing-input-response": "Token is missing",
	"invalid-input-response": "Token is invalid or malformed",
	"bad-request":            "Request was malformed",
	"timeout-or-duplicate":   "Token has expired or already been used",
	"internal-error":         "Internal error",
}

// 错误码映射（hCaptcha）
var hcaptchaErrorMessages = map[string]string{
	"missing-input-secret":             "Secret key is missing",
	"invalid-input-secret":             "Secret key is invalid",
	"missing-input-response":           "Token is missing",
	"invalid-input-response":           "Token is invalid or malformed",
	"bad-request":                      "Request was malformed",
	"invalid-or-already-seen-response": "Token has expired or already been used",
	"not-using-dummy-passcode":         "Not using test passcode",
	"sitekey-secret-mismatch":          "Site key and secret key mismatch",
}

// CaptchaResponse 验证 API 响应（通用格式）
type CaptchaResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes,omitempty"`
	ChallengeTS string   `json:"challenge_ts,omitempty"`
	Hostname    string   `json:"hostname,omitempty"`
}

// CaptchaConfig 前端配置响应
type CaptchaConfig struct {
	Type    string `json:"type"`
	SiteKey string `json:"siteKey"`
}

// CaptchaProviderConfig 单个验证器配置
type CaptchaProviderConfig struct {
	Type      string
	SiteKey   string
	SecretKey string
}

// CaptchaService 通用验证服务
type CaptchaService struct {
	providers map[string]*CaptchaProviderConfig // 所有可用的验证器
	client    *http.Client
	enabled   bool
}

// NewCaptchaService 创建验证服务
func NewCaptchaService(cfg *config.Config) *CaptchaService {
	if cfg == nil {
		utils.LogWarn("CAPTCHA", "Config is nil, service will be disabled", "")
		return &CaptchaService{
			enabled:   false,
			providers: make(map[string]*CaptchaProviderConfig),
			client: &http.Client{
				Timeout: captchaDefaultTimeout,
			},
		}
	}

	providers := make(map[string]*CaptchaProviderConfig)

	if cfg.TurnstileSecretKey != "" && cfg.TurnstileSiteKey != "" {
		providers[CaptchaTypeTurnstile] = &CaptchaProviderConfig{
			Type:      CaptchaTypeTurnstile,
			SiteKey:   cfg.TurnstileSiteKey,
			SecretKey: cfg.TurnstileSecretKey,
		}
		utils.LogInfo("CAPTCHA", fmt.Sprintf("Turnstile configured: siteKey=%s...", truncateCaptchaKey(cfg.TurnstileSiteKey, 8)))
	}

	if cfg.HCaptchaSecretKey != "" && cfg.HCaptchaSiteKey != "" {
		providers[CaptchaTypeHCaptcha] = &CaptchaProviderConfig{
			Type:      CaptchaTypeHCaptcha,
			SiteKey:   cfg.HCaptchaSiteKey,
			SecretKey: cfg.HCaptchaSecretKey,
		}
		utils.LogInfo("CAPTCHA", fmt.Sprintf("hCaptcha configured: siteKey=%s...", truncateCaptchaKey(cfg.HCaptchaSiteKey, 8)))
	}

	if len(providers) == 0 {
		utils.LogWarn("CAPTCHA", "No captcha providers configured, service will be disabled", "")
		return &CaptchaService{
			enabled:   false,
			providers: providers,
			client: &http.Client{
				Timeout: captchaDefaultTimeout,
			},
		}
	}

	utils.LogInfo("CAPTCHA", fmt.Sprintf("Service initialized: providers=%d, enabled=true", len(providers)))

	return &CaptchaService{
		providers: providers,
		enabled:   true,
		client: &http.Client{
			Timeout: captchaDefaultTimeout,
		},
	}
}

// Verify 验证 Token
func (s *CaptchaService) Verify(token, captchaType, remoteIP string) error {
	return s.VerifyWithContext(context.Background(), token, captchaType, remoteIP)
}

// VerifyWithContext 验证 Token（带上下文）
func (s *CaptchaService) VerifyWithContext(ctx context.Context, token, captchaType, remoteIP string) error {
	if !s.IsEnabled() {
		utils.LogWarn("CAPTCHA", "Service is disabled, skipping verification", "")
		return nil
	}

	if token == "" {
		utils.LogWarn("CAPTCHA", "Empty token provided", "")
		return ErrCaptchaEmptyToken
	}

	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return ErrCaptchaEmptyToken
	}

	if captchaType == "" {
		utils.LogError("CAPTCHA", "Verify", fmt.Errorf("empty captcha type"), "Captcha type is required")
		return ErrCaptchaUnsupportedType
	}

	provider, ok := s.providers[captchaType]
	if !ok {
		utils.LogError("CAPTCHA", "Verify", fmt.Errorf("unsupported type: %s", captchaType), "Unsupported or unconfigured captcha type")
		return ErrCaptchaUnsupportedType
	}

	switch captchaType {
	case CaptchaTypeTurnstile:
		return s.doVerifyJSON(ctx, turnstileVerifyURL, provider.SecretKey, cleanToken, remoteIP, turnstileErrorMessages)
	case CaptchaTypeHCaptcha:
		return s.doVerifyForm(ctx, hcaptchaVerifyURL, provider.SecretKey, cleanToken, remoteIP, hcaptchaErrorMessages)
	default:
		utils.LogError("CAPTCHA", "Verify", fmt.Errorf("unsupported type: %s", captchaType), "Unsupported captcha type")
		return ErrCaptchaUnsupportedType
	}
}

// IsEnabled 检查服务是否启用
func (s *CaptchaService) IsEnabled() bool {
	return s != nil && s.enabled && len(s.providers) > 0
}

// GetConfig 获取前端配置（返回所有可用的验证器）
func (s *CaptchaService) GetConfig() []CaptchaConfig {
	if s == nil || !s.enabled {
		return []CaptchaConfig{}
	}

	configs := make([]CaptchaConfig, 0, len(s.providers))
	for _, provider := range s.providers {
		configs = append(configs, CaptchaConfig{
			Type:    provider.Type,
			SiteKey: provider.SiteKey,
		})
	}
	return configs
}

// GetProviderCount 获取可用验证器数量
// 返回：
//   - int: 验证器数量
func (s *CaptchaService) GetProviderCount() int {
	if s == nil {
		return 0
	}
	return len(s.providers)
}

// HasProvider 检查是否支持指定的验证器类型
func (s *CaptchaService) HasProvider(captchaType string) bool {
	if s == nil {
		return false
	}
	_, ok := s.providers[captchaType]
	return ok
}

// doVerifyJSON 执行 JSON 格式验证请求（Turnstile）
func (s *CaptchaService) doVerifyJSON(ctx context.Context, verifyURL, secretKey, token, remoteIP string, errorMessages map[string]string) error {
	// 构建请求体
	reqBody := map[string]string{
		"secret":   secretKey,
		"response": token,
	}
	if remoteIP != "" {
		reqBody["remoteip"] = strings.TrimSpace(remoteIP)
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		utils.LogError("CAPTCHA", "doVerifyJSON", err, "Failed to build request body")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		utils.LogError("CAPTCHA", "doVerifyJSON", err, "Failed to create request")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}
	req.Header.Set("Content-Type", captchaContentTypeJSON)

	return s.sendAndParseResponse(req, remoteIP, errorMessages)
}

// doVerifyForm 执行 form-urlencoded 格式验证请求（hCaptcha）
func (s *CaptchaService) doVerifyForm(ctx context.Context, verifyURL, secretKey, token, remoteIP string, errorMessages map[string]string) error {
	// 构建 form 数据
	formData := fmt.Sprintf("secret=%s&response=%s", secretKey, token)
	if remoteIP != "" {
		formData += fmt.Sprintf("&remoteip=%s", strings.TrimSpace(remoteIP))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, strings.NewReader(formData))
	if err != nil {
		utils.LogError("CAPTCHA", "doVerifyForm", err, "Failed to create request")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return s.sendAndParseResponse(req, remoteIP, errorMessages)
}

// sendAndParseResponse 发送请求并解析响应
func (s *CaptchaService) sendAndParseResponse(req *http.Request, remoteIP string, errorMessages map[string]string) error {
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			utils.LogError("CAPTCHA", "doVerify", err, "Request timeout")
			return ErrCaptchaTimeout
		}
		utils.LogError("CAPTCHA", "doVerify", err, "Network error")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.LogWarn("CAPTCHA", "Failed to close response body", "")
		}
	}()

	if resp.StatusCode != http.StatusOK {
		utils.LogError("CAPTCHA", "doVerify", fmt.Errorf("status code %d", resp.StatusCode), "Unexpected status code")
		return fmt.Errorf("%w: status code %d", ErrCaptchaFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, captchaMaxResponseSize))
	if err != nil {
		utils.LogError("CAPTCHA", "doVerify", err, "Failed to read response")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}

	var result CaptchaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		utils.LogError("CAPTCHA", "doVerify", err, "Failed to parse response")
		return fmt.Errorf("%w: %v", ErrCaptchaInvalidResponse, err)
	}

	if !result.Success {
		errorMsg := formatCaptchaErrorCodes(result.ErrorCodes, errorMessages)
		utils.LogWarn("CAPTCHA", "Verification failed", fmt.Sprintf("error=%s, ip=%s", errorMsg, remoteIP))
		return ErrCaptchaFailed
	}

	utils.LogInfo("CAPTCHA", fmt.Sprintf("Verification successful: hostname=%s, ip=%s", result.Hostname, remoteIP))
	return nil
}

// formatCaptchaErrorCodes 格式化错误码
func formatCaptchaErrorCodes(codes []string, errorMessages map[string]string) string {
	if len(codes) == 0 {
		return "unknown error"
	}

	var messages []string
	for _, code := range codes {
		if msg, ok := errorMessages[code]; ok {
			messages = append(messages, fmt.Sprintf("%s (%s)", msg, code))
		} else {
			messages = append(messages, code)
		}
	}

	return strings.Join(messages, ", ")
}

// truncateCaptchaKey 截断密钥用于日志显示
func truncateCaptchaKey(key string, length int) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= length {
		return key
	}
	return key[:length] + "..."
}
