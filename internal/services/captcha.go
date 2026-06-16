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
	"sync"
	"time"

	"auth-system/internal/config"

	lru "github.com/hashicorp/golang-lru/v2"
)

var (
	ErrCaptchaNilConfig       = errors.New("captcha config is nil")
	ErrCaptchaEmptySecret     = errors.New("captcha secret key is empty")
	ErrCaptchaEmptyToken      = errors.New("captcha token is empty")
	ErrCaptchaFailed          = errors.New("CAPTCHA_FAILED")
	ErrCaptchaNetworkErr      = errors.New("CAPTCHA_NETWORK_ERROR")
	ErrCaptchaTimeout         = errors.New("CAPTCHA_TIMEOUT")
	ErrCaptchaInvalidResponse = errors.New("CAPTCHA_INVALID_RESPONSE")
	ErrCaptchaNotConfigured   = errors.New("CAPTCHA_NOT_CONFIGURED")
)

const (
	captchaVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	captchaDefaultTimeout  = 10 * time.Second
	captchaMaxResponseSize = 1024 * 1024
	captchaContentTypeJSON = "application/json"

	// 本地防重放：Cloudflare duplicate 检测是 best-effort，应用层需自行保证一次性
	captchaUsedTokenCapacity = 5000            // 容量覆盖 300s 窗口内的预期请求数
	captchaUsedTokenTTL      = 5 * time.Minute // 与 Turnstile token 有效期对齐
)

var captchaErrorMessages = map[string]string{
	"missing-input-secret":   "Secret key is missing",
	"invalid-input-secret":   "Secret key is invalid",
	"missing-input-response": "Token is missing",
	"invalid-input-response": "Token is invalid or malformed",
	"bad-request":            "Request was malformed",
	"timeout-or-duplicate":   "Token has expired or already been used",
	"internal-error":         "Internal error",
}

// CaptchaResponse 验证 API 响应
type CaptchaResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes,omitempty"`
	ChallengeTS string   `json:"challenge_ts,omitempty"`
	Hostname    string   `json:"hostname,omitempty"`
}

// CaptchaService 人机验证服务
type CaptchaService struct {
	siteKey   string
	secretKey string
	client    *http.Client
	enabled   bool
	// 本地防重放：记录已使用的 token，防止同一 token 在 300s 窗口内被复用
	usedTokens *lru.Cache[string, time.Time]
	mu         sync.Mutex // 保护 usedTokens 的检查+记录原子性
}

// NewCaptchaService 创建验证服务
func NewCaptchaService(cfg *config.Config) *CaptchaService {
	if cfg == nil {
		utils.LogWarn("CAPTCHA", "Config is nil, service will be disabled", "")
		return &CaptchaService{
			enabled: false,
			client: &http.Client{
				Timeout: captchaDefaultTimeout,
			},
		}
	}

	if cfg.TurnstileSiteKey == "" || cfg.TurnstileSecretKey == "" {
		panic("CAPTCHA configuration error: turnstile site key and secret key must be configured")
	}

	usedTokens, err := lru.New[string, time.Time](captchaUsedTokenCapacity)
	if err != nil {
		panic(fmt.Sprintf("CAPTCHA configuration error: failed to create used token cache: %v", err))
	}

	utils.LogInfo("CAPTCHA", fmt.Sprintf("Service initialized: siteKey=%s...", truncateCaptchaKey(cfg.TurnstileSiteKey, 8)))

	return &CaptchaService{
		siteKey:    cfg.TurnstileSiteKey,
		secretKey:  cfg.TurnstileSecretKey,
		enabled:    true,
		usedTokens: usedTokens,
		client: &http.Client{
			Timeout: captchaDefaultTimeout,
		},
	}
}

// Verify 验证 Token
func (s *CaptchaService) Verify(token, remoteIP string) error {
	return s.VerifyWithContext(context.Background(), token, remoteIP)
}

// VerifyWithContext 验证 Token（带上下文）
func (s *CaptchaService) VerifyWithContext(ctx context.Context, token, remoteIP string) error {
	if !s.IsEnabled() {
		utils.LogWarn("CAPTCHA", "Service is disabled, captcha verification cannot be performed", "")
		return ErrCaptchaNotConfigured
	}

	if token == "" {
		utils.LogWarn("CAPTCHA", "Empty token provided", "")
		return ErrCaptchaEmptyToken
	}

	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return ErrCaptchaEmptyToken
	}

	// 本地防重放：检查并预占 token，加锁保证原子性
	// 预占（而非验证成功后才记录）可防止并发请求在 doVerify 期间都通过检查
	s.mu.Lock()
	if _, used := s.usedTokens.Get(cleanToken); used {
		s.mu.Unlock()
		utils.LogWarn("CAPTCHA", "Token replay detected (local)", fmt.Sprintf("ip=%s", remoteIP))
		return ErrCaptchaFailed
	}
	// 预占：先记录，验证失败则回滚
	s.usedTokens.Add(cleanToken, time.Now())
	s.mu.Unlock()

	if err := s.doVerify(ctx, cleanToken, remoteIP); err != nil {
		// 验证失败回滚预占，允许该 token 重试
		s.mu.Lock()
		s.usedTokens.Remove(cleanToken)
		s.mu.Unlock()
		return err
	}

	return nil
}

// IsEnabled 检查服务是否启用
func (s *CaptchaService) IsEnabled() bool {
	return s != nil && s.enabled
}

// GetSiteKey 获取前端使用的 site key
func (s *CaptchaService) GetSiteKey() string {
	if s == nil || !s.enabled {
		return ""
	}
	return s.siteKey
}

// doVerify 执行验证请求
func (s *CaptchaService) doVerify(ctx context.Context, token, remoteIP string) error {
	reqBody := map[string]string{
		"secret":   s.secretKey,
		"response": token,
	}
	if remoteIP != "" {
		reqBody["remoteip"] = strings.TrimSpace(remoteIP)
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		utils.LogError("CAPTCHA", "doVerify", err, "Failed to build request body")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, captchaVerifyURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		utils.LogError("CAPTCHA", "doVerify", err, "Failed to create request")
		return fmt.Errorf("%w: %v", ErrCaptchaNetworkErr, err)
	}
	req.Header.Set("Content-Type", captchaContentTypeJSON)

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
		errorMsg := formatCaptchaErrorCodes(result.ErrorCodes)
		utils.LogWarn("CAPTCHA", "Verification failed", fmt.Sprintf("error=%s, ip=%s", errorMsg, remoteIP))
		return ErrCaptchaFailed
	}

	utils.LogInfo("CAPTCHA", fmt.Sprintf("Verification successful: hostname=%s, ip=%s", result.Hostname, remoteIP))
	return nil
}

// formatCaptchaErrorCodes 格式化错误码
func formatCaptchaErrorCodes(codes []string) string {
	if len(codes) == 0 {
		return "unknown error"
	}

	var messages []string
	for _, code := range codes {
		if msg, ok := captchaErrorMessages[code]; ok {
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
