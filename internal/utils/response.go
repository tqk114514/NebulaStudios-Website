/**
 * internal/utils/response.go
 * 统一 HTTP 响应工具模块
 *
 * 功能：
 * - 统一的错误响应格式
 * - 统一的成功响应格式
 * - 简化 Gin Context 的响应操作
 *
 * 依赖：
 * - net/http: HTTP 状态码
 * - github.com/gin-gonic/gin: Gin 框架
 */

package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ====================  响应辅助函数 ====================

// RespondError 返回错误响应
//
// 参数：
//   - c: Gin 上下文
//   - status: HTTP 状态码
//   - errorCode: 错误代码
func RespondError(c *gin.Context, status int, errorCode string) {
	c.JSON(status, gin.H{
		"success":   false,
		"errorCode": errorCode,
	})
}

// RespondSuccess 返回成功响应（gin.H 格式，键值对展开）
//
// 参数：
//   - c: Gin 上下文
//   - data: 响应数据（gin.H 类型，键值对会展开到响应中）
func RespondSuccess(c *gin.Context, data gin.H) {
	response := gin.H{"success": true}
	for k, v := range data {
		response[k] = v
	}
	c.JSON(http.StatusOK, response)
}

// RespondSuccessWithData 返回成功响应（data 字段格式）
//
// 参数：
//   - c: Gin 上下文
//   - data: 响应数据（会封装在 data 字段中）
func RespondSuccessWithData(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}
