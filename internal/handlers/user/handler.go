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
	"errors"
	"fmt"

	"auth-system/internal/middleware"
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

// UserHandler 用户管理 Handler
type UserHandler struct {
	userRepo           models.UserStore
	userLogRepo        models.UserLogStore
	tokenService       services.TokenManager
	emailService       services.EmailSender
	captchaService     services.CaptchaVerifier
	userCache          services.UserCacheStore
	r2Service          services.StorageService
	oauthService       services.OAuthClientManager
	limiterMgr         middleware.RateLimiterManager
	exportTokenService services.ExportTokenManager
	baseURL            string
}

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
	userRepo models.UserStore,
	userLogRepo models.UserLogStore,
	tokenService services.TokenManager,
	emailService services.EmailSender,
	captchaService services.CaptchaVerifier,
	userCache services.UserCacheStore,
	r2Service services.StorageService,
	oauthService services.OAuthClientManager,
	limiterMgr middleware.RateLimiterManager,
	exportTokenService services.ExportTokenManager,
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

	return &UserHandler{
		userRepo:           userRepo,
		userLogRepo:        userLogRepo,
		tokenService:       tokenService,
		emailService:       emailService,
		captchaService:     captchaService,
		userCache:          userCache,
		r2Service:          r2Service,
		oauthService:       oauthService,
		limiterMgr:         limiterMgr,
		exportTokenService: exportTokenService,
		baseURL:            baseURL,
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
