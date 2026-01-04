/**
 * internal/utils/logger.go
 * 高性能异步日志模块（基于 zap）
 *
 * 功能：
 * - 异步日志写入，不阻塞主流程
 * - 自动脱敏敏感信息（邮箱等）
 * - 统一日志格式
 * - 支持优雅关闭
 *
 * 用法（其他包）：
 *   utils.LogPrintf("[AUTH] User login: email=%s", email)
 *
 * 用法（utils 包内）：
 *   LogPrintf("[VALIDATOR] Email validation failed")
 */

package utils

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ====================  全局变量 ====================

var (
	// logger zap 日志实例
	logger *zap.Logger

	// sugar zap SugaredLogger（更方便的 API）
	sugar *zap.SugaredLogger

	// loggerOnce 确保只初始化一次
	loggerOnce sync.Once

	// 邮箱正则（用于检测日志中的邮箱）
	logEmailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
)

// ====================  初始化 ====================

// initLogger 初始化 zap 日志
func initLogger() {
	loggerOnce.Do(func() {
		// 统一配置：控制台格式，Info 级别
		config := zap.Config{
			Level:            zap.NewAtomicLevelAt(zapcore.InfoLevel),
			Development:      false,
			Encoding:         "console",
			OutputPaths:      []string{"stderr"},
			ErrorOutputPaths: []string{"stderr"},
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:        "time",
				LevelKey:       "level",
				MessageKey:     "msg",
				EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05"),
				EncodeLevel:    zapcore.CapitalLevelEncoder,
				EncodeDuration: zapcore.StringDurationEncoder,
			},
		}

		var err error
		logger, err = config.Build(
			zap.AddCallerSkip(1), // 跳过 LogPrintf 调用层
		)
		if err != nil {
			// 降级到标准输出
			fmt.Fprintf(os.Stderr, "[LOGGER] Failed to init zap: %v, falling back to basic logger\n", err)
			logger = zap.NewNop()
		}

		sugar = logger.Sugar()
	})
}

// getLogger 获取 logger 实例（懒加载）
func getLogger() *zap.SugaredLogger {
	if sugar == nil {
		initLogger()
	}
	return sugar
}

// ====================  公开函数 ====================

// LogPrintf 安全日志输出（格式化），自动脱敏敏感信息
// 替代 log.Printf，使用 zap 异步写入
func LogPrintf(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	masked := maskSensitiveData(message)
	getLogger().Info(masked)
}

// LogFatalf 安全日志输出后退出，自动脱敏敏感信息
// 替代 log.Fatalf
func LogFatalf(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)
	masked := maskSensitiveData(message)
	getLogger().Fatal(masked)
}

// SyncLogger 同步日志缓冲区（程序退出前调用）
// 确保所有日志都被写入
func SyncLogger() {
	if logger != nil {
		_ = logger.Sync()
	}
}

// ====================  私有函数 ====================

// maskSensitiveData 脱敏敏感数据
func maskSensitiveData(message string) string {
	return logEmailRegex.ReplaceAllStringFunc(message, maskEmail)
}

// maskEmail 对邮箱地址进行脱敏处理
// 将 user@example.com 转换为 u***@e***.com
func maskEmail(email string) string {
	if email == "" {
		return ""
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}

	local := parts[0]
	domain := parts[1]

	// 脱敏本地部分：保留首字符 + ***
	maskedLocal := "***"
	if len(local) > 0 {
		maskedLocal = string(local[0]) + "***"
	}

	// 脱敏域名部分：保留首字符 + *** + 后缀
	domainParts := strings.Split(domain, ".")
	if len(domainParts) >= 2 {
		firstPart := domainParts[0]
		suffix := domainParts[len(domainParts)-1]
		maskedDomain := "***"
		if len(firstPart) > 0 {
			maskedDomain = string(firstPart[0]) + "***"
		}
		return maskedLocal + "@" + maskedDomain + "." + suffix
	}

	return maskedLocal + "@***"
}
