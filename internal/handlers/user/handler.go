// Package user 提供用户管理 API Handler，包括账户操作、个人资料更新和数据导出。
package user

import (
	"errors"
	"fmt"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
)

var (
	ErrUserHandlerNilUserRepo       = errors.New("user repository is nil")
	ErrUserHandlerNilTokenService   = errors.New("token service is nil")
	ErrUserHandlerNilEmailService   = errors.New("email service is nil")
	ErrUserHandlerNilCaptchaService = errors.New("captcha service is nil")
	ErrUserHandlerNilUserCache      = errors.New("user cache is nil")
	ErrUserHandlerEmptyBaseURL      = errors.New("base URL is empty")
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

// NewUserHandler 创建用户管理 Handler，验证所有必需依赖后初始化。
// r2Service 和 oauthService 为可选参数。
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

// verifyCaptcha 验证人机验证 Token
func (h *UserHandler) verifyCaptcha(token, captchaType, clientIP string) error {
	if token == "" {
		return errors.New("captcha token is empty")
	}
	return h.captchaService.Verify(token, captchaType, clientIP)
}

func (h *UserHandler) invalidateUserCache(userUID string) {
	if h.userCache != nil {
		h.userCache.Invalidate(userUID)
		utils.LogInfo("USER", fmt.Sprintf("Cache invalidated: userUID=%s", userUID))
	}
}
