package provider

import (
	"net"
	"net/http"
	"time"
)

const (
	defaultTransportDialTimeout           = 30 * time.Second
	defaultTransportKeepAlive             = 30 * time.Second
	defaultTransportMaxIdleConns          = 100
	defaultTransportMaxIdleConnsPerHost   = 10
	defaultTransportIdleConnTimeout       = 90 * time.Second
	defaultTransportTLSHandshakeTimeout   = 10 * time.Second
	defaultTransportExpectContinueTimeout = 1 * time.Second
)

func newProviderHTTPClient(timeoutSeconds int) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DialContext = (&net.Dialer{
		Timeout:   defaultTransportDialTimeout,
		KeepAlive: defaultTransportKeepAlive,
	}).DialContext
	transport.ForceAttemptHTTP2 = true
	transport.MaxIdleConns = defaultTransportMaxIdleConns
	transport.MaxIdleConnsPerHost = defaultTransportMaxIdleConnsPerHost
	transport.IdleConnTimeout = defaultTransportIdleConnTimeout
	transport.TLSHandshakeTimeout = defaultTransportTLSHandshakeTimeout
	transport.ExpectContinueTimeout = defaultTransportExpectContinueTimeout

	client := &http.Client{Transport: transport}
	if timeoutSeconds > 0 {
		client.Timeout = time.Duration(timeoutSeconds) * time.Second
	}
	return client
}
