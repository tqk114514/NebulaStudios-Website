/**
 * internal/handlers/oauth/microsoft/utils.go
 * Microsoft OAuth 工具方法
 *
 * 功能：
 * - 邮箱提取
 * - 头像处理（上传、哈希计算、异步处理）
 * - 绑定/登录操作处理
 * - data URL 解析
 *
 * 依赖：
 * - auth-system/internal/handlers/oauth (公共类型和常量)
 * - internal/config (配置)
 * - internal/services (R2 服务)
 * - internal/utils (日志)
 */

package microsoft

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"auth-system/internal/handlers/oauth"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// ====================  工具方法 ====================

// extractEmail 从微软用户信息中提取邮箱
func (h *MicrosoftHandler) extractEmail(msUser map[string]interface{}) string {
	if mail, ok := msUser["mail"].(string); ok && mail != "" {
		return strings.ToLower(strings.TrimSpace(mail))
	}

	if upn, ok := msUser["userPrincipalName"].(string); ok && upn != "" {
		return strings.ToLower(strings.TrimSpace(upn))
	}

	return ""
}

// parseDataURL 解析 data URL，返回二进制数据和 content-type
func (h *MicrosoftHandler) parseDataURL(dataURL string) ([]byte, string) {
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, ""
	}

	commaIdx := strings.Index(dataURL, ",")
	if commaIdx == -1 {
		return nil, ""
	}

	header := dataURL[5:commaIdx]
	contentType := "image/jpeg"
	if semicolonIdx := strings.Index(header, ";"); semicolonIdx != -1 {
		contentType = header[:semicolonIdx]
	} else {
		contentType = header
	}

	base64Data := dataURL[commaIdx+1:]
	imageData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		utils.LogWarn("OAUTH-MS", "Failed to decode base64 avatar", "")
		return nil, ""
	}

	return imageData, contentType
}

// ====================  头像处理 ====================

// uploadAvatarToR2 上传头像到 R2 并返回 URL
// 如果 R2 未配置，返回 base64 data URL
func (h *MicrosoftHandler) uploadAvatarToR2(ctx context.Context, userID int64, imageData []byte, contentType string) string {
	if len(imageData) == 0 {
		return ""
	}

	if h.r2Service != nil && h.r2Service.IsConfigured() {
		avatarURL, err := h.r2Service.UploadAvatar(ctx, userID, imageData)
		if err != nil {
			utils.LogWarn("OAUTH-MS", "Failed to upload avatar to R2, falling back to base64", fmt.Sprintf("userID=%d", userID))
		} else {
			return avatarURL
		}
	}

	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(imageData)
}

// calculateAvatarHash 计算头像数据的 SHA256 哈希
func (h *MicrosoftHandler) calculateAvatarHash(imageData []byte) string {
	if len(imageData) == 0 {
		return ""
	}
	hash := sha256.Sum256(imageData)
	return hex.EncodeToString(hash[:])
}

