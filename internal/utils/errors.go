/**
 * internal/utils/errors.go
 * 统一错误处理和日志记录工具
 *
 * 功能：
 * - 统一的错误日志记录
 * - 数据库错误处理
 * - HTTP 错误响应
 * - 错误包装和转换
 *
 * 设计原则：
 * - DRY (Don't Repeat Yourself)
 * - 统一的错误处理模式
 * - 减少样板代码
 */

package utils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ====================  错误类型定义 ====================

// ErrorLevel 错误级别
type ErrorLevel string

const (
	// ErrorLevelDebug 调试级别
	ErrorLevelDebug ErrorLevel = "DEBUG"
	// ErrorLevelInfo 信息级别
	ErrorLevelInfo ErrorLevel = "INFO"
	// ErrorLevelWarn 警告级别
	ErrorLevelWarn ErrorLevel = "WARN"
	// ErrorLevelError 错误级别
	ErrorLevelError ErrorLevel = "ERROR"
	// ErrorLevelFatal 致命错误级别
	ErrorLevelFatal ErrorLevel = "FATAL"
)

// ====================  日志记录辅助函数 ====================

// LogError 记录错误日志并返回包装后的错误
// 参数：
//   - module: 模块名称（如 "AUTH", "USER", "DATABASE"）
//   - operation: 操作名称（如 "FindByID", "CreateUser"）
//   - err: 原始错误
//   - context: 额外的上下文信息（可选）
//
// 返回：
//   - error: 包装后的错误
func LogError(module, operation string, err error, context ...interface{}) error {
	if err == nil {
		return nil
	}

	// 构建日志消息
	msg := fmt.Sprintf("[%s] ERROR: %s failed", module, operation)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context...)
	}
	msg += fmt.Sprintf(", error=%v", err)

	LogPrintf(msg)

	// 包装错误
	return fmt.Errorf("%s failed: %w", operation, err)
}

// LogWarn 记录警告日志
// 参数：
//   - module: 模块名称
//   - message: 警告消息
//   - context: 额外的上下文信息（可选）
func LogWarn(module, message string, context ...interface{}) {
	msg := fmt.Sprintf("[%s] WARN: %s", module, message)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context...)
	}
	LogPrintf(msg)
}

// LogInfo 记录信息日志
// 参数：
//   - module: 模块名称
//   - message: 信息消息
//   - context: 额外的上下文信息（可选）
func LogInfo(module, message string, context ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", module, message)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context...)
	}
	LogPrintf(msg)
}

// LogDebug 记录调试日志
// 参数：
//   - module: 模块名称
//   - message: 调试消息
//   - context: 额外的上下文信息（可选）
func LogDebug(module, message string, context ...interface{}) {
	msg := fmt.Sprintf("[%s] DEBUG: %s", module, message)
	if len(context) > 0 {
		msg += fmt.Sprintf(": %v", context...)
	}
	LogPrintf(msg)
}

// ====================  数据库错误处理 ====================

