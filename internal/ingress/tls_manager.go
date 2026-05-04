package ingress

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TLSManager watches Kubernetes Secrets and serves TLS certificates for Ingress hosts.
type TLSManager struct {
	client    client.Reader
	namespace string
	mu        sync.RWMutex
	certs     map[string]*tls.Certificate // key: host
}

// NewTLSManager creates a new TLSManager.
func NewTLSManager(c client.Reader, namespace string) *TLSManager {
	return &TLSManager{
		client:    c,
		namespace: namespace,
		certs:     make(map[string]*tls.Certificate),
	}
}

// Load reads a TLS secret from Kubernetes and stores the certificate for the given host.
func (m *TLSManager) Load(ctx context.Context, host, secretName, secretNamespace string) error {
	if secretNamespace == "" {
		secretNamespace = m.namespace
	}

	var secret corev1.Secret
	key := types.NamespacedName{Name: secretName, Namespace: secretNamespace}
	if err := m.client.Get(ctx, key, &secret); err != nil {
		return fmt.Errorf("get secret %s/%s: %w", secretNamespace, secretName, err)
	}

	certPEM := secret.Data["tls.crt"]
	keyPEM := secret.Data["tls.key"]
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return fmt.Errorf("secret %s/%s missing tls.crt or tls.key", secretNamespace, secretName)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("parse certificate for host %s: %w", host, err)
	}

	m.mu.Lock()
	m.certs[host] = &cert
	m.mu.Unlock()

	return nil
}

// GetCertificate implements tls.Config.GetCertificate callback.
// Looks up the exact host first, then falls back to wildcard match.
func (m *TLSManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	host := hello.ServerName

	// Exact match.
	if cert, ok := m.certs[host]; ok {
		return cert, nil
	}

	// Wildcard match: try *.domain for subdomains.
	if idx := dotIndex(host); idx > 0 {
		wildcard := "*." + host[idx+1:]
		if cert, ok := m.certs[wildcard]; ok {
			return cert, nil
		}
	}

	return nil, nil
}

// Delete removes a host's certificate from the manager.
func (m *TLSManager) Delete(host string) {
	m.mu.Lock()
	delete(m.certs, host)
	m.mu.Unlock()
}

// WatchSecrets loads all specified secrets into the certificate store.
// hosts maps host -> secret reference (namespace/name).
func (m *TLSManager) WatchSecrets(ctx context.Context, hosts map[string]types.NamespacedName) error {
	var firstErr error
	for host, ref := range hosts {
		if err := m.Load(ctx, host, ref.Name, ref.Namespace); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// dotIndex returns the index of the first dot in s, or -1 if none.
func dotIndex(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}
