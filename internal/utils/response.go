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
