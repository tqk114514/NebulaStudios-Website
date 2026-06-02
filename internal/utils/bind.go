package utils

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	ErrBodyTooLarge = errors.New("request body too large")
)

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