// DatabaseError 数据库错误包装
type DatabaseError struct {
	Operation string // 操作名称
	Err       error  // 原始错误
	NotFound  bool   // 是否为"未找到"错误
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
//
// 参数：
//   - module: 模块名称
//   - operation: 操作名称
//   - err: 数据库错误
//   - identifier: 查询标识符（用于日志）
//
// 返回：
//   - error: 包装后的错误（DatabaseError 类型）
func HandleDatabaseError(module, operation string, err error, identifier interface{}) error {
	if err == nil {
		return nil
	}

	// 检查是否为"未找到"错误
	isNotFound := errors.Is(err, sql.ErrNoRows) || err.Error() == "no rows in result set"

	if isNotFound {
		// 未找到不记录 ERROR 日志，只记录 DEBUG
		LogDebug(module, fmt.Sprintf("%s not found: identifier=%v", operation, identifier))
		return &DatabaseError{
			Operation: operation,
			Err:       err,
			NotFound:  true,
		}
	}

	// 真正的数据库错误，记录 ERROR 日志
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

// ====================  HTTP 错误响应 ====================

// HTTPErrorResponse HTTP 错误响应辅助函数
// 自动记录日志并返回 JSON 错误响应
//
// 参数：
//   - c: Gin 上下文
//   - module: 模块名称
//   - statusCode: HTTP 状态码
//   - errorCode: 错误码（返回给客户端）
//   - logMessage: 日志消息（可选，不提供则使用 errorCode）
func HTTPErrorResponse(c *gin.Context, module string, statusCode int, errorCode string, logMessage ...string) {
	// 记录日志
	msg := errorCode
	if len(logMessage) > 0 && logMessage[0] != "" {
		msg = logMessage[0]
	}

	// 根据状态码选择日志级别
	switch {
	case statusCode >= 500:
		LogError(module, "HTTP", fmt.Errorf(msg))
	case statusCode >= 400:
		LogWarn(module, msg)
	default:
		LogInfo(module, msg)
	}

	// 返回 JSON 响应
	RespondError(c, statusCode, errorCode)
}

// HTTPDatabaseError 处理数据库错误并返回 HTTP 响应
// 自动区分"未找到"和其他数据库错误
//
// 参数：
//   - c: Gin 上下文
//   - module: 模块名称
//   - err: 数据库错误
//   - notFoundCode: "未找到"时的错误码（默认 "NOT_FOUND"）
func HTTPDatabaseError(c *gin.Context, module string, err error, notFoundCode ...string) {
	if err == nil {
		return
	}

	// 检查是否为"未找到"错误
	if IsDatabaseNotFound(err) {
		code := "NOT_FOUND"
		if len(notFoundCode) > 0 && notFoundCode[0] != "" {
			code = notFoundCode[0]
		}
		HTTPErrorResponse(c, module, http.StatusNotFound, code)
		return
	}

	// 其他数据库错误
	HTTPErrorResponse(c, module, http.StatusInternalServerError, "DATABASE_ERROR", err.Error())
}

// ====================  操作结果包装 ====================

// OperationResult 操作结果
// 用于统一处理操作的成功/失败
type OperationResult struct {
	Success bool
	Error   error
	Data    interface{}
}

// NewSuccess 创建成功结果
func NewSuccess(data interface{}) *OperationResult {
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
// 参数：
//   - module: 模块名称
//   - operation: 操作名称
//   - context: 上下文信息
func (r *OperationResult) LogAndReturn(module, operation string, context ...interface{}) *OperationResult {
	if r.Success {
		LogInfo(module, fmt.Sprintf("%s succeeded", operation), context...)
	} else {
		LogError(module, operation, r.Error, context...)
	}
	return r
}

// ====================  错误检查辅助函数 ====================

// CheckError 检查错误并记录日志
// 如果错误不为 nil，记录日志并返回 true
//
// 参数：
//   - module: 模块名称
//   - operation: 操作名称
//   - err: 错误
//   - context: 上下文信息
//
// 返回：
//   - bool: 是否有错误
func CheckError(module, operation string, err error, context ...interface{}) bool {
	if err != nil {
		LogError(module, operation, err, context...)
		return true
	}
	return false
}

// MustNotError 断言错误为 nil
// 如果错误不为 nil，记录 FATAL 日志并 panic
//
// 参数：
//   - module: 模块名称
//   - operation: 操作名称
//   - err: 错误
func MustNotError(module, operation string, err error) {
	if err != nil {
		msg := fmt.Sprintf("[%s] FATAL: %s failed: %v", module, operation, err)
		LogFatalf(msg)
	}
}

// ====================  上下文错误处理 ====================

// WithContext 为错误添加上下文信息
// 参数：
//   - err: 原始错误
//   - context: 上下文信息
//
// 返回：
//   - error: 包装后的错误
func WithContext(err error, context string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}

// WithContextf 为错误添加格式化的上下文信息
// 参数：
//   - err: 原始错误
//   - format: 格式化字符串
//   - args: 格式化参数
//
// 返回：
//   - error: 包装后的错误
func WithContextf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	context := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s: %w", context, err)
}

// ====================  批量错误处理 ====================

// ErrorCollector 错误收集器
// 用于收集多个操作的错误
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

	// 合并多个错误
	msg := fmt.Sprintf("multiple errors occurred (%d):", len(ec.errors))
	for i, err := range ec.errors {
		msg += fmt.Sprintf("\n  %d. %v", i+1, err)
	}

	return errors.New(msg)
}

// ====================  重试辅助函数 ====================

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int           // 最大尝试次数
	OnRetry     func(attempt int, err error) // 重试回调
}

// Retry 重试执行函数
// 参数：
//   - ctx: 上下文
//   - config: 重试配置
//   - fn: 要执行的函数
//
// 返回：
//   - error: 最后一次的错误
func Retry(ctx context.Context, config RetryConfig, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// 执行函数
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// 如果不是最后一次尝试，调用回调
		if attempt < config.MaxAttempts && config.OnRetry != nil {
			config.OnRetry(attempt, err)
		}
	}

	return lastErr
}
