/**
 * internal/handlers/admin/system.go
 * 管理后台 API Handler - 系统管理
 *
 * 功能：
 * - 系统统计
 * - 操作日志
 * - 邮箱白名单管理
 */

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  统计响应结构 ====================

// statsResponse 统计响应
type statsResponse struct {
	TotalUsers    int64 `json:"totalUsers"`
	TodayNewUsers int64 `json:"todayNewUsers"`
	AdminCount    int64 `json:"adminCount"`
	BannedCount   int64 `json:"bannedCount"`
}

// ====================  操作日志响应结构 ====================

// logListResponse 日志列表响应
type logListResponse struct {
	Logs       []*models.AdminLogPublic `json:"logs"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"pageSize"`
	TotalPages int                      `json:"totalPages"`
}

// ====================  邮箱白名单管理响应/请求结构 ====================

// emailWhitelistListResponse 邮箱白名单列表响应
type emailWhitelistListResponse struct {
	Whitelist  []*models.EmailWhitelist `json:"whitelist"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"pageSize"`
	TotalPages int                      `json:"totalPages"`
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
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	utils.RespondSuccessWithData(c, stats)
}

// ====================  操作日志 ====================

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
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	// 计算总页数
	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	utils.RespondSuccessWithData(c, logListResponse{
		Logs:       logs,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// ====================  邮箱白名单管理 ====================

// GetEmailWhitelist 获取邮箱白名单
// GET /admin/api/email-whitelist?page=1&pageSize=20
//
// 权限：仅超级管理员
func (h *AdminHandler) GetEmailWhitelist(c *gin.Context) {
	if h.emailWhitelistRepo == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "EMAIL_WHITELIST_NOT_CONFIGURED")
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(defaultPageSize)))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > maxPageSize {
		pageSize = defaultPageSize
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	whitelist, total, err := h.emailWhitelistRepo.FindAllPaginated(ctx, page, pageSize)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	utils.RespondSuccessWithData(c, emailWhitelistListResponse{
		Whitelist:  whitelist,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// CreateEmailWhitelist 创建邮箱白名单条目
// POST /admin/api/email-whitelist
//
// 权限：仅超级管理员
//
// 请求体：
//   - domain: 邮箱域名（必需，如 example.com）
//   - signup_url: 注册页面 URL（必需）
func (h *AdminHandler) CreateEmailWhitelist(c *gin.Context) {
	if h.emailWhitelistRepo == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "EMAIL_WHITELIST_NOT_CONFIGURED")
		return
	}

	var req struct {
		Domain    string `json:"domain"`
		SignupURL string `json:"signup_url"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}
	domain := strings.TrimSpace(req.Domain)
	signupURL := strings.TrimSpace(req.SignupURL)

	if domain == "" {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "MISSING_DOMAIN", "Domain is required")
		return
	}
	if signupURL == "" {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "MISSING_SIGNUP_URL", "Signup URL is required")
		return
	}

	operatorUID, _ := middleware.GetUID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	item, err := h.emailWhitelistRepo.Create(ctx, domain, signupURL)
	if err != nil {
		if errors.Is(err, models.ErrEmailWhitelistDomainExists) {
			utils.HTTPErrorResponse(c, "ADMIN", http.StatusConflict, "DOMAIN_EXISTS", fmt.Sprintf("Domain %s already exists", domain))
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogEmailWhitelistCreate(ctx, operatorUID, item); err != nil {
		utils.LogWarn("ADMIN", "Failed to log create email whitelist", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("Email whitelist created: operatorUID=%s, domain=%s", operatorUID, domain))
	utils.RespondSuccessWithData(c, gin.H{"item": item})
}

// UpdateEmailWhitelist 更新邮箱白名单条目
// PUT /admin/api/email-whitelist/:id
//
// 权限：仅超级管理员
//
// 请求体：
//   - domain: 邮箱域名（可选）
//   - signup_url: 注册页面 URL（可选）
//   - is_enabled: 是否启用（可选）
func (h *AdminHandler) UpdateEmailWhitelist(c *gin.Context) {
	if h.emailWhitelistRepo == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "EMAIL_WHITELIST_NOT_CONFIGURED")
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "INVALID_ID", "Invalid ID")
		return
	}

	var req struct {
		Domain    *string `json:"domain"`
		SignupURL *string `json:"signup_url"`
		IsEnabled *bool   `json:"is_enabled"`
	}

	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	operatorUID, _ := middleware.GetUID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	existing, err := h.emailWhitelistRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, models.ErrEmailWhitelistNotFound) {
			utils.HTTPErrorResponse(c, "ADMIN", http.StatusNotFound, "NOT_FOUND", "Email whitelist entry not found")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "GET_FAILED", err.Error())
		return
	}

	domain := existing.Domain
	if req.Domain != nil && *req.Domain != "" {
		domain = strings.TrimSpace(*req.Domain)
	}

	signupURL := existing.SignupURL
	if req.SignupURL != nil && *req.SignupURL != "" {
		signupURL = strings.TrimSpace(*req.SignupURL)
	}

	isEnabled := existing.IsEnabled
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	item, err := h.emailWhitelistRepo.Update(ctx, id, domain, signupURL, isEnabled)
	if err != nil {
		if errors.Is(err, models.ErrEmailWhitelistNotFound) {
			utils.HTTPErrorResponse(c, "ADMIN", http.StatusNotFound, "NOT_FOUND", "Email whitelist entry not found")
			return
		}
		if errors.Is(err, models.ErrEmailWhitelistDomainExists) {
			utils.HTTPErrorResponse(c, "ADMIN", http.StatusConflict, "DOMAIN_EXISTS", fmt.Sprintf("Domain %s already exists", domain))
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogEmailWhitelistUpdate(ctx, operatorUID, item); err != nil {
		utils.LogWarn("ADMIN", "Failed to log update email whitelist", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("Email whitelist updated: operatorUID=%s, id=%d", operatorUID, id))
	utils.RespondSuccessWithData(c, gin.H{"item": item})
}

// DeleteEmailWhitelist 删除邮箱白名单条目
// DELETE /admin/api/email-whitelist/:id
//
// 权限：仅超级管理员
func (h *AdminHandler) DeleteEmailWhitelist(c *gin.Context) {
	if h.emailWhitelistRepo == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "EMAIL_WHITELIST_NOT_CONFIGURED")
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "INVALID_ID", "Invalid ID")
		return
	}

	operatorUID, _ := middleware.GetUID(c)
	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	err = h.emailWhitelistRepo.Delete(ctx, id)
	if err != nil {
		if errors.Is(err, models.ErrEmailWhitelistNotFound) {
			utils.HTTPErrorResponse(c, "ADMIN", http.StatusNotFound, "NOT_FOUND", "Email whitelist entry not found")
			return
		}
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogEmailWhitelistDelete(ctx, operatorUID, id); err != nil {
		utils.LogWarn("ADMIN", "Failed to log delete email whitelist", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("Email whitelist deleted: operatorUID=%s, id=%d", operatorUID, id))
	utils.RespondSuccess(c, gin.H{"message": "Email whitelist entry deleted"})
}
