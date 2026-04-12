package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/limiter"
)

type MetricsRecorder interface {
	RecordError(surface, providerName, result, errorClass string)
}

// Middleware composes auth and guard middleware behind the legacy API.
type Middleware struct {
	auth  *AuthMiddleware
	guard *GuardMiddleware
}

func New(store repository.Store, limiterSvc *limiter.Limiter, metrics MetricsRecorder) *Middleware {
	authMW := NewAuthMiddleware(store, metrics)
	return &Middleware{
		auth:  authMW,
		guard: NewGuardMiddleware(authMW.Service(), limiterSvc, metrics),
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
