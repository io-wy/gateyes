package middleware

import (
	"net/http"

	"gateyes/internal/config"
	servicebudget "gateyes/internal/service/budget"
)

type RateLimiter struct {
	service *servicebudget.RateLimiter
}

func NewRateLimiter(cfg config.RateLimitConfig, auth config.AuthConfig) *RateLimiter {
	return &RateLimiter{service: servicebudget.NewRateLimiter(cfg, auth)}
}

func (r *RateLimiter) InitError() error {
	return r.service.InitError()
}

func (r *RateLimiter) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return r.service.Handle(next)
	}
}
