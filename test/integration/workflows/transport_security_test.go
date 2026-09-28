package workflows

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/tlsconfig"
	"github.com/ericfitz/tmi/test/integration/framework"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

// TestTransportSecurity_PlaintextRejected_Integration pins PR 6's goal: the
// harness Redis refuses plaintext and the harness NATS refuses a client that
// has no certificate, while the TLS/mTLS control paths work.
// SEM@new: verify harness Redis rejects plaintext and NATS rejects cert-less clients (reads env, network)
func TestTransportSecurity_PlaintextRejected_Integration(t *testing.T) {
	if os.Getenv("TEST_REDIS_TLS_CA_FILE") == "" || os.Getenv("TMI_NATS_TLS_CA_FILE") == "" {
		t.Skip("harness TLS not configured; run via make test-integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	addr := net.JoinHostPort(getEnvOrDefault("TEST_REDIS_HOST", "localhost"), getEnvOrDefault("TEST_REDIS_PORT", "6379"))
	plain := redis.NewClient(&redis.Options{Addr: addr, DialTimeout: 3 * time.Second, ReadTimeout: 3 * time.Second})
	defer plain.Close()
	if err := plain.Ping(ctx).Err(); err == nil {
		t.Fatal("plaintext Redis PING succeeded: the harness Redis must listen TLS-only")
	}

	opts, err := framework.RedisOptions()
	if err != nil {
		t.Fatal(err)
	}
	secure := redis.NewClient(opts)
	defer secure.Close()
	if err := secure.Ping(ctx).Err(); err != nil {
		t.Fatalf("TLS + password Redis PING failed: %v", err)
	}

	natsURL := os.Getenv("TMI_TEST_NATS_URL")
	caOnly, err := tlsconfig.Load(os.Getenv("TMI_NATS_TLS_CA_FILE"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Trusting the CA but presenting no client cert isolates the failure to
	// the server's verify: true (not an unknown-server-cert error).
	if nc, err := nats.Connect(natsURL, nats.Secure(caOnly), nats.Timeout(3*time.Second)); err == nil {
		nc.Close()
		t.Fatal("cert-less NATS connect succeeded: the harness NATS must require client certificates")
	}
	nc, err := nats.Connect(natsURL, append(framework.NATSTLSOptions(t), nats.Timeout(3*time.Second))...)
	if err != nil {
		t.Fatalf("mTLS NATS connect failed: %v", err)
	}
	nc.Close()
}
