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
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	// ErrAdminNilUserRepo 用户仓库为空
	ErrAdminNilUserRepo = errors.New("user repository is nil")
	// ErrAdminNilUserCache 用户缓存为空
	ErrAdminNilUserCache = errors.New("user cache is nil")
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
	userRepo  *models.UserRepository
	userCache *cache.UserCache
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
//
// 返回：
//   - *AdminHandler: Handler 实例
//   - error: 错误信息
func NewAdminHandler(userRepo *models.UserRepository, userCache *cache.UserCache) (*AdminHandler, error) {
	if userRepo == nil {
		return nil, ErrAdminNilUserRepo
	}
	if userCache == nil {
		return nil, ErrAdminNilUserCache
	}

	utils.LogPrintf("[ADMIN] Admin handler initialized")

	return &AdminHandler{
		userRepo:  userRepo,
		userCache: userCache,
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

	// 记录审计日志
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

	// 记录审计日志
	utils.LogPrintf("[ADMIN] User deleted: operatorID=%d, targetID=%d, targetUsername=%s",
		operatorID, targetUserID, targetUser.Username)

	h.respondSuccess(c, gin.H{"message": "User deleted"})
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
