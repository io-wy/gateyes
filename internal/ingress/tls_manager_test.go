package ingress

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTLSManager_LoadAndGet(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t, "example.com")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tls-secret",
		},
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	m := NewTLSManager(c, "default")
	ctx := context.Background()

	if err := m.Load(ctx, "example.com", "tls-secret", "default"); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Exact match.
	cert, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.com"})
	if err != nil {
		t.Fatalf("GetCertificate error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected certificate for exact match")
	}
}

func TestTLSManager_WildcardMatch(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t, "*.example.com")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "wildcard-secret",
		},
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	m := NewTLSManager(c, "default")
	ctx := context.Background()

	if err := m.Load(ctx, "*.example.com", "wildcard-secret", "default"); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Wildcard match for subdomain.
	cert, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "sub.example.com"})
	if err != nil {
		t.Fatalf("GetCertificate error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected certificate for wildcard match")
	}

	// Non-matching domain should return nil.
	cert, _ = m.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.com"})
	if cert != nil {
		t.Error("expected nil certificate for non-matching domain")
	}
}

func TestTLSManager_Delete(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t, "example.com")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tls-secret",
		},
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	m := NewTLSManager(c, "default")
	ctx := context.Background()

	if err := m.Load(ctx, "example.com", "tls-secret", "default"); err != nil {
		t.Fatalf("Load error: %v", err)
	}

	m.Delete("example.com")

	cert, _ := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.com"})
	if cert != nil {
		t.Error("expected nil certificate after delete")
	}
}

func TestTLSManager_WatchSecrets(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t, "example.com")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "tls-secret",
		},
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	m := NewTLSManager(c, "default")
	ctx := context.Background()

	hosts := map[string]types.NamespacedName{
		"example.com": {Namespace: "default", Name: "tls-secret"},
	}

	if err := m.WatchSecrets(ctx, hosts); err != nil {
		t.Fatalf("WatchSecrets error: %v", err)
	}

	cert, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.com"})
	if err != nil {
		t.Fatalf("GetCertificate error: %v", err)
	}
	if cert == nil {
		t.Fatal("expected certificate after WatchSecrets")
	}
}

func TestTLSManager_Load_MissingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	m := NewTLSManager(c, "default")
	ctx := context.Background()

	if err := m.Load(ctx, "example.com", "missing-secret", "default"); err == nil {
		t.Error("expected error for missing secret")
	}
}

func TestTLSManager_Load_MissingKeys(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "bad-secret",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("not-a-cert"),
		},
	}

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	m := NewTLSManager(c, "default")
	ctx := context.Background()

	if err := m.Load(ctx, "example.com", "bad-secret", "default"); err == nil {
		t.Error("expected error for secret missing tls.key")
	}
}

// generateTestCert creates a self-signed certificate and returns PEM-encoded cert and key.
func generateTestCert(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{cn},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}
