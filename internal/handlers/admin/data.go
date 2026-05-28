/**
 * internal/handlers/admin/data.go
 * 管理后台 API Handler - 数据导入导出
 *
 * 功能：
 * - 导出数据加密下载（OTAC 授权）
 * - 导入数据解密写入
 *
 * 权限：仅限超级管理员（SuperAdminMiddleware）
 */

package admin

import (
	"auth-system/internal/utils"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ====================  请求/响应结构 ====================

type exportRequestResponse struct {
	RequestID string `json:"requestId"`
	ExpiresIn int    `json:"expiresIn"`
}

type exportDownloadRequest struct {
	RequestID string `json:"requestId"`
	OTAC      string `json:"otac"`
}

type importPreviewResponse struct {
	FileToken  string `json:"fileToken"`
	UsersCount int    `json:"usersCount"`
	LogsCount  int    `json:"logsCount"`
	ExportedAt string `json:"exportedAt"`
	ExportedBy string `json:"exportedBy"`
}

type importExecuteRequest struct {
	FileToken string `json:"fileToken"`
	Strategy  string `json:"strategy"` // "merge" (default) or "overwrite"
}

type importExecuteResponse struct {
	UsersImported int `json:"usersImported"`
	LogsImported  int `json:"logsImported"`
}

// ====================  Handler 方法 ====================

// RequestExport 生成 OTAC（一次性授权码）
// POST /admin/api/data/export/request
func (h *AdminHandler) RequestExport(c *gin.Context) {
	requestID, otac, expiresAt := h.exportService.GenerateOTAC()

	utils.LogInfo("DATA-EXPORT", fmt.Sprintf("OTAC: %s | request_id: %s", otac, requestID))

	utils.RespondSuccess(c, gin.H{
		"requestId": requestID,
		"expiresIn": int(time.Until(expiresAt).Seconds()),
	})
}

// DownloadExport 验证 OTAC 并返回加密数据
// POST /admin/api/data/export/download
func (h *AdminHandler) DownloadExport(c *gin.Context) {
	operatorUID := c.GetString("uid")

	var req exportDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	if err := h.exportService.ValidateOTAC(req.RequestID, req.OTAC); err != nil {
		errMsg := err.Error()
		errorCode := "OTAC_INVALID"
		statusCode := http.StatusForbidden

		if errMsg == "OTAC expired" {
			errorCode = "OTAC_EXPIRED"
		} else if len(errMsg) > 0 && errMsg[:7] == "OTAC in" {
			errorCode = "OTAC_MAX_TRIES"
		}

		utils.RespondError(c, statusCode, errorCode)
		return
	}

	salt1, err := utils.ParseExportSalt1(h.dataExportSalt)
	if err != nil {
		utils.LogError("DATA-EXPORT", "DownloadExport", err)
		utils.RespondError(c, http.StatusInternalServerError, "EXPORT_SALT_NOT_CONFIGURED")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout)
	defer cancel()

	users, err := h.queryAllUsers(ctx)
	if err != nil {
		utils.LogError("DATA-EXPORT", "DownloadExport", err, "Failed to query users")
		utils.RespondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	logs, err := h.queryAllUserLogs(ctx)
	if err != nil {
		utils.LogError("DATA-EXPORT", "DownloadExport", err, "Failed to query user logs")
		utils.RespondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	salt2 := utils.GenerateExportSalt2()

	now := time.Now().UTC().Format(time.RFC3339)
	header := &utils.ExportHeader{
		Version:    1,
		ExportedAt: now,
		ExportedBy: operatorUID,
		UsersCount: len(users),
		LogsCount:  len(logs),
	}

	payload := &utils.ExportPayload{
		Users:    users,
		UserLogs: logs,
	}

	encrypted, err := utils.ExportEncrypt(salt1, salt2, header, payload)
	if err != nil {
		utils.LogError("DATA-EXPORT", "DownloadExport", err, "Encryption failed")
		utils.RespondError(c, http.StatusInternalServerError, "ENCRYPTION_FAILED")
		return
	}

	filename := fmt.Sprintf("nebula-backup-%s.enc", time.Now().In(utils.ShanghaiLocation()).Format("2006-01-02T15-04-05"))

	if err := h.logRepo.LogDataExport(ctx, operatorUID, len(users), len(logs)); err != nil {
		utils.LogWarn("DATA-EXPORT", "Failed to log export", err.Error())
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/octet-stream", encrypted)
}

// PreviewImport 上传文件并返回预览信息
// POST /admin/api/data/import/preview
func (h *AdminHandler) PreviewImport(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "FILE_REQUIRED")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		utils.RespondError(c, http.StatusBadRequest, "FILE_READ_ERROR")
		return
	}

	exportHeader, err := utils.ExportDecryptHeader(data)
	if err != nil {
		utils.LogWarn("DATA-IMPORT", fmt.Sprintf("PreviewImport: %v", err))
		utils.RespondError(c, http.StatusBadRequest, "INVALID_FILE_FORMAT")
		return
	}

	fileToken := h.exportService.StoreFile(data, header.Filename)

	utils.RespondSuccess(c, gin.H{
		"fileToken":  fileToken,
		"usersCount": exportHeader.UsersCount,
		"logsCount":  exportHeader.LogsCount,
		"exportedAt": exportHeader.ExportedAt,
		"exportedBy": exportHeader.ExportedBy,
	})
}

// ExecuteImport 确认导入
// POST /admin/api/data/import/execute
func (h *AdminHandler) ExecuteImport(c *gin.Context) {
	operatorUID := c.GetString("uid")

	var req importExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	salt1, err := utils.ParseExportSalt1(h.dataExportSalt)
	if err != nil {
		utils.LogError("DATA-IMPORT", "ExecuteImport", err)
		utils.RespondError(c, http.StatusInternalServerError, "EXPORT_SALT_NOT_CONFIGURED")
		return
	}

	data, _, err := h.exportService.RetrieveFile(req.FileToken)
	if err != nil {
		utils.RespondError(c, http.StatusNotFound, "FILE_TOKEN_NOT_FOUND")
		return
	}

	payload, err := utils.ExportDecrypt(salt1, data)
	if err != nil {
		utils.LogWarn("DATA-IMPORT", fmt.Sprintf("ExecuteImport: %v", err))
		utils.RespondError(c, http.StatusBadRequest, "DECRYPTION_FAILED")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), adminTimeout*3)
	defer cancel()

	if req.Strategy == "overwrite" {
		if err := h.deleteAllUserLogs(ctx); err != nil {
			utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to clear user logs for overwrite")
			utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
			return
		}
		if err := h.deleteAllUsers(ctx); err != nil {
			utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to clear users for overwrite")
			utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
			return
		}
	}

	usersImported, err := h.importUsers(ctx, payload.Users)
	if err != nil {
		utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to import users")
		utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
		return
	}

	logsImported, err := h.importUserLogs(ctx, payload.UserLogs)
	if err != nil {
		utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to import user logs")
		utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
		return
	}

	if err := h.logRepo.LogDataImport(ctx, operatorUID, usersImported, logsImported); err != nil {
		utils.LogWarn("DATA-IMPORT", "Failed to log import", err.Error())
	}

	utils.RespondSuccess(c, gin.H{
		"usersImported": usersImported,
		"logsImported":  logsImported,
	})
}

// RevokeOTAC 主动撤销当前 OTAC
// DELETE /admin/api/data/otac
func (h *AdminHandler) RevokeOTAC(c *gin.Context) {
	h.exportService.RevokeOTAC()
	utils.RespondSuccess(c, gin.H{"message": "OTAC revoked"})
}

// ====================  私有辅助方法 ====================

// queryAllUsers 查询所有用户
func (h *AdminHandler) queryAllUsers(ctx context.Context) ([]map[string]any, error) {
	p := h.pool
	if p == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := p.Query(ctx, `
		SELECT uid, username, email, password, avatar_url, microsoft_id,
		       microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
		       is_banned, ban_reason, banned_at, banned_by, unban_at, role,
		       created_at, updated_at
		FROM users
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]map[string]any, 0)
	for rows.Next() {
		var (
			uid, username, email, password, avatarURL                           string
			microsoftID, microsoftName, microsoftAvatarURL, microsoftAvatarHash *string
			isBanned                                                            bool
			banReason, bannedBy                                                 *string
			bannedAt, unbanAt                                                   *time.Time
			role                                                                int
			createdAt, updatedAt                                                time.Time
		)

		if err := rows.Scan(
			&uid, &username, &email, &password, &avatarURL,
			&microsoftID, &microsoftName, &microsoftAvatarURL, &microsoftAvatarHash,
			&isBanned, &banReason, &bannedAt, &bannedBy, &unbanAt, &role,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		user := map[string]any{
			"uid":        uid,
			"username":   username,
			"email":      email,
			"password":   password,
			"avatar_url": avatarURL,
			"is_banned":  isBanned,
			"role":       role,
			"created_at": createdAt.Format(time.RFC3339),
			"updated_at": updatedAt.Format(time.RFC3339),
		}

		setNullableString(user, "microsoft_id", microsoftID)
		setNullableString(user, "microsoft_name", microsoftName)
		setNullableString(user, "microsoft_avatar_url", microsoftAvatarURL)
		setNullableString(user, "microsoft_avatar_hash", microsoftAvatarHash)
		setNullableString(user, "ban_reason", banReason)
		setNullableString(user, "banned_by", bannedBy)
		setNullableTime(user, "banned_at", bannedAt)
		setNullableTime(user, "unban_at", unbanAt)

		users = append(users, user)
	}

	return users, rows.Err()
}

// queryAllUserLogs 查询所有用户操作日志
func (h *AdminHandler) queryAllUserLogs(ctx context.Context) ([]map[string]any, error) {
	p := h.pool
	if p == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	rows, err := p.Query(ctx, `
		SELECT id, user_uid, action, details, created_at
		FROM user_logs
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id        int64
			userUID   string
			action    string
			details   []byte
			createdAt time.Time
		)

		if err := rows.Scan(&id, &userUID, &action, &details, &createdAt); err != nil {
			return nil, err
		}

		log := map[string]any{
			"id":         id,
			"user_uid":   userUID,
			"action":     action,
			"created_at": createdAt.Format(time.RFC3339),
		}

		if len(details) > 0 {
			log["details"] = string(details)
		}

		logs = append(logs, log)
	}

	return logs, rows.Err()
}

