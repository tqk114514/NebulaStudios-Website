/**
 * internal/handlers/user/handler.go
 * 用户管理 API Handler - 核心结构和基础方法
 *
 * 功能：
 * - UserHandler 结构定义
 * - 构造函数
 * - 私有辅助方法
 * - 数据导出 Token 管理
 *
 * 依赖：
 * - UserRepository: 用户数据访问
 * - TokenService: 验证码管理
 * - EmailService: 邮件发送
 * - CaptchaService: 人机验证
 * - UserCache: 用户缓存
 */

package user

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
)

// ====================  错误定义 ====================

var (
	// ErrUserHandlerNilUserRepo 用户仓库为空
	ErrUserHandlerNilUserRepo = errors.New("user repository is nil")
	// ErrUserHandlerNilTokenService Token 服务为空
	ErrUserHandlerNilTokenService = errors.New("token service is nil")
	// ErrUserHandlerNilEmailService 邮件服务为空
	ErrUserHandlerNilEmailService = errors.New("email service is nil")
	// ErrUserHandlerNilCaptchaService 验证码服务为空
	ErrUserHandlerNilCaptchaService = errors.New("captcha service is nil")
	// ErrUserHandlerNilUserCache 用户缓存为空
	ErrUserHandlerNilUserCache = errors.New("user cache is nil")
	// ErrUserHandlerEmptyBaseURL BaseURL 为空
	ErrUserHandlerEmptyBaseURL = errors.New("base URL is empty")
)

// ====================  数据结构 ====================

// UserHandler 用户管理 Handler
type UserHandler struct {
	userRepo       *models.UserRepository
	userLogRepo    *models.UserLogRepository
	tokenService   *services.TokenService
	emailService   *services.EmailService
	captchaService *services.CaptchaService
	userCache      *cache.UserCache
	r2Service      *services.R2Service
	oauthService   *services.OAuthService
	baseURL        string
}

// dataExportToken 数据导出 Token（内存存储，一次性使用）
type dataExportToken struct {
	UserUID   string
	ExpiresAt time.Time
}

// dataExportTokens 数据导出 Token 存储（内存）
// 带有最大容量限制，达到上限时按 FIFO 淘汰旧条目
// FIFO 使用自增计数器实现
var (
	dataExportTokens       = make(map[string]*dataExportToken)
	dataExportTokensMu     sync.RWMutex
	dataExportCleanupOnce  sync.Once
	dataExportTokenIndex   = make(map[string]int64)
	dataExportTokenCounter int64
	dataExportStopChan     chan struct{}
)

const (
	// DataExportCleanupInterval 数据导出 Token 清理任务间隔
	DataExportCleanupInterval = 5 * time.Minute

	// maxDataExportTokensCapacity dataExportTokens 最大容量
	maxDataExportTokensCapacity = 1000
)

// ====================  构造函数 ====================

// NewUserHandler 创建用户管理 Handler
// 参数：
//   - userRepo: 用户数据仓库
//   - userLogRepo: 用户日志仓库
//   - tokenService: Token 服务
//   - emailService: 邮件服务
//   - captchaService: 验证码服务
//   - userCache: 用户缓存
//   - r2Service: R2 存储服务（可选）
//   - oauthService: OAuth 服务（可选）
//   - baseURL: 基础 URL
//
// 返回：
//   - *UserHandler: Handler 实例
//   - error: 错误信息
func NewUserHandler(
	userRepo *models.UserRepository,
	userLogRepo *models.UserLogRepository,
	tokenService *services.TokenService,
	emailService *services.EmailService,
	captchaService *services.CaptchaService,
	userCache *cache.UserCache,
	r2Service *services.R2Service,
	oauthService *services.OAuthService,
	baseURL string,
) (*UserHandler, error) {
	if userRepo == nil {
		return nil, ErrUserHandlerNilUserRepo
	}
	if tokenService == nil {
		return nil, ErrUserHandlerNilTokenService
	}
	if emailService == nil {
		return nil, ErrUserHandlerNilEmailService
	}
	if captchaService == nil {
		return nil, ErrUserHandlerNilCaptchaService
	}
	if userCache == nil {
		return nil, ErrUserHandlerNilUserCache
	}
	if baseURL == "" {
		return nil, ErrUserHandlerEmptyBaseURL
	}

	utils.LogInfo("USER", "Handler initialized successfully")

	StartDataExportCleanup()

	return &UserHandler{
		userRepo:       userRepo,
		userLogRepo:    userLogRepo,
		tokenService:   tokenService,
		emailService:   emailService,
		captchaService: captchaService,
		userCache:      userCache,
		r2Service:      r2Service,
		oauthService:   oauthService,
		baseURL:        baseURL,
	}, nil
}

