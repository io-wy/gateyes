//go:build etcd

package discovery

import (
	"testing"
)

func TestNewEtcdDiscovery_InvalidEndpoints(t *testing.T) {
	// Empty endpoints should result in an error from etcd client creation.
	_, err := NewEtcdDiscovery([]string{}, "", "", "")
	if err == nil {
		t.Error("expected error for empty endpoints")
	}
}

func TestNewEtcdDiscovery_InvalidURL(t *testing.T) {
	// Malformed endpoint URL should fail.
	_, err := NewEtcdDiscovery([]string{"://bad-url"}, "", "", "")
	if err == nil {
		t.Error("expected error for malformed endpoint URL")
	}
}

func TestEtcdDiscovery_Watch(t *testing.T) {
	t.Skip("requires running etcd server")
}

func TestEtcdDiscovery_Close(t *testing.T) {
	// Closing without a valid client should not panic.
	d := &EtcdDiscovery{client: nil}
	if err := d.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
