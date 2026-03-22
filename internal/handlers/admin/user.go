/**
 * internal/handlers/admin/user.go
 * 管理后台 API Handler - 用户管理
 *
 * 功能：
 * - 用户列表（分页、搜索）
 * - 用户详情
 * - 禁用/启用用户
 * - 管理员任免（仅超管）
 * - 删除用户（仅超管）
 */

package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"auth-system/internal/middleware"
	adminmw "auth-system/internal/middleware/admin"
	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  用户管理响应/请求结构 ====================

// userListResponse 用户列表响应
type userListResponse struct {
	Users      []*models.UserPublic `json:"users"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"pageSize"`
	TotalPages int                  `json:"totalPages"`
}

// setRoleRequest 设置角色请求
type setRoleRequest struct {
	Role int `json:"role"`
}

// banUserRequest 封禁用户请求
type banUserRequest struct {
	Reason string `json:"reason"`
	Days   int    `json:"days"` // 0 表示永久封禁
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
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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

	utils.RespondSuccessWithData(c, userListResponse{
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
		utils.RespondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询用户
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	utils.RespondSuccessWithData(c, user.ToPublic())
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
		utils.RespondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	// 不能修改自己的角色
	if targetUserID == operatorID {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "CANNOT_MODIFY_SELF", "Attempted to modify own role")
		return
	}

	// 解析请求
	var req setRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 验证角色值
	if req.Role < models.RoleUser || req.Role > models.RoleAdmin {
		// 超管只能设置 user 或 admin，不能设置 super_admin
		utils.RespondError(c, http.StatusBadRequest, "INVALID_ROLE")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	// 不能修改超级管理员的角色
	if targetUser.IsSuperAdmin() {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusForbidden, "CANNOT_MODIFY_SUPER_ADMIN", "Attempted to modify super admin role")
		return
	}

	// 不能将封禁用户设为管理员
	if req.Role > models.RoleUser && targetUser.CheckBanned() {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "CANNOT_PROMOTE_BANNED_USER", "Attempted to promote banned user")
		return
	}

	// 执行更新
	err = h.userRepo.Update(ctx, targetUserID, map[string]interface{}{
		"role": req.Role,
	})
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogSetRole(ctx, operatorID, targetUserID, targetUser.Username, targetUser.Role, req.Role); err != nil {
		utils.LogWarn("ADMIN", "Failed to log set_role", err.Error())
	}

	// 记录审计日志到控制台
	utils.LogInfo("ADMIN", fmt.Sprintf("Role updated: operatorID=%d, operatorRole=%d, targetID=%d, oldRole=%d, newRole=%d",
		operatorID, operatorRole, targetUserID, targetUser.Role, req.Role))

	utils.RespondSuccess(c, gin.H{"message": "Role updated"})
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
		utils.RespondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	// 不能删除自己
	if targetUserID == operatorID {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "CANNOT_DELETE_SELF", "Attempted to delete self")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	// 不能删除超级管理员
	if targetUser.IsSuperAdmin() {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusForbidden, "CANNOT_DELETE_SUPER_ADMIN", "Attempted to delete super admin")
		return
	}

	// 不能删除其他管理员（只有超管能删普通用户）
	if targetUser.IsAdmin() {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusForbidden, "CANNOT_DELETE_ADMIN", "Attempted to delete admin")
		return
	}

	// 执行删除
	err = h.userRepo.Delete(ctx, targetUserID)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogDeleteUser(ctx, operatorID, targetUserID, targetUser.Username, targetUser.Email); err != nil {
		utils.LogWarn("ADMIN", "Failed to log delete_user", err.Error())
	}

	// 记录审计日志到控制台
	utils.LogInfo("ADMIN", fmt.Sprintf("User deleted: operatorID=%d, targetID=%d, targetUsername=%s",
		operatorID, targetUserID, targetUser.Username))

	utils.RespondSuccess(c, gin.H{"message": "User deleted"})
}

// ====================  封禁管理 ====================

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
		utils.RespondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	// 不能封禁自己
	if targetUserID == operatorID {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "CANNOT_BAN_SELF", "Attempted to ban self")
		return
	}

	// 解析请求
	var req banUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	// 验证封禁原因
	allowedReasons := map[string]bool{
		"violation": true,
		"abuse":     true,
		"malicious": true,
		"spam":      true,
	}
	if req.Reason == "" {
		utils.RespondError(c, http.StatusBadRequest, "REASON_REQUIRED")
		return
	}
	if !allowedReasons[req.Reason] {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REASON")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	// 不能封禁管理员及以上
	if targetUser.IsAdmin() {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusForbidden, "CANNOT_BAN_ADMIN", "Attempted to ban admin")
		return
	}

	// 检查是否已被封禁
	if targetUser.CheckBanned() {
		utils.RespondError(c, http.StatusBadRequest, "ALREADY_BANNED")
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
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "BAN_FAILED", err.Error())
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogBanUser(ctx, operatorID, targetUserID, targetUser.Username, req.Reason, unbanAt); err != nil {
		utils.LogWarn("ADMIN", "Failed to log ban_user", err.Error())
	}

	// 记录用户日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogBanned(ctx, targetUserID, req.Reason, unbanAt); err != nil {
			utils.LogWarn("ADMIN", "Failed to log user banned", err.Error())
		}
	}

	// 记录审计日志到控制台
	utils.LogInfo("ADMIN", fmt.Sprintf("User banned: operatorID=%d, targetID=%d, reason=%s, days=%d",
		operatorID, targetUserID, req.Reason, req.Days))

	utils.RespondSuccess(c, gin.H{"message": "User banned"})
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
		utils.RespondError(c, http.StatusBadRequest, "INVALID_USER_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	// 查询目标用户
	targetUser, err := h.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondError(c, http.StatusNotFound, "USER_NOT_FOUND")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	// 检查是否已被封禁
	if !targetUser.CheckBanned() {
		utils.RespondError(c, http.StatusBadRequest, "NOT_BANNED")
		return
	}

	// 执行解封
	err = h.userRepo.Unban(ctx, targetUserID)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "UNBAN_FAILED", err.Error())
		return
	}

	// 使缓存失效
	h.userCache.Invalidate(targetUserID)

	// 记录审计日志到数据库
	if err := h.logRepo.LogUnbanUser(ctx, operatorID, targetUserID, targetUser.Username); err != nil {
		utils.LogWarn("ADMIN", "Failed to log unban_user", err.Error())
	}

	// 记录用户日志
	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogUnbanned(ctx, targetUserID); err != nil {
			utils.LogWarn("ADMIN", "Failed to log user unbanned", err.Error())
		}
	}

	// 记录审计日志到控制台
	utils.LogInfo("ADMIN", fmt.Sprintf("User unbanned: operatorID=%d, targetID=%d",
		operatorID, targetUserID))

	utils.RespondSuccess(c, gin.H{"message": "User unbanned"})
}
