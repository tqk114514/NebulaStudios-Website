/**
 * internal/handlers/admin/admin.go
 * 管理后台 API Handler
 *
 * 功能：
 * - 用户列表（分页、搜索）
 * - 用户详情
 * - 禁用/启用用户
 * - 管理员任免（仅超管）
 * - 删除用户（仅超管）
 * - 系统统计
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
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"auth-system/internal/cache"
	"auth-system/internal/middleware"
	adminmw "auth-system/internal/middleware/admin"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
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
	userRepo     *models.UserRepository
	userCache    *cache.UserCache
	logRepo      *models.AdminLogRepository
	userLogRepo  *models.UserLogRepository
	oauthService *services.OAuthService
}

// userListResponse 用户列表响应
type userListResponse struct {
	Users      []*models.UserPublic `json:"users"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"pageSize"`
	TotalPages int                  `json:"totalPages"`
}

// statsResponse 统计响应
type statsResponse struct {
	TotalUsers    int64 `json:"totalUsers"`
	TodayNewUsers int64 `json:"todayNewUsers"`
	AdminCount    int64 `json:"adminCount"`
	MicrosoftLinked int64 `json:"microsoftLinked"`
}

// setRoleRequest 设置角色请求
type setRoleRequest struct {
	Role int `json:"role"`
}

// ====================  构造函数 ====================

// NewAdminHandler 创建管理后台 Handler
// 参数：
//   - userRepo: 用户数据仓库
//   - userCache: 用户缓存
//   - logRepo: 管理员日志仓库
//   - userLogRepo: 用户日志仓库
//   - oauthService: OAuth 服务（可选）
//
// 返回：
//   - *AdminHandler: Handler 实例
//   - error: 错误信息
func NewAdminHandler(userRepo *models.UserRepository, userCache *cache.UserCache, logRepo *models.AdminLogRepository, userLogRepo *models.UserLogRepository, oauthService *services.OAuthService) (*AdminHandler, error) {
	if userRepo == nil {
		return nil, ErrAdminNilUserRepo
	}
	if userCache == nil {
		return nil, ErrAdminNilUserCache
	}
	if logRepo == nil {
		return nil, ErrAdminNilLogRepo
	}

	utils.LogPrintf("[ADMIN] Admin handler initialized")

	return &AdminHandler{
		userRepo:     userRepo,
		userCache:    userCache,
		logRepo:      logRepo,
		userLogRepo:  userLogRepo,
		oauthService: oauthService,
	}, nil
}


// ====================  用户管理 ====================

// GetUsers 获取用户列表
// GET /admin/api/users?page=1&pageSize=20&search=xxx
//
// 权限：管理员
func (h *AdminHandler) GetUsers(c *gin.Context) {
	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(defaultPageSize)))
	search := c.Query("search")

	// 参数校验
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询用户列表
	users, total, err := h.userRepo.FindAll(ctx, page, pageSize, search)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to get users: error=%v", err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	// 转换为公开信息
	publicUsers := make([]*models.UserPublic, len(users))
	for i, u := range users {
		publicUsers[i] = u.ToPublic()
	}

	// 计算总页数
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	h.respondSuccess(c, userListResponse{
		Users:      publicUsers,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// GetUser 获取用户详情
// GET /admin/api/users/:id
//
// 权限：管理员
func (h *AdminHandler) GetUser(c *gin.Context) {
	// 解析用户 ID
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询用户
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.LogPrintf("[ADMIN] ERROR: Failed to get user: userID=%d, error=%v", userID, err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	h.respondSuccess(c, user.ToPublic())
}

// SetUserRole 设置用户角色
// PUT /admin/api/users/:id/role
//
// 权限：超级管理员
func (h *AdminHandler) SetUserRole(c *gin.Context) {
	// 获取当前操作者信息
	operatorID, _ := middleware.GetUserID(c)
	operatorRole, _ := adminmw.GetUserRole(c)

	// 解析目标用户 ID
	targetUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	// 不能修改自己的角色
	if targetUserID == operatorID {
		utils.LogPrintf("[ADMIN] WARN: Attempted to modify own role: userID=%d", operatorID)
		h.respondError(c, http.StatusBadRequest, "CANNOT_MODIFY_SELF")
		return
	}

	// 解析请求
	var req setRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 验证角色值
	if req.Role < models.RoleUser || req.Role > models.RoleAdmin {
		// 超管只能设置 user 或 admin，不能设置 super_admin
		h.respondError(c, http.StatusBadRequest, "INVALID_ROLE")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.LogPrintf("[ADMIN] ERROR: Failed to get target user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	// 不能修改超级管理员的角色
	if targetUser.IsSuperAdmin() {
		utils.LogPrintf("[ADMIN] WARN: Attempted to modify super admin role: operatorID=%d, targetID=%d",
			operatorID, targetUserID)
		h.respondError(c, http.StatusForbidden, "CANNOT_MODIFY_SUPER_ADMIN")
		return
	}

	// 不能将封禁用户设为管理员
	if req.Role > models.RoleUser && targetUser.CheckBanned() {
		utils.LogPrintf("[ADMIN] WARN: Attempted to promote banned user: operatorID=%d, targetID=%d",
			operatorID, targetUserID)
		h.respondError(c, http.StatusBadRequest, "CANNOT_PROMOTE_BANNED_USER")
		return
	}

	// 执行更新
	err = h.userRepo.Update(ctx, targetUserID, map[string]interface{}{
		"role": req.Role,
	})
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to update role: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "UPDATE_FAILED")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogSetRole(ctx, operatorID, targetUserID, targetUser.Username, targetUser.Role, req.Role); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log set_role: error=%v", err)
	}

	// 记录审计日志到控制台
	utils.LogPrintf("[ADMIN] Role updated: operatorID=%d, operatorRole=%d, targetID=%d, oldRole=%d, newRole=%d",
		operatorID, operatorRole, targetUserID, targetUser.Role, req.Role)

	h.respondSuccess(c, gin.H{"message": "Role updated"})
}


// DeleteUser 删除用户
// DELETE /admin/api/users/:id
//
// 权限：超级管理员
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	// 获取当前操作者信息
	operatorID, _ := middleware.GetUserID(c)

	// 解析目标用户 ID
	targetUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	// 不能删除自己
	if targetUserID == operatorID {
		utils.LogPrintf("[ADMIN] WARN: Attempted to delete self: userID=%d", operatorID)
		h.respondError(c, http.StatusBadRequest, "CANNOT_DELETE_SELF")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.LogPrintf("[ADMIN] ERROR: Failed to get target user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	// 不能删除超级管理员
	if targetUser.IsSuperAdmin() {
		utils.LogPrintf("[ADMIN] WARN: Attempted to delete super admin: operatorID=%d, targetID=%d",
			operatorID, targetUserID)
		h.respondError(c, http.StatusForbidden, "CANNOT_DELETE_SUPER_ADMIN")
		return
	}

	// 不能删除其他管理员（只有超管能删普通用户）
	if targetUser.IsAdmin() {
		utils.LogPrintf("[ADMIN] WARN: Attempted to delete admin: operatorID=%d, targetID=%d",
			operatorID, targetUserID)
		h.respondError(c, http.StatusForbidden, "CANNOT_DELETE_ADMIN")
		return
	}

	// 执行删除
	err = h.userRepo.Delete(ctx, targetUserID)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to delete user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "DELETE_FAILED")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogDeleteUser(ctx, operatorID, targetUserID, targetUser.Username, targetUser.Email); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log delete_user: error=%v", err)
	}

	// 记录审计日志到控制台
	utils.LogPrintf("[ADMIN] User deleted: operatorID=%d, targetID=%d, targetUsername=%s",
		operatorID, targetUserID, targetUser.Username)

	h.respondSuccess(c, gin.H{"message": "User deleted"})
}

// ====================  封禁管理 ====================

// banUserRequest 封禁用户请求
type banUserRequest struct {
	Reason  string `json:"reason"`
	Days    int    `json:"days"` // 0 表示永久封禁
}

// BanUser 封禁用户
// POST /admin/api/users/:id/ban
//
// 权限：管理员（不能封禁管理员及以上）
func (h *AdminHandler) BanUser(c *gin.Context) {
	// 获取当前操作者信息
	operatorID, _ := middleware.GetUserID(c)

	// 解析目标用户 ID
	targetUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	// 不能封禁自己
	if targetUserID == operatorID {
		utils.LogPrintf("[ADMIN] WARN: Attempted to ban self: userID=%d", operatorID)
		h.respondError(c, http.StatusBadRequest, "CANNOT_BAN_SELF")
		return
	}

	// 解析请求
	var req banUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 验证封禁原因
	if req.Reason == "" {
		h.respondError(c, http.StatusBadRequest, "REASON_REQUIRED")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.LogPrintf("[ADMIN] ERROR: Failed to get target user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	// 不能封禁管理员及以上
	if targetUser.IsAdmin() {
		utils.LogPrintf("[ADMIN] WARN: Attempted to ban admin: operatorID=%d, targetID=%d",
			operatorID, targetUserID)
		h.respondError(c, http.StatusForbidden, "CANNOT_BAN_ADMIN")
		return
	}

	// 检查是否已被封禁
	if targetUser.CheckBanned() {
		h.respondError(c, http.StatusBadRequest, "ALREADY_BANNED")
		return
	}

	// 计算解封时间
	var unbanAt *time.Time
	if req.Days > 0 {
		t := time.Now().AddDate(0, 0, req.Days)
		unbanAt = &t
	}

	// 执行封禁
	err = h.userRepo.Ban(ctx, targetUserID, operatorID, req.Reason, unbanAt)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to ban user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "BAN_FAILED")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogBanUser(ctx, operatorID, targetUserID, targetUser.Username, req.Reason, unbanAt); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log ban_user: error=%v", err)
	}

	// 记录用户日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogBanned(ctx, targetUserID, req.Reason, unbanAt); err != nil {
			utils.LogPrintf("[ADMIN] WARN: Failed to log user banned: error=%v", err)
		}
	}

	// 记录审计日志到控制台
	utils.LogPrintf("[ADMIN] User banned: operatorID=%d, targetID=%d, reason=%s, days=%d",
		operatorID, targetUserID, req.Reason, req.Days)

	h.respondSuccess(c, gin.H{"message": "User banned"})
}

// UnbanUser 解封用户
// POST /admin/api/users/:id/unban
//
// 权限：管理员
func (h *AdminHandler) UnbanUser(c *gin.Context) {
	// 获取当前操作者信息
	operatorID, _ := middleware.GetUserID(c)

	// 解析目标用户 ID
	targetUserID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			h.respondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.LogPrintf("[ADMIN] ERROR: Failed to get target user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	// 检查是否已被封禁
	if !targetUser.CheckBanned() {
		h.respondError(c, http.StatusBadRequest, "NOT_BANNED")
		return
	}

	// 执行解封
	err = h.userRepo.Unban(ctx, targetUserID)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to unban user: userID=%d, error=%v", targetUserID, err)
		h.respondError(c, http.StatusInternalServerError, "UNBAN_FAILED")
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogUnbanUser(ctx, operatorID, targetUserID, targetUser.Username); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log unban_user: error=%v", err)
	}

	// 记录用户日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogUnbanned(ctx, targetUserID); err != nil {
			utils.LogPrintf("[ADMIN] WARN: Failed to log user unbanned: error=%v", err)
		}
	}

	// 记录审计日志到控制台
	utils.LogPrintf("[ADMIN] User unbanned: operatorID=%d, targetID=%d",
		operatorID, targetUserID)

	h.respondSuccess(c, gin.H{"message": "User unbanned"})
}

// ====================  统计 ====================

// GetStats 获取系统统计
// GET /admin/api/stats
//
// 权限：管理员
func (h *AdminHandler) GetStats(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	stats, err := h.userRepo.GetStats(ctx)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to get stats: error=%v", err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	h.respondSuccess(c, stats)
}

// ====================  操作日志 ====================

// logListResponse 日志列表响应
type logListResponse struct {
	Logs       []*models.AdminLogPublic `json:"logs"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"pageSize"`
	TotalPages int                      `json:"totalPages"`
}

// GetLogs 获取操作日志列表
// GET /admin/api/logs?page=1&pageSize=20
//
// 权限：超级管理员
func (h *AdminHandler) GetLogs(c *gin.Context) {
	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(defaultPageSize)))

	// 参数校验
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询日志列表
	logs, total, err := h.logRepo.FindAll(ctx, page, pageSize)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to get logs: error=%v", err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	// 计算总页数
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	h.respondSuccess(c, logListResponse{
		Logs:       logs,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// ====================  辅助方法 ====================

// respondSuccess 返回成功响应
func (h *AdminHandler) respondSuccess(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// respondError 返回错误响应
func (h *AdminHandler) respondError(c *gin.Context, status int, errorCode string) {
	c.JSON(status, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
}

// ====================  OAuth 客户端管理 ====================

// oauthClientListResponse OAuth 客户端列表响应
type oauthClientListResponse struct {
	Clients    []*models.OAuthClient `json:"clients"`
	Total      int64                 `json:"total"`
	Page       int                   `json:"page"`
	PageSize   int                   `json:"pageSize"`
	TotalPages int                   `json:"totalPages"`
}

// createOAuthClientRequest 创建 OAuth 客户端请求
type createOAuthClientRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=100"`
	Description string `json:"description" binding:"max=500"`
	RedirectURI string `json:"redirect_uri" binding:"required"`
}

// updateOAuthClientRequest 更新 OAuth 客户端请求
type updateOAuthClientRequest struct {
	Name        string `json:"name" binding:"omitempty,min=1,max=100"`
	Description string `json:"description" binding:"max=500"`
	RedirectURI string `json:"redirect_uri"`
}

// toggleOAuthClientRequest 启用/禁用 OAuth 客户端请求
type toggleOAuthClientRequest struct {
	Enabled bool `json:"enabled"`
}

// GetOAuthClients 获取 OAuth 客户端列表
// GET /admin/api/oauth/clients?page=1&pageSize=20&search=xxx
//
// 权限：超级管理员
func (h *AdminHandler) GetOAuthClients(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	// 解析分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(defaultPageSize)))
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	clients, total, err := h.oauthService.GetClients(ctx, page, pageSize, search)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to get OAuth clients: error=%v", err)
		h.respondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	h.respondSuccess(c, oauthClientListResponse{
		Clients:    clients,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// GetOAuthClient 获取 OAuth 客户端详情
// GET /admin/api/oauth/clients/:id
//
// 权限：超级管理员
func (h *AdminHandler) GetOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to get OAuth client: id=%d, error=%v", clientID, err)
		h.respondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	h.respondSuccess(c, client)
}

// CreateOAuthClient 创建 OAuth 客户端
// POST /admin/api/oauth/clients
//
// 权限：超级管理员
func (h *AdminHandler) CreateOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorID, _ := middleware.GetUserID(c)

	var req createOAuthClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Invalid create OAuth client request: error=%v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, clientSecret, err := h.oauthService.CreateClient(ctx, req.Name, req.Description, req.RedirectURI)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to create OAuth client: error=%v", err)
		h.respondError(c, http.StatusInternalServerError, "CREATE_FAILED")
		return
	}

	// 记录审计日志
	if err := h.logRepo.LogOAuthClientCreate(ctx, operatorID, client.ID, client.ClientID, client.Name); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log create OAuth client: error=%v", err)
	}

	utils.LogPrintf("[ADMIN] OAuth client created: operatorID=%d, clientID=%s, name=%s",
		operatorID, client.ClientID, client.Name)

	h.respondSuccess(c, gin.H{
		"client":        client,
		"client_secret": clientSecret,
	})
}

// UpdateOAuthClient 更新 OAuth 客户端
// PUT /admin/api/oauth/clients/:id
//
// 权限：超级管理员
func (h *AdminHandler) UpdateOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorID, _ := middleware.GetUserID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	var req updateOAuthClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Invalid update OAuth client request: error=%v", err)
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		h.respondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	err = h.oauthService.UpdateClient(ctx, clientID, req.Name, req.Description, req.RedirectURI)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to update OAuth client: id=%d, error=%v", clientID, err)
		h.respondError(c, http.StatusInternalServerError, "UPDATE_FAILED")
		return
	}

	// 记录审计日志
	if err := h.logRepo.LogOAuthClientUpdate(ctx, operatorID, clientID, client.ClientID, client.Name); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log update OAuth client: error=%v", err)
	}

	utils.LogPrintf("[ADMIN] OAuth client updated: operatorID=%d, clientID=%s", operatorID, client.ClientID)

	h.respondSuccess(c, gin.H{"message": "Client updated"})
}

// DeleteOAuthClient 删除 OAuth 客户端
// DELETE /admin/api/oauth/clients/:id
//
// 权限：超级管理员
func (h *AdminHandler) DeleteOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorID, _ := middleware.GetUserID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		h.respondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	err = h.oauthService.DeleteClient(ctx, clientID)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to delete OAuth client: id=%d, error=%v", clientID, err)
		h.respondError(c, http.StatusInternalServerError, "DELETE_FAILED")
		return
	}

	// 记录审计日志
	if err := h.logRepo.LogOAuthClientDelete(ctx, operatorID, clientID, client.ClientID, client.Name); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log delete OAuth client: error=%v", err)
	}

	utils.LogPrintf("[ADMIN] OAuth client deleted: operatorID=%d, clientID=%s, name=%s",
		operatorID, client.ClientID, client.Name)

	h.respondSuccess(c, gin.H{"message": "Client deleted"})
}

// RegenerateOAuthClientSecret 重新生成 OAuth 客户端密钥
// POST /admin/api/oauth/clients/:id/regenerate-secret
//
// 权限：超级管理员
func (h *AdminHandler) RegenerateOAuthClientSecret(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorID, _ := middleware.GetUserID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		h.respondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	newSecret, err := h.oauthService.RegenerateSecret(ctx, clientID)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to regenerate OAuth client secret: id=%d, error=%v", clientID, err)
		h.respondError(c, http.StatusInternalServerError, "REGENERATE_FAILED")
		return
	}

	// 记录审计日志
	if err := h.logRepo.LogOAuthClientRegenerateSecret(ctx, operatorID, clientID, client.ClientID, client.Name); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log regenerate OAuth client secret: error=%v", err)
	}

	utils.LogPrintf("[ADMIN] OAuth client secret regenerated: operatorID=%d, clientID=%s", operatorID, client.ClientID)

	h.respondSuccess(c, gin.H{"client_secret": newSecret})
}

// ToggleOAuthClient 启用/禁用 OAuth 客户端
// POST /admin/api/oauth/clients/:id/toggle
//
// 权限：超级管理员
func (h *AdminHandler) ToggleOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		h.respondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorID, _ := middleware.GetUserID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		h.respondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	var req toggleOAuthClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.respondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		h.respondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	err = h.oauthService.ToggleClient(ctx, clientID, req.Enabled)
	if err != nil {
		utils.LogPrintf("[ADMIN] ERROR: Failed to toggle OAuth client: id=%d, error=%v", clientID, err)
		h.respondError(c, http.StatusInternalServerError, "TOGGLE_FAILED")
		return
	}

	// 记录审计日志
	if err := h.logRepo.LogOAuthClientToggle(ctx, operatorID, clientID, client.ClientID, client.Name, req.Enabled); err != nil {
		utils.LogPrintf("[ADMIN] WARN: Failed to log toggle OAuth client: error=%v", err)
	}

	status := "disabled"
	if req.Enabled {
		status = "enabled"
	}
	utils.LogPrintf("[ADMIN] OAuth client %s: operatorID=%d, clientID=%s", status, operatorID, client.ClientID)

	h.respondSuccess(c, gin.H{"message": "Client " + status})
}
