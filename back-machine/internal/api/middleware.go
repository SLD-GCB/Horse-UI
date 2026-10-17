package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// authMiddleware 校验登录令牌与令牌版本。
func (a *App) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			unauthorized(c, "未登录")
			c.Abort()
			return
		}
		claims, err := a.parseToken(token)
		if err != nil {
			unauthorized(c, "登录已失效，请重新登录")
			c.Abort()
			return
		}
		st := a.currentAdmin()
		if claims.Username != st.Username || claims.TokenVersion != st.TokenVer {
			// 令牌版本不匹配说明已改密或已登出，旧令牌作废。
			unauthorized(c, "登录已失效，请重新登录")
			c.Abort()
			return
		}
		c.Set("username", claims.Username)

		// 强制改密门禁：仅放行改密、登出与个人资料接口。
		if st.MustChange && !passwordChangeAllowed(c) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 40301, "msg": "请先修改初始密码"})
			return
		}
		c.Next()
	}
}

// passwordChangeAllowed 判断当前路由是否在强制改密的放行名单内。
func passwordChangeAllowed(c *gin.Context) bool {
	switch c.FullPath() {
	case "/api/auth/password", "/api/auth/logout", "/api/auth/profile":
		return true
	}
	return false
}
