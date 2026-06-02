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

var (
	loggerOnce sync.Once

	logEmailRegex = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

	// IPv4 正则（用于检测日志中的 IP 地址）
	// 匹配格式：192.168.1.100
	logIPv4Regex = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)

	// IPv6 正则（用于检测日志中的 IPv6 地址）
	// 匹配完整和缩写格式：2001:0db8:85a3:0000:0000:8a2e:0370:7334 / 2001:db8:85a3::8a2e:370:7334 / ::1
	logIPv6Regex = regexp.MustCompile(`(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){1,7}:|(?:[0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|(?:[0-9a-fA-F]{1,4}:){1,5}(?::[0-9a-fA-F]{1,4}){1,2}|(?:[0-9a-fA-F]{1,4}:){1,4}(?::[0-9a-fA-F]{1,4}){1,3}|(?:[0-9a-fA-F]{1,4}:){1,3}(?::[0-9a-fA-F]{1,4}){1,4}|(?:[0-9a-fA-F]{1,4}:){1,2}(?::[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:(?::[0-9a-fA-F]{1,4}){1,6}|:(?::[0-9a-fA-F]{1,4}){1,7}|::(?:[fF]{4}:)?(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)|::`)

	// Token 正则（用于检测日志中的 JWT 或长字符串 Token）
	// 匹配格式：eyJhbGciOiJIUzI1NiIs... 或其他 32+ 字符的 base64 字符串
	// 通过 key=value 模式匹配，避免误伤普通文本
	logTokenRegex = regexp.MustCompile(`(?i)(token|bearer|authorization)[=:\s]+([a-zA-Z0-9_\-\.]{32,})`)
)

var loggerInstance Logger

// zapLogger 基于 zap 的 Logger 实现
type zapLogger struct {
	zap   *zap.Logger
	sugar *zap.SugaredLogger
}

func (l *zapLogger) Debug(category, message string) {
	masked := maskSensitiveData(message)
	if category == "" {
		l.sugar.Debug(masked)
	} else {
		l.sugar.Debugw(masked, "category", category)
	}
}

func (l *zapLogger) Info(category, message string) {
	masked := maskSensitiveData(message)
	if category == "" {
		l.sugar.Info(masked)
	} else {
		l.sugar.Infow(masked, "category", category)
	}
}

func (l *zapLogger) Warn(category, message string) {
	masked := maskSensitiveData(message)
	if category == "" {
		l.sugar.Warn(masked)
	} else {
		l.sugar.Warnw(masked, "category", category)
	}
}

func (l *zapLogger) Error(category, message string) {
	masked := maskSensitiveData(message)
	if category == "" {
		l.sugar.Error(masked)
	} else {
		l.sugar.Errorw(masked, "category", category)
	}
}

func (l *zapLogger) Printf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	masked := maskSensitiveData(message)
	l.sugar.Info(masked)
}

func (l *zapLogger) Fatalf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	masked := maskSensitiveData(message)
	l.sugar.Fatal(masked)
}

func (l *zapLogger) Sync() {
	_ = l.zap.Sync()
}

// SetLogger 注入自定义 Logger 实现（用于测试、替换日志库等）
func SetLogger(l Logger) {
	if l != nil {
		loggerInstance = l
	}
}

// GetLogger 获取当前 Logger 实例
func GetLogger() Logger {
	if loggerInstance == nil {
		initLogger()
	}
	return loggerInstance
}

func initLogger() {
	loggerOnce.Do(func() {
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

		zl, err := config.Build(
			zap.AddCallerSkip(1),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[LOGGER] Failed to init zap: %v, falling back to basic logger\n", err)
			zl = zap.NewNop()
		}

		loggerInstance = &zapLogger{
			zap:   zl,
			sugar: zl.Sugar(),
		}
	})
}

// Log 安全日志输出，自动脱敏敏感信息
func Log(message string) {
	masked := maskSensitiveData(message)
	if loggerInstance == nil {
		initLogger()
	}
	if l, ok := loggerInstance.(*zapLogger); ok {
		l.sugar.Info(masked)
	}
}

// LogPrintf 安全日志输出（格式化），自动脱敏敏感信息
func LogPrintf(format string, args ...any) {
	GetLogger().Printf(format, args...)
}

