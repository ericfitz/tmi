// Package tlsconfig builds the client tls.Config values TMI uses for its
// in-cluster Redis and NATS connections (PR 6, threats T356/T388).
//
// Trust is pinned to one CA file (never the system pool), TLS 1.2 is the
// floor, and a client certificate is re-read from disk on every handshake so
// a cert-manager renewal (which rewrites the mounted Secret) takes effect on
// the next reconnect without a restart.
package tlsconfig

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Environment variables for the NATS client TLS files. The endpoint itself
// is TMI_NATS_URL (see internal/worker). All are optional; TLS is off when
// EnvNATSCAFile is unset.
const (
	EnvNATSCAFile   = "TMI_NATS_TLS_CA_FILE"
	EnvNATSCertFile = "TMI_NATS_TLS_CERT_FILE"
	EnvNATSKeyFile  = "TMI_NATS_TLS_KEY_FILE"
	// EnvNATSServerName overrides the hostname verified against the server
	// certificate. Needed when TMI_NATS_URL carries an IP address (the
	// extractor dials the ClusterIP because its sandbox has no DNS).
	EnvNATSServerName = "TMI_NATS_TLS_SERVER_NAME"
)

// Load builds a client TLS config that trusts only caFile. When certFile and
// keyFile are both set the config presents that client certificate,
// re-reading the pair on every handshake.
// SEM@d2c63e2: build a CA-pinned client TLS config, re-reading the client cert per handshake
func Load(caFile, certFile, keyFile string) (*tls.Config, error) {
	if caFile == "" {
		return nil, fmt.Errorf("tlsconfig: CA file path is empty")
	}
	caPEM, err := os.ReadFile(caFile) // #nosec G304 G703 -- operator-controlled path
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: read CA %s: %w", caFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("tlsconfig: no certificates found in CA file %s", caFile)
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}

	if certFile == "" && keyFile == "" {
		return cfg, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("tlsconfig: client cert and key must both be set (cert=%q key=%q)", certFile, keyFile)
	}
	// Fail at startup on an unreadable pair; the handshake path below re-reads.
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("tlsconfig: load client cert: %w", err)
	}
	cfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		c, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("tlsconfig: reload client cert: %w", err)
		}
		return &c, nil
	}
	return cfg, nil
}

// NATSFromEnv returns the NATS client TLS config described by the
// environment variables above, or (nil, nil) when EnvNATSCAFile is unset
// (plaintext, the code default). Callers pass a non-nil result to nats.Secure.
// SEM@d2c63e2: build the NATS client TLS config from NATS TLS env vars; nil when unset (reads env)
func NATSFromEnv() (*tls.Config, error) {
	ca := os.Getenv(EnvNATSCAFile)
	if ca == "" {
		return nil, nil
	}
	cfg, err := Load(ca, os.Getenv(EnvNATSCertFile), os.Getenv(EnvNATSKeyFile))
	if err != nil {
		return nil, fmt.Errorf("nats tls: %w", err)
	}
	cfg.ServerName = os.Getenv(EnvNATSServerName)
	return cfg, nil
}
