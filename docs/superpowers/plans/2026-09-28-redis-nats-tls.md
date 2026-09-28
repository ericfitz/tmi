# Redis/NATS In-Cluster TLS (PR 6) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Encrypt every Redis (server TLS + password) and NATS (mTLS) connection in every shipped environment, with cert-manager issuing and renewing the certificates and Stakater Reloader rolling pods when a cert Secret changes.

**Architecture:** A self-signed cert-manager ClusterIssuer bootstraps a 5-year internal CA whose key lives only in a Secret; a CA ClusterIssuer signs 90-day leaf certs for the Redis and NATS servers and one client cert per NATS client. The Go side gains one small package (`internal/tlsconfig`) that every Redis/NATS client path uses; defaults stay plaintext, every shipped manifest turns TLS on. The integration harness generates a throwaway PKI and runs its Redis/NATS containers in TLS mode.

**Tech Stack:** Go 1.x (`crypto/tls`, `crypto/x509`), go-redis v9.22, nats.go v1.54, cert-manager v1.21.2, Stakater Reloader v1.4.22, kustomize overlays, Python (uv) dev/deploy scripts, bash `scripts/deploy-aws.sh`.

**Spec:** `docs/superpowers/specs/2026-09-28-redis-nats-tls-design.md` (approved by Eric 2026-09-28). The plan argues from the spec; executors read both.

## Open questions (resolved 2026-09-28)

HUMAN DECISIONS (Eric, 2026-09-28): #2 throwaway per-machine test Redis password; #6 accept NATS JetStream wipe on Reloader roll. The rest (#1, #3, #4, #5, #7, #8) use the defaults below, which the orchestrator accepted as implementation details.

None of these block starting Tasks 1-4. Each has the default the plan assumes; say so if you want a different answer.

1. **Extractor hostname verification (not in spec).** `tmi-extractor` dials NATS by ClusterIP (`nats://$(NATS_SERVICE_HOST):4222`, because `egress: none` has no DNS). A ClusterIP is not a stable SAN, so the plan adds an optional env var `TMI_NATS_TLS_SERVER_NAME` (read by the shared helper; nats.go honours a preset `tls.Config.ServerName`) and sets it to `nats` in `tmi-extractor.yml`. Default: do this.
2. **Integration-test Redis password.** Spec decision 3 says "password everywhere". The harness Redis is a throwaway container bound to 127.0.0.1:6380. The plan generates a per-machine random password into gitignored `.local/test-tls/` (0600), feeds Redis a `redis.conf` bind-mount (never argv), the server container an `--env-file`, and the Go tests `TEST_REDIS_PASSWORD` in the child env (same channel the harness already uses for `TEST_DB_PASSWORD`). Default: do this. Alternative: no password for the isolated test container.
3. **Client-cert Secret naming in the renderer.** Spec names `nats-client-extractor` / `nats-client-chunk-embed`, but the components are `tmi-extractor` / `tmi-chunk-embed`. Plan rule: `nats-client-<component name with the tmi- prefix removed>`. Default: this rule.
4. **Renderer Secret volume is `optional: true`.** Without it a TMIComponent that never dials NATS (the kind e2e `e2e-probe`, a static image) would never schedule. A worker that does dial fails loudly in `tlsconfig.Load` on the missing files. Default: optional.
5. **Reloader namespace.** The upstream static manifest installs into `default`. Plan vendors it with `namespace: default` rewritten to `namespace: reloader` and a Namespace object prepended (documented in the file header). Default: rewrite.
6. **NATS rolls lose JetStream state.** A Reloader roll of the NATS StatefulSet (every ~60 days on renewal) wipes the emptyDir JetStream store (streams, consumers, payload bucket), exactly as any NATS pod restart does today. The spec only records this trade-off for Redis. Default: accept, and add "re-ensure streams/object store after NATS reconnect" to the #965 follow-up list.
7. **kind e2e harness (`make e2e-platform-up`, `test/e2e/platform`) is not in the spec.** Plan extends it minimally (apply cert-manager/Reloader/PKI; `tls://` in the embedded component YAML) but does NOT convert its host-side plaintext port-forward dials; those tests need a client-cert export and are listed as a follow-up. Default: minimal extension + follow-up issue.
8. **Terraform one-liner.** The `tmi-server-config` ConfigMap's `TMI_NATS_URL` (`terraform/modules/kubernetes/aws/k8s_resources.tf:119`) is shadowed by the overlay patch but would read `nats://` while everything else says `tls://`. Plan changes that one value (Eric runs `terraform plan` at the deferred AWS deploy). Default: change it.

## Global Constraints

- **Make targets only**: `make build-server`, `make lint`, `make test-unit name=TestX count1=true`, `make test-integration`, `make generate-config-example`, `make generate-config-docs`. Never `go run`, `go test`, `./bin/...` for the server. The integration runner (`scripts/run-integration-tests.py`) may itself invoke `go build`/`go run`, as it already does for `worker-probe`.
- **`name=` with alternation must be double-quoted inside the single quotes**: `make test-unit name='"TestLoad|TestNATSFromEnv"' count1=true` (the recipe expands `--name $(name)` unquoted, so a bare `|` becomes a shell pipe). Verified: `make -n test-unit name='"A|B"'` prints `--name "A|B"`.
- **Logging**: `github.com/ericfitz/tmi/internal/slogging` only; never `log` or `fmt.Println`.
- **SEM markers**: every new or behaviour-changed Go function/method/type gets a one-line `// SEM@new: <intent>` comment directly above it (`new` is the placeholder anchor; Task 8 step 1 replaces anchors with `/dev:sem-annotate --update` after the commits exist).
- **Secret safety**: no secret value on a command line, in an env var of a long-lived process, in a log, or in the model's context. Passwords go through `umask 077` files + `--from-file` / `--env-file` / bind mounts.
- **Terraform owns infra/bootstrap objects; kustomize overlays own workloads.** cert-manager, Reloader, PKI, NATS, KEDA are applied with kubectl by `scripts/lib/deploy.py` and `scripts/deploy-aws.sh` (like `keda.yml`), never by Terraform.
- **Code defaults stay off** (spec §3): with no `TMI_*_TLS_*` env vars, every client connects in plaintext exactly as today. Every shipped manifest turns TLS on.
- **Names and paths (fixed here, used by Tasks 3-7)**:
  - Env vars: `TMI_REDIS_TLS_ENABLED`, `TMI_REDIS_TLS_CA_FILE`, `TMI_NATS_TLS_CA_FILE`, `TMI_NATS_TLS_CERT_FILE`, `TMI_NATS_TLS_KEY_FILE`, `TMI_NATS_TLS_SERVER_NAME` (optional), `TMI_NATS_URL` (single NATS endpoint; `TMI_WORKER_NATS_URL` is removed).
  - Go API: `tlsconfig.Load(caFile, certFile, keyFile string) (*tls.Config, error)` and `tlsconfig.NATSFromEnv() (*tls.Config, error)` (nil, nil when `TMI_NATS_TLS_CA_FILE` is unset).
  - Secrets (namespace `tmi-platform`): `redis-tls`, `nats-tls`, `nats-client-server`, `nats-client-controller`, `nats-client-extractor`, `nats-client-chunk-embed`, `nats-client-worker-probe`. CA Secret `tmi-internal-ca` in namespace `cert-manager`. Password Secret `tmi-secrets`, key `TMI_REDIS_PASSWORD` (Terraform on AWS, `deploy.py` on dev clusters).
  - Mount paths: Redis and NATS servers `/tls`; server + controller + workers NATS client cert `/etc/tmi-nats-tls`; server Redis CA `/etc/tmi-redis-tls` (`ca.crt` item only).
  - URLs: `tls://nats.tmi-platform.svc:4222` everywhere except the extractor (`tls://$(NATS_SERVICE_HOST):4222` + `TMI_NATS_TLS_SERVER_NAME=nats`).
  - Certificates: ECDSA P-256; CA `duration: 43800h`; leaves `duration: 2160h`, `renewBefore: 720h`. Server SANs `<svc>`, `<svc>.tmi-platform.svc`, `<svc>.tmi-platform.svc.cluster.local`. Client cert CN = component name (`server`, `controller`, `extractor`, `chunk-embed`, `worker-probe`).
  - Reloader annotations: server, controller, worker Deployments `reloader.stakater.com/auto: "true"`; Redis `secret.reloader.stakater.com/reload: "redis-tls"`; NATS `secret.reloader.stakater.com/reload: "nats-tls"`.
  - Vendored versions (URLs verified 2026-09-28): cert-manager `https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.yaml`; Reloader `https://raw.githubusercontent.com/stakater/Reloader/v1.4.22/deployments/kubernetes/reloader.yaml` (image `ghcr.io/stakater/reloader:v1.4.22`).
  - Verified: `cgr.dev/chainguard/redis:latest` and `redis:7-alpine` (k3s) are both built with TLS (`--tls-port` is accepted; they fail only on the missing cert file). `nats:2.10-alpine` accepts `--tlsverify --tlscert --tlskey --tlscacert`.
- **Conventional commits**, one per task, on branch `fix/pr6-redis-nats-tls` in `/Users/efitz/Projects/tmi-pr6`. Every commit message ends with the two trailer lines the session provides (`Co-Authored-By: ...` / `Claude-Session: ...`).
- **No Oracle review needed** (spec §4: nothing touches the database).
- **Test naming**: integration test functions must contain `Integration`.

## Review Focus

