package middleware

import (
	"net/http"

	"gateyes/internal/config"
	servicebudget "gateyes/internal/service/budget"
)

type Quota struct {
	service *servicebudget.Quota
}

func NewQuota(cfg config.QuotaConfig, auth config.AuthConfig) *Quota {
	return &Quota{service: servicebudget.NewQuota(cfg, auth)}
}

func (q *Quota) InitError() error {
	return q.service.InitError()
}

func (q *Quota) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return q.service.Handle(next)
	}
}
