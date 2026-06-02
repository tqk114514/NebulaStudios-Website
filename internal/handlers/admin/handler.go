// Package admin 提供管理后台 API Handler，包括用户管理、数据导入导出、OAuth 配置和系统操作。
// 所有接口需要管理员权限，敏感操作需要超级管理员权限（SuperAdminMiddleware）。
package admin

import (
	"errors"
	"time"

	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAdminNilUserRepo  = errors.New("user repository is nil")
	ErrAdminNilUserCache = errors.New("user cache is nil")
	ErrAdminNilLogRepo   = errors.New("admin log repository is nil")
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	adminTimeout    = 10 * time.Second
)

// AdminHandler 管理后台 Handler
type AdminHandler struct {
	userRepo           models.UserStore
	userCache          services.UserCacheStore
	logRepo            models.AdminLogStore
	userLogRepo        models.UserLogStore
	oauthService       services.OAuthClientManager
	emailWhitelistRepo models.EmailWhitelistStore
	exportService      services.ExportManager
	dataExportSalt     string
	pool               *pgxpool.Pool
}

// NewAdminHandler 创建管理后台 Handler，验证必需依赖（userRepo、userCache、logRepo）后初始化。
// oauthService 和 emailWhitelistRepo 为可选参数。
func NewAdminHandler(userRepo models.UserStore, userCache services.UserCacheStore, logRepo models.AdminLogStore, userLogRepo models.UserLogStore, oauthService services.OAuthClientManager, emailWhitelistRepo models.EmailWhitelistStore, exportService services.ExportManager, dataExportSalt string, pool *pgxpool.Pool) (*AdminHandler, error) {
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
		exportService:      exportService,
		dataExportSalt:     dataExportSalt,
		pool:               pool,
	}, nil
}
