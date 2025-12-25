/**
 * internal/services/turnstile.go
 * Cloudflare Turnstile 验证服务
 *
 * 功能：
 * - 验证 Turnstile Token（人机验证）
 * - 支持 IP 地址验证
 * - 错误码解析
 *
 * Turnstile 错误码说明：
 * - missing-input-secret: 密钥缺失
 * - invalid-input-secret: 密钥无效
 * - missing-input-response: Token 缺失
 * - invalid-input-response: Token 无效
 * - bad-request: 请求格式错误
 * - timeout-or-duplicate: Token 超时或重复使用
 * - internal-error: Cloudflare 内部错误
 *
 * 依赖：
 * - Cloudflare Turnstile API
 * - Config: Turnstile 配置
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
	// ErrTurnstileNilConfig 配置为空
	ErrTurnstileNilConfig = errors.New("turnstile config is nil")
	// ErrTurnstileEmptySecret 密钥为空
	ErrTurnstileEmptySecret = errors.New("turnstile secret key is empty")
	// ErrTurnstileEmptyToken Token 为空
	ErrTurnstileEmptyToken = errors.New("turnstile token is empty")
	// ErrTurnstileFailed 验证失败
	ErrTurnstileFailed = errors.New("TURNSTILE_FAILED")
	// ErrTurnstileNetworkErr 网络错误
	ErrTurnstileNetworkErr = errors.New("TURNSTILE_NETWORK_ERROR")
	// ErrTurnstileTimeout 请求超时
	ErrTurnstileTimeout = errors.New("TURNSTILE_TIMEOUT")
	// ErrTurnstileInvalidResponse 无效的响应
	ErrTurnstileInvalidResponse = errors.New("TURNSTILE_INVALID_RESPONSE")
)

// ====================  常量定义 ====================

const (
	// turnstileVerifyURL Turnstile 验证 API 地址
	turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	// defaultTimeout 默认请求超时时间
	defaultTimeout = 10 * time.Second

	// maxResponseSize 最大响应大小（防止内存溢出）
	maxResponseSize = 1024 * 1024 // 1MB

	// contentTypeJSON JSON Content-Type
	contentTypeJSON = "application/json"
)

// Turnstile 错误码映射
var turnstileErrorMessages = map[string]string{
	"missing-input-secret":   "Secret key is missing",
	"invalid-input-secret":   "Secret key is invalid",
	"missing-input-response": "Token is missing",
	"invalid-input-response": "Token is invalid or malformed",
	"bad-request":            "Request was malformed",
	"timeout-or-duplicate":   "Token has expired or already been used",
	"internal-error":         "Cloudflare internal error",
}

// ====================  数据结构 ====================

// TurnstileResponse Turnstile API 响应
type TurnstileResponse struct {
	Success     bool     `json:"success"`
	ErrorCodes  []string `json:"error-codes,omitempty"`
	ChallengeTS string   `json:"challenge_ts,omitempty"`
	Hostname    string   `json:"hostname,omitempty"`
	Action      string   `json:"action,omitempty"`
	CData       string   `json:"cdata,omitempty"`
}

// TurnstileService Turnstile 验证服务
type TurnstileService struct {
	secretKey string
	siteKey   string
	client    *http.Client
	enabled   bool
}

// ====================  构造函数 ====================

// NewTurnstileService 创建 Turnstile 服务
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - *TurnstileService: Turnstile 服务实例
func NewTurnstileService(cfg *config.Config) *TurnstileService {
	// 参数验证
	if cfg == nil {
		log.Println("[TURNSTILE] WARN: Config is nil, service will be disabled")
		return &TurnstileService{
			enabled: false,
			client: &http.Client{
				Timeout: defaultTimeout,
			},
		}
	}

	// 检查密钥
	if cfg.TurnstileSecretKey == "" {
		log.Println("[TURNSTILE] WARN: Secret key is empty, service will be disabled")
		return &TurnstileService{
			enabled: false,
			client: &http.Client{
				Timeout: defaultTimeout,
			},
		}
	}

	log.Printf("[TURNSTILE] Service initialized: enabled=true, siteKey=%s...",
		truncateKey(cfg.TurnstileSiteKey, 8))

	return &TurnstileService{
		secretKey: cfg.TurnstileSecretKey,
		siteKey:   cfg.TurnstileSiteKey,
		enabled:   true,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// NewTurnstileServiceWithValidation 创建 Turnstile 服务（带验证）
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - *TurnstileService: Turnstile 服务实例
//   - error: 配置无效时返回错误
func NewTurnstileServiceWithValidation(cfg *config.Config) (*TurnstileService, error) {
	if cfg == nil {
		return nil, ErrTurnstileNilConfig
	}

	if cfg.TurnstileSecretKey == "" {
		return nil, ErrTurnstileEmptySecret
	}

	return NewTurnstileService(cfg), nil
}

// ====================  公开方法 ====================

// VerifyToken 验证 Turnstile Token
// 参数：
//   - token: Turnstile Token
//   - remoteIP: 客户端 IP 地址（可选）
//
// 返回：
//   - error: 验证失败时返回错误
func (s *TurnstileService) VerifyToken(token, remoteIP string) error {
	return s.VerifyTokenWithContext(context.Background(), token, remoteIP)
}

// VerifyTokenWithContext 验证 Turnstile Token（带上下文）
// 参数：
//   - ctx: 上下文
//   - token: Turnstile Token
//   - remoteIP: 客户端 IP 地址（可选）
//
// 返回：
//   - error: 验证失败时返回错误
func (s *TurnstileService) VerifyTokenWithContext(ctx context.Context, token, remoteIP string) error {
	// 检查服务是否启用
	if !s.IsEnabled() {
		log.Println("[TURNSTILE] WARN: Service is disabled, skipping verification")
		return nil
	}

	// 参数验证
	if token == "" {
		log.Println("[TURNSTILE] WARN: Empty token provided")
		return ErrTurnstileEmptyToken
	}

	// 清理 Token
	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return ErrTurnstileEmptyToken
	}

	// 构建请求体
	reqBody, err := s.buildRequestBody(cleanToken, remoteIP)
	if err != nil {
		log.Printf("[TURNSTILE] ERROR: Failed to build request body: %v", err)
		return fmt.Errorf("%w: %v", ErrTurnstileNetworkErr, err)
	}

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileVerifyURL, bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("[TURNSTILE] ERROR: Failed to create request: %v", err)
		return fmt.Errorf("%w: %v", ErrTurnstileNetworkErr, err)
	}
	req.Header.Set("Content-Type", contentTypeJSON)

	// 发送请求
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
			log.Printf("[TURNSTILE] ERROR: Request timeout: %v", err)
			return ErrTurnstileTimeout
		}
		log.Printf("[TURNSTILE] ERROR: Network error: %v", err)
		return fmt.Errorf("%w: %v", ErrTurnstileNetworkErr, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("[TURNSTILE] WARN: Failed to close response body: %v", err)
		}
	}()

	// 检查 HTTP 状态码
	if resp.StatusCode != http.StatusOK {
		log.Printf("[TURNSTILE] ERROR: Unexpected status code: %d", resp.StatusCode)
		return fmt.Errorf("%w: status code %d", ErrTurnstileFailed, resp.StatusCode)
	}

	// 读取响应（限制大小）
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		log.Printf("[TURNSTILE] ERROR: Failed to read response: %v", err)
		return fmt.Errorf("%w: %v", ErrTurnstileNetworkErr, err)
	}

	// 解析响应
	var result TurnstileResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[TURNSTILE] ERROR: Failed to parse response: %v", err)
		return fmt.Errorf("%w: %v", ErrTurnstileInvalidResponse, err)
	}

	// 检查验证结果
	if !result.Success {
		errorMsg := s.formatErrorCodes(result.ErrorCodes)
		log.Printf("[TURNSTILE] Verification failed: %s, ip=%s", errorMsg, remoteIP)
		return ErrTurnstileFailed
	}

	log.Printf("[TURNSTILE] Verification successful: hostname=%s, ip=%s", result.Hostname, remoteIP)
	return nil
}

// IsEnabled 检查服务是否启用
// 返回：
//   - bool: 是否启用
func (s *TurnstileService) IsEnabled() bool {
	return s != nil && s.enabled && s.secretKey != ""
}

// GetSiteKey 获取 Site Key（用于前端）
// 返回：
//   - string: Site Key
func (s *TurnstileService) GetSiteKey() string {
	if s == nil {
		return ""
	}
	return s.siteKey
}

// ====================  私有方法 ====================

// buildRequestBody 构建请求体
// 参数：
//   - token: Turnstile Token
//   - remoteIP: 客户端 IP
//
// 返回：
//   - []byte: JSON 请求体
//   - error: 错误信息
func (s *TurnstileService) buildRequestBody(token, remoteIP string) ([]byte, error) {
	reqBody := map[string]string{
		"secret":   s.secretKey,
		"response": token,
	}

	// 添加 IP 地址（如果提供）
	if remoteIP != "" {
		cleanIP := strings.TrimSpace(remoteIP)
		if cleanIP != "" {
			reqBody["remoteip"] = cleanIP
		}
	}

	return json.Marshal(reqBody)
}

// formatErrorCodes 格式化错误码
// 参数：
//   - codes: 错误码列表
//
// 返回：
//   - string: 格式化的错误消息
func (s *TurnstileService) formatErrorCodes(codes []string) string {
	if len(codes) == 0 {
		return "unknown error"
	}

	var messages []string
	for _, code := range codes {
		if msg, ok := turnstileErrorMessages[code]; ok {
			messages = append(messages, fmt.Sprintf("%s (%s)", msg, code))
		} else {
			messages = append(messages, code)
		}
	}

	return strings.Join(messages, ", ")
}

// ====================  辅助函数 ====================

// truncateKey 截断密钥用于日志显示
// 参数：
//   - key: 密钥
//   - length: 显示长度
//
// 返回：
//   - string: 截断后的密钥
func truncateKey(key string, length int) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= length {
		return key
	}
	return key[:length] + "..."
}