// importUsers 批量导入用户
func (h *AdminHandler) importUsers(ctx context.Context, users []map[string]any) (int, error) {
	p := h.pool
	if p == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	imported := 0
	for _, user := range users {
		uid, _ := user["uid"].(string)
		if uid == "" {
			continue
		}

		role, _ := toInt(user["role"])

		_, err := p.Exec(ctx, `
			INSERT INTO users (uid, username, email, password, avatar_url,
			                   microsoft_id, microsoft_name, microsoft_avatar_url, microsoft_avatar_hash,
			                   is_banned, ban_reason, banned_at, banned_by, unban_at, role, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			ON CONFLICT (uid) DO UPDATE SET
				username = EXCLUDED.username,
				email = EXCLUDED.email,
				password = EXCLUDED.password,
				avatar_url = EXCLUDED.avatar_url,
				microsoft_id = EXCLUDED.microsoft_id,
				microsoft_name = EXCLUDED.microsoft_name,
				microsoft_avatar_url = EXCLUDED.microsoft_avatar_url,
				microsoft_avatar_hash = EXCLUDED.microsoft_avatar_hash,
				is_banned = EXCLUDED.is_banned,
				ban_reason = EXCLUDED.ban_reason,
				banned_at = EXCLUDED.banned_at,
				banned_by = EXCLUDED.banned_by,
				unban_at = EXCLUDED.unban_at,
				role = EXCLUDED.role,
				updated_at = EXCLUDED.updated_at
		`,
			uid,
			toString(user["username"]),
			toString(user["email"]),
			toString(user["password"]),
			toString(user["avatar_url"]),
			toNullableString(user["microsoft_id"]),
			toNullableString(user["microsoft_name"]),
			toNullableString(user["microsoft_avatar_url"]),
			toNullableString(user["microsoft_avatar_hash"]),
			toBool(user["is_banned"]),
			toNullableString(user["ban_reason"]),
			toNullableTime(user["banned_at"]),
			toNullableString(user["banned_by"]),
			toNullableTime(user["unban_at"]),
			role,
			toTime(user["created_at"]),
			toTime(user["updated_at"]),
		)
		if err != nil {
			utils.LogWarn("DATA-IMPORT", fmt.Sprintf("Failed to import user %s: %v", uid, err))
			continue
		}
		imported++
	}

	return imported, nil
}

