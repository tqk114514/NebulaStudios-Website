/**
 * internal/utils/bind.go
 * 请求体绑定工具模块
 *
 * 功能：
 * - 统一的 JSON 请求体绑定（封装 ShouldBindJSON）
 * - 自动识别 body-too-large 错误并返回 413
 * - 提供哨兵错误供调用方精确判断
 *
 * 依赖：
 * - net/http: HTTP 状态码
 * - github.com/gin-gonic/gin: Gin 框架
 */

package utils

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ====================  哨兵错误 ====================

var (
	// ErrBodyTooLarge 请求体超过大小限制
	// 由 http.MaxBytesReader 在读取超限时触发，BindJSON 自动检测并返回此错误
	ErrBodyTooLarge = errors.New("request body too large")
)

// ====================  绑定函数 ====================

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