// LogFatalf 安全日志输出后退出，自动脱敏敏感信息
func LogFatalf(format string, args ...any) {
	GetLogger().Fatalf(format, args...)
}

// SyncLogger 同步日志缓冲区（程序退出前调用）
func SyncLogger() {
	GetLogger().Sync()
}

func logDebug(message string) {
	GetLogger().Debug("", message)
}

func logInfo(message string) {
	GetLogger().Info("", message)
}

func logWarn(message string) {
	GetLogger().Warn("", message)
}

func logError(message string) {
	GetLogger().Error("", message)
}

// maskSensitiveData 脱敏敏感数据
// 按顺序处理：邮箱 -> IP -> Token
// 先做字符串包含预检查，避免不必要的正则扫描
func maskSensitiveData(message string) string {
	if strings.Contains(message, "@") && strings.Contains(message, ".") {
		message = logEmailRegex.ReplaceAllStringFunc(message, maskEmail)
	}

	if len(message) > 7 && strings.Contains(message, ".") {
		message = logIPv4Regex.ReplaceAllStringFunc(message, maskIPv4)
	}

	if len(message) > 5 && strings.Contains(message, ":") {
		message = logIPv6Regex.ReplaceAllStringFunc(message, maskIPv6)
	}

	if strings.Contains(message, "oken") || strings.Contains(message, "OKEN") ||
		strings.Contains(message, "earer") || strings.Contains(message, "EARER") ||
		strings.Contains(message, "uthorization") || strings.Contains(message, "UTHORIZATION") {
		message = logTokenRegex.ReplaceAllStringFunc(message, maskToken)
	}

	return message
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

	maskedLocal := "***"
	if len(local) > 0 {
		maskedLocal = string(local[0]) + "***"
	}

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

// maskIPv4 对 IPv4 地址进行脱敏处理
// 将 192.168.1.100 转换为 192.168.***.***
// 保留前两段用于定位网段，隐藏后两段保护具体主机
func maskIPv4(ip string) string {
	if ip == "" {
		return ""
	}

	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return "***.***.***"
	}

	return parts[0] + "." + parts[1] + ".***.***"
}

// maskIPv6 对 IPv6 地址进行脱敏处理
// 将 2001:0db8:85a3:0000:8a2e:0370:7334 转换为 2001:0db8:****:****:****:****:****:****
// 保留前两段用于定位网段，隐藏其余部分保护具体主机
func maskIPv6(ip string) string {
	if ip == "" {
		return ""
	}

	if strings.Contains(ip, "::") {
		colonCount := strings.Count(ip, ":")
		missingSegments := 8 - (colonCount - 1)
		if missingSegments < 1 {
			missingSegments = 1
		}

		zeros := "0"
		for i := 1; i < missingSegments; i++ {
			zeros += ":0"
		}

		if ip == "::" {
			ip = "0:0:0:0:0:0:0:0"
		} else if strings.HasPrefix(ip, "::") {
			ip = zeros + ip[1:]
		} else if strings.HasSuffix(ip, "::") {
			ip = ip[:len(ip)-1] + zeros
		} else {
			ip = strings.Replace(ip, "::", ":"+zeros+":", 1)
		}
	}

	parts := strings.Split(ip, ":")
	if len(parts) < 3 {
		return "****:****"
	}

	result := parts[0] + ":" + parts[1]
	for i := 2; i < len(parts); i++ {
		result += ":****"
	}
	return result
}

// maskToken 对 Token 进行脱敏处理
// 将 token=eyJhbGciOiJIUzI1NiIs... 转换为 token=eyJh***[MASKED]
// 保留前 4 个字符用于识别 Token 类型，隐藏其余部分
func maskToken(match string) string {
	if match == "" {
		return ""
	}

	separatorIdx := -1
	for i, c := range match {
		if c == '=' || c == ':' || c == ' ' {
			separatorIdx = i
			break
		}
	}

	if separatorIdx == -1 {
		return match
	}

	key := match[:separatorIdx+1]
	value := strings.TrimSpace(match[separatorIdx+1:])

	if len(value) <= 8 {
		return key + "***[MASKED]"
	}

	return key + value[:4] + "***[MASKED]"
}
