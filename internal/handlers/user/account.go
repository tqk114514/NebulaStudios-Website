package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"auth-system/internal/handlers"
	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/paths"
	"auth-system/internal/services"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

type sendDeleteCodeRequest struct {
	CaptchaToken string `json:"captchaToken"`
	Language     string `json:"language"`
}

// deleteAccountRequest 删除账户请求
type deleteAccountRequest struct {
	Code     string `json:"code"`
	Password string `json:"password"`
}

// SendDeleteCode 发送删除账户验证码
// POST /api/auth/send-delete-code
func (h *UserHandler) SendDeleteCode(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to SendDeleteCode")
		return
	}

	var req sendDeleteCodeRequest
	if !utils.BindJSONOrError(c, "USER", &req, "INVALID_REQUEST") {
		return
	}
	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err, "USER_NOT_FOUND")
		return
	}

	if err := h.verifyCaptcha(req.CaptchaToken, utils.GetClientIP(c)); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for delete code: userUID=%s", userUID))
		return
	}

	if !h.limiterMgr.EmailAllow(user.Email) {
		waitTime := h.limiterMgr.EmailWaitTime(user.Email)
		utils.LogWarnCtx(c.Request.Context(), "USER", fmt.Sprintf("Email rate limit exceeded for delete: email=%s, wait=%ds", user.Email, waitTime))
		utils.RespondRateLimit(c, time.Now().Add(time.Duration(waitTime)*time.Second).Unix())
		return
	}

	token, _, err := h.tokenService.CreateToken(ctx, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "TOKEN_CREATE_FAILED", fmt.Sprintf("Token creation failed: userUID=%s", userUID))
		return
	}

	// SPA 前端用 query 传 token（vue-router route.query 原生支持，hash 会被路由重写丢失）
	verifyURL := h.baseURL + paths.PathAccountVerify + "?token=" + token

	language := req.Language
	if language == "" {
		language = "en"
	}

	h.emailService.SendVerificationEmailAsync(user.Email, "delete_account", language, verifyURL, "USER")

	utils.LogInfoCtx(c.Request.Context(), "USER", "Delete code sent (async)", "user_uid", userUID, "email", user.Email)
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
	if !utils.BindJSONOrError(c, "USER", &req, "INVALID_REQUEST") {
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
		utils.LogErrorCtx(c.Request.Context(), "USER", "DeleteAccount", err, "user_uid", userUID)
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "INTERNAL_ERROR", "")
		return
	}
	if !match {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "WRONG_PASSWORD", fmt.Sprintf("Delete account - wrong password: userUID=%s, email=%s", userUID, user.Email))
		return
	}

	_, err = h.tokenService.VerifyCode(ctx, req.Code, user.Email, services.TokenTypeDeleteAccount)
	if err != nil {
		handlers.RespondTokenError(c, "USER", err, fmt.Sprintf("Delete account - code verification failed: userUID=%s", userUID))
		return
	}

	if err := h.userRepo.Delete(ctx, userUID); err != nil {
		utils.LogErrorCtx(c.Request.Context(), "USER", "DeleteAccount", err, "user_uid", userUID)
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "DELETE_FAILED", "")
		return
	}

	if h.oauthService != nil {
		if err := h.oauthService.RevokeUserTokens(ctx, userUID); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to revoke OAuth tokens after account deletion", "user_uid", userUID)
		}
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogDeleteAccount(ctx, userUID); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log delete account", "user_uid", userUID)
		}
	}

	if h.storageService != nil && h.storageService.IsConfigured() {
		if err := h.storageService.DeleteAvatar(ctx, userUID); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to delete avatar", "user_uid", userUID)
		}
	}

	h.invalidateUserCache(c.Request.Context(), userUID)

	if err := h.tokenService.InvalidateCodeByEmail(ctx, user.Email, nil); err != nil {
		utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to invalidate codes after delete", "email", user.Email)
	}

	utils.ClearTokenCookieGin(c)

	utils.LogInfoCtx(c.Request.Context(), "USER", "Account deleted", "user_uid", userUID, "email", user.Email)
	utils.RespondSuccess(c, gin.H{})
}

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
		utils.LogErrorCtx(c.Request.Context(), "USER", "GetLogs", err, "user_uid", userUID)
		utils.RespondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	// data 嵌套返回（前端 api client 统一读 payload.data）
	utils.RespondSuccessWithData(c, gin.H{
		"logs":       logs,
		"total":      total,
		"page":       page,
		"pageSize":   pageSize,
		"totalPages": totalPages,
	})
}

