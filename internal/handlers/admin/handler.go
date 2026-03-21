/**
 * internal/handlers/admin/handler.go
 * 管理后台 API Handler - 核心定义
 *
 * 功能：
 * - Handler 结构定义
 * - 构造函数
 * - 错误和常量定义
 *
 * 安全说明：
 * - 所有接口需要管理员权限
 * - 敏感操作需要超级管理员权限
 * - 操作记录审计日志
 *
 * 依赖：
 * - UserRepository: 用户数据访问
 * - UserCache: 用户缓存（用于失效）
 */

package admin

import (
	"errors"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"
)

// ====================  错误定义 ====================

var (
	// ErrAdminNilUserRepo 用户仓库为空
	ErrAdminNilUserRepo = errors.New("user repository is nil")
	// ErrAdminNilUserCache 用户缓存为空
	ErrAdminNilUserCache = errors.New("user cache is nil")
	// ErrAdminNilLogRepo 日志仓库为空
	ErrAdminNilLogRepo = errors.New("admin log repository is nil")
)

// ====================  常量定义 ====================

const (
	// defaultPageSize 默认分页大小
	defaultPageSize = 20
	// maxPageSize 最大分页大小
	maxPageSize = 100
	// adminTimeout 管理操作超时时间
	adminTimeout = 10 * time.Second
)

// ====================  数据结构 ====================

// AdminHandler 管理后台 Handler
type AdminHandler struct {
	userRepo           *models.UserRepository
	userCache          *cache.UserCache
	logRepo            *models.AdminLogRepository
	userLogRepo        *models.UserLogRepository
	oauthService       *services.OAuthService
	emailWhitelistRepo *models.EmailWhitelistRepository
}

// ====================  构造函数 ====================

// NewAdminHandler 创建管理后台 Handler
// 参数：
//   - userRepo: 用户数据仓库
//   - userCache: 用户缓存
//   - logRepo: 管理员日志仓库
//   - userLogRepo: 用户日志仓库
//   - oauthService: OAuth 服务（可选）
//   - emailWhitelistRepo: 邮箱白名单仓库（可选）
//
// 返回：
//   - *AdminHandler: Handler 实例
//   - error: 错误信息
func NewAdminHandler(userRepo *models.UserRepository, userCache *cache.UserCache, logRepo *models.AdminLogRepository, userLogRepo *models.UserLogRepository, oauthService *services.OAuthService, emailWhitelistRepo *models.EmailWhitelistRepository) (*AdminHandler, error) {
	if userRepo == nil {
		return nil, ErrAdminNilUserRepo
	}
	if userCache == nil {
		return nil, ErrAdminNilUserCache
	}
	if logRepo == nil {
		return nil, ErrAdminNilLogRepo
	}

	utils.LogInfo("ADMIN", "Admin handler initialized")

	return &AdminHandler{
		userRepo:           userRepo,
		userCache:          userCache,
		logRepo:            logRepo,
		userLogRepo:        userLogRepo,
		oauthService:       oauthService,
		emailWhitelistRepo: emailWhitelistRepo,
	}, nil
}
