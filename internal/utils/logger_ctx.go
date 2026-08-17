package utils

import (
	"context"
	"fmt"
)

// LogErrorCtx 记录错误日志并返回包装后的错误，自动携带 ctx 中的 request_id。
// 用于请求处理链路内：ctx 传 c.Request.Context()（由 RequestID 中间件注入 request_id）。
func LogErrorCtx(ctx context.Context, module, operation string, err error, keysAndValues ...any) error {
	if err == nil {
		return nil
	}

	msg := fmt.Sprintf("%s failed", operation)
	fields := append([]any{"error", err.Error()}, keysAndValues...)
	LoggerFromContext(ctx).Error(module, msg, fields...)

	return fmt.Errorf("%s failed: %w", operation, err)
}

// LogWarnCtx 记录警告日志，自动携带 ctx 中的 request_id。
func LogWarnCtx(ctx context.Context, module, message string, keysAndValues ...any) {
	LoggerFromContext(ctx).Warn(module, message, keysAndValues...)
}

// LogInfoCtx 记录信息日志，自动携带 ctx 中的 request_id。
func LogInfoCtx(ctx context.Context, module, message string, keysAndValues ...any) {
	LoggerFromContext(ctx).Info(module, message, keysAndValues...)
}

// LogDebugCtx 记录调试日志，自动携带 ctx 中的 request_id。
func LogDebugCtx(ctx context.Context, module, message string, keysAndValues ...any) {
	LoggerFromContext(ctx).Debug(module, message, keysAndValues...)
}
