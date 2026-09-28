package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"github.com/ericfitz/tmi/internal/tlsconfig/testpki"
)

func newPKI(t *testing.T) *testpki.PKI {
	t.Helper()
	p, err := testpki.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_CAOnly(t *testing.T) {
	p := newPKI(t)
	cfg, err := Load(filepath.Join(p.Dir, "ca.crt"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs must be the CA file, not nil (system pool)")
	}
	if cfg.GetClientCertificate != nil {
		t.Fatal("no client cert requested, GetClientCertificate must be nil")
	}
}

func TestLoad_BadCA(t *testing.T) {
	dir := t.TempDir()
	junk := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, ca := range map[string]string{"empty path": "", "missing file": filepath.Join(dir, "nope.crt"), "no certs in file": junk} {
		if _, err := Load(ca, "", ""); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestLoad_MissingKey(t *testing.T) {
	p := newPKI(t)
	if err := p.WriteClient("client", "one"); err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(p.Dir, "ca.crt")
	crt := filepath.Join(p.Dir, "client.crt")
	if _, err := Load(ca, crt, ""); err == nil {
		t.Error("cert without key: want error")
	}
	if _, err := Load(ca, "", filepath.Join(p.Dir, "client.key")); err == nil {
		t.Error("key without cert: want error")
	}
	if _, err := Load(ca, crt, filepath.Join(p.Dir, "missing.key")); err == nil {
		t.Error("nonexistent key file: want error")
	}
}

func TestLoad_ClientCertRereadPerHandshake(t *testing.T) {
	p := newPKI(t)
	if err := p.WriteClient("client", "before"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(filepath.Join(p.Dir, "ca.crt"), filepath.Join(p.Dir, "client.crt"), filepath.Join(p.Dir, "client.key"))
	if err != nil {
		t.Fatal(err)
	}
	cn := func() string {
		c, err := cfg.GetClientCertificate(&tls.CertificateRequestInfo{})
		if err != nil {
			t.Fatal(err)
		}
		leaf, err := x509.ParseCertificate(c.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		return leaf.Subject.CommonName
	}
	if got := cn(); got != "before" {
		t.Fatalf("initial CN = %q", got)
	}
	// Simulate cert-manager rewriting the mounted Secret.
	if err := p.WriteClient("client", "after"); err != nil {
		t.Fatal(err)
	}
	if got := cn(); got != "after" {
		t.Fatalf("CN after rotation = %q, want %q (cert must be re-read per handshake)", got, "after")
	}
}

func TestNATSFromEnv(t *testing.T) {
	t.Setenv(EnvNATSCAFile, "")
	if cfg, err := NATSFromEnv(); err != nil || cfg != nil {
		t.Fatalf("unset CA: want (nil, nil), got (%v, %v)", cfg, err)
	}

	p := newPKI(t)
	if err := p.WriteClient("client", "worker"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvNATSCAFile, filepath.Join(p.Dir, "ca.crt"))
	t.Setenv(EnvNATSCertFile, filepath.Join(p.Dir, "client.crt"))
	t.Setenv(EnvNATSKeyFile, filepath.Join(p.Dir, "client.key"))
	t.Setenv(EnvNATSServerName, "nats")
	cfg, err := NATSFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "nats" {
		t.Fatalf("ServerName = %q", cfg.ServerName)
	}
	if cfg.GetClientCertificate == nil {
		t.Fatal("client cert must be configured")
	}

	t.Setenv(EnvNATSCAFile, filepath.Join(p.Dir, "missing.crt"))
	if _, err := NATSFromEnv(); err == nil {
		t.Fatal("missing CA file must be an error, not a silent plaintext fallback")
	}
}
