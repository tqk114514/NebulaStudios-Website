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
	"errors"
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
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to UpdateUsername")
		return
	}

	var req updateUsernameRequest
	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	if err := h.verifyCaptcha(req.CaptchaToken, req.CaptchaType, utils.GetClientIP(c)); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "CAPTCHA_FAILED", fmt.Sprintf("Captcha verification failed for username change: userUID=%s", userUID))
		return
	}

	usernameResult := utils.ValidateUsername(req.Username)
	if !usernameResult.Valid {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, usernameResult.ErrorCode, fmt.Sprintf("Username validation failed: userUID=%s", userUID))
		return
	}

	ctx := c.Request.Context()
	newUsername := usernameResult.Value

	currentUser, err := h.userRepo.FindByUID(ctx, userUID)
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
	if existingUser != nil && existingUser.UID != userUID {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: username=%s, existingUserUID=%s, requestUserUID=%s", newUsername, existingUser.UID, userUID))
		return
	}

	if err := h.userRepo.Update(ctx, userUID, map[string]any{"username": newUsername}); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to update username: userUID=%s", userUID))
		return
	}

	h.invalidateUserCache(userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeUsername(ctx, userUID, oldUsername, newUsername); err != nil {
			utils.LogWarn("USER", "Failed to log username change", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	utils.LogInfo("USER", fmt.Sprintf("Username updated: userUID=%s, newUsername=%s", userUID, newUsername))
	utils.RespondSuccess(c, gin.H{"username": newUsername})
}

// UpdateAvatar 更新头像
// POST /api/user/avatar
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to UpdateAvatar")
		return
	}

	var req updateAvatarRequest
	if err := utils.BindJSON(c, &req); err != nil {
		if errors.Is(err, utils.ErrBodyTooLarge) {
			return
		}
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}

	urlResult := utils.ValidateAvatarURL(req.AvatarURL)
	if !urlResult.Valid {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, urlResult.ErrorCode, fmt.Sprintf("Avatar URL validation failed: userUID=%s", userUID))
		return
	}

	ctx := c.Request.Context()

	currentUser, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err)
		return
	}
	oldAvatarURL := currentUser.AvatarURL

	if err := h.userRepo.Update(ctx, userUID, map[string]any{"avatar_url": urlResult.Value}); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to update avatar: userUID=%s", userUID))
		return
	}

	h.invalidateUserCache(userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeAvatar(ctx, userUID, oldAvatarURL, urlResult.Value); err != nil {
			utils.LogWarn("USER", "Failed to log avatar change", fmt.Sprintf("userUID=%s", userUID))
		}
	}

	utils.LogInfo("USER", fmt.Sprintf("Avatar updated: userUID=%s", userUID))
	utils.RespondSuccess(c, gin.H{"avatar_url": urlResult.Value})
}
