package user

import (
	"errors"
	"fmt"
	"net/http"

	"auth-system/internal/middleware"
	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

type updateUsernameRequest struct {
	Username     string `json:"username"`
	CaptchaToken string `json:"captchaToken"`
}

// updateAvatarRequest 更新头像请求
type updateAvatarRequest struct {
	AvatarURL string `json:"avatar_url"`
}

// UpdateUsername 更新用户名
// PATCH /api/user/username
func (h *UserHandler) UpdateUsername(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to UpdateUsername")
		return
	}

	var req updateUsernameRequest
	if !utils.BindJSONOrError(c, "USER", &req, "INVALID_REQUEST") {
		return
	}

	if err := h.verifyCaptcha(req.CaptchaToken, utils.GetClientIP(c)); err != nil {
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
		utils.HTTPErrorResponse(c, "USER", http.StatusConflict, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: username=%s", newUsername))
		return
	}

	if err := h.userRepo.Update(ctx, userUID, map[string]any{"username": newUsername}); err != nil {
		if errors.Is(err, models.ErrUsernameExists) {
			utils.HTTPErrorResponse(c, "USER", http.StatusConflict, "USERNAME_ALREADY_EXISTS", fmt.Sprintf("Username already exists: username=%s", newUsername))
			return
		}
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to update username: userUID=%s", userUID))
		return
	}

	h.invalidateUserCache(c.Request.Context(), userUID)

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogChangeUsername(ctx, userUID, oldUsername, newUsername); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log username change", "user_uid", userUID)
		}
	}

	utils.LogInfoCtx(c.Request.Context(), "USER", "Username updated", "user_uid", userUID, "new_username", newUsername)
	utils.RespondSuccess(c, gin.H{"username": newUsername})
}

// UpdateAvatar 更新头像
// PATCH /api/user/avatar
func (h *UserHandler) UpdateAvatar(c *gin.Context) {
	userUID, ok := middleware.GetUID(c)
	if !ok {
		utils.HTTPErrorResponse(c, "USER", http.StatusUnauthorized, "UNAUTHORIZED", "Unauthorized access to UpdateAvatar")
		return
	}

	var req updateAvatarRequest
	if !utils.BindJSONOrError(c, "USER", &req, "INVALID_REQUEST") {
		return
	}

	ctx := c.Request.Context()

	// 移除头像：清空 avatar_url、删除本地文件、关闭微软头像自动同步、清除微软头像哈希（确保恢复同步时重新拉取）
	if req.AvatarURL == "" {
		currentUser, err := h.userRepo.FindByUID(ctx, userUID)
		if err != nil {
			utils.HTTPDatabaseError(c, "USER", err)
			return
		}
		oldAvatarURL := currentUser.AvatarURL

		// 删除本地存储的头像文件（幂等，文件不存在时忽略）
		if h.storageService != nil && h.storageService.IsConfigured() {
			_ = h.storageService.DeleteAvatar(ctx, userUID)
		}

		// 仅当当前使用微软头像时改为默认头像 URL；使用自定义头像时保留原 URL
		updates := map[string]any{
			"microsoft_avatar_sync": false,
			"microsoft_avatar_hash": nil,
		}
		if currentUser.AvatarURL == "microsoft" {
			updates["avatar_url"] = h.defaultAvatarURL
		}

		if err := h.userRepo.Update(ctx, userUID, updates); err != nil {
			utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to remove avatar: userUID=%s", userUID))
			return
		}

		h.invalidateUserCache(c.Request.Context(), userUID)

		// 隐私日志：仅当同步此前开启（true→false 实际变化）时记录关闭同步
		if h.userLogRepo != nil {
			if currentUser.MicrosoftAvatarSync {
				if err := h.userLogRepo.LogDisableAvatarSync(ctx, userUID, "microsoft"); err != nil {
					utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log avatar sync disable", "user_uid", userUID)
				}
			}
			if err := h.userLogRepo.LogChangeAvatar(ctx, userUID, oldAvatarURL, ""); err != nil {
				utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log avatar removal", "user_uid", userUID)
			}
		}

		// 响应当前生效的头像 URL：微软头像→默认头像；自定义头像→保持原样
		resultAvatarURL := currentUser.AvatarURL
		if currentUser.AvatarURL == "microsoft" {
			resultAvatarURL = h.defaultAvatarURL
		}
		utils.LogInfoCtx(c.Request.Context(), "USER", "Avatar removed", "user_uid", userUID)
		utils.RespondSuccess(c, gin.H{"avatar_url": resultAvatarURL})
		return
	}

	urlResult := utils.ValidateAvatarURL(req.AvatarURL)
	if !urlResult.Valid {
		utils.HTTPErrorResponse(c, "USER", http.StatusBadRequest, urlResult.ErrorCode, fmt.Sprintf("Avatar URL validation failed: userUID=%s", userUID))
		return
	}

	currentUser, err := h.userRepo.FindByUID(ctx, userUID)
	if err != nil {
		utils.HTTPDatabaseError(c, "USER", err)
		return
	}
	oldAvatarURL := currentUser.AvatarURL

	updates := map[string]any{"avatar_url": urlResult.Value}
	// 使用微软头像时重新开启自动同步
	if urlResult.Value == "microsoft" {
		updates["microsoft_avatar_sync"] = true
	}

	if err := h.userRepo.Update(ctx, userUID, updates); err != nil {
		utils.HTTPErrorResponse(c, "USER", http.StatusInternalServerError, "UPDATE_FAILED", fmt.Sprintf("Failed to update avatar: userUID=%s", userUID))
		return
	}

	h.invalidateUserCache(c.Request.Context(), userUID)

	if h.userLogRepo != nil {
		// 隐私日志：仅当同步此前关闭（false→true 实际变化）时记录开启同步
		if urlResult.Value == "microsoft" && !currentUser.MicrosoftAvatarSync {
			if err := h.userLogRepo.LogEnableAvatarSync(ctx, userUID, "microsoft"); err != nil {
				utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log avatar sync enable", "user_uid", userUID)
			}
		}
		if err := h.userLogRepo.LogChangeAvatar(ctx, userUID, oldAvatarURL, urlResult.Value); err != nil {
			utils.LogWarnCtx(c.Request.Context(), "USER", "Failed to log avatar change", "user_uid", userUID)
		}
	}

	utils.LogInfoCtx(c.Request.Context(), "USER", "Avatar updated", "user_uid", userUID)
	utils.RespondSuccess(c, gin.H{"avatar_url": urlResult.Value})
}
