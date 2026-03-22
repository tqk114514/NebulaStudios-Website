/**
 * internal/handlers/user/account.go
 * 用户账户管理 API Handler
 *
 * 功能：
 * - 发送删除账户验证码
 * - 删除账户
 * - 获取用户操作日志
 * - OAuth 授权管理
 * - 数据导出
 *
 * 依赖：
 * - UserHandler 核心结构
 */

package user

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  请求结构 ====================

// sendDeleteCodeRequest 发送删除验证码请求
type sendDeleteCodeRequest struct {
	CaptchaToken string `json:"captchaToken"`
	CaptchaType  string `json:"captchaType"`
	Language     string `json:"language"`
}

// deleteAccountRequest 删除账户请求
type deleteAccountRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// ====================  账户管理 ====================

// SendDeleteCode 发送删除账户验证码
// POST /api/auth/send-delete-code
func (h *UserHandler) SendDeleteCode(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to SendDeleteCode")
		return
	}

	var req sendDeleteCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err, "USER_NOT_FOUND")
		return
	}

	if err := h.verifyCaptcha(req.CaptchaToken, req.CaptchaType, utils.GetClientIP(c)); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for delete code: userUID=%s", userUID))
		return
	}

	if !middleware.EmailLimiter.Allow(user.Email) {
		utils.HTTPErrorResponse(c, "USER", http.StatusTooManyRequests, "RATE_LIMIT", fmt.Sprintf("Email rate limit exceeded for delete: email=%s", user.Email))
		return
	}

	token, _, err := h.tokenService.CreateToken(ctx, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "TOKEN_CREATE_FAILED", fmt.Sprintf("Token creation failed: userUID=%s", userUID))
		return
	}

	verifyURL := fmt.Sprintf("%s/account/verify?token=%s", h.baseURL, token)

	language := req.Language
	if language == "" {
		language = "zh-CN"
	}

	h.emailService.SendVerificationEmailAsync(user.Email, "delete_account", language, verifyURL, "USER")

	utils.LogInfo("USER", fmt.Sprintf("Delete code sent (async): userUID=%s, email=%s", userUID, user.Email))
	utils.RespondSuccess(c, gin.H{})
}

