/**
 * internal/services/captcha.go
 * 通用人机验证服务
 *
 * 功能：
 * - 支持多种验证器（Turnstile、hCaptcha、reCAPTCHA 等）
 * - 统一验证接口
 * - 根据前端指定类型验证
 * - 支持前端随机选择验证器
 *
 * 当前支持：
 * - Turnstile (Cloudflare)
 * - hCaptcha
 *
 * 依赖：
 * - Config: 验证器配置
 */

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/config"
)

// ====================  错误定义 ====================

var (
	// ErrCaptchaNilConfig 配置为空
	ErrCaptchaNilConfig = errors.New("captcha config is nil")
	// ErrCaptchaEmptySecret 密钥为空
	ErrCaptchaEmptySecret = errors.New("captcha secret key is empty")
	// ErrCaptchaEmptyToken Token 为空
	ErrCaptchaEmptyToken = errors.New("captcha token is empty")
	// ErrCaptchaFailed 验证失败
	ErrCaptchaFailed = errors.New("CAPTCHA_FAILED")
	// ErrCaptchaNetworkErr 网络错误
	ErrCaptchaNetworkErr = errors.New("CAPTCHA_NETWORK_ERROR")
	// ErrCaptchaTimeout 请求超时
	ErrCaptchaTimeout = errors.New("CAPTCHA_TIMEOUT")
	// ErrCaptchaInvalidResponse 无效的响应
	ErrCaptchaInvalidResponse = errors.New("CAPTCHA_INVALID_RESPONSE")
	// ErrCaptchaUnsupportedType 不支持的验证器类型
	ErrCaptchaUnsupportedType = errors.New("CAPTCHA_UNSUPPORTED_TYPE")
)

// ====================  常量定义 ====================

const (
	// 验证器类型
	CaptchaTypeTurnstile = "turnstile"
	CaptchaTypeHCaptcha  = "hcaptcha"
	CaptchaTypeRecaptcha = "recaptcha"

	// 验证 API 地址
	turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	hcaptchaVerifyURL  = "https://api.hcaptcha.com/siteverify"

	// 默认请求超时时间
	captchaDefaultTimeout = 10 * time.Second

	// 最大响应大小
	captchaMaxResponseSize = 1024 * 1024 // 1MB

	// JSON Content-Type
	captchaContentTypeJSON = "application/json"
)

// 错误码映射（Turnstile）
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
	"missing-input-secret":   "Secret key is missing",
	"invalid-input-secret":   "Secret key is invalid",
	"missing-input-response": "Token is missing",
	"invalid-input-response": "Token is invalid or malformed",
	"bad-request":            "Request was malformed",
	"invalid-or-already-seen-response": "Token has expired or already been used",
	"not-using-dummy-passcode":         "Not using test passcode",
	"sitekey-secret-mismatch":          "Site key and secret key mismatch",
}

// ====================  数据结构 ====================

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

// ====================  构造函数 ====================

// NewCaptchaService 创建验证服务
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - *CaptchaService: 验证服务实例
func NewCaptchaService(cfg *config.Config) *CaptchaService {
	// 参数验证
	if cfg == nil {
		log.Println("[CAPTCHA] WARN: Config is nil, service will be disabled")
		return &CaptchaService{
			enabled:   false,
			providers: make(map[string]*CaptchaProviderConfig),
			client: &http.Client{
				Timeout: captchaDefaultTimeout,
			},
		}
	}

	providers := make(map[string]*CaptchaProviderConfig)

	// 加载 Turnstile 配置
	if cfg.TurnstileSecretKey != "" && cfg.TurnstileSiteKey != "" {
		providers[CaptchaTypeTurnstile] = &CaptchaProviderConfig{
			Type:      CaptchaTypeTurnstile,
			SiteKey:   cfg.TurnstileSiteKey,
			SecretKey: cfg.TurnstileSecretKey,
		}
		log.Printf("[CAPTCHA] Turnstile configured: siteKey=%s...", truncateCaptchaKey(cfg.TurnstileSiteKey, 8))
	}

	// 加载 hCaptcha 配置
	if cfg.HCaptchaSecretKey != "" && cfg.HCaptchaSiteKey != "" {
		providers[CaptchaTypeHCaptcha] = &CaptchaProviderConfig{
			Type:      CaptchaTypeHCaptcha,
			SiteKey:   cfg.HCaptchaSiteKey,
			SecretKey: cfg.HCaptchaSecretKey,
		}
		log.Printf("[CAPTCHA] hCaptcha configured: siteKey=%s...", truncateCaptchaKey(cfg.HCaptchaSiteKey, 8))
	}

	// 检查是否有可用的验证器
	if len(providers) == 0 {
		log.Println("[CAPTCHA] WARN: No captcha providers configured, service will be disabled")
		return &CaptchaService{
			enabled:   false,
			providers: providers,
			client: &http.Client{
				Timeout: captchaDefaultTimeout,
			},
		}
	}

	log.Printf("[CAPTCHA] Service initialized: providers=%d, enabled=true", len(providers))

	return &CaptchaService{
		providers: providers,
		enabled:   true,
		client: &http.Client{
			Timeout: captchaDefaultTimeout,
		},
	}
}