// GetOAuthGrants 获取用户已授权的 OAuth 应用列表
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
		utils.LogErrorCtx(c.Request.Context(), "USER", "GetOAuthGrants", err, "user_uid", userUID)
		utils.RespondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	// data 嵌套返回（前端 api client 统一读 payload.data）
	utils.RespondSuccessWithData(c, gin.H{
		"grants": grants,
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

	// 校验 client 存在
	client, err := h.oauthService.GetClientByClientID(ctx, clientID)
	if err != nil {
		utils.RespondError(c, http.StatusNotFound, "CLIENT_NOT_FOUND")
		return
	}

	// 校验 grant 归属当前用户，防止撤销他人或不存在的授权
	if _, err := h.oauthService.FindUserGrant(ctx, userUID, clientID); err != nil {
		if errors.Is(err, models.ErrOAuthGrantNotFound) {
			utils.RespondError(c, http.StatusNotFound, "GRANT_NOT_FOUND")
			return
		}
		utils.LogErrorCtx(c.Request.Context(), "USER", "RevokeOAuthGrant", err, "user_uid", userUID, "client_id", clientID)
		utils.RespondError(c, http.StatusInternalServerError, "GRANT_LOOKUP_FAILED")
		return
	}

	if err := h.oauthService.RevokeUserClientTokens(ctx, userUID, clientID); err != nil {
		utils.LogErrorCtx(c.Request.Context(), "USER", "RevokeOAuthGrant", err, "user_uid", userUID, "client_id", clientID)
		utils.RespondError(c, http.StatusInternalServerError, "REVOKE_FAILED")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogOAuthRevoke(ctx, userUID, clientID, client.Name); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log OAuth revoke", "user_uid", userUID)
		}
	}

	utils.LogInfoCtx(c.Request.Context(), "USER", "OAuth grant revoked", "user_uid", userUID, "client_id", clientID)
	utils.RespondSuccess(c, gin.H{})
}

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

	if !h.limiterMgr.DataExportAllow(userUID) {
		waitTime := h.limiterMgr.DataExportWaitTime(userUID)
		utils.LogWarnCtx(c.Request.Context(), "USER", "Data export rate limit exceeded", "user_uid", userUID, "wait_time", waitTime)
		utils.RespondRateLimit(c, time.Now().Add(time.Duration(waitTime)*time.Second).Unix())
		return
	}

	token, err := h.exportTokenService.Generate(userUID)
	if err != nil {
		utils.LogErrorCtx(c.Request.Context(), "USER", "RequestDataExport", err, "user_uid", userUID)
		utils.RespondError(c, http.StatusInternalServerError, "TOKEN_GENERATE_FAILED")
		return
	}

	utils.LogInfoCtx(c.Request.Context(), "USER", "Data export token generated", "user_uid", userUID)
	// data 嵌套返回（前端 api client 统一读 payload.data）
	utils.RespondSuccessWithData(c, gin.H{
		"token": token,
	})
}

// DownloadUserData 下载用户数据
// GET /api/user/export/:token
func (h *UserHandler) DownloadUserData(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		utils.RespondError(c, http.StatusBadRequest, "MISSING_TOKEN")
		return
	}

	userUID, valid := h.exportTokenService.ValidateAndConsume(token)

	if !valid || userUID == "" {
		utils.LogWarnCtx(c.Request.Context(), "USER", "Invalid export token")
		utils.RespondError(c, http.StatusBadRequest, "INVALID_TOKEN")
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.LogErrorCtx(c.Request.Context(), "USER", "DownloadUserData", err, "user_uid", userUID)
		utils.RespondError(c, http.StatusInternalServerError, "DATABASE_ERROR")
		return
	}

	var logs []*models.UserLog
	if h.userLogRepo != nil {
		logs, _, err = h.userLogRepo.FindByUserUID(ctx, userUID, 1, 10000)
		if err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to get logs for export", "user_uid", userUID)
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
		utils.LogErrorCtx(c.Request.Context(), "USER", "DownloadUserData", err, "user_uid", userUID)
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

	filename := fmt.Sprintf("nebula_account_data_%s_%s.txt", userUID, time.Now().In(utils.ShanghaiLocation()).Format("20060102_150405"))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", finalData)

	utils.LogInfoCtx(c.Request.Context(), "USER", "Data exported", "user_uid", userUID, "size", len(finalData))
}
