package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
)

type AuthMiddleware struct {
	auth *auth.Auth
}

func NewAuthMiddleware(store repository.Store) *AuthMiddleware {
	return &AuthMiddleware{
		auth: auth.NewAuth(store),
	}
}

func (m *AuthMiddleware) Service() *auth.Auth {
	return m.auth
}

// Auth 验证 API Key
func (m *AuthMiddleware) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		key, secret := m.auth.ExtractKey(c.GetHeader("Authorization"))
		identity, err := m.auth.Authenticate(c.Request.Context(), key, secret)
		if err != nil {
			status := http.StatusUnauthorized
			message := "invalid API key"
			if err == auth.ErrInactiveAPIKey {
				status = http.StatusForbidden
				message = "inactive API key"
			}
			c.JSON(status, gin.H{"error": gin.H{"message": message, "type": "invalid_request_error"}})
			c.Abort()
			return
		}

		SetIdentity(c, identity)
		c.Next()
	}
}

// RequireRoles 角色校验
func (m *AuthMiddleware) RequireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := Identity(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}
		if !repository.HasRole(identity.Role, roles...) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}
