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

type exportRequestResponse struct {
	RequestID string `json:"requestId"`
	ExpiresIn int    `json:"expiresIn"`
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
// GET /admin/api/data/export/:requestId/download?otac=xxx
func (h *AdminHandler) DownloadExport(c *gin.Context) {
	operatorUID := c.GetString("uid")

	requestID := c.Param("requestId")
	otac := c.Query("otac")

	if requestID == "" || otac == "" {
		utils.RespondError(c, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}

	if err := h.exportService.ValidateOTAC(requestID, otac); err != nil {
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

	users, err := h.dataExportRepo.QueryAllUsers(ctx)
	if err != nil {
		utils.LogError("DATA-EXPORT", "DownloadExport", err, "Failed to query users")
		utils.RespondError(c, http.StatusInternalServerError, "QUERY_FAILED")
		return
	}

	logs, err := h.dataExportRepo.QueryAllUserLogs(ctx)
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
		if err := h.dataExportRepo.DeleteAllUserLogs(ctx); err != nil {
			utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to clear user logs for overwrite")
			utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
			return
		}
		if err := h.dataExportRepo.DeleteAllUsers(ctx); err != nil {
			utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to clear users for overwrite")
			utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
			return
		}
	}

	usersImported, err := h.dataExportRepo.ImportUsers(ctx, payload.Users)
	if err != nil {
		utils.LogError("DATA-IMPORT", "ExecuteImport", err, "Failed to import users")
		utils.RespondError(c, http.StatusInternalServerError, "IMPORT_FAILED")
		return
	}

	logsImported, err := h.dataExportRepo.ImportUserLogs(ctx, payload.UserLogs)
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
// DELETE /admin/api/data/one-time-access-code
func (h *AdminHandler) RevokeOTAC(c *gin.Context) {
	h.exportService.RevokeOTAC()
	utils.RespondSuccess(c, gin.H{"message": "OTAC revoked"})
}
