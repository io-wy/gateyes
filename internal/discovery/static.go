package discovery

import (
	"context"
	"fmt"
	"strings"
)

// StaticDiscovery returns pre-configured endpoints.
type StaticDiscovery struct {
	endpoints map[string][]Endpoint
}

func NewStaticDiscovery() *StaticDiscovery {
	return &StaticDiscovery{endpoints: make(map[string][]Endpoint)}
}

func (d *StaticDiscovery) Register(serviceName string, endpoints []Endpoint) {
	d.endpoints[serviceName] = endpoints
}

func (d *StaticDiscovery) Watch(_ context.Context, serviceName string) ([]Endpoint, error) {
	eps, ok := d.endpoints[serviceName]
	if !ok {
		// Treat serviceName as a direct address list if not registered.
		addrs := strings.Split(serviceName, ",")
		var out []Endpoint
		for _, addr := range addrs {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			out = append(out, Endpoint{Address: addr, Weight: 1})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("no static endpoints for %s", serviceName)
		}
		return out, nil
	}
	return eps, nil
}

func (d *StaticDiscovery) Close() error { return nil }
