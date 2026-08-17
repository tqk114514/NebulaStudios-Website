package utils

// Logger 日志抽象接口。
// Debug/Info/Warn/Error 的 keysAndValues 采用 zap 风格的 key/value 交替序列，
// 例如：Info("CACHE", "user loaded", "uid", uid, "duration", d)。
// 兼容性：若传入奇数个参数（末尾是裸字符串），末尾裸字符串会被并入 message，
// 保证旧式调用（context 直接拼字符串）不丢信息。
type Logger interface {
	Debug(category, message string, keysAndValues ...any)
	Info(category, message string, keysAndValues ...any)
	Warn(category, message string, keysAndValues ...any)
	Error(category, message string, keysAndValues ...any)
	Printf(format string, args ...any)
	Fatalf(format string, args ...any)
	Sync()
}
