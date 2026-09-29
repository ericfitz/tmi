// Package testpki writes a throwaway certificate authority and leaf
// certificates for unit tests and the integration harness. Never use it for
// a real deployment: private keys are written world-readable (0644) so the
// non-root users inside the redis/nats test containers can read a bind mount.
package testpki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// PKI is a throwaway CA rooted at Dir (Dir/ca.crt).
// SEM@d2c63e2: throwaway certificate authority for tests, writing PEM files under one directory
type PKI struct {
	Dir    string
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
}

// New creates a CA valid for one year and writes dir/ca.crt.
// SEM@d2c63e2: create a throwaway CA and write its certificate to a directory
func New(dir string) (*PKI, error) {
	// #nosec G301 -- throwaway test PKI
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tmi-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	if err := writePEM(filepath.Join(dir, "ca.crt"), "CERTIFICATE", der); err != nil {
		return nil, err
	}
	return &PKI{Dir: dir, caCert: cert, caKey: key}, nil
}

// WriteServer writes Dir/<name>.crt and Dir/<name>.key for a server with the
// given DNS and IP SANs (server auth usage).
// SEM@d2c63e2: issue and write a server certificate with DNS and IP SANs signed by the test CA
func (p *PKI) WriteServer(name string, dnsNames []string, ips []net.IP) error {
	return p.write(name, &x509.Certificate{
		Subject:     pkix.Name{CommonName: name},
		DNSNames:    dnsNames,
		IPAddresses: ips,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
}

// WriteClient writes Dir/<name>.crt and Dir/<name>.key for a client with
// CN=cn (client auth usage). Calling it again with the same name overwrites
// both files, which is how tests simulate a cert-manager renewal.
// SEM@d2c63e2: issue and write a client-auth certificate with the given CN signed by the test CA
func (p *PKI) WriteClient(name, cn string) error {
	return p.write(name, &x509.Certificate{
		Subject:     pkix.Name{CommonName: cn},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
}

// SEM@d2c63e2: sign a leaf template with the CA and write its cert and key PEM files
func (p *PKI) write(name string, tmpl *x509.Certificate) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl.SerialNumber = big.NewInt(time.Now().UnixNano())
	tmpl.NotBefore = time.Now().Add(-time.Hour)
	tmpl.NotAfter = time.Now().Add(30 * 24 * time.Hour)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, p.caCert, &key.PublicKey, p.caKey)
	if err != nil {
		return fmt.Errorf("testpki: sign %s: %w", name, err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := writePEM(filepath.Join(p.Dir, name+".crt"), "CERTIFICATE", der); err != nil {
		return err
	}
	return writePEM(filepath.Join(p.Dir, name+".key"), "EC PRIVATE KEY", keyDER)
}

// SEM@d2c63e2: write one DER blob as a PEM file, world-readable for container bind mounts
func writePEM(path, typ string, der []byte) error {
	// 0644 on purpose: see the package comment.
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o644) // #nosec G306 -- throwaway test PKI
}
