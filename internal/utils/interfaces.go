package utils

type Logger interface {
	Debug(category, message string)
	Info(category, message string)
	Warn(category, message string)
	Error(category, message string)
	Printf(format string, args ...any)
	Fatalf(format string, args ...any)
	Sync()
}