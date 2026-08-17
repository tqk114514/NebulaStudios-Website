package middleware

import (
	"github.com/gin-gonic/gin"

	"auth-system/internal/utils"
)

// RequestIDHeader 是请求/响应中携带 request_id 的 HTTP 头名称。
const RequestIDHeader = "X-Request-ID"

// RequestID 生成/透传 request_id 并注入请求链路：
//   - 若客户端已提供合法的 X-Request-ID（用于与上游代理、前端埋点关联），直接沿用；
//   - 否则生成 32 位十六进制随机 ID；
//   - 写入 gin 上下文与 request.Context()，供 handler/日志通过 utils.LogInfoCtx 等自动携带；
//   - 回写 X-Request-ID 响应头，便于客户端与排障时关联。
//
// 必须注册在所有其他中间件之前，保证同一次请求的全部日志行都能按 ID 归组。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(RequestIDHeader)
		if !utils.ValidRequestID(id) {
			id = utils.NewRequestID()
		}

		c.Set(utils.RequestIDKey, id)
		c.Request = c.Request.WithContext(utils.WithRequestID(c.Request.Context(), id))
		c.Header(RequestIDHeader, id)

		c.Next()
	}
}

// GetRequestID 从 gin 上下文读取 request_id（由 RequestID 中间件注入），
// 读取不到时回退到 request.Context()。返回空串表示未经过 RequestID 中间件。
func GetRequestID(c *gin.Context) string {
	if v, ok := c.Get(utils.RequestIDKey); ok {
		if id, ok := v.(string); ok && id != "" {
			return id
		}
	}
	return utils.RequestIDFrom(c.Request.Context())
}
