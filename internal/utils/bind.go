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

// BindJSONOrError 绑定 JSON，失败时自动响应（body 过大返回 413，其他返回 400）并返回 false。
// 统一 BindJSON 错误处理样板：
//
//	if !utils.BindJSONOrError(c, "MODULE", &req, "INVALID_REQUEST") {
//	    return
//	}
func BindJSONOrError(c *gin.Context, module string, obj interface{}, errorCode string) bool {
	if err := BindJSON(c, obj); err != nil {
		if errors.Is(err, ErrBodyTooLarge) {
			return false // 413 已由 BindJSON 自动响应
		}
		HTTPErrorResponse(c, module, http.StatusBadRequest, errorCode, err.Error())
		return false
	}
	return true
}