// DeleteAccount 删除用户账户
// POST /api/auth/delete-account
func (h *UserHandler) DeleteAccount(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to DeleteAccount")
		return
	}

	var req deleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if req.Code == "" || req.Password == "" {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "MISSING_PARAMETERS", fmt.Sprintf("Missing parameters for delete account: userUID=%s", userUID))
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err, "USER_NOT_FOUND")
		return
	}

	match, err := utils.VerifyPassword(req.Password, user.Password)
	if err != nil {
		utils.LogError("USER", "DeleteAccount", err, fmt.Sprintf("Password verification error: userUID=%s", userUID))
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "INTERNAL_ERROR", "")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "WRONG_PASSWORD", fmt.Sprintf("Delete account - wrong password: userUID=%s, email=%s", userUID, user.Email))
		return
	}

	_, err = h.tokenService.VerifyCode(ctx, req.Code, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, err.Error(), fmt.Sprintf("Delete account - code verification failed: userUID=%s", userUID))
		return
	}

	if err := h.userRepo.Delete(ctx, userUID); err != nil {
		utils.LogError("USER", "DeleteAccount", err, fmt.Sprintf("Failed to delete user: userUID=%s", userUID))
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "DELETE_FAILED", "")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogDeleteAccount(ctx, userUID); err != nil {
			utils.LogWarn("USER", "Failed to log delete account", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	if h.r2Service != nil && h.r2Service.IsConfigured() {
		if err := h.r2Service.DeleteAvatar(ctx, userUID); err != nil {
			utils.LogWarn("USER", "Failed to delete R2 avatar", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	h.invalidateUserCache(userUID)

	if err := h.tokenService.InvalidateCodeByEmail(ctx, user.Email, nil); err != nil {
		utils.LogWarn("USER", "Failed to invalidate codes after delete", fmt.Sprintf("email=%s", user.Email))
	}

	utils.ClearTokenCookieGin(c)

	utils.LogInfo("USER", fmt.Sprintf("Account deleted: userUID=%s, email=%s", userUID, user.Email))
	utils.RespondSuccess(c, gin.H{})
}

// ====================  操作日志 ====================

// GetLogs 获取用户操作日志
// GET /api/user/logs?page=1&pageSize=20
func (h *UserHandler) GetLogs(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	if h.userLogRepo == nil {
		utils.RespondError(c, http.StatusInternalServerError, "SERVICE_UNAVAILABLE")
		return
	}

	page := 1
	pageSize := 20
	if p := c.Query("page"); p != "" {
		if _, err := fmt.Sscanf(p, "%d", &page); err != nil || page < 1 {
			page = 1
		}
	}
	if ps := c.Query("pageSize"); ps != "" {
		if _, err := fmt.Sscanf(ps, "%d", &pageSize); err != nil || pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}
	}

	ctx := c.Request.Context()
	logs, total, err := h.userLogRepo.FindByUserUID(ctx, userUID, page, pageSize)
	if err != nil {
		utils.LogError("USER", "GetLogs", err, fmt.Sprintf("Failed to get logs: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"logs":       logs,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// ====================  OAuth 授权管理 ====================

// GetOAuthGrants 获取用户已授权的应用列表
// GET /api/user/oauth/grants
func (h *UserHandler) GetOAuthGrants(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	if h.oauthService == nil {
		utils.RespondError(c, http.StatusInternalServerError, "SERVICE_UNAVAILABLE")
		return
	}

	ctx := c.Request.Context()
	grants, err := h.oauthService.GetUserGrants(ctx, userUID)
	if err != nil {
		utils.LogError("USER", "GetOAuthGrants", err, fmt.Sprintf("Failed to get OAuth grants: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"grants":  grants,
	})
}

// RevokeOAuthGrant 撤销用户对某应用的授权
// DELETE /api/user/oauth/grants/:client_id
func (h *UserHandler) RevokeOAuthGrant(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	clientID := c.Param("client_id")
	if clientID == "" {
		utils.RespondError(c, http.StatusBadRequest, "MISSING_CLIENT_ID")
		return
	}

	if h.oauthService == nil {
		utils.RespondError(c, http.StatusInternalServerError, "SERVICE_UNAVAILABLE")
		return
	}

	ctx := c.Request.Context()

	client, err := h.oauthService.GetClientByClientID(ctx, clientID)
	if err != nil {
		utils.LogWarn("USER", "OAuth client not found for revoke", fmt.Sprintf("userUID=%s, clientID=%s", userUID, clientID))
	}

	if err := h.oauthService.RevokeUserClientTokens(ctx, userUID, clientID); err != nil {
		utils.LogError("USER", "RevokeOAuthGrant", err, fmt.Sprintf("Failed to revoke OAuth grant: userUID=%s, clientID=%s", userUID, clientID))
		utils.RespondError(c, http.StatusInternalServerError, "REVOKE_FAILED")
		return
	}

	if h.userLogRepo != nil && client != nil {
		if err := h.userLogRepo.LogOAuthRevoke(ctx, userUID, clientID, client.Name); err != nil {
			utils.LogWarn("USER", "Failed to log OAuth revoke", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	utils.LogInfo("USER", fmt.Sprintf("OAuth grant revoked: userUID=%s, clientID=%s", userUID, clientID))
	utils.RespondSuccess(c, gin.H{})
}

// ====================  数据导出 ====================

// getDataExportFooter 获取数据导出文件的本地化页脚
func getDataExportFooter(lang string, utcTime string) string {
	switch lang {
	case "zh-CN":
		return fmt.Sprintf("\n\n数据截止 %s", utcTime)
	case "zh-TW":
		return fmt.Sprintf("\n\n資料截止 %s", utcTime)
	case "ja":
		return fmt.Sprintf("\n\nデータ取得日時 %s", utcTime)
	case "ko":
		return fmt.Sprintf("\n\n데이터 기준 %s", utcTime)
	default:
		return fmt.Sprintf("\n\nData as of %s", utcTime)
	}
}

// RequestDataExport 请求数据导出（生成一次性下载 Token）
// POST /api/user/export/request
func (h *UserHandler) RequestDataExport(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.RespondError(c, http.StatusUnauthorized, "UNAUTHORIZED")
		return
	}

	if !middleware.DataExportLimiter.Allow(userUID) {
		waitTime := middleware.DataExportLimiter.GetWaitTime(userUID)
		utils.LogWarn("USER", "Data export rate limit exceeded", fmt.Sprintf("userUID=%s, waitTime=%ds", userUID, waitTime))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success":   false,
			"errorCode": "RATE_LIMIT",
			"waitTime":  waitTime,
		})
		return
	}

	token, err := generateExportToken()
	if err != nil {
		utils.LogError("USER", "RequestDataExport", err, fmt.Sprintf("Failed to generate export token: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "TOKEN_GENERATE_FAILED")
		return
	}

	dataExportTokensMu.Lock()
	if len(dataExportTokens) >= maxDataExportTokensCapacity {
		evictCount := maxDataExportTokensCapacity / 10
		toEvict := findOldestExportTokens(evictCount)
		for _, t := range toEvict {
			delete(dataExportTokens, t)
			delete(dataExportTokenIndex, t)
		}
	}
	dataExportTokenCounter++
	dataExportTokens[token] = &dataExportToken{
		UserUID:   userUID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	dataExportTokenIndex[token] = dataExportTokenCounter
	dataExportTokensMu.Unlock()

	utils.LogInfo("USER", fmt.Sprintf("Data export token generated: userUID=%s", userUID))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"token":   token,
	})
}

// DownloadUserData 下载用户数据
// GET /api/user/export/download?token=xxx
func (h *UserHandler) DownloadUserData(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		utils.RespondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	dataExportTokensMu.Lock()
	tokenData, exists := dataExportTokens[token]
	if exists {
		delete(dataExportTokens, token)
		delete(dataExportTokenIndex, token)
	}
	dataExportTokensMu.Unlock()

	if !exists {
		utils.LogWarn("USER", "Invalid export token", "")
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	if time.Now().After(tokenData.ExpiresAt) {
		utils.LogWarn("USER", "Export token expired", fmt.Sprintf("userUID=%s", tokenData.UserUID))
		utils.RespondError(c, http.StatusBadRequest, "TOKEN_EXPIRED")
		return
	}

	userUID := tokenData.UserUID
	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.LogError("USER", "DownloadUserData", err, fmt.Sprintf("FindByUID failed for export: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	var logs []*models.UserLog
	if h.userLogRepo != nil {
		logs, _, err = h.userLogRepo.FindByUserUID(ctx, userUID, 1, 10000)
		if err != nil {
			utils.LogWarn("USER", "Failed to get logs for export", fmt.Sprintf("userUID=%s", userUID))
			logs = []*models.UserLog{}
		}
	}

	exportData := gin.H{
		"export_info": gin.H{
			"exported_at": time.Now().UTC().Format(time.RFC3339),
			"user_uid":    userUID,
		},
		"user_info": gin.H{
			"username":         user.Username,
			"email":            user.Email,
			"avatar_url":       user.AvatarURL,
			"microsoft_id":     user.MicrosoftID,
			"microsoft_name":   user.MicrosoftName,
			"microsoft_avatar": user.MicrosoftAvatarURL,
			"created_at":       user.CreatedAt,
			"updated_at":       user.UpdatedAt,
		},
		"operation_logs": logs,
	}

	jsonData, err := json.MarshalIndent(exportData, "", "  ")
	if err != nil {
		utils.LogError("USER", "DownloadUserData", err, fmt.Sprintf("Failed to marshal export data: userUID=%s", userUID))
		utils.RespondError(c, http.StatusInternalServerError, "EXPORT_FAILED")
		return
	}

	lang := utils.GetLanguageCookie(c)
	if lang == "" {
		lang = "en"
	}

	now := time.Now().UTC()
	utcTimeStr := now.Format("2006-01-02 15:04:05") + " UTC"

	footer := getDataExportFooter(lang, utcTimeStr)
	finalData := append(jsonData, []byte(footer)...)

	filename := fmt.Sprintf("nebula_account_data_%s_%s.txt", userUID, time.Now().Format("20060102_150405"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", finalData)

	utils.LogInfo("USER", fmt.Sprintf("Data exported: userUID=%s, size=%d bytes", userUID, len(finalData)))
}
