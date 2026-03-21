/**
 * internal/handlers/user/profile.go
 * 用户个人资料 API Handler
 *
 * 功能：
 * - 更新用户名
 * - 更新头像
 *
 * 依赖：
 * - UserHandler 核心结构
 */

package user

import (
	"fmt"
	"net/http"

	"auth-system/internal/middleware"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  请求结构 ====================

// updateUsernameRequest 更新用户名请求
type updateUsernameRequest struct {
	Username     string `json:"username"`
	CaptchaToken string `json:"captchaToken"`
	CaptchaType  string `json:"captchaType"`
}

// updateAvatarRequest 更新头像请求
type updateAvatarRequest struct {
	AvatarURL string `json:"avatar_url"`
}

// ====================  公开方法 ====================

// UpdateUsername 更新用户名
// POST /api/user/username
func (h *UserHandler) UpdateUsername(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to UpdateUsername")
		return
	}

	var req updateUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if err := h.verifyCaptcha(req.CaptchaToken, req.CaptchaType, utils.GetClientIP(c)); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for username change: userID=%d", userID))
		return
	}

	usernameResult := utils.ValidateUsername(req.Username)
	if !usernameResult.Valid {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, usernameResult.ErrorCode, fmt.Sprintf("Username validation failed: userID=%d", userID))
		return
	}

	ctx := c.Request.Context()
	newUsername := usernameResult.Value

	currentUser, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err)
		return
	}
	oldUsername := currentUser.Username

	existingUser, err := h.userRepo.FindByUsername(ctx, newUsername)
	if err != nil {
		if !utils.IsDatabaseNotFound(err) {
			utils.HTTPDatabaseError(c, "USER", err)
			return
		}
	}
	if existingUser != nil && existingUser.ID != userID {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: username=%s, existingUserID=%d, requestUserID=%d", newUsername, existingUser.ID, userID))
		return
	}

	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"username": newUsername}); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to update username: userID=%d", userID))
		return
	}

	h.invalidateUserCache(userID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeUsername(ctx, userID, oldUsername, newUsername); err != nil {
			utils.LogWarn("USER", "Failed to log username change", fmt.Sprintf("userID=%d", userID))
		}
	}

	utils.LogInfo("USER", fmt.Sprintf("Username updated: userID=%d, newUsername=%s", userID, newUsername))
	utils.RespondSuccess(c, gin.H{"username": newUsername})
}

// UpdateAvatar 更新头像
// POST /api/user/avatar
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to UpdateAvatar")
		return
	}

	var req updateAvatarRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	urlResult := utils.ValidateAvatarURL(req.AvatarURL)
	if !urlResult.Valid {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, urlResult.ErrorCode, fmt.Sprintf("Avatar URL validation failed: userID=%d", userID))
		return
	}

	ctx := c.Request.Context()

	currentUser, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err)
		return
	}
	oldAvatarURL := currentUser.AvatarURL

	if err := h.userRepo.Update(ctx, userID, map[string]interface{}{"avatar_url": urlResult.Value}); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to update avatar: userID=%d", userID))
		return
	}

	h.invalidateUserCache(userID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeAvatar(ctx, userID, oldAvatarURL, urlResult.Value); err != nil {
			utils.LogWarn("USER", "Failed to log avatar change", fmt.Sprintf("userID=%d", userID))
		}
	}

	utils.LogInfo("USER", fmt.Sprintf("Avatar updated: userID=%d", userID))
	utils.RespondSuccess(c, gin.H{"avatar_url": urlResult.Value})
}
