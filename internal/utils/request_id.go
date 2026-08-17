package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// RequestIDKey 是 request_id 的统一字段名：
// 既作为 gin 上下文中的存储 key，也作为日志中的结构化字段名。
const RequestIDKey = "request_id"

type requestIDCtxKey struct{}

// validRequestIDRegex 限制外部传入的 request_id 字符集，
// 防止换行/控制字符等通过 X-Request-ID 头注入日志。
var validRequestIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// ValidRequestID 判断 request_id 是否可作为合法 ID 使用
// （长度 8-64，仅允许字母/数字/连字符/下划线）。
func ValidRequestID(id string) bool {
	return id != "" && validRequestIDRegex.MatchString(id)
}

// NewRequestID 生成新的 request_id（16 字节 crypto/rand，编码为 32 位十六进制）。
func NewRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}

	// crypto/rand 基本不会失败；此处兜底保证始终返回合法 ID。
	return fmt.Sprintf("rid-%d-%x", time.Now().UnixNano(), time.Now().UnixNano())
}

// WithRequestID 将 request_id 存入 context.Context。
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, id)
}

// RequestIDFrom 从 context.Context 中取出 request_id，未设置时返回空字符串。
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDCtxKey{}).(string); ok {
		return id
	}
	return ""
}
