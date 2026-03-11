package gateway

import (
	"gateyes/internal/config"
	serviceproxy "gateyes/internal/service/proxy"
)

type Service = serviceproxy.OpenAIProxy

func New(
	cfg config.GatewayConfig,
	authCfg config.AuthConfig,
	providers map[string]config.ProviderConfig,
) (*Service, error) {
	return serviceproxy.NewOpenAIProxy(cfg, authCfg, providers)
}
