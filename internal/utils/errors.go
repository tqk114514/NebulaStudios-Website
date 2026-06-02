package utils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ErrorLevel 错误级别
type ErrorLevel string

const (
	ErrorLevelDebug ErrorLevel = "DEBUG"
	ErrorLevelInfo  ErrorLevel = "INFO"
	ErrorLevelWarn  ErrorLevel = "WARN"
	ErrorLevelError ErrorLevel = "ERROR"
	ErrorLevelFatal ErrorLevel = "FATAL"
)

// LogError 记录错误日志并返回包装后的错误
func LogError(module, operation string, err error, context ...any) error {
	if err == nil {
		return nil
	}

	msg := fmt.Sprintf("[%s] ERROR: %s failed", module, operation)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context)
	}
	msg += fmt.Sprintf(", error=%v", err)

	logError(msg)

	return fmt.Errorf("%s failed: %w", operation, err)
}

// LogWarn 记录警告日志
func LogWarn(module, message string, context ...any) {
	msg := fmt.Sprintf("[%s] WARN: %s", module, message)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context)
	}
	logWarn(msg)
}

// LogInfo 记录信息日志
func LogInfo(module, message string, context ...any) {
	msg := fmt.Sprintf("[%s] %s", module, message)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context)
	}
	logInfo(msg)
}

// LogDebug 记录调试日志
func LogDebug(module, message string, context ...any) {
	msg := fmt.Sprintf("[%s] DEBUG: %s", module, message)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context)
	}
	logDebug(msg)
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
		LogDebug(module, fmt.Sprintf("%s not found: identifier=%v", operation, identifier))
		return &DatabaseError{
			Operation: operation,
			Err:       err,
			NotFound:  true,
		}
	}

	LogError(module, operation, err, fmt.Sprintf("identifier=%v", identifier))

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

// OperationResult 操作结果
type OperationResult struct {
	Success bool
	Error   error
	Data    any
}

// NewSuccess 创建成功结果
func NewSuccess(data any) *OperationResult {
	return &OperationResult{
		Success: true,
		Data:    data,
	}
}

// NewFailure 创建失败结果
func NewFailure(err error) *OperationResult {
	return &OperationResult{
		Success: false,
		Error:   err,
	}
}

// LogAndReturn 记录日志并返回结果
func (r *OperationResult) LogAndReturn(module, operation string, context ...any) *OperationResult {
	if r.Success {
		LogInfo(module, fmt.Sprintf("%s succeeded", operation), context...)
	} else {
		LogError(module, operation, r.Error, context...)
	}
	return r
}

// CheckError 检查错误并记录日志
func CheckError(module, operation string, err error, context ...any) bool {
	if err != nil {
		LogError(module, operation, err, context...)
		return true
	}
	return false
}

// MustNotError 断言错误为 nil
func MustNotError(module, operation string, err error) {
	if err != nil {
		LogFatalf("[%s] FATAL: %s failed: %v", module, operation, err)
	}
}

// WithContext 为错误添加上下文信息
func WithContext(err error, context string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}

// WithContextf 为错误添加格式化的上下文信息
func WithContextf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	context := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s: %w", context, err)
}

// ErrorCollector 错误收集器
type ErrorCollector struct {
	errors []error
}

// NewErrorCollector 创建错误收集器
func NewErrorCollector() *ErrorCollector {
	return &ErrorCollector{
		errors: make([]error, 0),
	}
}

// Add 添加错误
func (ec *ErrorCollector) Add(err error) {
	if err != nil {
		ec.errors = append(ec.errors, err)
	}
}

// HasErrors 是否有错误
func (ec *ErrorCollector) HasErrors() bool {
	return len(ec.errors) > 0
}

// Error 返回合并后的错误
func (ec *ErrorCollector) Error() error {
	if !ec.HasErrors() {
		return nil
	}

	if len(ec.errors) == 1 {
		return ec.errors[0]
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("multiple errors occurred (%d):", len(ec.errors)))
	for i, err := range ec.errors {
		msg.WriteString(fmt.Sprintf("\n  %d. %v", i+1, err))
	}

	return errors.New(msg.String())
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int
	Backoff     time.Duration
	MaxBackoff  time.Duration
	Multiplier  float64
	OnRetry     func(attempt int, err error)
}

// Retry 重试执行函数
func Retry(ctx context.Context, config RetryConfig, fn func() error) error {
	var lastErr error
	var backoff time.Duration

	multiplier := config.Multiplier
	if multiplier == 0 {
		multiplier = 2.0
	}

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if attempt < config.MaxAttempts {
			if config.Backoff > 0 {
				if attempt == 1 {
					backoff = config.Backoff
				} else {
					backoff = time.Duration(float64(backoff) * multiplier)
					if config.MaxBackoff > 0 && backoff > config.MaxBackoff {
						backoff = config.MaxBackoff
					}
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoff):
				}
			}

			if config.OnRetry != nil {
				config.OnRetry(attempt, err)
			}
		}
	}

	return lastErr
}
