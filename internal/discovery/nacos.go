//go:build nacos

package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// NacosDiscovery discovers endpoints via Nacos naming service.
type NacosDiscovery struct {
	client naming_client.INamingClient
}

// NewNacosDiscovery creates a Nacos discovery client.
func NewNacosDiscovery(serverAddr, namespaceID string) (*NacosDiscovery, error) {
	host, portStr, err := net.SplitHostPort(serverAddr)
	if err != nil {
		// Try appending default port
		host = serverAddr
		portStr = "8848"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid nacos port %q: %w", portStr, err)
	}

	clientConfig := constant.ClientConfig{
		NamespaceId:         namespaceID,
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		LogDir:              "/tmp/nacos/log",
		CacheDir:            "/tmp/nacos/cache",
		LogLevel:            "error",
	}

	serverConfigs := []constant.ServerConfig{
		{
			IpAddr: host,
			Port:   uint64(port),
		},
	}

	client, err := clients.NewNamingClient(
		vo.NacosClientParam{
			ClientConfig:  &clientConfig,
			ServerConfigs: serverConfigs,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create nacos naming client: %w", err)
	}

	return &NacosDiscovery{client: client}, nil
}

// Watch returns healthy endpoints for the given service name.
func (d *NacosDiscovery) Watch(ctx context.Context, serviceName string) ([]Endpoint, error) {
	instances, err := d.client.SelectInstances(
		vo.SelectInstancesParam{
			ServiceName: serviceName,
			HealthyOnly: true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("select nacos instances for %s: %w", serviceName, err)
	}

	var endpoints []Endpoint
	for _, inst := range instances {
		addr := fmt.Sprintf("%s:%d", inst.Ip, inst.Port)
		weight := int(inst.Weight)
		if weight <= 0 {
			weight = 1
		}
		endpoints = append(endpoints, Endpoint{
			Address: addr,
			Weight:  weight,
			Metadata: map[string]string{
				"instanceId": inst.InstanceId,
				"cluster":    inst.ClusterName,
			},
		})
	}
	return endpoints, nil
}

// Close releases resources held by the discovery client.
func (d *NacosDiscovery) Close() error {
	if d.client != nil {
		// INamingClient does not expose CloseClient in all versions;
		// attempt via interface assertion and ignore if unavailable.
		if c, ok := d.client.(interface{ CloseClient() }); ok {
			c.CloseClient()
		}
	}
	return nil
}
