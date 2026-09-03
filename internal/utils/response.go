package utils

import (
	"maps"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RespondError 返回错误响应
func RespondError(c *gin.Context, status int, errorCode string) {
	c.JSON(status, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
}

// RespondRateLimit 返回 429 限流响应，retryAt 为限制结束时间戳（Unix 秒），供前端计算剩余等待
func RespondRateLimit(c *gin.Context, retryAt int64) {
	c.JSON(http.StatusTooManyRequests, gin.H{
		"success":   false,
		"errorCode": ErrCodeRateLimit,
		"retryAt":   retryAt,
	})
}

// RespondSuccess 返回成功响应（gin.H 格式，键值对展开）
func RespondSuccess(c *gin.Context, data gin.H) {
	response := gin.H{"success": true}
	maps.Copy(response, data)
	c.JSON(http.StatusOK, response)
}

// RespondSuccessWithData 返回成功响应（data 字段格式）
func RespondSuccessWithData(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}
