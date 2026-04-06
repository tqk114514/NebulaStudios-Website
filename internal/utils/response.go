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
	"errors"
	"maps"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ====================  错误定义 ====================

var (
	ErrBodyTooLarge = errors.New("request body too large")
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
	maps.Copy(response, data)
	c.JSON(http.StatusOK, response)
}

// RespondSuccessWithData 返回成功响应（data 字段格式）
//
// 参数：
//   - c: Gin 上下文
//   - data: 响应数据（会封装在 data 字段中）
func RespondSuccessWithData(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}

// BindJSON 绑定 JSON 请求体，自动识别 body-too-large 并返回 413
// 调用方模式：
//
//	if err := utils.BindJSON(c, &req); err != nil {
//	    if errors.Is(err, utils.ErrBodyTooLarge) { return }  // 413 已自动响应
//	    utils.HTTPErrorResponse(c, ..., http.StatusBadRequest, ...)
//	    return
//	}
func BindJSON(c *gin.Context, obj interface{}) error {
	err := c.ShouldBindJSON(obj)
	if err != nil && strings.Contains(err.Error(), "request body too large") {
		RespondError(c, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE")
		return ErrBodyTooLarge
	}
	return err
}

// IsBodyTooLarge 判断错误是否为请求体过大
func IsBodyTooLarge(err error) bool {
	return errors.Is(err, ErrBodyTooLarge)
}
