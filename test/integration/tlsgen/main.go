// tlsgen writes the throwaway PKI the integration harness runs its Redis and
// NATS containers with: ca.crt, redis.crt/.key, nats.crt/.key, client.crt/.key,
// a redis.conf (TLS-only listener + requirepass) and secrets.env for
// --env-file. Idempotent: an existing <out>/ca.crt is left alone so a test
// container that is already running keeps the cert it started with (remove
// the directory and `manage-redis.py --test clean` / `manage-nats.py --test
// clean` to rotate).
//
// Usage (from test/integration): go run ./tlsgen -out ../../.local/test-tls
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericfitz/tmi/internal/tlsconfig/testpki"
)

// SEM@249dea6: parse -out flag and generate the harness PKI, exiting nonzero on failure (writes files)
func main() {
	out := flag.String("out", "", "output directory (created if missing)")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "tlsgen: -out is required")
		os.Exit(2)
	}
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "tlsgen:", err)
		os.Exit(1)
	}
}

// SEM@249dea6: generate the harness CA, server/client certs, redis.conf and secrets.env once (writes files)
func run(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err == nil {
		return ensureSettingsKey(dir) // PKI already generated; older dirs lack the settings key
	}
	p, err := testpki.New(dir)
	if err != nil {
		return err
	}
	// The server container dials Redis as host.docker.internal; the Go tests
	// and worker-probe dial both as localhost/127.0.0.1.
	sans := []string{"localhost", "host.docker.internal"}
	ips := []net.IP{net.ParseIP("127.0.0.1")}
	if err := p.WriteServer("redis", sans, ips); err != nil {
		return err
	}
	if err := p.WriteServer("nats", sans, ips); err != nil {
		return err
	}
	if err := p.WriteClient("client", "tmi-integration-client"); err != nil {
		return err
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(raw)
	if err := os.WriteFile(filepath.Join(dir, "redis-password"), []byte(password+"\n"), 0o600); err != nil {
		return err
	}
	env := "TMI_REDIS_PASSWORD=" + password + "\nREDISCLI_AUTH=" + password + "\n"
	if err := os.WriteFile(filepath.Join(dir, "secrets.env"), []byte(env), 0o600); err != nil {
		return err
	}
	// Read by the redis user inside the container: 0644 like the certs
	// (throwaway, gitignored, loopback-only container).
	conf := fmt.Sprintf(`port 0
tls-port 6379
tls-cert-file /tls/redis.crt
tls-key-file /tls/redis.key
tls-ca-cert-file /tls/ca.crt
tls-auth-clients no
bind 0.0.0.0
protected-mode no
save ""
appendonly no
requirepass %s
`, password)
	if err := os.WriteFile(filepath.Join(dir, "redis.conf"), []byte(conf), 0o644); err != nil { // #nosec G306 -- throwaway harness config
		return err
	}
	return ensureSettingsKey(dir)
}

// SEM@<sha>: append a random settings encryption key to secrets.env when absent (writes file)
func ensureSettingsKey(dir string) error {
	path := filepath.Join(dir, "secrets.env")
	cur, err := os.ReadFile(path) // #nosec G304 -- harness-owned path
	if err != nil {
		return err
	}
	if strings.Contains(string(cur), "TMI_SECRET_SETTINGS_ENCRYPTION_KEY=") {
		return nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	add := "TMI_SECRET_SETTINGS_ENCRYPTION_KEY=" + hex.EncodeToString(raw) + "\n"
	if len(cur) > 0 && cur[len(cur)-1] != '\n' {
		add = "\n" + add
	}
	if err := os.WriteFile(path, append(cur, add...), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600) // WriteFile keeps an existing file's mode
}
