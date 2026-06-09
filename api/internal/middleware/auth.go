// Package middleware Gin 中间件集合。
package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helios-cicd/helios/api/internal/authstore"
	heliosjwt "github.com/helios-cicd/helios/api/pkg/jwt"
)

const (
	CtxClaimsKey = "helios.claims"
	CtxUserIDKey = "helios.uid"
)

// AuthDeps 中间件依赖
type AuthDeps struct {
	Issuer    *heliosjwt.Issuer
	Authstore *authstore.Store
}

// RequireAuth 解析 Authorization: Bearer xxx,校验签名 + 黑名单,失败 401。
// 成功后把 *jwt.Claims 注入 c.Set(CtxClaimsKey, ...)。
func RequireAuth(deps AuthDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if raw == "" {
			abort401(c, "missing Authorization header")
			return
		}
		parts := strings.SplitN(raw, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			abort401(c, "Authorization must be 'Bearer <token>'")
			return
		}
		claims, err := deps.Issuer.Parse(parts[1])
		if err != nil {
			switch {
			case errors.Is(err, heliosjwt.ErrTokenExpired):
				abort401(c, "token expired")
			default:
				abort401(c, "invalid token")
			}
			return
		}
		if claims.TokenType != heliosjwt.TokenTypeAccess {
			abort401(c, "wrong token type")
			return
		}
		// 黑名单检查
		bl, err := deps.Authstore.IsAccessBlacklisted(c.Request.Context(), claims.ID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error":   "auth_check_failed",
				"message": "internal error",
			})
			return
		}
		if bl {
			abort401(c, "token revoked")
			return
		}

		c.Set(CtxClaimsKey, claims)
		c.Set(CtxUserIDKey, claims.UserID)
		c.Next()
	}
}

// ClaimsFrom 从 gin.Context 取出当前 claims (路由 handler 用)。
func ClaimsFrom(c *gin.Context) (*heliosjwt.Claims, bool) {
	v, ok := c.Get(CtxClaimsKey)
	if !ok {
		return nil, false
	}
	cl, ok := v.(*heliosjwt.Claims)
	return cl, ok
}

func abort401(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   "unauthorized",
		"message": msg,
	})
}
