package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"horseui/back-machine/internal/model"
)

// Claims 面板登录令牌载荷。
type Claims struct {
	Username string `json:"u"`
	// TokenVersion 与 Admin.TokenVersion 对应，改密/登出后旧令牌立即失效。
	TokenVersion int `json:"tv"`
	jwt.RegisteredClaims
}

func (a *App) jwtSecret() []byte {
	return []byte(a.settings.Get(model.SettingJWTSecret))
}

// signToken 签发登录令牌。
func (a *App) signToken(username string, tokenVersion int, ttl time.Duration) (string, error) {
	claims := Claims{
		Username:     username,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   username,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "horseui",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.jwtSecret())
}

// parseToken 校验并解析登录令牌。
func (a *App) parseToken(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", t.Header["alg"])
		}
		return a.jwtSecret(), nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, fmt.Errorf("令牌无效")
	}
	return claims, nil
}

// bearerToken 从请求头提取 Bearer 令牌。
func bearerToken(c bearerGetter) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(h)
}

type bearerGetter interface {
	GetHeader(key string) string
}
