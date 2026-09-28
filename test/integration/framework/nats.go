package framework

import (
	"testing"

	"github.com/ericfitz/tmi/internal/tlsconfig"
	"github.com/nats-io/nats.go"
)

// NATSTLSOptions returns nats.Secure with the client cert named by the
// NATS TLS env vars of the production contract (scripts/run-integration-tests.py sets them to the
// harness PKI); nil when TMI_NATS_TLS_CA_FILE is unset, so a developer's
// ad-hoc plaintext NATS still works. Same contract as internal/worker.
// SEM@249dea6: build NATS mTLS connect options from the production NATS TLS env contract (reads env)
func NATSTLSOptions(t *testing.T) []nats.Option {
	t.Helper()
	cfg, err := tlsconfig.NATSFromEnv()
	if err != nil {
		t.Fatalf("harness NATS TLS: %v", err)
	}
	if cfg == nil {
		return nil
	}
	return []nats.Option{nats.Secure(cfg)}
}
