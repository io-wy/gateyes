//go:build consul

package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/hashicorp/consul/api"
)

// ConsulDiscovery discovers endpoints via Consul health API.
type ConsulDiscovery struct {
	client     *api.Client
	datacenter string
}

// NewConsulDiscovery creates a Consul discovery client.
func NewConsulDiscovery(addr, datacenter, token string) (*ConsulDiscovery, error) {
	config := &api.Config{
		Address:    addr,
		Datacenter: datacenter,
		Token:      token,
	}
	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("create consul client: %w", err)
	}
	return &ConsulDiscovery{
		client:     client,
		datacenter: datacenter,
	}, nil
}

// Watch returns healthy endpoints for the given service name.
func (d *ConsulDiscovery) Watch(ctx context.Context, serviceName string) ([]Endpoint, error) {
	opts := &api.QueryOptions{
		Datacenter: d.datacenter,
		Context:    ctx,
	}
	entries, _, err := d.client.Health().Service(serviceName, "", true, opts)
	if err != nil {
		return nil, fmt.Errorf("query consul health for %s: %w", serviceName, err)
	}

	var endpoints []Endpoint
	for _, entry := range entries {
		host := entry.Service.Address
		if host == "" {
			host = entry.Node.Address
		}
		addr := net.JoinHostPort(host, strconv.Itoa(entry.Service.Port))

		weight := 1
		if wStr, ok := entry.Service.Meta["weight"]; ok {
			if w, err := strconv.Atoi(wStr); err == nil && w > 0 {
				weight = w
			}
		}

		endpoints = append(endpoints, Endpoint{
			Address:  addr,
			Weight:   weight,
			Metadata: entry.Service.Meta,
		})
	}
	return endpoints, nil
}

// Close releases resources held by the discovery client.
func (d *ConsulDiscovery) Close() error { return nil }