1. A plaintext client (no `TLSConfig`) dialing the TLS-only Redis port, and a NATS client with the CA but no client cert, must both be refused by the servers (pinned by Task 7's `TestTransportSecurity_PlaintextRejected_Integration`).
2. `TMI_REDIS_TLS_ENABLED=true` with no CA file, or `TMI_NATS_TLS_CA_FILE` pointing at a missing/empty file, must fail loudly at startup (config validation / `tlsconfig.Load` error), never fall back to plaintext or to the system trust store (Task 1 `TestLoad_BadCA`, Task 2 `TestRedisOptions_TLSWithoutCAFails`, `TestValidate_RedisTLSNeedsCAFile`).
3. A `rediss://` URL must enable TLS even when `TMI_REDIS_TLS_ENABLED` is unset (Task 2 `TestBuildRedisConfig_RedissURLEnablesTLS`).
4. After cert-manager rewrites a client cert Secret, the next NATS reconnect must present the new cert without a process restart (Task 1 `TestLoad_ClientCertRereadPerHandshake`).
5. A worker whose `nats-client-<name>` Secret does not exist must still be scheduled (volume optional) and must fail at connect time with an error naming the missing file (Task 4 `TestRenderDeployment_NATSClientTLS`, Task 1 `TestLoad_MissingKey`).

---

## File structure

| Path | Responsibility |
|---|---|
| `internal/tlsconfig/tlsconfig.go` (new) | `Load` and `NATSFromEnv`: the only place client `tls.Config`s are built |
| `internal/tlsconfig/testpki/testpki.go` (new) | Throwaway CA/cert generator (stdlib only) shared by unit tests and the integration `tlsgen` tool |
| `internal/tlsconfig/tlsconfig_test.go` (new) | Unit tests for `Load`/`NATSFromEnv` |
| `internal/config/config.go`, `setting_defs_server.go` | Two new Redis settings (struct fields + registry) and validation |
| `auth/db/redis.go`, `auth/db/redis_options_test.go` (new) | `RedisConfig` TLS fields, `redisOptions` builder |
| `auth/config.go` | Legacy env-only `RedisConfig` gains the two fields |
| `cmd/server/main.go`, `cmd/server/redis_config_test.go` (new) | `buildRedisConfig` passes TLS through and honours `rediss://` |
| `internal/worker/nats.go`, `nats_options_test.go` (new) | `natsOptions` builder, `Connect` uses `NATSFromEnv` |
| `internal/platform/controller/jetstream_provisioner.go` | Provisioner uses `NATSFromEnv` |
| `cmd/worker-probe/main.go` | Probe uses `NATSFromEnv` |
| `internal/config/bootstrap/bootstrap.go` + test, `internal/config/process_env.go` | `TMI_NATS_URL` replaces `TMI_WORKER_NATS_URL`; TLS env vars documented |
| `internal/platform/controller/render_deployment.go` + test, `tmicomponent_controller.go` | Worker Deployment gets client-cert volume, env, Reloader annotation |
| `deployments/k8s/platform/{cert-manager,reloader,pki}.yml` (new) | Platform PKI |
| `scripts/lib/deploy.py`, `scripts/deploy-aws.sh`, `Makefile` (`e2e-platform-up`) | Apply order and readiness waits |
| `deployments/k8s/dev/redis.yml`, `deployments/k8s/platform/nats.yml` | Servers in TLS mode |
| `deployments/k8s/dev/{server,server-oracle,controller}.yml`, `deployments/k8s/dev/aws/{kustomization.yaml,patches/server-config.yaml,README.md}`, `deployments/k8s/platform/components/*.yml`, `deployments/k8s/dev/networkpolicy*.yml`, `terraform/modules/kubernetes/aws/k8s_resources.tf`, `scripts/cats-prep.py`, `test/e2e/platform/workers_e2e_test.go` | Clients in TLS mode |
| `test/integration/tlsgen/main.go` (new), `scripts/manage-redis.py`, `scripts/manage-nats.py`, `scripts/run-integration-tests.py`, `test/integration/framework/redis.go`, `test/integration/framework/nats.go` (new), `test/integration/workflows/transport_security_test.go` (new), `test/integration/workflows/{step_up_round_trip,worker_probe_integration}_test.go` | Integration harness in TLS mode |

---

### Task 1: `internal/tlsconfig` and the shared test PKI

**Files:**
- Create: `internal/tlsconfig/tlsconfig.go`
- Create: `internal/tlsconfig/testpki/testpki.go`
- Create: `internal/tlsconfig/tlsconfig_test.go`

**Interfaces:**
- Consumes: nothing project-specific (stdlib only).
- Produces:
  - `tlsconfig.Load(caFile, certFile, keyFile string) (*tls.Config, error)`
  - `tlsconfig.NATSFromEnv() (*tls.Config, error)` and the constants `EnvNATSCAFile`, `EnvNATSCertFile`, `EnvNATSKeyFile`, `EnvNATSServerName`
  - `testpki.New(dir string) (*testpki.PKI, error)`, `(*PKI).WriteServer(name string, dnsNames []string, ips []net.IP) error`, `(*PKI).WriteClient(name, cn string) error` — files land at `<dir>/ca.crt`, `<dir>/<name>.crt`, `<dir>/<name>.key`.

- [ ] **Step 1: Write the test PKI generator (test infrastructure, no test of its own)**

`internal/tlsconfig/testpki/testpki.go`:

```go
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
// SEM@new: throwaway certificate authority for tests, writing PEM files under one directory
type PKI struct {
	Dir    string
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
}

// New creates a CA valid for one year and writes dir/ca.crt.
// SEM@new: create a throwaway CA and write its certificate to a directory
func New(dir string) (*PKI, error) {
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
// SEM@new: issue and write a server certificate with DNS and IP SANs signed by the test CA
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
// SEM@new: issue and write a client-auth certificate with the given CN signed by the test CA
func (p *PKI) WriteClient(name, cn string) error {
	return p.write(name, &x509.Certificate{
		Subject:     pkix.Name{CommonName: cn},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
}

// SEM@new: sign a leaf template with the CA and write its cert and key PEM files
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

// SEM@new: write one DER blob as a PEM file, world-readable for container bind mounts
func writePEM(path, typ string, der []byte) error {
	// 0644 on purpose: see the package comment.
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o644) // #nosec G306 -- throwaway test PKI
}
```

- [ ] **Step 2: Write the failing tests**

`internal/tlsconfig/tlsconfig_test.go`:

```go
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `make test-unit name=TestLoad count1=true` then `make test-unit name=TestNATSFromEnv count1=true`
Expected: build failure (`undefined: Load`, `undefined: NATSFromEnv`).

- [ ] **Step 4: Write the implementation**

`internal/tlsconfig/tlsconfig.go`:

```go
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
// SEM@new: build a CA-pinned client TLS config, re-reading the client cert per handshake
func Load(caFile, certFile, keyFile string) (*tls.Config, error) {
	if caFile == "" {
		return nil, fmt.Errorf("tlsconfig: CA file path is empty")
	}
	caPEM, err := os.ReadFile(caFile) // #nosec G304 -- operator-controlled path
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
// TMI_NATS_TLS_* variables, or (nil, nil) when TMI_NATS_TLS_CA_FILE is unset
// (plaintext, the code default). Callers pass a non-nil result to nats.Secure.
// SEM@new: build the NATS client TLS config from TMI_NATS_TLS_* env vars; nil when unset (reads env)
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `make test-unit name='"TestLoad|TestNATSFromEnv"' count1=true`
Expected: PASS (5 tests).

- [ ] **Step 6: Lint and commit**

Run: `make lint` (fix anything it reports in the new files; the `#nosec` comments must stay on their own lines as shown).

```bash
cd /Users/efitz/Projects/tmi-pr6
git add internal/tlsconfig
git commit -m "feat(tls): add internal/tlsconfig client TLS builder and test PKI helper

CA-pinned tls.Config for the Redis and NATS clients (PR 6, T356/T388).
Client certs are re-read on every handshake so cert-manager renewals
apply on reconnect. testpki is the throwaway CA generator shared by the
unit tests and the integration harness."
```

---

### Task 2: Redis client TLS (config registry, go-redis options, `rediss://`)

**Files:**
- Modify: `internal/config/config.go` (`RedisConfig` struct ~line 138; validation ~line 1101-1115)
- Modify: `internal/config/setting_defs_server.go` (after the `database.redis.db` entry, ~line 299-307)
- Modify: `auth/db/redis.go` (`RedisConfig` ~line 18, `NewRedisDB` ~line 72-116)
- Create: `auth/db/redis_options_test.go`
- Modify: `auth/config.go` (`RedisConfig` ~line 48, env load ~line 184, `ToRedisConfig` ~line 252)
- Modify: `cmd/server/main.go` (`buildRedisConfig` ~line 2775-2804)
- Create: `cmd/server/redis_config_test.go`
- Regenerate: `config-example.yml`, `config-reference.md`

**Interfaces:**
- Consumes: `tlsconfig.Load` (Task 1), `testpki` (Task 1).
- Produces: `db.RedisConfig{TLSEnabled bool; TLSCAFile string}`; `config.RedisConfig{TLSEnabled, TLSCAFile}` with env tags `TMI_REDIS_TLS_ENABLED` / `TMI_REDIS_TLS_CA_FILE`; `db.redisOptions(cfg RedisConfig) (*redis.Options, error)` (unexported, tested in-package).

- [ ] **Step 1: Write the failing tests**

`auth/db/redis_options_test.go`:

```go
package db

import (
	"path/filepath"
	"testing"

	"github.com/ericfitz/tmi/internal/tlsconfig/testpki"
)

func TestRedisOptions_PlaintextByDefault(t *testing.T) {
	opts, err := redisOptions(RedisConfig{Host: "localhost", Port: "6379"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TLSConfig != nil {
		t.Fatal("TLS must be off unless enabled")
	}
	if opts.Addr != "localhost:6379" {
		t.Fatalf("Addr = %q", opts.Addr)
	}
}

func TestRedisOptions_TLSWithoutCAFails(t *testing.T) {
	if _, err := redisOptions(RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true}); err == nil {
		t.Fatal("TLS enabled without a CA file must be an error, never a system-pool fallback")
	}
}

func TestRedisOptions_TLSPinsCAAndServerName(t *testing.T) {
	p, err := testpki.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts, err := redisOptions(RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true, TLSCAFile: filepath.Join(p.Dir, "ca.crt")})
	if err != nil {
		t.Fatal(err)
	}
	if opts.TLSConfig == nil || opts.TLSConfig.RootCAs == nil {
		t.Fatal("TLSConfig with pinned RootCAs expected")
	}
	if opts.TLSConfig.ServerName != "redis" {
		t.Fatalf("ServerName = %q, want the configured host", opts.TLSConfig.ServerName)
	}
}
```

`cmd/server/redis_config_test.go` (package `main`, alongside the existing `*_test.go` files there):

```go
package main

import (
	"testing"

	"github.com/ericfitz/tmi/internal/config"
)

func TestBuildRedisConfig_PassesTLSFieldsThrough(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Redis = config.RedisConfig{Host: "redis", Port: "6379", TLSEnabled: true, TLSCAFile: "/etc/tmi-redis-tls/ca.crt"}
	rc := buildRedisConfig(cfg)
	if !rc.TLSEnabled || rc.TLSCAFile != "/etc/tmi-redis-tls/ca.crt" {
		t.Fatalf("TLS fields not passed through: %+v", rc)
	}
}

func TestBuildRedisConfig_RedissURLEnablesTLS(t *testing.T) {
	cfg := &config.Config{}
	cfg.Database.Redis = config.RedisConfig{URL: "rediss://:pw@redis.example:6380/2", TLSCAFile: "/ca.crt"}
	rc := buildRedisConfig(cfg)
	if !rc.TLSEnabled {
		t.Fatal("rediss:// URL must enable TLS")
	}
	if rc.Host != "redis.example" || rc.Port != "6380" || rc.Password != "pw" || rc.DB != 2 {
		t.Fatalf("URL fields not parsed: %+v", rc)
	}
	cfg.Database.Redis.URL = "redis://redis.example:6379"
	if rc := buildRedisConfig(cfg); rc.TLSEnabled {
		t.Fatal("redis:// URL must not enable TLS on its own")
	}
}
```

Add to `internal/config` (new file `internal/config/redis_tls_validate_test.go`, package `config`):

```go
package config

import (
	"strings"
	"testing"
)

func TestValidate_RedisTLSNeedsCAFile(t *testing.T) {
	c := DefaultConfig()
	c.Database.URL = "postgres://u:p@localhost:5432/db"
	c.Database.Redis.TLSEnabled = true
	c.Database.Redis.TLSCAFile = ""
	err := c.validateDatabase()
	if err == nil || !strings.Contains(err.Error(), "TMI_REDIS_TLS_CA_FILE") {
		t.Fatalf("want CA-file validation error, got %v", err)
	}
	c.Database.Redis.TLSCAFile = "/etc/tmi-redis-tls/ca.crt"
	if err := c.validateDatabase(); err != nil {
		t.Fatalf("valid TLS config rejected: %v", err)
	}
}
```

Before writing this test, confirm the real names: `rg -n "func DefaultConfig|func (c \*Config) validate" internal/config/config.go`. The validator that contains `c.Database.Redis.URL == "" && c.Database.Redis.Host == ""` (~line 1110) is the one to call; use its actual name in the test and in step 4.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name='"TestRedisOptions|TestBuildRedisConfig|TestValidate_RedisTLS"' count1=true`
Expected: build failures (`undefined: redisOptions`, unknown fields `TLSEnabled`).

- [ ] **Step 3: Config struct, registry, validation**

`internal/config/config.go`, `RedisConfig`:

```go
type RedisConfig struct {
	URL        string `yaml:"url" env:"TMI_REDIS_URL"` // Connection string URL (redis://[:password@]host:port[/db]), takes precedence over individual fields; rediss:// also enables TLS
	Host       string `yaml:"host" env:"TMI_REDIS_HOST"`
	Port       string `yaml:"port" env:"TMI_REDIS_PORT"`
	Password   string `yaml:"password" env:"TMI_REDIS_PASSWORD"`
	DB         int    `yaml:"db" env:"TMI_REDIS_DB"`
	TLSEnabled bool   `yaml:"tls_enabled" env:"TMI_REDIS_TLS_ENABLED"` // Connect with TLS; the server cert is verified against tls_ca_file only
	TLSCAFile  string `yaml:"tls_ca_file" env:"TMI_REDIS_TLS_CA_FILE"`  // PEM CA that signed the Redis server certificate (required when TLS is on)
}
```

`internal/config/setting_defs_server.go`, insert after the `database.redis.db` def (the `setting_defs_bijection_test` fails until both are present):

```go
	{
		Key:         "database.redis.tls_enabled",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Connect to Redis with TLS (a rediss:// URL also enables it); the server certificate is verified against tls_ca_file only",
		YAMLPath:    "database.redis.tls_enabled",
		EnvVar:      "TMI_REDIS_TLS_ENABLED",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Database.Redis.TLSEnabled) },
	},
	{
		Key:           "database.redis.tls_ca_file",
		Class:         bootstrapClass(false, VisibilityInternal, false),
		Type:          "string",
		Description:   "PEM CA file that signed the Redis server certificate; the only CA trusted (required when tls_enabled)",
		YAMLPath:      "database.redis.tls_ca_file",
		EnvVar:        "TMI_REDIS_TLS_CA_FILE",
		Get:           func(c *Config) string { return c.Database.Redis.TLSCAFile },
		OmitWhenEmpty: true,
	},
```

Validation: in the function holding the `c.Database.Redis.URL == "" && c.Database.Redis.Port == ""` check, add after it:

```go
	if c.Database.Redis.TLSEnabled && c.Database.Redis.TLSCAFile == "" {
		return fmt.Errorf("database.redis.tls_ca_file (TMI_REDIS_TLS_CA_FILE) is required when database.redis.tls_enabled is true")
	}
```

Update that function's `// SEM@...` line to `// SEM@new: validate database URL, Redis coordinates, and that Redis TLS names a CA file (pure)`.

- [ ] **Step 4: go-redis options builder**

`auth/db/redis.go`: add the import `"github.com/ericfitz/tmi/internal/tlsconfig"`, extend the struct, and split `NewRedisDB`:

```go
// RedisConfig holds the configuration for Redis connection
// SEM@new: Redis connection coordinates, credentials, and CA-pinned TLS settings (pure)
type RedisConfig struct {
	Host       string
	Port       string
	Password   string
	DB         int
	TLSEnabled bool   // connect with TLS, verifying the server against TLSCAFile only
	TLSCAFile  string // PEM CA file; required when TLSEnabled
}

// redisOptions builds the go-redis client options for cfg. With TLS on, the
// only trusted CA is cfg.TLSCAFile and the server name checked is cfg.Host.
// SEM@new: build go-redis client options, adding CA-pinned TLS when enabled (pure)
func redisOptions(cfg RedisConfig) (*redis.Options, error) {
	opts := &redis.Options{
		Addr:            fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password:        cfg.Password,
		DB:              cfg.DB,
		DialTimeout:     5 * time.Second,
		ReadTimeout:     3 * time.Second,
		WriteTimeout:    3 * time.Second,
		PoolSize:        10,
		MinIdleConns:    2,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 30 * time.Minute,
	}
	if !cfg.TLSEnabled {
		return opts, nil
	}
	if cfg.TLSCAFile == "" {
		return nil, fmt.Errorf("redis: TLS enabled but no CA file configured (TMI_REDIS_TLS_CA_FILE)")
	}
	tlsCfg, err := tlsconfig.Load(cfg.TLSCAFile, "", "")
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}
	tlsCfg.ServerName = cfg.Host
	opts.TLSConfig = tlsCfg
	return opts, nil
}
```

In `NewRedisDB`, replace the inline `redis.NewClient(&redis.Options{...})` with:

```go
	opts, err := redisOptions(cfg)
	if err != nil {
		logger.Error("Invalid Redis configuration: %v", err)
		return nil, err
	}
	logger.Debug("Initializing Redis connection to %s DB=%d tls=%v", opts.Addr, cfg.DB, cfg.TLSEnabled)
	client := redis.NewClient(opts)
```

(Keep the existing pool-parameter debug line, OTel instrumentation, and ping.) Update `NewRedisDB`'s SEM line: `// SEM@new: connect to Redis (optionally TLS) with OpenTelemetry instrumentation and verify liveness`.

`auth/config.go`: add `TLSEnabled bool` and `TLSCAFile string` to `RedisConfig`; in the env loader block set

```go
			TLSEnabled: envutil.Get("TMI_REDIS_TLS_ENABLED", "false") == "true",
			TLSCAFile:  envutil.Get("TMI_REDIS_TLS_CA_FILE", ""),
```

and copy both in `ToRedisConfig` (`TLSEnabled: c.Redis.TLSEnabled, TLSCAFile: c.Redis.TLSCAFile`). Update both SEM lines to `SEM@new` with the TLS words added.

`cmd/server/main.go`, replace `buildRedisConfig` (add `"strings"` to imports if missing):

```go
// buildRedisConfig creates a Redis configuration from the application config.
// If TMI_REDIS_URL is set, it takes precedence over individual fields; a
// rediss:// scheme turns TLS on (the CA file still comes from
// TMI_REDIS_TLS_CA_FILE).
// SEM@new: build a Redis connection config from URL or fields, enabling TLS for rediss URLs (pure)
func buildRedisConfig(cfg *config.Config) db.RedisConfig {
	log := slogging.Get()
	rc := db.RedisConfig{
		Host:       cfg.Database.Redis.Host,
		Port:       cfg.Database.Redis.Port,
		Password:   cfg.Database.Redis.Password,
		DB:         cfg.Database.Redis.DB,
		TLSEnabled: cfg.Database.Redis.TLSEnabled,
		TLSCAFile:  cfg.Database.Redis.TLSCAFile,
	}
	if cfg.Database.Redis.URL == "" {
		return rc
	}
	log.Info("Using TMI_REDIS_URL for Redis configuration")
	host, port, password, dbNum, err := db.ParseRedisURL(cfg.Database.Redis.URL)
	if err != nil {
		log.Error("Failed to parse TMI_REDIS_URL: %v, falling back to individual fields", err)
		return rc
	}
	rc.Host, rc.Port, rc.Password, rc.DB = host, port, password, dbNum
	if strings.HasPrefix(cfg.Database.Redis.URL, "rediss://") {
		rc.TLSEnabled = true
	}
	return rc
}
```

- [ ] **Step 5: Run the tests**

Run: `make test-unit name='"TestRedisOptions|TestBuildRedisConfig|TestValidate_RedisTLS|TestSettingDefs|TestRepoTMIEnvTokens"' count1=true`
Expected: PASS. (`TestSettingDefs*` is the struct-tag/registry bijection gate; `TestRepoTMIEnvTokens_AreAllDocumented` checks every `TMI_*` token in non-test Go code is registered.)

- [ ] **Step 6: Regenerate the config artifacts and build**

Run: `make generate-config-example && make generate-config-docs && make build-server && make lint`
Expected: `config-example.yml` gains `tls_enabled`/`tls_ca_file` under `database.redis`; `config-reference.md` gains two `database.redis.tls_*` rows. Verify: `rg -n "redis.tls" config-example.yml config-reference.md`.

- [ ] **Step 7: Commit**

```bash
cd /Users/efitz/Projects/tmi-pr6
git add internal/config auth/db auth/config.go cmd/server/main.go cmd/server/redis_config_test.go config-example.yml config-reference.md
git commit -m "feat(redis): CA-pinned TLS for the Redis client (TMI_REDIS_TLS_ENABLED, rediss://)

Adds database.redis.tls_enabled / tls_ca_file to the config registry and
db.RedisConfig; go-redis gets a tls.Config from internal/tlsconfig with
ServerName = host. A rediss:// URL also enables TLS. Defaults stay off."
```

---

### Task 3: NATS client TLS and `TMI_WORKER_NATS_URL` removal

**Files:**
- Modify: `internal/worker/nats.go` (`Connect` ~line 54-80)
- Create: `internal/worker/nats_options_test.go`
- Modify: `internal/platform/controller/jetstream_provisioner.go` (`NewNATSProvisioner` ~line 43-61)
- Modify: `cmd/worker-probe/main.go` (~line 163-169)
- Modify: `internal/config/bootstrap/bootstrap.go` (~line 40-44), `internal/config/bootstrap/bootstrap_test.go` (lines 10, 19, 33-37, 42)
- Modify: `internal/config/process_env.go` (lines 55, 64)
- Regenerate: `config-reference.md`

**Interfaces:**
- Consumes: `tlsconfig.NATSFromEnv` (Task 1).
- Produces: `worker.natsOptions(cfg Config, credsFile string, tlsCfg *tls.Config) []nats.Option` (unexported); `bootstrap.LoadWorker` now reads `TMI_NATS_URL`.

- [ ] **Step 1: Write the failing tests**

`internal/worker/nats_options_test.go`:

```go
package worker

import (
	"crypto/tls"
	"testing"

	"github.com/nats-io/nats.go"
)

func applyNATSOptions(t *testing.T, opts []nats.Option) nats.Options {
	t.Helper()
	o := nats.GetDefaultOptions()
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			t.Fatal(err)
		}
	}
	return o
}

func TestNATSOptions_PlaintextByDefault(t *testing.T) {
	o := applyNATSOptions(t, natsOptions(Config{ComponentName: "probe"}, "", nil))
	if o.Secure || o.TLSConfig != nil {
		t.Fatal("no TLS config given: Secure/TLSConfig must stay unset (plaintext default)")
	}
	if o.Name != "tmi-probe" || o.MaxReconnect != -1 {
		t.Fatalf("name/reconnect options lost: name=%q maxReconnect=%d", o.Name, o.MaxReconnect)
	}
}

func TestNATSOptions_TLS(t *testing.T) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "nats"}
	o := applyNATSOptions(t, natsOptions(Config{ComponentName: "probe"}, "", cfg))
	if !o.Secure || o.TLSConfig != cfg {
		t.Fatal("TLS config must be applied through nats.Secure")
	}
}
```

Edit `internal/config/bootstrap/bootstrap_test.go`: replace every `TMI_WORKER_NATS_URL` with `TMI_NATS_URL` (lines 10, 34, 36, 42) and the message check at line 36 accordingly.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name='"TestNATSOptions|TestLoadWorker"' count1=true`
Expected: `undefined: natsOptions`; `TestLoadWorker_MissingNATSURLFails` fails on the message text.

- [ ] **Step 3: Implement**

`internal/worker/nats.go`: add imports `"crypto/tls"` and `"github.com/ericfitz/tmi/internal/tlsconfig"`; add

```go
// natsOptions builds the connect options: client name, unlimited reconnects,
// optional credentials file, and TLS (nats.Secure) when tlsCfg is non-nil.
// SEM@new: build NATS connect options from worker config, credentials path, and optional TLS (pure)
func natsOptions(cfg Config, credsFile string, tlsCfg *tls.Config) []nats.Option {
	opts := []nats.Option{
		nats.Name("tmi-" + cfg.ComponentName),
		nats.MaxReconnects(-1),
	}
	if credsFile != "" {
		opts = append(opts, nats.UserCredentials(credsFile))
	}
	if tlsCfg != nil {
		opts = append(opts, nats.Secure(tlsCfg))
	}
	return opts
}
```

and change the top of `Connect` to:

```go
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	tlsCfg, err := tlsconfig.NATSFromEnv()
	if err != nil {
		return nil, fmt.Errorf("worker: %w", err)
	}
	nc, err := nats.Connect(cfg.NATSURL, natsOptions(cfg, os.Getenv("TMI_NATS_CREDS"), tlsCfg)...)
```

(delete the old `opts := ...`/creds block). Extend `Connect`'s doc comment with: "If TMI_NATS_TLS_CA_FILE is set (see internal/tlsconfig), the connection uses TLS with that CA and, when TMI_NATS_TLS_CERT_FILE/KEY_FILE are set, a client certificate." Update its SEM line to `// SEM@new: connect to NATS (TLS when configured), open JetStream, and ensure the payload object store`.

`internal/platform/controller/jetstream_provisioner.go` (import `tlsconfig`):

```go
func NewNATSProvisioner(url string) (*NATSProvisioner, error) {
	opts := []nats.Option{
		nats.Name("tmi-component-controller"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}
	tlsCfg, err := tlsconfig.NATSFromEnv()
	if err != nil {
		return nil, fmt.Errorf("controller: %w", err)
	}
	if tlsCfg != nil {
		opts = append(opts, nats.Secure(tlsCfg))
	}
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("controller: nats connect %s: %w", url, err)
	}
	// ... unchanged JetStream context ...
```

SEM line: `// SEM@new: connect to NATS with retry and optional TLS, returning a JetStream provisioner`. In the type comment above, change the in-cluster URL example to `tls://nats.tmi-platform.svc:4222`.

`cmd/worker-probe/main.go` step 2 (import `tlsconfig`):

```go
	// Step 2: connect to NATS via plain nats.go (see package doc for rationale).
	// TLS follows the same TMI_NATS_TLS_* contract as internal/worker.
	tlsCfg, err := tlsconfig.NATSFromEnv()
	if err != nil {
		return fmt.Errorf("NATS TLS config: %w", err)
	}
	natsOpts := []nats.Option{nats.Name("tmi-worker-probe")}
	if tlsCfg != nil {
		natsOpts = append(natsOpts, nats.Secure(tlsCfg))
	}
	nc, err := nats.Connect(wb.NATSURL, natsOpts...)
```

Update `run`'s SEM line to `// SEM@new: bootstrap, connect to NATS (TLS when configured), receive one probe job, and publish the result`.

`internal/config/bootstrap/bootstrap.go`: `TMI_WORKER_NATS_URL` → `TMI_NATS_URL` (both the `Getenv` and the error string); struct comment `NATSURL is the JetStream connection URL (env TMI_NATS_URL, shared with internal/worker).`; `LoadWorker` SEM line → `SEM@new`.

`internal/config/process_env.go`: delete the `TMI_WORKER_NATS_URL` row; change the `TMI_NATS_URL` purpose to `"NATS endpoint (tls:// or nats://). Required by every worker (internal/worker and internal/config/bootstrap); also read by the server (extraction wiring) and component-controller (JetStream provisioning)"`; add after `TMI_NATS_CREDS`:

```go
	{Name: "TMI_NATS_TLS_CA_FILE", Binary: "workers", Purpose: "PEM CA that signed the NATS server certificate; setting it turns on TLS for every NATS client (workers, server, component-controller). See internal/tlsconfig"},
	{Name: "TMI_NATS_TLS_CERT_FILE", Binary: "workers", Purpose: "Client certificate presented to NATS (mTLS); re-read on every handshake so renewals apply on reconnect"},
	{Name: "TMI_NATS_TLS_KEY_FILE", Binary: "workers", Purpose: "Private key for TMI_NATS_TLS_CERT_FILE (the file is secret; the path is not)"},
	{Name: "TMI_NATS_TLS_SERVER_NAME", Binary: "workers", Purpose: "Hostname verified against the NATS server certificate when TMI_NATS_URL carries an IP (the extractor dials the ClusterIP); defaults to the URL host"},
```

- [ ] **Step 4: Run the tests, regenerate docs, build**

Run: `make test-unit name='"TestNATSOptions|TestLoadWorker|TestProcessEnvVars|TestRepoTMIEnvTokens"' count1=true && make generate-config-docs && make build-server && make lint`
Expected: PASS; `rg -n "TMI_WORKER_NATS_URL" --glob '!docs/**' --glob '!graphify-out/**' .` returns only `test/integration/workflows/worker_probe_integration_test.go:123` (fixed in Task 7).

- [ ] **Step 5: Commit**

```bash
cd /Users/efitz/Projects/tmi-pr6
git add internal/worker internal/platform/controller/jetstream_provisioner.go cmd/worker-probe/main.go internal/config/bootstrap internal/config/process_env.go config-reference.md
git commit -m "feat(nats): mTLS for every NATS client; TMI_NATS_URL replaces TMI_WORKER_NATS_URL

worker.Connect, the controller's JetStream provisioner and worker-probe
all apply nats.Secure from tlsconfig.NATSFromEnv (TMI_NATS_TLS_*). The
worker bootstrap now reads the same TMI_NATS_URL as everything else."
```

---

### Task 4: Worker Deployment renderer: client-cert volume, env, Reloader annotation

**Files:**
- Modify: `internal/platform/controller/render_deployment.go`
- Modify: `internal/platform/controller/render_deployment_test.go` (`TestRenderDeployment_ScratchVolumeWhenRequested` + new test)
- Modify: `internal/platform/controller/tmicomponent_controller.go` (`liveSatisfies` ~line 177-190)
- Modify: `internal/platform/controller/tmicomponent_apply_test.go` (new case)

**Interfaces:**
- Consumes: Global Constraints names (`/etc/tmi-nats-tls`, `nats-client-<name minus tmi->`, `reloader.stakater.com/auto`).
- Produces: every TMIComponent Deployment carries volume `nats-client-tls` (Secret, optional), mount `/etc/tmi-nats-tls` (read-only), env `TMI_NATS_TLS_CA_FILE|CERT_FILE|KEY_FILE`, and the annotation. No controller RBAC change: the kubelet resolves Secret volumes; the controller only writes the PodSpec (spec §3 asked to check).

- [ ] **Step 1: Write the failing tests**

Append to `render_deployment_test.go`:

```go
func TestRenderDeployment_NATSClientTLS(t *testing.T) {
	d := RenderDeployment(deployComp())
	if d.Annotations["reloader.stakater.com/auto"] != "true" {
		t.Fatal("worker Deployment must carry reloader.stakater.com/auto=true so a renewed cert Secret rolls the pods")
	}
	pod := d.Spec.Template.Spec
	var vol *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == "nats-client-tls" {
			vol = &pod.Volumes[i]
		}
	}
	if vol == nil || vol.Secret == nil {
		t.Fatal("nats-client-tls Secret volume missing")
	}
	if vol.Secret.SecretName != "nats-client-extractor" {
		t.Fatalf("secret name = %q, want nats-client-extractor (component tmi-extractor)", vol.Secret.SecretName)
	}
	if vol.Secret.Optional == nil || !*vol.Secret.Optional {
		t.Fatal("volume must be optional so a component that never dials NATS still schedules")
	}
	m, ok := volumeMountByPath(pod.Containers[0], "/etc/tmi-nats-tls")
	if !ok || !m.ReadOnly || m.Name != "nats-client-tls" {
		t.Fatalf("client cert must be mounted read-only at /etc/tmi-nats-tls, got %+v", m)
	}
	want := map[string]string{
		"TMI_NATS_TLS_CA_FILE":   "/etc/tmi-nats-tls/ca.crt",
		"TMI_NATS_TLS_CERT_FILE": "/etc/tmi-nats-tls/tls.crt",
		"TMI_NATS_TLS_KEY_FILE":  "/etc/tmi-nats-tls/tls.key",
	}
	for _, e := range pod.Containers[0].Env {
		if v, ok := want[e.Name]; ok && v == e.Value {
			delete(want, e.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing NATS TLS env vars: %v", want)
	}
}

func TestRenderDeployment_ScratchAndTLSVolumesCoexist(t *testing.T) {
	c := deployComp()
	c.Spec.ScratchVolume = &platformv1alpha1.ScratchVolume{MountPath: "/scratch", SizeLimit: resource.MustParse("256Mi")}
	pod := RenderDeployment(c).Spec.Template.Spec
	if len(pod.Volumes) != 2 {
		t.Fatalf("want scratch + nats-client-tls volumes, got %d", len(pod.Volumes))
	}
}
```

In `TestRenderDeployment_ScratchVolumeWhenRequested`, replace the `len(pod.Volumes) != 1 || pod.Volumes[0].EmptyDir == nil` check with a lookup by name:

```go
	var scratch *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == "scratch" {
			scratch = &pod.Volumes[i]
		}
	}
	if scratch == nil || scratch.EmptyDir == nil {
		t.Fatal("scratchVolume must render an emptyDir volume named scratch")
	}
	if scratch.EmptyDir.SizeLimit == nil {
```

Append to `tmicomponent_apply_test.go` (pure, no envtest):

```go
func TestLiveSatisfies_RequiresReloaderAnnotation(t *testing.T) {
	rendered := RenderDeployment(deployComp())
	live := rendered.DeepCopy()
	if !liveSatisfies(rendered, live) {
		t.Fatal("identical objects must satisfy")
	}
	delete(live.Annotations, "reloader.stakater.com/auto")
	if liveSatisfies(rendered, live) {
		t.Fatal("a live Deployment missing the Reloader annotation must be updated")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name='"TestRenderDeployment|TestLiveSatisfies"' count1=true`
Expected: `TestRenderDeployment_NATSClientTLS`, `_ScratchAndTLSVolumesCoexist`, `TestLiveSatisfies_RequiresReloaderAnnotation` FAIL.

- [ ] **Step 3: Implement**

`render_deployment.go`: add import `"strings"` and

```go
const (
	// natsTLSMountPath is where every worker finds its NATS client cert
	// (ca.crt, tls.crt, tls.key from the cert-manager Secret).
	natsTLSMountPath  = "/etc/tmi-nats-tls"
	natsTLSVolumeName = "nats-client-tls"
	// reloaderAutoAnnotation makes Stakater Reloader roll the Deployment when
	// any Secret/ConfigMap it mounts changes (cert renewals included).
	reloaderAutoAnnotation = "reloader.stakater.com/auto"
)

// natsClientSecretName maps a component to its cert-manager client-cert
// Secret: nats-client-<name without the tmi- prefix>, e.g. tmi-extractor ->
// nats-client-extractor, matching deployments/k8s/platform/pki.yml.
// SEM@new: derive the NATS client-cert Secret name for a component (pure)
func natsClientSecretName(c *platformv1alpha1.TMIComponent) string {
	return "nats-client-" + strings.TrimPrefix(c.Name, "tmi-")
}

// natsTLSEnv points internal/worker.Connect (via tlsconfig.NATSFromEnv) at
// the mounted client cert.
// SEM@new: build the TMI_NATS_TLS_* env vars for the mounted client cert (pure)
func natsTLSEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "TMI_NATS_TLS_CA_FILE", Value: natsTLSMountPath + "/ca.crt"},
		{Name: "TMI_NATS_TLS_CERT_FILE", Value: natsTLSMountPath + "/tls.crt"},
		{Name: "TMI_NATS_TLS_KEY_FILE", Value: natsTLSMountPath + "/tls.key"},
	}
}
```

In `RenderDeployment`: `env = append(env, natsTLSEnv()...)` after `secretEnv`; change the scratch block to `append` instead of assigning (`pod.Volumes = append(pod.Volumes, ...)`, `pod.Containers[0].VolumeMounts = append(...)`), then add after it:

```go
	// Optional: a component that never dials NATS (the e2e static-image probe)
	// still schedules; one that does dial fails loudly in tlsconfig.Load on
	// the missing files rather than silently falling back to plaintext.
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name: natsTLSVolumeName,
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: natsClientSecretName(c),
			Optional:   boolPtr(true),
		}},
	})
	pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{
		Name: natsTLSVolumeName, MountPath: natsTLSMountPath, ReadOnly: true,
	})
```

and set `Annotations: map[string]string{reloaderAutoAnnotation: "true"}` in the Deployment's `ObjectMeta`. SEM line → `// SEM@new: build a hardened worker Deployment with NATS client-cert mount and Reloader annotation (pure)`.

`tmicomponent_controller.go`, `liveSatisfies` Deployment case:

```go
	case *appsv1.Deployment:
		lv, ok := live.(*appsv1.Deployment)
		return ok && apiequality.Semantic.DeepDerivative(d.Spec, lv.Spec) &&
			lv.Annotations[reloaderAutoAnnotation] == d.Annotations[reloaderAutoAnnotation]
```

SEM line → `// SEM@new: report whether a live object already matches the rendered spec and Reloader annotation (pure)`.

- [ ] **Step 4: Run the tests**

Run: `make test-unit name='"TestRenderDeployment|TestLiveSatisfies|TestApply"' count1=true && make lint`
Expected: PASS (the `TestApply_*` cases use envtest; if they skip for missing assets that is pre-existing, not a regression).

- [ ] **Step 5: Commit**

```bash
cd /Users/efitz/Projects/tmi-pr6
git add internal/platform/controller
git commit -m "feat(controller): mount the NATS client cert and Reloader annotation on worker Deployments

Every TMIComponent Deployment now mounts Secret nats-client-<name> at
/etc/tmi-nats-tls (optional), sets TMI_NATS_TLS_*, and carries
reloader.stakater.com/auto so renewals roll the pods."
```

---

### Task 5: Platform PKI (cert-manager, Reloader, ClusterIssuers, Certificates) and apply order

**Files:**
- Create: `deployments/k8s/platform/cert-manager.yml` (vendored), `deployments/k8s/platform/reloader.yml` (vendored), `deployments/k8s/platform/pki.yml`
- Modify: `scripts/lib/deploy.py` (`apply_platform_base` line 429-433)
- Modify: `scripts/deploy-aws.sh` (`apply_platform_base` line 841-856)
- Modify: `Makefile` (`e2e-platform-up` line 828-834)

**Interfaces:**
- Produces: Secrets listed in Global Constraints, each with `ca.crt`, `tls.crt`, `tls.key`; ClusterIssuers `tmi-bootstrap`, `tmi-internal`; namespaces `cert-manager`, `reloader`.

- [ ] **Step 1: Vendor cert-manager and Reloader**

```bash
cd /Users/efitz/Projects/tmi-pr6
{ printf '# Vendored from: https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.yaml\n# Vendored on: 2026-09-28\n# DO NOT EDIT: to update, change the URL/version and re-fetch.\n'; curl -fsSL https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.yaml; } > deployments/k8s/platform/cert-manager.yml
{ printf '# Vendored from: https://raw.githubusercontent.com/stakater/Reloader/v1.4.22/deployments/kubernetes/reloader.yaml\n# Vendored on: 2026-09-28\n# Rewritten at vendor time (the upstream static manifest installs into the\n# default namespace): "namespace: default" -> "namespace: reloader", and a\n# Namespace object prepended. DO NOT EDIT by hand: to update, change the\n# URL/version, re-fetch, and re-apply the same sed.\napiVersion: v1\nkind: Namespace\nmetadata:\n  name: reloader\n---\n'; curl -fsSL https://raw.githubusercontent.com/stakater/Reloader/v1.4.22/deployments/kubernetes/reloader.yaml | sed 's/namespace: default/namespace: reloader/'; } > deployments/k8s/platform/reloader.yml
rg -c "namespace: default" deployments/k8s/platform/reloader.yml; rg -n "image:" deployments/k8s/platform/reloader.yml; rg -c "^kind: CustomResourceDefinition" deployments/k8s/platform/cert-manager.yml
```

Expected: `0` (or no match), `image: ghcr.io/stakater/reloader:v1.4.22`, `6`.

- [ ] **Step 2: Write `pki.yml`**

```yaml
# Internal PKI for Redis/NATS TLS (PR 6, T356/T388). Needs cert-manager
# (cert-manager.yml) and is applied by scripts/lib/deploy.py and
# scripts/deploy-aws.sh after the cert-manager webhook is Available.
#
# The CA private key exists only in Secret cert-manager/tmi-internal-ca:
# never in Terraform state or on disk. CA rotation is manual (5-year cert);
# runbook in #965: trust old+new CA, reissue leaves, drop the old CA.
#
# Every leaf Secret carries ca.crt, tls.crt and tls.key, so one mount gives
# a pod both its identity and its trust anchor. Namespace tmi-platform must
# already exist (deploy.py ensure_namespace / Terraform on AWS).
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: tmi-bootstrap
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: tmi-internal-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: tmi-internal-ca
  secretName: tmi-internal-ca
  duration: 43800h # 5 years
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: tmi-bootstrap
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: tmi-internal
spec:
  ca:
    secretName: tmi-internal-ca
---
# ---- server certificates (90 days, renewed 30 days early) -------------------
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: redis-tls
  namespace: tmi-platform
spec:
  secretName: redis-tls
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, server auth]
  dnsNames:
    - redis
    - redis.tmi-platform.svc
    - redis.tmi-platform.svc.cluster.local
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-tls
  namespace: tmi-platform
spec:
  secretName: nats-tls
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, server auth]
  dnsNames:
    - nats
    - nats.tmi-platform.svc
    - nats.tmi-platform.svc.cluster.local
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
---
# ---- NATS client certificates (mTLS); CN = component name -------------------
# Secret name = nats-client-<component without the tmi- prefix>; the
# TMIComponent controller derives it the same way (render_deployment.go).
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-client-server
  namespace: tmi-platform
spec:
  secretName: nats-client-server
  commonName: server
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, client auth]
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-client-controller
  namespace: tmi-platform
spec:
  secretName: nats-client-controller
  commonName: controller
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, client auth]
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-client-extractor
  namespace: tmi-platform
spec:
  secretName: nats-client-extractor
  commonName: extractor
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, client auth]
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-client-chunk-embed
  namespace: tmi-platform
spec:
  secretName: nats-client-chunk-embed
  commonName: chunk-embed
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, client auth]
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: nats-client-worker-probe
  namespace: tmi-platform
spec:
  secretName: nats-client-worker-probe
  commonName: worker-probe
  duration: 2160h
  renewBefore: 720h
  privateKey: { algorithm: ECDSA, size: 256 }
  usages: [digital signature, client auth]
  issuerRef: { name: tmi-internal, kind: ClusterIssuer }
```

Syntax check (no cluster needed): `uv run --with pyyaml python -c "import yaml; docs=list(yaml.safe_load_all(open('deployments/k8s/platform/pki.yml'))); print(len(docs))"` → `10`.

- [ ] **Step 3: `deploy.py` apply order**

Replace `apply_platform_base` in `scripts/lib/deploy.py` (imports `sys` and `time` already exist; `log_warn`/`log_error` are defined in the module):

```python
CERT_MANAGER_DEPLOYMENTS = ("cert-manager", "cert-manager-cainjector", "cert-manager-webhook")


def apply_platform_base() -> None:
    """Apply the cluster-wide platform in dependency order: cert-manager and
    Reloader, the internal PKI, then NATS, KEDA and the TMIComponent CRD.

    cert-manager's webhook admits every cert-manager.io object, so pki.yml
    waits for the three cert-manager Deployments; even then the webhook's
    serving cert (injected by cainjector) can lag a few seconds, hence the
    retry. NATS mounts nats-tls and the workloads mount the client certs, so
    every Certificate must be Ready before anything that uses one is applied.
    Namespace tmi-platform must already exist (ensure_namespace())."""
    project_root = get_project_root()
    platform = project_root / PLATFORM_DIR
    kubectl(["apply", "--server-side", "-f", str(platform / "cert-manager.yml")])
    for dep in CERT_MANAGER_DEPLOYMENTS:
        kubectl(["-n", "cert-manager", "rollout", "status", f"deploy/{dep}", "--timeout=180s"])
    kubectl(["apply", "-f", str(platform / "reloader.yml")])
    _apply_with_retry(str(platform / "pki.yml"))
    kubectl(["-n", "cert-manager", "wait", "--for=condition=Ready", "certificate/tmi-internal-ca", "--timeout=120s"])
    kubectl(["-n", NS, "wait", "--for=condition=Ready", "certificate", "--all", "--timeout=180s"])
    kubectl(["apply", "-f", str(platform / "nats.yml")])
    kubectl(["apply", "--server-side", "-f", str(platform / "keda.yml")])
    kubectl(["apply", "-f", str(project_root / "config/crd/bases/tmi.dev_tmicomponents.yaml")])
    log_success("Platform base applied (cert-manager, Reloader, PKI, NATS, KEDA, CRD)")


def _apply_with_retry(path: str, attempts: int = 5, delay_s: float = 3.0) -> None:
    """kubectl apply with retries, for objects admitted by a webhook that may
    still be warming up (cert-manager right after its rollout)."""
    for attempt in range(1, attempts + 1):
        result = kubectl(["apply", "-f", path], check=False, capture=True)
        if result.returncode == 0:
            return
        if attempt == attempts:
            log_error(f"kubectl apply -f {path} failed after {attempts} attempts:\n{result.stderr}")
            sys.exit(1)
        log_warn(f"kubectl apply -f {path} failed (attempt {attempt}/{attempts}); retrying in {delay_s:.0f}s")
        time.sleep(delay_s)
```

Confirm `run_cmd(..., check=False, capture=True)` returns a `CompletedProcess` (`rg -n "def run_cmd" -A15 scripts/lib/tmi_common.py`); if it returns something else, adapt the `returncode`/`stderr` reads.

- [ ] **Step 4: `deploy-aws.sh` apply order**

Replace `apply_platform_base` in `scripts/deploy-aws.sh`:

```bash
apply_platform_base() {
    log_step "Phase 4: Platform Base (cert-manager, Reloader, PKI, NATS, KEDA, TMIComponent CRD)"

    # Re-asserted here rather than trusting the check in configure_kubeconfig:
    # this is the first mutating apply, and the two are separated by the image
    # build/push phase, which takes long enough for the environment to change.
    assert_cluster_identity

    # Mirrors apply_platform_base() in scripts/lib/deploy.py; keep in sync.
    kubectl apply --server-side -f "${PLATFORM_DIR}/cert-manager.yml"
    local dep
    for dep in cert-manager cert-manager-cainjector cert-manager-webhook; do
        kubectl -n cert-manager rollout status "deploy/${dep}" --timeout=180s
    done
    kubectl apply -f "${PLATFORM_DIR}/reloader.yml"
    # The webhook's serving cert can lag its rollout by a few seconds.
    local attempt
    for attempt in 1 2 3 4 5; do
        if kubectl apply -f "${PLATFORM_DIR}/pki.yml"; then break; fi
        if [[ "${attempt}" == "5" ]]; then log_error "pki.yml apply failed after 5 attempts"; exit 1; fi
        log_warning "pki.yml apply failed (attempt ${attempt}/5); retrying in 3s"
        sleep 3
    done
    kubectl -n cert-manager wait --for=condition=Ready certificate/tmi-internal-ca --timeout=120s
    kubectl -n "${NAMESPACE}" wait --for=condition=Ready certificate --all --timeout=180s
    kubectl apply -f "${PLATFORM_DIR}/nats.yml"
    kubectl apply --server-side -f "${PLATFORM_DIR}/keda.yml"
    kubectl apply -f "${PROJECT_ROOT}/config/crd/bases/tmi.dev_tmicomponents.yaml"

    log_success "Platform base applied"
}
```

Also update the header comment of `deployments/k8s/dev/aws/kustomization.yaml` (`NATS + KEDA + the TMIComponent CRD are applied by scripts/deploy-aws.sh before this overlay`) to list cert-manager, Reloader and the PKI too.

- [ ] **Step 5: kind e2e harness**

In `Makefile` `e2e-platform-up`, replace the three `kubectl ... apply` lines after the node wait with:

```make
	kubectl --context kind-tmi-platform apply --server-side -f deployments/k8s/platform/cert-manager.yml
	for d in cert-manager cert-manager-cainjector cert-manager-webhook; do kubectl --context kind-tmi-platform -n cert-manager rollout status deploy/$$d --timeout=180s; done
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/reloader.yml
	kubectl --context kind-tmi-platform create namespace tmi-platform --dry-run=client -o yaml | kubectl --context kind-tmi-platform apply -f -
	for i in 1 2 3 4 5; do kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/pki.yml && break; sleep 3; done
	kubectl --context kind-tmi-platform -n cert-manager wait --for=condition=Ready certificate/tmi-internal-ca --timeout=120s
	kubectl --context kind-tmi-platform -n tmi-platform wait --for=condition=Ready certificate --all --timeout=180s
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/nats.yml
	kubectl --context kind-tmi-platform apply --server-side -f deployments/k8s/platform/keda.yml
	kubectl --context kind-tmi-platform apply -f config/crd/bases/tmi.dev_tmicomponents.yaml
```

and update the target's `##` help text to `(cert-manager, Reloader, PKI, NATS, KEDA, CRD)`.

- [ ] **Step 6: Verify the scripts still parse**

Run: `uv run python -c "import ast,sys; ast.parse(open('scripts/lib/deploy.py').read())" && bash -n scripts/deploy-aws.sh && make -n e2e-platform-up >/dev/null && make lint`
Expected: no output from the parsers; `make lint` clean.

- [ ] **Step 7: Commit**

```bash
cd /Users/efitz/Projects/tmi-pr6
git add deployments/k8s/platform/cert-manager.yml deployments/k8s/platform/reloader.yml deployments/k8s/platform/pki.yml scripts/lib/deploy.py scripts/deploy-aws.sh Makefile deployments/k8s/dev/aws/kustomization.yaml
git commit -m "feat(platform): vendor cert-manager and Reloader; add the internal PKI

Self-signed bootstrap ClusterIssuer -> 5-year ECDSA CA (key only in the
cert-manager/tmi-internal-ca Secret) -> CA ClusterIssuer signing 90-day
server certs for Redis/NATS and NATS client certs per component.
deploy.py, deploy-aws.sh and e2e-platform-up apply cert-manager, wait
for its webhook, apply the PKI, wait for Ready certs, then NATS/KEDA."
```

---

### Task 6: Redis and NATS servers in TLS mode; every workload manifest as a TLS client

**Files:**
- Modify: `deployments/k8s/dev/redis.yml`, `deployments/k8s/platform/nats.yml`
- Modify: `deployments/k8s/dev/server.yml`, `deployments/k8s/dev/server-oracle.yml`, `deployments/k8s/dev/controller.yml`
- Modify: `deployments/k8s/dev/aws/kustomization.yaml`, `deployments/k8s/dev/aws/patches/server-config.yaml`, `deployments/k8s/dev/aws/README.md`; Delete: `deployments/k8s/dev/aws/patches/redis-auth.yaml`
- Modify: `deployments/k8s/platform/components/tmi-extractor.yml`, `tmi-chunk-embed.yml`; `test/e2e/platform/workers_e2e_test.go:135`
- Modify: `deployments/k8s/dev/networkpolicy.yml` (comment line 40), `deployments/k8s/dev/networkpolicy-redis.yml` (comment lines 3-6)
- Modify: `scripts/lib/deploy.py` (`start()`; new `ensure_redis_password_secret`)
- Modify: `scripts/cats-prep.py` (`_redis_cli_shell` line 246-267)
- Modify: `terraform/modules/kubernetes/aws/k8s_resources.tf:115-119`

**Interfaces:**
- Consumes: Secrets from Task 5; env var contract from Tasks 2-3; renderer from Task 4.
- Produces: no plaintext 6379/4222 listener anywhere; `tmi-secrets/TMI_REDIS_PASSWORD` on dev clusters.

- [ ] **Step 1: Redis base manifest**

Replace the Deployment in `deployments/k8s/dev/redis.yml` (Service unchanged):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: tmi-platform
  annotations:
    # Roll ONLY when the TLS Secret is re-issued (cert-manager renewal, about
    # every 60 days). Not `auto`: that would also roll on every tmi-secrets
    # change (#965 rotations) and wipe the in-memory sessions each time.
    secret.reloader.stakater.com/reload: "redis-tls"
spec:
  replicas: 1
  selector:
    matchLabels: { app: redis }
  template:
    metadata:
      labels: { app: redis }
    spec:
      containers:
        - name: redis
          image: cgr.dev/chainguard/redis:latest
          # TLS only: --port 0 closes the plaintext listener, clients verify
          # the server cert (redis-tls, SANs redis / redis.tmi-platform.svc /
          # ...svc.cluster.local); the server does not require client certs,
          # the password does that job (PR 6, T388).
          #
          # $(REDIS_PASSWORD) is expanded by the kubelet from the container's
          # own env, not by a shell: the literal string is what appears in
          # the PodSpec; the value is visible only in the container's process
          # table, which is inherent to --requirepass. --protected-mode no is
          # kept: with requirepass it is redundant, and enabling it would
          # reject the server pod (another pod IP, not loopback).
          args:
            - "--save"
            - ""
            - "--appendonly"
            - "no"
            - "--bind"
            - "0.0.0.0"
            - "--protected-mode"
            - "no"
            - "--port"
            - "0"
            - "--tls-port"
            - "6379"
            - "--tls-cert-file"
            - "/tls/tls.crt"
            - "--tls-key-file"
            - "/tls/tls.key"
            - "--tls-ca-cert-file"
            - "/tls/ca.crt"
            - "--tls-auth-clients"
            - "no"
            - "--requirepass"
            - "$(REDIS_PASSWORD)"
          env:
            - name: REDIS_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: tmi-secrets
                  key: TMI_REDIS_PASSWORD
          ports:
            - containerPort: 6379
          volumeMounts:
            - name: tls
              mountPath: /tls
              readOnly: true
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { cpu: 500m, memory: 256Mi }
      volumes:
        - name: tls
          secret:
            secretName: redis-tls
```

- [ ] **Step 2: NATS manifest**

In `deployments/k8s/platform/nats.yml`: ConfigMap `nats.conf` becomes

```
    jetstream {
      store_dir: "/data/jetstream"
      max_memory_store: 256MB
      max_file_store: 2GB
    }
    # Client port 4222: TLS with client-cert verification (mTLS, PR 6/T356).
    # Every client presents a cert from the tmi-internal CA (pki.yml).
    # Monitoring (8222) stays plain HTTP for KEDA's nats-jetstream scaler;
    # the nats-allow-clients NetworkPolicy restricts who can reach it.
    tls {
      cert_file: "/tls/tls.crt"
      key_file: "/tls/tls.key"
      ca_file: "/tls/ca.crt"
      verify: true
      timeout: 2
    }
    http: 8222
```

StatefulSet: add `annotations: { secret.reloader.stakater.com/reload: "nats-tls" }` under `metadata` (with a comment: a roll re-reads the renewed cert; JetStream is an emptyDir and is lost on any restart, as today), add `- { name: tls, mountPath: /tls, readOnly: true }` to `volumeMounts` and `- name: tls\n          secret: { secretName: nats-tls }` to `volumes`.

- [ ] **Step 3: Server, controller manifests (base)**

`deployments/k8s/dev/server.yml`: add under `metadata` of the Deployment

```yaml
  annotations:
    # Reloader rolls the server (Recreate strategy: a few seconds of downtime)
    # when any mounted Secret/ConfigMap changes, including cert renewals.
    reloader.stakater.com/auto: "true"
```

Replace the two env lines `TMI_REDIS_HOST` / `TMI_NATS_URL` with

```yaml
            - { name: TMI_REDIS_HOST, value: "redis" }
            # Redis is TLS-only with a password (PR 6). tmi-secrets is created
            # by deploy.py on dev clusters (random password, never printed) and
            # by Terraform on AWS.
            - name: TMI_REDIS_PASSWORD
              valueFrom:
                secretKeyRef: { name: tmi-secrets, key: TMI_REDIS_PASSWORD }
            - { name: TMI_REDIS_TLS_ENABLED, value: "true" }
            - { name: TMI_REDIS_TLS_CA_FILE, value: "/etc/tmi-redis-tls/ca.crt" }
            # NATS is mTLS; the client cert is the cert-manager Secret
            # nats-client-server mounted below.
            - { name: TMI_NATS_URL, value: "tls://nats.tmi-platform.svc:4222" }
            - { name: TMI_NATS_TLS_CA_FILE, value: "/etc/tmi-nats-tls/ca.crt" }
            - { name: TMI_NATS_TLS_CERT_FILE, value: "/etc/tmi-nats-tls/tls.crt" }
            - { name: TMI_NATS_TLS_KEY_FILE, value: "/etc/tmi-nats-tls/tls.key" }
```

Add to `volumeMounts`:

```yaml
            - name: redis-tls
              mountPath: /etc/tmi-redis-tls
              readOnly: true
            - name: nats-client-tls
              mountPath: /etc/tmi-nats-tls
              readOnly: true
```

and to `volumes`:

```yaml
        - name: redis-tls
          secret:
            secretName: redis-tls
            items: [{ key: ca.crt, path: ca.crt }] # trust anchor only; the server key never reaches a client
        - name: nats-client-tls
          secret:
            secretName: nats-client-server
```

Apply the identical annotation, env, volumeMounts and volumes edits to `deployments/k8s/dev/server-oracle.yml` (lines 57-58, 98, 145; the two manifests have a keep-in-sync contract).

`deployments/k8s/dev/controller.yml`: Deployment `metadata.annotations: { reloader.stakater.com/auto: "true" }`; env becomes

```yaml
            - name: TMI_NATS_URL
              value: tls://nats.tmi-platform.svc:4222
            - { name: TMI_NATS_TLS_CA_FILE, value: "/etc/tmi-nats-tls/ca.crt" }
            - { name: TMI_NATS_TLS_CERT_FILE, value: "/etc/tmi-nats-tls/tls.crt" }
            - { name: TMI_NATS_TLS_KEY_FILE, value: "/etc/tmi-nats-tls/tls.key" }
```

plus `volumeMounts: [{ name: nats-client-tls, mountPath: /etc/tmi-nats-tls, readOnly: true }]` on the container and `volumes: [{ name: nats-client-tls, secret: { secretName: nats-client-controller } }]` on the pod (keep the existing comment about why `TMI_NATS_URL` must be set).

- [ ] **Step 4: AWS overlay**

- `git rm deployments/k8s/dev/aws/patches/redis-auth.yaml` and delete its `patches:` entry in `deployments/k8s/dev/aws/kustomization.yaml`.
- `deployments/k8s/dev/aws/patches/server-config.yaml`: the env list is `$patch: replace`, so add the same seven entries as in step 3 (`TMI_REDIS_TLS_ENABLED`, `TMI_REDIS_TLS_CA_FILE`, `TMI_NATS_URL` → `tls://...`, the three `TMI_NATS_TLS_*`); `TMI_REDIS_PASSWORD` is already there. No volume edits: `volumeMounts`/`volumes` are strategic-merged by `name`, so the base `server.yml` entries (`redis-tls`, `nats-client-tls`) survive alongside the patch's `tls` server-cert entry (confirm in step 8: the aws render shows all three). Rewrite the `TMI_REDIS_PASSWORD` comment paragraph: requirepass now lives in the base `redis.yml` for every environment; local dev gets the Secret from `deploy.py`.
- `deployments/k8s/dev/aws/README.md`: update the "Redis authentication" paragraph (line ~132) and the mention near line 162 to say the password and TLS are base behaviour (PR 6), the patch file is gone, and the cert Secrets come from `pki.yml`.

- [ ] **Step 5: Worker components and e2e YAML**

`deployments/k8s/platform/components/tmi-extractor.yml`:

```yaml
    TMI_NATS_URL: tls://$(NATS_SERVICE_HOST):4222
    # The URL carries an IP, so tell the TLS client which SAN to verify
    # (nats-tls is issued for nats / nats.tmi-platform.svc / ...cluster.local).
    TMI_NATS_TLS_SERVER_NAME: nats
```

`tmi-chunk-embed.yml` and `test/e2e/platform/workers_e2e_test.go:135`: `TMI_NATS_URL: tls://nats.tmi-platform.svc:4222`.

- [ ] **Step 6: NetworkPolicy comments, cats-prep, Terraform**

- `networkpolicy.yml:40`: `# NATS speaks mTLS (PR 6); this policy is the network-layer control on top of it.`
- `networkpolicy-redis.yml:3-4`: `Defence in depth alongside the TLS + --requirepass in ../redis.yml (base, every environment since PR 6):`.
- `scripts/cats-prep.py` `_redis_cli_shell`: both branches get `--tls --cacert /tls/ca.crt` right after `redis-cli` (the container mounts the redis-tls Secret at /tls); update the docstring: every cluster's Redis is TLS + password now.
- `terraform/modules/kubernetes/aws/k8s_resources.tf:119`: `TMI_NATS_URL = "tls://nats.tmi-platform.svc:4222"` and amend the comment above it (`NATS runs in-cluster with mTLS; the client cert env vars are set by the overlay patch`). Do not run `terraform apply`; Eric plans at the deferred AWS deploy.

- [ ] **Step 7: `deploy.py`: dev-cluster Redis password Secret**

Add to `scripts/lib/deploy.py` (imports: add `import secrets` and `import tempfile`):

```python
def ensure_redis_password_secret() -> None:
    """Create Secret/tmi-secrets with a random TMI_REDIS_PASSWORD on a dev
    cluster if it does not exist. AWS gets this Secret from Terraform; since
    PR 6 the base redis.yml/server.yml read it in every environment.

    The value is written to a 0600 file in a private temp dir and handed to
    kubectl with --from-file, so it never appears on a command line, in the
    environment, in a log, or on this script's stdout."""
    if kubectl(["-n", NS, "get", "secret", "tmi-secrets"], check=False, capture=True).returncode == 0:
        return
    old_umask = os.umask(0o077)
    try:
        with tempfile.TemporaryDirectory() as tmp:
            pw_file = Path(tmp) / "TMI_REDIS_PASSWORD"
            pw_file.write_text(secrets.token_urlsafe(32))
            kubectl(["-n", NS, "create", "secret", "generic", "tmi-secrets",
                     f"--from-file=TMI_REDIS_PASSWORD={pw_file}"], capture=True)
    finally:
        os.umask(old_umask)
    log_success("Secret/tmi-secrets created with a random TMI_REDIS_PASSWORD")
```

Call it in `start()` right after `ensure_namespace()`. `dev-nuke` deletes the namespace, so the next `start()` regenerates it (Redis has no persistence; nothing to migrate).

- [ ] **Step 8: Verify every overlay renders with TLS on and no plaintext leftovers**

```bash
cd /Users/efitz/Projects/tmi-pr6
for o in docker-desktop docker-desktop-oracle k3s aws; do
  echo "== $o"
  kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/$o > /tmp/pr6-$o.yml
  rg -c -- '--tls-port' /tmp/pr6-$o.yml            # 1
  rg -c 'secretName: redis-tls' /tmp/pr6-$o.yml     # 2 (redis + server CA mount)
  rg -c 'secretName: nats-client-server' /tmp/pr6-$o.yml   # 1
  rg -c 'secretName: nats-client-controller' /tmp/pr6-$o.yml   # 1
  rg -c 'tls://' /tmp/pr6-$o.yml                    # 4 (server, controller, extractor, chunk-embed)
  rg -n 'nats://' /tmp/pr6-$o.yml || echo "no plaintext nats URLs"
  rg -c 'reloader.stakater.com' /tmp/pr6-$o.yml     # 3 (redis, server, controller)
done
uv run --with pyyaml python -c "import yaml; list(yaml.safe_load_all(open('deployments/k8s/platform/nats.yml')))"
rg -n "TMI_WORKER_NATS_URL|redis-auth" deployments scripts terraform test/e2e || echo "clean"
make lint
```

Expected counts as annotated; the final `rg` prints `clean`.

- [ ] **Step 9: Commit**

```bash
cd /Users/efitz/Projects/tmi-pr6
git add deployments scripts/lib/deploy.py scripts/cats-prep.py terraform/modules/kubernetes/aws/k8s_resources.tf test/e2e/platform/workers_e2e_test.go
git commit -m "feat(deploy): run Redis (TLS + password) and NATS (mTLS) in every cluster

Redis listens on --tls-port only with requirepass in the base manifest
(deploy.py creates tmi-secrets on dev clusters); NATS verifies client
certs. Server, controller and worker components mount their cert-manager
Secrets and dial tls://. Reloader annotations roll pods on renewal."
```

---

### Task 7: Integration harness in TLS mode and the plaintext-rejection test

**Files:**
- Create: `test/integration/tlsgen/main.go`
- Modify: `scripts/manage-redis.py` (parser + `cmd_start`), `scripts/manage-nats.py` (parser + `cmd_start`, `TEST_DEFAULTS.port`)
- Modify: `scripts/run-integration-tests.py` (`ensure_redis` ~213, `clear_redis_rate_limits` ~120, `start_test_server_container` ~284, `run_pg` ~375-435)
- Modify: `test/integration/framework/redis.go`; Create: `test/integration/framework/nats.go`
- Modify: `test/integration/workflows/step_up_round_trip_test.go:265-270`, `test/integration/workflows/worker_probe_integration_test.go` (line 83, 121-125 and every later `nats.Connect`)
- Create: `test/integration/workflows/transport_security_test.go`

**Interfaces:**
- Consumes: `testpki` and `tlsconfig.Load` (Task 1; importable from the `test/integration` module through its `replace github.com/ericfitz/tmi => ../..` because the import path is under `github.com/ericfitz/tmi/`), env contract from Tasks 2-3.
- Produces: `.local/test-tls/` (gitignored via `.local/`) holding `ca.crt`, `redis.crt/.key`, `nats.crt/.key`, `client.crt/.key`, `redis.conf`, `redis-password` (0600), `secrets.env` (0600). Runner env for Go tests: `TEST_REDIS_TLS_CA_FILE`, `TEST_REDIS_PASSWORD`, `TMI_TEST_NATS_URL=tls://127.0.0.1:4223`, and the production contract itself: `TMI_NATS_TLS_CA_FILE` / `TMI_NATS_TLS_CERT_FILE` / `TMI_NATS_TLS_KEY_FILE` pointing at `ca.crt` / `client.crt` / `client.key`, so every in-process `worker.Connect` (api/ NATS tests included) and the spawned worker-probe (inherits `os.Environ()`) get mTLS with no harness-only variable. (`TestRepoTMIEnvTokens_AreAllDocumented` scans every non-`_test.go` file in the repo, `test/` included, so a new `TMI_TEST_*` token in `framework/` would fail `make test-unit`; that is why the harness reuses the documented names.) `TMI_RUN_NATS_TESTS=1` is set for the workflows phase only (the api/ NATS-gated tests keep their opt-in; they now work over mTLS when a developer opts in). Helpers `framework.RedisOptions() (*redis.Options, error)`, `framework.NATSTLSOptions(t) []nats.Option`.
- Cosmetic: `Dockerfile.redis`'s `HEALTHCHECK redis-cli ping` cannot speak TLS, so `docker ps` reports `tmi-redis-test` as `unhealthy`. Nothing in the harness reads that status; do not chase it.

- [ ] **Step 1: tlsgen**

`test/integration/tlsgen/main.go`:

```go
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
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/ericfitz/tmi/internal/tlsconfig/testpki"
)

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

// SEM@new: generate the harness CA, server/client certs, redis.conf and secrets.env once (writes files)
func run(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err == nil {
		return nil // already generated
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
	return os.WriteFile(filepath.Join(dir, "redis.conf"), []byte(conf), 0o644) // #nosec G306 -- throwaway harness config
}
```

Check: `cd test/integration && go vet ./tlsgen` (module tooling, not a server run).

- [ ] **Step 2: Container scripts**

`scripts/manage-redis.py`: add `parser.add_argument("--tls-dir", help="Directory with the harness PKI (ca.crt, redis.crt/.key, redis.conf); Redis then listens TLS-only with requirepass")` and in `resolve_config` `cfg["tls_dir"] = args.tls_dir`. In `cmd_start`:

```python
    tls_dir = cfg.get("tls_dir")
    ensure_container(
        name=cfg["container"],
        host_port=cfg["port"],
        container_port=REDIS_CONTAINER_PORT,
        image=cfg["image"],
        volumes={str(Path(tls_dir).resolve()): "/tls"} if tls_dir else None,
        # The image's entrypoint is redis-server; a config path as the only
        # argument replaces the image CMD flags entirely.
        cmd_args=["/tls/redis.conf"] if tls_dir else None,
    )
```

`scripts/manage-nats.py`: `TEST_DEFAULTS["port"] = 4223` (4222 is the default for `TMI_TEST_NATS_URL` and for ad-hoc dev NATS; the isolated test container gets its own port like the test DB and Redis, #778). Add the same `--tls-dir` option; in `cmd_start`:

```python
    tls_dir = cfg.get("tls_dir")
    cmd_args = list(NATS_CMD_ARGS)
    if tls_dir:
        cmd_args += ["--tlsverify", "--tlscert", "/tls/nats.crt", "--tlskey", "/tls/nats.key", "--tlscacert", "/tls/ca.crt"]
    ensure_container(
        name=cfg["container"], host_port=cfg["port"], container_port=NATS_CONTAINER_PORT,
        image=cfg["image"],
        volumes={str(Path(tls_dir).resolve()): "/tls"} if tls_dir else None,
        cmd_args=cmd_args,
    )
```

(`ensure_container` already accepts `volumes` and `cmd_args`.) Note `cmd_wait` only probes the TCP port, which still works under TLS.

- [ ] **Step 3: Runner**

`scripts/run-integration-tests.py`:

```python
TEST_TLS_DIR = ".local/test-tls"   # gitignored throwaway PKI + Redis password (tlsgen)
TEST_NATS_CONTAINER = "tmi-nats-test"
TEST_NATS_HOST_PORT = "4223"


def ensure_test_tls(project_root: Path) -> Path:
    """Generate the harness PKI once (see test/integration/tlsgen)."""
    tls_dir = project_root / TEST_TLS_DIR
    if not (tls_dir / "ca.crt").exists():
        log_info(f"Generating harness TLS material in {TEST_TLS_DIR}")
        subprocess.run(["go", "run", "./tlsgen", "-out", str(tls_dir)],
                       cwd=str(project_root / "test" / "integration"), check=True)
    return tls_dir


def ensure_nats(project_root: Path, tls_dir: Path) -> bool:
    """Start the isolated test NATS container in mTLS mode on its own port."""
    try:
        subprocess.run(
            ["uv", "run", str(project_root / "scripts" / "manage-nats.py"), "--test",
             "--tls-dir", str(tls_dir), "start"],
            cwd=str(project_root), check=True, capture_output=True,
        )
        subprocess.run(
            ["uv", "run", str(project_root / "scripts" / "manage-nats.py"), "--test", "wait"],
            cwd=str(project_root), check=True, capture_output=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        log_error(f"Could not start the test NATS container: {exc}")
        return False
    return True
```

- `ensure_redis(project_root, tls_dir)`: pass `"--tls-dir", str(tls_dir)` before `"start"`.
- `clear_redis_rate_limits(redis_db, tls_dir)`: both `docker exec` calls become `["docker", "exec", "--env-file", str(tls_dir / "secrets.env"), TEST_REDIS_CONTAINER, "redis-cli", "--tls", "--cacert", "/tls/ca.crt", "-n", redis_db, ...]` (the `-i` variant keeps `-i` before `--env-file`). The password reaches redis-cli as `REDISCLI_AUTH` from the file, never on argv.
- `start_test_server_container(..., tls_dir: Path)`: add `"-v", f"{tls_dir}:/etc/tmi-test-tls:ro"`, `"--env-file", str(tls_dir / "secrets.env")` (carries `TMI_REDIS_PASSWORD`), `"-e", "TMI_REDIS_TLS_ENABLED=true"`, `"-e", "TMI_REDIS_TLS_CA_FILE=/etc/tmi-test-tls/ca.crt"`. Update the docstring (Redis is TLS + password; the password never appears on the docker command line).
- In `run_pg`, before `ensure_redis`: `tls_dir = ensure_test_tls(project_root)`; then `if not ensure_redis(project_root, tls_dir): return 1, None` and `if not ensure_nats(project_root, tls_dir): return 1, None`. Extend `base_env` with:

```python
        "TEST_REDIS_TLS_CA_FILE": str(tls_dir / "ca.crt"),
        "TEST_REDIS_PASSWORD": (tls_dir / "redis-password").read_text().strip(),
        "TMI_TEST_NATS_URL": f"tls://127.0.0.1:{TEST_NATS_HOST_PORT}",
        # The production TLS contract (internal/tlsconfig): every in-process
        # worker.Connect and the spawned worker-probe present the harness
        # client cert.
        "TMI_NATS_TLS_CA_FILE": str(tls_dir / "ca.crt"),
        "TMI_NATS_TLS_CERT_FILE": str(tls_dir / "client.crt"),
        "TMI_NATS_TLS_KEY_FILE": str(tls_dir / "client.key"),
```

  Add `"TMI_RUN_NATS_TESTS": "1"` only to the workflows-phase env (the `{**base_env, ...}` dict near line 505 that carries `TMI_SERVER_URL`), so the worker-probe contract test runs every time while the api/ NATS-gated tests (`api/dlq_integration_test.go`, `api/extraction_async_integration_test.go`) keep their existing opt-in. Thread `tls_dir` into the `start_test_server_container(...)` and `clear_redis_rate_limits(...)` calls. Leave `run_oci` alone unless it calls these helpers (it starts no containers); if it does, pass `tls_dir` the same way.

- [ ] **Step 4: Framework helpers**

`test/integration/framework/redis.go`: add imports `"github.com/ericfitz/tmi/internal/tlsconfig"` and replace the client construction:

```go
// RedisOptions returns client options for the harness Redis: TEST_REDIS_HOST/
// PORT/DB, TEST_REDIS_PASSWORD, and CA-pinned TLS when TEST_REDIS_TLS_CA_FILE
// is set (scripts/run-integration-tests.py sets all of them). Without the CA
// variable it is a plaintext client, for a developer pointing the tests at an
// ad-hoc Redis.
// SEM@new: build go-redis options for the harness Redis from TEST_REDIS_* env (reads env)
func RedisOptions() (*redis.Options, error) {
	host := getEnvOrDefault("TEST_REDIS_HOST", "localhost")
	opts := &redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, getEnvOrDefault("TEST_REDIS_PORT", "6379")),
		Password: os.Getenv("TEST_REDIS_PASSWORD"),
		DB:       TestRedisDB(),
	}
	if ca := os.Getenv("TEST_REDIS_TLS_CA_FILE"); ca != "" {
		tlsCfg, err := tlsconfig.Load(ca, "", "")
		if err != nil {
			return nil, fmt.Errorf("harness redis tls: %w", err)
		}
		tlsCfg.ServerName = host
		opts.TLSConfig = tlsCfg
	}
	return opts, nil
}
```

`ClearRateLimits` uses `opts, err := RedisOptions(); if err != nil { return err }; client := redis.NewClient(opts)`.

`test/integration/framework/nats.go` (new):

```go
package framework

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ericfitz/tmi/internal/tlsconfig"
	"github.com/nats-io/nats.go"
)

// NATSTLSOptions returns nats.Secure with the client cert named by the
// TMI_NATS_TLS_* env vars (scripts/run-integration-tests.py sets them to the
// harness PKI); nil when TMI_NATS_TLS_CA_FILE is unset, so a developer's
// ad-hoc plaintext NATS still works. Same contract as internal/worker.
// SEM@new: build NATS mTLS connect options from the TMI_NATS_TLS_* env contract (reads env)
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
```

(imports: `testing`, `github.com/ericfitz/tmi/internal/tlsconfig`, `github.com/nats-io/nats.go` only.) The worker-probe child process needs no helper: it inherits `os.Environ()`, which already carries `TMI_NATS_TLS_*`.

Also fix the api/ suite's own Redis helper, which the api/ phase runs with `TEST_REDIS_*` set and which would now dial a TLS-only server in plaintext: in `api/content_oauth_integration_test.go` `openIntegrationRedis` (shared by `confluence_delegated_integration_test.go:114` and `google_workspace_delegated_integration_test.go:88`), replace the final `redis.NewClient(&redis.Options{Addr: ..., DB: db})` with

```go
	opts := &redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: os.Getenv("TEST_REDIS_PASSWORD"),
		DB:       db,
	}
	if ca := os.Getenv("TEST_REDIS_TLS_CA_FILE"); ca != "" {
		tlsCfg, err := tlsconfig.Load(ca, "", "")
		if err != nil {
			t.Fatalf("TEST_REDIS_TLS_CA_FILE: %v", err)
		}
		tlsCfg.ServerName = host
		opts.TLSConfig = tlsCfg
	}
	return redis.NewClient(opts)
```

(import `github.com/ericfitz/tmi/internal/tlsconfig`; this is a `_test.go` file in the root module, so no env-token or module concerns).

- [ ] **Step 5: Existing tests**

- `step_up_round_trip_test.go:265-270`: replace the hand-built `redis.NewClient(&redis.Options{...})` with `opts, err := framework.RedisOptions(); if err != nil { t.Fatal(err) }; rdb := redis.NewClient(opts)` (keep the rest).
- `worker_probe_integration_test.go`: line 83 `nats.Connect(natsURL, append(framework.NATSTLSOptions(t), nats.Timeout(3*time.Second))...)`; every later `nats.Connect(natsURL, ...)` in the file gets `framework.NATSTLSOptions(t)...` appended the same way; line 123 becomes `"TMI_NATS_URL="+natsURL,` (the `TMI_NATS_TLS_*` vars reach the probe through the `os.Environ()` the slice already starts from). Update the `workerProbeNATSURL` comment: the runner sets `TMI_TEST_NATS_URL=tls://127.0.0.1:4223`.

- [ ] **Step 6: The new negative test**

`test/integration/workflows/transport_security_test.go`:

```go
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
```

(`getEnvOrDefault` already exists in the `workflows` package; if not, use the framework's or add a two-line local one.)

- [ ] **Step 7: Run the harness**

Run: `make test-integration`
Expected: `.local/test-tls/` is generated once; `tmi-redis-test` (6380, TLS) and `tmi-nats-test` (4223, mTLS) start; the api/ suite, `TestTransportSecurity_PlaintextRejected_Integration`, `TestWorkerProbe_ContractEndToEnd_Integration` (no longer skipped) and the step-up tests pass; SUMMARY shows 0 failed and the worker-probe test is listed as run (memory: a stale integration module can silently skip tests; if the count looks low, `cd test/integration && go mod tidy` and re-run).

If a test container from before this task is still running it holds the old, plaintext config: `uv run scripts/manage-redis.py --test clean && uv run scripts/manage-nats.py --test clean` and re-run.

- [ ] **Step 8: Lint and commit**

Run: `make lint`

```bash
cd /Users/efitz/Projects/tmi-pr6
git add test/integration scripts/manage-redis.py scripts/manage-nats.py scripts/run-integration-tests.py
git commit -m "test(integration): run the harness Redis and NATS with TLS and assert plaintext is refused

tlsgen writes a throwaway PKI to .local/test-tls; the test containers
mount it (Redis TLS-only + requirepass via redis.conf, NATS --tlsverify on
4223); the server container and Go tests get the CA/password without
anything on a command line. New TestTransportSecurity_PlaintextRejected_
Integration; the worker-probe contract test now runs every time."
```

---

### Task 8: SEM anchors, full gates, and cluster verification (docker-desktop, then k3s)

**Files:** none new; this task verifies. AWS deployment is deliberately NOT part of it (waits for #968, #972, #965 per Eric).

- [ ] **Step 1: Replace SEM placeholders**

Run `/dev:sem-annotate --update` over every Go file touched in Tasks 1-7 (`git diff --name-only main -- '*.go'`), then `rg -n "SEM@new" --glob '*.go' .` must print nothing. Commit: `chore(sem): anchor SEM markers for the Redis/NATS TLS change`.

- [ ] **Step 2: Full local gates**

Run: `make lint && make build-server && make test-unit && make test-integration`
Expected: all green. `rg -n "TMI_WORKER_NATS_URL" --glob '!docs/**' --glob '!graphify-out/**' .` prints nothing.

- [ ] **Step 3: docker-desktop cluster**

```bash
make dev-nuke CLUSTER=docker-desktop      # fresh namespace: exercises ensure_redis_password_secret and the PKI apply
kubectl -n cert-manager get certificate tmi-internal-ca
kubectl -n tmi-platform get certificate                     # all READY=True
kubectl -n tmi-platform get pods                            # redis, nats-0, tmi-server, controller Running
kubectl -n tmi-platform logs deploy/tmi-server | rg -n "Redis connection established|async extraction pipeline connected"
kubectl -n tmi-platform logs deploy/tmi-component-controller | rg -n "JetStream provisioning enabled"
curl -s http://localhost:8080/ | head -c 200                # root health OK
make test-integration                                       # unchanged (isolated containers), sanity only
```

Plaintext rejection in-cluster (from inside the Redis pod, so the `redis-allow-tmi-server` NetworkPolicy on k3s cannot mask the result as a timeout): `kubectl -n tmi-platform exec deploy/redis -- redis-cli -p 6379 ping` must fail with a protocol/reset error (plaintext against the TLS listener), and `kubectl -n tmi-platform exec deploy/redis -- redis-cli --tls --cacert /tls/ca.crt ping` returns `NOAUTH` (TLS works, password enforced).

Worker path (KEDA scale-from-zero over mTLS): upload a document through the API (or run the Timmy ingest smoke used for #965) and watch `kubectl -n tmi-platform get pods -w` for `tmi-extractor-*` to appear and exit cleanly; `kubectl -n tmi-platform logs deploy/tmi-extractor | rg -n "nats connect"` shows no TLS errors.

Renewal + Reloader: `kubectl -n tmi-platform delete secret nats-client-server` (cert-manager reissues within seconds); `kubectl -n tmi-platform get secret nats-client-server` reappears; `kubectl -n tmi-platform rollout status deploy/tmi-server` shows a new pod (Reloader `auto`); then `kubectl -n tmi-platform delete secret redis-tls` and confirm `deploy/redis` rolls but `deploy/tmi-server` rolls too (it mounts the CA, `auto`), and that `delete secret tmi-secrets`-style changes would NOT roll Redis (targeted annotation): `kubectl -n tmi-platform annotate deploy/redis --list | rg reload` prints `secret.reloader.stakater.com/reload=redis-tls`.

CATS prep still works: `uv run scripts/cats-prep.py --help` then whatever the `cats-tmi` skill's smoke command is for the redis-cli path (it now runs `redis-cli --tls --cacert /tls/ca.crt`).

- [ ] **Step 4: k3s cluster**

Repeat step 3 with `CLUSTER=k3s` (`make dev-nuke CLUSTER=k3s`). Redis there is `redis:7-alpine` (16 KB-page nodes); confirm the pod starts with the TLS args and that `networkpolicy-k3s.yml` needs no change (cert-manager and Reloader only talk to the API server).

- [ ] **Step 5: Record and hand off**

Update `HANDOFF.md` (untracked) with: both clusters verified; an existing dev cluster must be brought forward with `make dev-up` or `make dev-reset` (only `start()` applies cert-manager/PKI and creates `tmi-secrets`; `make dev-restart` alone leaves the old plaintext Redis/NATS running); the AWS deploy deferred and its two commands (`scripts/deploy-aws.sh` after Terraform plan shows only the `TMI_NATS_URL` ConfigMap change); and the follow-up list below. No code commit in this step.

---

## Follow-ups (wiki and issues; not part of this PR)

Docs live in the GitHub wiki, so these are recorded here for the session close-out rather than done as tasks:

1. Wiki **Configuration Reference** is regenerated (`config-reference.md`); the wiki **Deployment** / **Local Dev Environment** pages need: cert-manager + Reloader are cluster prerequisites applied by `dev-up`/`deploy-aws.sh`; `.local/test-tls/` and the `--tls-dir` flags of `manage-redis.py`/`manage-nats.py`; the test NATS port 4223; `tmi-secrets` on dev clusters.
2. #965: CA rotation runbook (trust old+new, reissue leaves, drop old); Redis persistence question; re-ensure JetStream streams/object store after a NATS roll (Open question 6).
3. Issue: kind e2e harness (`test/e2e/platform`) host-side dials need a client-cert export (Open question 7).
4. Issue: NATS `authorization {}` with `verify_and_map` per-component subject permissions (spec §2, deferred).
5. AWS deploy of this PR after #968, #972, #965 (Eric).
