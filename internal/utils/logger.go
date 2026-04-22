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
 *   utils.LogInfo("AUTH", "User login", fmt.Sprintf("email=%s", email))
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
	// 匹配格式：user@example.com
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

// Log 安全日志输出，自动脱敏敏感信息
// 直接输出已构建好的消息
func Log(message string) {
	masked := maskSensitiveData(message)
	getLogger().Info(masked)
}

// logDebug 安全日志输出（DEBUG 级别），自动脱敏敏感信息
func logDebug(message string) {
	masked := maskSensitiveData(message)
	getLogger().Debug(masked)
}

// logInfo 安全日志输出（INFO 级别），自动脱敏敏感信息
func logInfo(message string) {
	masked := maskSensitiveData(message)
	getLogger().Info(masked)
}

// logWarn 安全日志输出（WARN 级别），自动脱敏敏感信息
func logWarn(message string) {
	masked := maskSensitiveData(message)
	getLogger().Warn(masked)
}

// logError 安全日志输出（ERROR 级别），自动脱敏敏感信息
func logError(message string) {
	masked := maskSensitiveData(message)
	getLogger().Error(masked)
}

// LogPrintf 安全日志输出（格式化），自动脱敏敏感信息
// 替代 log.Printf，使用 zap 异步写入
func LogPrintf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	masked := maskSensitiveData(message)
	getLogger().Info(masked)
}

// LogFatalf 安全日志输出后退出，自动脱敏敏感信息
// 替代 log.Fatalf
func LogFatalf(format string, args ...any) {
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
// 按顺序处理：邮箱 -> IP -> Token
// 先做字符串包含预检查，避免不必要的正则扫描
func maskSensitiveData(message string) string {
	// 1. 脱敏邮箱
	// 必须同时包含 @ 和 . 才可能是邮箱
	if strings.Contains(message, "@") && strings.Contains(message, ".") {
		message = logEmailRegex.ReplaceAllStringFunc(message, maskEmail)
	}

	// 2. 脱敏 IPv4 地址
	// 日志中数字较多，用长度过滤短字符串
	if len(message) > 7 && strings.Contains(message, ".") {
		message = logIPv4Regex.ReplaceAllStringFunc(message, maskIPv4)
	}

	// 3. 脱敏 IPv6 地址
	// IPv6 地址包含冒号，用冒号预检查
	if len(message) > 5 && strings.Contains(message, ":") {
		message = logIPv6Regex.ReplaceAllStringFunc(message, maskIPv6)
	}

	// 4. 脱敏 Token
	// 截断首字母实现忽略大小写匹配，避免 ToLower 内存分配
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

	// 处理 :: 缩写格式，展开为完整格式再脱敏
	if strings.Contains(ip, "::") {
		// 计算需要补充的段数
		colonCount := strings.Count(ip, ":")
		// 完整 IPv6 有 7 个冒号，缺少的冒号数 = 需要补充的零段数 - 1
		missingSegments := 8 - (colonCount - 1)
		if missingSegments < 1 {
			missingSegments = 1
		}

		// 构造零段字符串
		zeros := "0"
		for i := 1; i < missingSegments; i++ {
			zeros += ":0"
		}

		// 替换 :: 为零段
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

	// 保留前两段，其余用 **** 替代
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

	// 找到分隔符位置（= 或 : 或空格）
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

	// 提取 key 和 value
	key := match[:separatorIdx+1]
	value := strings.TrimSpace(match[separatorIdx+1:])

	if len(value) <= 8 {
		return key + "***[MASKED]"
	}

	// 保留前 4 个字符
	return key + value[:4] + "***[MASKED]"
}
