package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"auth-system/internal/models"
	"auth-system/internal/utils"

	"github.com/gin-gonic/gin"
)

// businessTokenErrors 可直接作为 errorCode 返回给客户端的业务语义错误集合（与前端约定一致）
var businessTokenErrors = []error{
	models.ErrInvalidToken,
	models.ErrTokenExpired,
	models.ErrTokenUsed,
	models.ErrInvalidCode,
	models.ErrCodeExpired,
	models.ErrEmailMismatch,
	models.ErrTypeMismatch,
	models.ErrCodeNotVerified,
	models.ErrCodeUsed,
}

// RespondTokenError 令牌/验证码校验错误的统一响应：
// 业务错误（哨兵错误）→ 400 + 固定 errorCode；
// 其他错误（数据库/网络等内部错误）→ 500 + INTERNAL_ERROR，详细信息仅写入日志，
// 避免内部错误文本泄露给客户端。
func RespondTokenError(c *gin.Context, module string, err error, logMessage string) {
	for _, sentinel := range businessTokenErrors {
		if errors.Is(err, sentinel) {
			utils.HTTPErrorResponse(c, module, http.StatusBadRequest, sentinel.Error(), logMessage)
			return
		}
	}
	utils.HTTPErrorResponse(c, module, http.StatusInternalServerError, "INTERNAL_ERROR", fmt.Sprintf("%s: %v", logMessage, err))
}
