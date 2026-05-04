//go:build etcd

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdDiscovery discovers service endpoints from an etcd cluster.
type EtcdDiscovery struct {
	client *clientv3.Client
	prefix string
}

// NewEtcdDiscovery creates a new etcd-based service discovery client.
func NewEtcdDiscovery(endpoints []string, username, password, prefix string) (*EtcdDiscovery, error) {
	cfg := clientv3.Config{
		Endpoints:   endpoints,
		Username:    username,
		Password:    password,
		DialTimeout: 5 * time.Second,
	}

	cli, err := clientv3.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("create etcd client: %w", err)
	}

	if prefix == "" {
		prefix = "/gateyes/services"
	}
	// Normalize prefix: no trailing slash.
	prefix = strings.TrimSuffix(prefix, "/")

	return &EtcdDiscovery{
		client: cli,
		prefix: prefix,
	}, nil
}

// Watch queries etcd for endpoints under the service's prefix key.
func (d *EtcdDiscovery) Watch(ctx context.Context, serviceName string) ([]Endpoint, error) {
	key := d.prefix + "/" + serviceName

	resp, err := d.client.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("etcd get %s: %w", key, err)
	}

	var endpoints []Endpoint
	for _, kv := range resp.Kvs {
		ep := d.parseValue(string(kv.Value))
		if ep.Address != "" {
			endpoints = append(endpoints, ep)
		}
	}

	return endpoints, nil
}

// parseValue attempts to parse the etcd value as JSON Endpoint,
// falling back to treating it as a plain host:port address.
func (d *EtcdDiscovery) parseValue(value string) Endpoint {
	value = strings.TrimSpace(value)
	if value == "" {
		return Endpoint{}
	}

	// Try JSON first.
	var ep Endpoint
	if err := json.Unmarshal([]byte(value), &ep); err == nil && ep.Address != "" {
		return ep
	}

	// Fall back to plain address.
	return Endpoint{
		Address: value,
		Weight:  1,
	}
}

// Close closes the etcd client connection.
func (d *EtcdDiscovery) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}