// ====================  公开方法 ====================

// Verify 验证 Token
// 参数：
//   - token: 验证 Token
//   - captchaType: 验证器类型（由前端指定）
//   - remoteIP: 客户端 IP 地址（可选）
//
// 返回：
//   - error: 验证失败时返回错误
func (s *CaptchaService) Verify(token, captchaType, remoteIP string) error {
	return s.VerifyWithContext(context.Background(), token, captchaType, remoteIP)
}

// VerifyWithContext 验证 Token（带上下文）
// 参数：
//   - ctx: 上下文
//   - token: 验证 Token
//   - captchaType: 验证器类型（由前端指定，必需）
//   - remoteIP: 客户端 IP 地址（可选）
//
// 返回：
//   - error: 验证失败时返回错误
func (s *CaptchaService) VerifyWithContext(ctx context.Context, token, captchaType, remoteIP string) error {
	// 检查服务是否启用
	if !s.IsEnabled() {
		log.Println("[CAPTCHA] WARN: Service is disabled, skipping verification")
		return nil
	}

	// 参数验证
	if token == "" {
		log.Println("[CAPTCHA] WARN: Empty token provided")
		return ErrCaptchaEmptyToken
	}

	// 清理 Token
	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return ErrCaptchaEmptyToken
	}

	// 验证器类型必须由前端指定
	if captchaType == "" {
		log.Println("[CAPTCHA] WARN: Empty captcha type provided")
		return ErrCaptchaUnsupportedType
	}

	// 获取对应的验证器配置
	provider, ok := s.providers[captchaType]
	if !ok {
		log.Printf("[CAPTCHA] ERROR: Unsupported or unconfigured captcha type: %s", captchaType)
		return ErrCaptchaUnsupportedType
	}

	// 根据类型选择验证方法
	switch captchaType {
	case CaptchaTypeTurnstile:
		return s.doVerifyJSON(ctx, turnstileVerifyURL, provider.SecretKey, cleanToken, remoteIP, turnstileErrorMessages)
	case CaptchaTypeHCaptcha:
		return s.doVerifyForm(ctx, hcaptchaVerifyURL, provider.SecretKey, cleanToken, remoteIP, hcaptchaErrorMessages)
	default:
		log.Printf("[CAPTCHA] ERROR: Unsupported captcha type: %s", captchaType)
		return ErrCaptchaUnsupportedType
	}
}

// IsEnabled 检查服务是否启用
// 返回：
//   - bool: 是否启用
func (s *CaptchaService) IsEnabled() bool {
	return s != nil && s.enabled && len(s.providers) > 0
}

// GetConfig 获取前端配置（返回所有可用的验证器）
// 返回：
//   - []CaptchaConfig: 所有可用验证器的配置
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

// HasProvider 检查是否有指定类型的验证器
// 参数：
//   - captchaType: 验证器类型
//
// 返回：
//   - bool: 是否存在
func (s *CaptchaService) HasProvider(captchaType string) bool {
	if s == nil {
		return false
	}
	_, ok := s.providers[captchaType]
	return ok
}

// ====================  私有方法 ====================

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
		log.Printf("[CAPTCHA] ERROR: Failed to build request body: %v", err)
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("[CAPTCHA] ERROR: Failed to create request: %v", err)
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

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, verifyURL, strings.NewReader(formData))
	if err != nil {
		log.Printf("[CAPTCHA] ERROR: Failed to create request: %v", err)
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	return s.sendAndParseResponse(req, remoteIP, errorMessages)
}

// sendAndParseResponse 发送请求并解析响应
func (s *CaptchaService) sendAndParseResponse(req *http.Request, remoteIP string, errorMessages map[string]string) error {
	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			log.Printf("[CAPTCHA] ERROR: Request timeout: %v", err)
			return ErrCaptchaTimeout
		}
		log.Printf("[CAPTCHA] ERROR: Network error: %v", err)
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[CAPTCHA] WARN: Failed to close response body: %v", err)
		}
	}()

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		log.Printf("[CAPTCHA] ERROR: Unexpected status code: %d", resp.StatusCode)
		return fmt.Errorf("%w: status code %d", ErrCaptchaFailed, resp.StatusCode)
	}

	// 读取响应
	body, err := io.ReadAll(io.LimitReader(resp.Body, captchaMaxResponseSize))
	if err != nil {
		log.Printf("[CAPTCHA] ERROR: Failed to read response: %v", err)
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}

	// 解析响应
	var result CaptchaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[CAPTCHA] ERROR: Failed to parse response: %v", err)
		return fmt.Errorf("%w: %v", ErrCaptchaInvalidResponse, err)
	}

	// 检查验证结果
	if !result.Success {
		errorMsg := formatCaptchaErrorCodes(result.ErrorCodes, errorMessages)
		log.Printf("[CAPTCHA] Verification failed: %s, ip=%s", errorMsg, remoteIP)
		return ErrCaptchaFailed
	}

	log.Printf("[CAPTCHA] Verification successful: hostname=%s, ip=%s", result.Hostname, remoteIP)
	return nil
}

// ====================  辅助函数 ====================

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
