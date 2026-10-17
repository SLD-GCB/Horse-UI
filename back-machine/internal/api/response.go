package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ok 输出成功响应。
func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
}

// fail 输出错误响应。
func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "msg": msg})
}

// badRequest 参数错误。
func badRequest(c *gin.Context, msg string) { fail(c, http.StatusBadRequest, msg) }

// unauthorized 未授权。
func unauthorized(c *gin.Context, msg string) { fail(c, http.StatusUnauthorized, msg) }

// serverError 服务器错误。
func serverError(c *gin.Context, err error) { fail(c, http.StatusInternalServerError, err.Error()) }
