/**
 * internal/handlers/admin/oauth.go
 * 管理后台 API Handler - OAuth 客户端管理
 *
 * 功能：
 * - OAuth 客户端管理
 */

package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  OAuth 客户端管理响应/请求结构 ====================

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

// ====================  OAuth 客户端管理 ====================

// GetOAuthClients 获取 OAuth 客户端列表
// GET /admin/api/oauth/clients?page=1&pageSize=20&search=xxx
//
// 权限：超级管理员
func (h *AdminHandler) GetOAuthClients(c *gin.Context) {
	if h.oauthService == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

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
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}

	utils.RespondSuccessWithData(c, oauthClientListResponse{
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
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusNotFound, "CLIENT_NOT_FOUND", err.Error())
		return
	}

	utils.RespondSuccessWithData(c, client)
}

// CreateOAuthClient 创建 OAuth 客户端
// POST /admin/api/oauth/clients
//
// 权限：超级管理员
func (h *AdminHandler) CreateOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorUID, _ := middleware.GetUID(c)

	var req createOAuthClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, clientSecret, err := h.oauthService.CreateClient(ctx, req.Name, req.Description, req.RedirectURI)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogOAuthClientCreate(ctx, operatorUID, client.ID, client.ClientID, client.Name); err != nil {
		utils.LogWarn("ADMIN", "Failed to log create OAuth client", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("OAuth client created: operatorUID=%s, clientID=%s, name=%s",
		operatorUID, client.ClientID, client.Name))

	utils.RespondSuccess(c, gin.H{
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
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorUID, _ := middleware.GetUID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	var req updateOAuthClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		utils.RespondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	err = h.oauthService.UpdateClient(ctx, clientID, req.Name, req.Description, req.RedirectURI)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogOAuthClientUpdate(ctx, operatorUID, clientID, client.ClientID, client.Name); err != nil {
		utils.LogWarn("ADMIN", "Failed to log update OAuth client", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("OAuth client updated: operatorUID=%s, clientID=%s", operatorUID, client.ClientID))

	utils.RespondSuccess(c, gin.H{"message": "Client updated"})
}

// DeleteOAuthClient 删除 OAuth 客户端
// DELETE /admin/api/oauth/clients/:id
//
// 权限：超级管理员
func (h *AdminHandler) DeleteOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorUID, _ := middleware.GetUID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		utils.RespondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	err = h.oauthService.DeleteClient(ctx, clientID)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogOAuthClientDelete(ctx, operatorUID, clientID, client.ClientID, client.Name); err != nil {
		utils.LogWarn("ADMIN", "Failed to log delete OAuth client", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("OAuth client deleted: operatorUID=%s, clientID=%s, name=%s",
		operatorUID, client.ClientID, client.Name))

	utils.RespondSuccess(c, gin.H{"message": "Client deleted"})
}

// RegenerateOAuthClientSecret 重新生成 OAuth 客户端密钥
// POST /admin/api/oauth/clients/:id/regenerate-secret
//
// 权限：超级管理员
func (h *AdminHandler) RegenerateOAuthClientSecret(c *gin.Context) {
	if h.oauthService == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorUID, _ := middleware.GetUID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		utils.RespondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	newSecret, err := h.oauthService.RegenerateSecret(ctx, clientID)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "REGENERATE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogOAuthClientRegenerateSecret(ctx, operatorUID, clientID, client.ClientID, client.Name); err != nil {
		utils.LogWarn("ADMIN", "Failed to log regenerate OAuth client secret", err.Error())
	}

	utils.LogInfo("ADMIN", fmt.Sprintf("OAuth client secret regenerated: operatorUID=%s, clientID=%s", operatorUID, client.ClientID))

	utils.RespondSuccess(c, gin.H{"client_secret": newSecret})
}

// ToggleOAuthClient 启用/禁用 OAuth 客户端
// POST /admin/api/oauth/clients/:id/toggle
//
// 权限：超级管理员
func (h *AdminHandler) ToggleOAuthClient(c *gin.Context) {
	if h.oauthService == nil {
		utils.RespondError(c, http.StatusServiceUnavailable, "OAUTH_NOT_CONFIGURED")
		return
	}

	operatorUID, _ := middleware.GetUID(c)

	clientID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || clientID <= 0 {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_CLIENT_ID")
		return
	}

	var req toggleOAuthClientRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	client, err := h.oauthService.GetClient(ctx, clientID)
	if err != nil {
		utils.RespondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	err = h.oauthService.ToggleClient(ctx, clientID, req.Enabled)
	if err != nil {
		utils.HTTPErrorResponse(c, "ADMIN", http.StatusInternalServerError, "TOGGLE_FAILED", err.Error())
		return
	}

	if err := h.logRepo.LogOAuthClientToggle(ctx, operatorUID, clientID, client.ClientID, client.Name, req.Enabled); err != nil {
		utils.LogWarn("ADMIN", "Failed to log toggle OAuth client", err.Error())
	}

	status := "disabled"
	if req.Enabled {
		status = "enabled"
	}
	utils.LogInfo("ADMIN", fmt.Sprintf("OAuth client %s: operatorUID=%s, clientID=%s", status, operatorUID, client.ClientID))

	utils.RespondSuccess(c, gin.H{"message": "Client " + status})
}