// processAvatarAsync 异步处理头像上传
// 在后台 goroutine 中执行，不阻塞登录流程
func (h *MicrosoftHandler) processAvatarAsync(userID int64, oldAvatarHash string, avatarData []byte, avatarContentType string) {
	defer func() {
		if r := recover(); r != nil {
			utils.LogError("OAUTH-MS", "processAvatarAsync", fmt.Errorf("panic: %v", r), fmt.Sprintf("userID=%d", userID))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	newAvatarHash := h.calculateAvatarHash(avatarData)

	if newAvatarHash != "" && newAvatarHash != oldAvatarHash {
		microsoftAvatarURL := h.uploadAvatarToR2(ctx, userID, avatarData, avatarContentType)

		err := h.userRepo.Update(ctx, userID, map[string]interface{}{
			"microsoft_avatar_url":  microsoftAvatarURL,
			"microsoft_avatar_hash": newAvatarHash,
		})
		if err != nil {
			utils.LogError("OAUTH-MS", "processAvatarAsync", err, fmt.Sprintf("Failed to update avatar: userID=%d", userID))
			return
		}

		h.userCache.Invalidate(userID)
		utils.LogInfo("OAUTH-MS", fmt.Sprintf("Avatar updated async: userID=%d", userID))

	} else if newAvatarHash == "" && oldAvatarHash != "" {
		err := h.userRepo.Update(ctx, userID, map[string]interface{}{
			"microsoft_avatar_url":  nil,
			"microsoft_avatar_hash": nil,
		})
		if err != nil {
			utils.LogError("OAUTH-MS", "processAvatarAsync", err, fmt.Sprintf("Failed to clear avatar: userID=%d", userID))
			return
		}

		h.userCache.Invalidate(userID)
		utils.LogInfo("OAUTH-MS", fmt.Sprintf("Avatar cleared async: userID=%d", userID))

	} else {
		utils.LogInfo("OAUTH-MS", fmt.Sprintf("Avatar unchanged, skipping: userID=%d", userID))
	}
}

// ====================  操作处理 ====================

// handleLinkAction 处理绑定操作
func (h *MicrosoftHandler) handleLinkAction(c *gin.Context, ctx context.Context, currentUserID int64, microsoftID, displayName string, avatarData []byte, avatarContentType string) {
	existingUser, err := h.userRepo.FindByMicrosoftID(ctx, microsoftID)
	if err != nil {
		utils.LogDebug("OAUTH-MS", "FindByMicrosoftID error in handleLinkAction")
	}

	if existingUser != nil && existingUser.ID != currentUserID {
		utils.LogWarn("OAUTH-MS", "Microsoft account already linked to another user", fmt.Sprintf("msID=%s, existingUserID=%d, currentUserID=%d", microsoftID, existingUser.ID, currentUserID))
		oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "microsoft_already_linked")
		return
	}

	err = h.userRepo.Update(ctx, currentUserID, map[string]interface{}{
		"microsoft_id":   microsoftID,
		"microsoft_name": displayName,
	})
	if err != nil {
		utils.LogError("OAUTH-MS", "handleLinkAction", err, fmt.Sprintf("Failed to update user with Microsoft info: userID=%d", currentUserID))
		oauth.RedirectWithError(c, h.baseURL, "/account/dashboard", "link_failed")
		return
	}

	if h.userLogRepo != nil {
		if err := h.userLogRepo.LogLinkMicrosoft(ctx, currentUserID, microsoftID, displayName); err != nil {
			utils.LogWarn("OAUTH-MS", "Failed to log link microsoft", fmt.Sprintf("userID=%d", currentUserID))
		}
	}

	h.userCache.Invalidate(currentUserID)

	go h.processAvatarAsync(currentUserID, "", avatarData, avatarContentType)

	utils.LogInfo("OAUTH-MS", fmt.Sprintf("Microsoft account linked: userID=%d, msID=%s", currentUserID, microsoftID))
	oauth.RedirectWithSuccess(c, h.baseURL, "/account/dashboard", "microsoft_linked")
}

// handleLoginAction 处理登录操作
func (h *MicrosoftHandler) handleLoginAction(c *gin.Context, ctx context.Context, microsoftID, email, displayName string, avatarData []byte, avatarContentType string) {
	user, err := h.userRepo.FindByMicrosoftID(ctx, microsoftID)
	if err != nil {
		utils.LogDebug("OAUTH-MS", "FindByMicrosoftID error in handleLoginAction")
	}

	if user != nil {
		oldAvatarHash := ""
		if user.MicrosoftAvatarHash.Valid {
			oldAvatarHash = user.MicrosoftAvatarHash.String
		}

		err = h.userRepo.Update(ctx, user.ID, map[string]interface{}{
			"microsoft_name": displayName,
		})
		if err != nil {
			utils.LogWarn("OAUTH-MS", "Failed to update Microsoft name", fmt.Sprintf("userID=%d", user.ID))
		}
		h.userCache.Invalidate(user.ID)

		go h.processAvatarAsync(user.ID, oldAvatarHash, avatarData, avatarContentType)
	}

	if user == nil && email != "" {
		existingUser, err := h.userRepo.FindByEmail(ctx, email)
		if err != nil {
			utils.LogDebug("OAUTH-MS", "FindByEmail error in handleLoginAction")
		}

		if existingUser != nil && !existingUser.MicrosoftID.Valid {
			linkToken, err := oauth.GenerateLinkToken()
			if err != nil {
				utils.LogError("OAUTH-MS", "handleLoginAction", err, "Failed to generate link token")
				oauth.RedirectWithError(c, h.baseURL, "/account/login", "oauth_error")
				return
			}

			var providerAvatarURL string
			if len(avatarData) > 0 {
				providerAvatarURL = "data:" + avatarContentType + ";base64," + base64.StdEncoding.EncodeToString(avatarData)
			}

			oauth.SavePendingLink(linkToken, &oauth.PendingLink{
				UserID:            existingUser.ID,
				ProviderID:        microsoftID,
				DisplayName:       displayName,
				ProviderAvatarURL: providerAvatarURL,
				Email:             email,
				Timestamp:         time.Now().UnixMilli(),
			})

			utils.LogInfo("OAUTH-MS", fmt.Sprintf("Found existing user with same email, redirecting to confirm: email=%s, userID=%d", email, existingUser.ID))
			utils.SetLinkTokenCookieGin(c, linkToken)
			c.Redirect(http.StatusFound, h.baseURL+"/account/link")
			return
		}
	}

	if user == nil {
		utils.LogInfo("OAUTH-MS", fmt.Sprintf("No linked account found for Microsoft ID: %s", microsoftID))
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "no_linked_account")
		return
	}

	token, err := h.sessionService.GenerateToken(user.ID)
	if err != nil {
		utils.LogError("OAUTH-MS", "handleLoginAction", err, fmt.Sprintf("Token generation failed: userID=%d", user.ID))
		oauth.RedirectWithError(c, h.baseURL, "/account/login", "token_error")
		return
	}

	oauth.SetAuthCookie(c, token)
	utils.LogInfo("OAUTH-MS", fmt.Sprintf("Microsoft login successful: username=%s, userID=%d", user.Username, user.ID))
	c.Redirect(http.StatusFound, h.baseURL+"/account/dashboard")
}
