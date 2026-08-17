package utils

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// LogError 记录错误日志并返回包装后的错误。
// keysAndValues 为结构化字段（key/value 交替序列），如 "uid", uid。
func LogError(module, operation string, err error, keysAndValues ...any) error {
	if err == nil {
		return nil
	}

	msg := fmt.Sprintf("%s failed", operation)
	fields := append([]any{"error", err.Error()}, keysAndValues...)
	GetLogger().Error(module, msg, fields...)

	return fmt.Errorf("%s failed: %w", operation, err)
}

// LogWarn 记录警告日志。
// keysAndValues 为结构化字段（key/value 交替序列）。
func LogWarn(module, message string, keysAndValues ...any) {
	GetLogger().Warn(module, message, keysAndValues...)
}

// LogInfo 记录信息日志。
// keysAndValues 为结构化字段（key/value 交替序列）。
func LogInfo(module, message string, keysAndValues ...any) {
	GetLogger().Info(module, message, keysAndValues...)
}

// LogDebug 记录调试日志。
// keysAndValues 为结构化字段（key/value 交替序列）。
func LogDebug(module, message string, keysAndValues ...any) {
	GetLogger().Debug(module, message, keysAndValues...)
}

// DatabaseError 数据库错误包装
type DatabaseError struct {
	Operation string
	Err       error
	NotFound  bool
}

// Error 实现 error 接口
func (e *DatabaseError) Error() string {
	if e.NotFound {
		return fmt.Sprintf("%s: not found", e.Operation)
	}
	return fmt.Sprintf("%s failed: %v", e.Operation, e.Err)
}

// Unwrap 实现 errors.Unwrap 接口
func (e *DatabaseError) Unwrap() error {
	return e.Err
}

// HandleDatabaseError 处理数据库错误
// 自动识别"未找到"错误，并记录日志
func HandleDatabaseError(module, operation string, err error, identifier any) error {
	if err == nil {
		return nil
	}

	isNotFound := errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set"

	if isNotFound {
		LogDebug(module, operation+" not found", "identifier", identifier)
		return &DatabaseError{
			Operation: operation,
			Err:       err,
			NotFound:  true,
		}
	}

	LogError(module, operation, err, "identifier", identifier)

	return &DatabaseError{
		Operation: operation,
		Err:       err,
		NotFound:  false,
	}
}

// IsDatabaseNotFound 检查是否为"未找到"错误
func IsDatabaseNotFound(err error) bool {
	var dbErr *DatabaseError
	if errors.As(err, &dbErr) {
		return dbErr.NotFound
	}
	return false
}

// TruncateIdentifier 截断标识符用于日志显示，防止敏感 token/码明文写入日志
// 保留前 8 个字符用于定位，其余用 *** 替代
func TruncateIdentifier(id string) string {
	if id == "" {
		return "(empty)"
	}
	if len(id) <= 8 {
		return id
	}
	return id[:8] + "***"
}

// HTTPErrorResponse HTTP 错误响应辅助函数
// 自动记录日志并返回 JSON 错误响应
func HTTPErrorResponse(c *gin.Context, module string, statusCode int, errorCode string, logMessage ...string) {
	msg := errorCode
	if len(logMessage) > 0 && logMessage[0] != "" {
		msg = logMessage[0]
	}

	switch {
	case statusCode >= 500:
		LogError(module, "HTTP", errors.New(msg))
	case statusCode >= 400:
		LogWarn(module, msg)
	default:
		LogInfo(module, msg)
	}

	RespondError(c, statusCode, errorCode)
}

// HTTPDatabaseError 处理数据库错误并返回 HTTP 响应
// 自动区分"未找到"和其他数据库错误
func HTTPDatabaseError(c *gin.Context, module string, err error, notFoundCode ...string) {
	if err == nil {
		return
	}

	if IsDatabaseNotFound(err) {
		code := "NOT_FOUND"
		if len(notFoundCode) > 0 && notFoundCode[0] != "" {
			code = notFoundCode[0]
		}
		HTTPErrorResponse(c, module, http.StatusNotFound, code)
		return
	}

	HTTPErrorResponse(c, module, http.StatusInternalServerError, "DATABASE_ERROR", err.Error())
}