// importUserLogs 批量导入用户操作日志
func (h *AdminHandler) importUserLogs(ctx context.Context, logs []map[string]any) (int, error) {
	p := h.pool
	if p == nil {
		return 0, fmt.Errorf("database not initialized")
	}

	imported := 0
	for _, log := range logs {
		id, _ := toInt(log["id"])
		if id == 0 {
			continue
		}

		userUID, _ := log["user_uid"].(string)
		action, _ := log["action"].(string)
		details, _ := log["details"].(string)
		createdAt := toTime(log["created_at"])

		var detailsBytes []byte
		if details != "" {
			detailsBytes = []byte(details)
		}

		_, err := p.Exec(ctx, `
			INSERT INTO user_logs (id, user_uid, action, details, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO NOTHING
		`, id, userUID, action, detailsBytes, createdAt)
		if err != nil {
			utils.LogWarn("DATA-IMPORT", fmt.Sprintf("Failed to import user log %d: %v", id, err))
			continue
		}
		imported++
	}

	return imported, nil
}

// ====================  类型转换辅助函数 ====================

func (h *AdminHandler) deleteAllUsers(ctx context.Context) error {
	p := h.pool
	if p == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := p.Exec(ctx, `DELETE FROM users`)
	return err
}

func (h *AdminHandler) deleteAllUserLogs(ctx context.Context) error {
	p := h.pool
	if p == nil {
		return fmt.Errorf("database not initialized")
	}

	_, err := p.Exec(ctx, `DELETE FROM user_logs`)
	return err
}

func setNullableString(m map[string]any, key string, v *string) {
	if v != nil {
		m[key] = *v
	} else {
		m[key] = nil
	}
}

func setNullableTime(m map[string]any, key string, t *time.Time) {
	if t != nil {
		m[key] = t.Format(time.RFC3339)
	} else {
		m[key] = nil
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func toInt(v any) (int, error) {
	switch val := v.(type) {
	case float64:
		return int(val), nil
	case int:
		return val, nil
	case int64:
		return int(val), nil
	default:
		return 0, fmt.Errorf("cannot convert to int: %T", v)
	}
}

func toNullableString(v any) *string {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}

func toNullableTime(v any) *time.Time {
	if v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func toTime(v any) time.Time {
	s, _ := v.(string)
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now()
	}
	return t
}
