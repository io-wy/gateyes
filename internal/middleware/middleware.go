package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/limiter"
)

// Middleware composes auth and guard middleware behind the legacy API.
type Middleware struct {
	auth  *AuthMiddleware
	guard *GuardMiddleware
}

func New(store repository.Store, limiterSvc *limiter.Limiter) *Middleware {
	authMW := NewAuthMiddleware(store)
	return &Middleware{
		auth:  authMW,
		guard: NewGuardMiddleware(authMW.Service(), limiterSvc),
	}
}

func (m *Middleware) AuthService() *auth.Auth {
	return m.auth.Service()
}

func (m *Middleware) Auth() gin.HandlerFunc {
	return m.auth.Auth()
}

func (m *Middleware) RequireRoles(roles ...string) gin.HandlerFunc {
	return m.auth.RequireRoles(roles...)
}

func (m *Middleware) GuardLLMRequest() gin.HandlerFunc {
	return m.guard.GuardLLMRequest()
}