// ====================  私有辅助方法 ====================

// verifyCaptcha 验证人机验证 Token
// 参数：
//   - token: 验证码 Token
//   - captchaType: 验证码类型
//   - clientIP: 客户端 IP
//
// 返回：
//   - error: 验证失败时返回错误
func (h *UserHandler) verifyCaptcha(token, captchaType, clientIP string) error {
	if token == "" {
		return errors.New("captcha token is empty")
	}
	return h.captchaService.Verify(token, captchaType, clientIP)
}

// invalidateUserCache 使用户缓存失效
// 参数：
//   - userUID: 用户 UID
func (h *UserHandler) invalidateUserCache(userUID string) {
	if h.userCache != nil {
		h.userCache.Invalidate(userUID)
		utils.LogInfo("USER", fmt.Sprintf("Cache invalidated: userUID=%s", userUID))
	}
}

// ====================  数据导出 Token 管理 ====================

// generateExportToken 生成数据导出 Token
func generateExportToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// StartDataExportCleanup 启动数据导出 Token 清理任务
// 定期清理过期的导出 Token，可通过 StopDataExportCleanup 优雅停止
func StartDataExportCleanup() {
	dataExportCleanupOnce.Do(func() {
		dataExportStopChan = make(chan struct{})
		go func() {
			ticker := time.NewTicker(DataExportCleanupInterval)
			defer ticker.Stop()

			utils.LogInfo("USER", "Data export cleanup task started")

			for {
				select {
				case <-ticker.C:
					CleanupExpiredExportTokens()
				case <-dataExportStopChan:
					utils.LogInfo("USER", "Data export cleanup task stopped")
					return
				}
			}
		}()
	})
}

// StopDataExportCleanup 停止数据导出 Token 清理任务
func StopDataExportCleanup() {
	if dataExportStopChan != nil {
		select {
		case <-dataExportStopChan:
		default:
			close(dataExportStopChan)
		}
	}
}

// CleanupExpiredExportTokens 清理过期的导出 Token（应定期调用）
func CleanupExpiredExportTokens() {
	dataExportTokensMu.Lock()
	defer dataExportTokensMu.Unlock()

	now := time.Now()
	count := 0
	for t, data := range dataExportTokens {
		if now.After(data.ExpiresAt) {
			delete(dataExportTokens, t)
			delete(dataExportTokenIndex, t)
			count++
		}
	}

	if count > 0 {
		utils.LogInfo("USER", fmt.Sprintf("Cleanup completed: expired export tokens=%d", count))
	}
}

// findOldestExportTokens 找出序号最小的 N 个 token
func findOldestExportTokens(count int) []string {
	if len(dataExportTokenIndex) <= count {
		keys := make([]string, 0, len(dataExportTokenIndex))
		for k := range dataExportTokenIndex {
			keys = append(keys, k)
		}
		return keys
	}

	heap := make([]exportFifoKv, 0, count)

	for k, v := range dataExportTokenIndex {
		if len(heap) < count {
			heap = append(heap, exportFifoKv{k, v})
			if len(heap) == count {
				buildExportFifoMinHeap(heap)
			}
		} else if v < heap[0].value {
			heap[0] = exportFifoKv{k, v}
			exportFifoHeapify(heap, 0)
		}
	}

	result := make([]string, count)
	for i := range heap {
		result[i] = heap[i].key
	}
	return result
}

type exportFifoKv struct {
	key   string
	value int64
}

func buildExportFifoMinHeap(h []exportFifoKv) {
	for i := len(h)/2 - 1; i >= 0; i-- {
		exportFifoHeapify(h, i)
	}
}

func exportFifoHeapify(h []exportFifoKv, i int) {
	min := i
	left := 2*i + 1
	right := 2*i + 2
	if left < len(h) && h[left].value < h[min].value {
		min = left
	}
	if right < len(h) && h[right].value < h[min].value {
		min = right
	}
	if min != i {
		h[i], h[min] = h[min], h[i]
		exportFifoHeapify(h, min)
	}
}
