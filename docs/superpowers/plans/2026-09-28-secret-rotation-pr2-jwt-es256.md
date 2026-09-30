# Secret Rotation PR 2: JWT on ES256 with a JWKS keyring (#965) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sign every TMI JWT with ES256 from a rotatable keyring (`TMI_JWT_KEYRING`), select verification keys by `kid`, publish the verify set at `/.well-known/jwks.json`, and let `tmi-rotator` roll keys with no logout.

**Architecture:** `auth/jwt_keyring.go` defines the keyring document (one Secret value, atomic): a signing key and a list of verify keys, ECDSA P-256, `kid` = RFC 7638 thumbprint. `auth.JWTKeyManager` is rewritten around it: HS256/RS256 and the single-key file settings go away. A new `rotator.JWTKeyringRotation` (PR 1 interfaces) advances one phase per run: stage (add verify key) -> promote (sign with it) -> drop (remove the old key after the access-token lifetime). Nothing seeds the keyring by hand: the rotator's first run bootstraps it, and deploy scripts trigger that run.

**Tech Stack:** Go 1.26, `golang-jwt/jwt/v5` (`jwt.WithValidMethods`, `token.Header["kid"]`), `crypto/ecdsa`, `crypto/x509` (PKCS8/PKIX PEM), PR 1's `internal/rotator`.

**Spec:** `docs/superpowers/specs/2026-09-28-secret-rotation-design.md` §3 and §5. Builds on PR 1 (`docs/superpowers/plans/2026-09-28-secret-rotation-pr1-rotator.md`): `rotator.Env`, `rotator.Rotation`, `Env.Transition`, `Env.WaitServerRolled`, `rotator.Run`, annotation constants `AnnPhase`, `AnnRotatedAt`, `AnnGeneration`, `AnnPromotedAt`, `rotator.MemorySecretStore`, `rotator.NewFakeRolloutWaiter`, `cmd/rotator` `options`/`run`, `scripts/rotate-secret.py`, `deployments/k8s/dev/rotator.yml`.

## Open questions (resolved 2026-09-28)

HUMAN DECISIONS (Eric, 2026-09-28): (C) item 1: one rotation phase per daily run is kept (a full JWT rotation takes three days). (D) item 2: the JWT keyring is bootstrapped by the rotator's first run, accepting the brief `CreateContainerConfigError` on a first deploy. Items 3 and 4 keep the defaults stated below. Also relevant: (B) Redis persistence lands in PR 1 (Task 5 there), so the HS256 to ES256 cutover does not additionally lose sessions to a Redis roll.

1. **One phase per run.** The spec's stage/promote/drop each need a server roll; with the daily CronJob this plan advances exactly one phase per run, so a full JWT rotation takes three days (a forced `make rotate-secret name=jwt-keyring` advances one phase). The upside: the JWKS `Cache-Control: max-age=3600` floor for any external verifier is met by construction. Say if you want stage+promote in one run.
2. **Bootstrap through the rotator.** Terraform cannot compute an RFC 7638 thumbprint, so it no longer seeds a JWT value at all; the rotator's first run writes the keyring and `deploy.py` / `deploy-aws.sh` trigger that run right after applying the overlay. Until it completes (a minute or two on a fresh cluster), the `tmi-server` pod sits in `CreateContainerConfigError` (the `TMI_JWT_KEYRING` secretKeyRef is required and the key does not exist yet); the kubelet retries on its own once the Secret is written. Alternative: generate the keyring in the deploy scripts with the same Go code (`bin/tmi-rotator -print-new-jwt-keyring > umask-077 file`). The plan does the former; both are cheap.
3. **`auth.jwt.secret` in dev config files.** `config-development.yml` / `config-test.yml` are gitignored and still carry `jwt.secret` / `signing_method`. The plan keeps the YAML fields as deprecated no-ops that log one warning, rather than failing startup, for one release.
4. **Drop grace.** The old verify key is dropped `TMI_ROTATOR_JWT_PREVIOUS_GRACE` (default `24h`) after promotion. `auth.jwt.expiration_seconds` is operator-configurable (default 3600 s); the runbook says the grace must exceed it. Reading the live setting instead is possible but couples the rotator to the settings table.

## Global Constraints

Same as PR 1 (make targets only, slogging only, secret safety, SEM markers, overlays own workloads, commit trailer). Plus:

- **No HS256 verify path**, not even behind a flag (spec: "no HS256 verify path"). Old tokens get 401; clients refresh.
- **`kid` is mandatory** on issued tokens and on verification; unknown `kid` or any `alg` other than `ES256` is rejected before signature verification.
- **Keyring document** (`TMI_JWT_KEYRING`, compact single-line JSON so it survives `--env-file` and Secret data):

  ```json
  {"signing":{"kid":"<thumbprint>","private_pem":"-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n"},"verify":[{"kid":"<thumbprint>","public_pem":"-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----\n"}]}
  ```

  PKCS#8 private, PKIX public, P-256 only, `signing.kid` must appear in `verify`, every `kid` must equal the thumbprint of its key.
- **PR title:** `feat(auth)!: sign JWTs with ES256 from a rotatable JWKS keyring (#965)` (the `!` bumps the OpenAPI schema MAJOR: `TMI_JWT_SECRET` and HS256 disappear from the public contract).
- **Consumers (spec):** tmi-ux, tmi-mcp, tmi-tf-wh and the wiki treat tokens as opaque and refresh on 401; none verify locally. `/oauth2/token` client_credentials stays unchanged. Task 8 has the Agentbus and wiki steps.

## Review Focus

1. A token with `alg: none` or `alg: HS256` signed with any key: expected 401 before any key lookup (Task 2 `TestVerifyToken_RejectsOtherAlgs`).
2. A token whose `kid` is a valid verify key but whose signature was made with another key: expected 401 (Task 2 `TestVerifyToken_WrongKeyForKid`).
3. A keyring whose `signing.kid` is not in `verify` (a hand-edited Secret): expected the server refuses to start with a clear message, never signs tokens it cannot verify (Task 1 `TestParseKeyring_SigningKidMustBeVerifiable`).
4. A `TMI_JWT_KEYRING` with Windows line endings or trailing whitespace pasted by an operator: expected parse succeeds (PEM decoders tolerate `\r\n`; JSON parse trims) (Task 1 `TestParseKeyring_TrimsAndTolerates`).
5. Rotation drop while an access token signed by the old key is still inside its lifetime: expected 401 only after the grace, so the grace must exceed `expiration_seconds`; the rotator refuses a grace shorter than 1h (Task 5 `TestJWTKeyringRotation_GraceFloor`).

---

## File map

| File | Change |
|---|---|
| `auth/jwt_keyring.go` (new) + test | keyring document, generation, thumbprint, parse/serialize |
| `auth/jwt_key_manager.go` + tests | ES256/keyring only: `kid` on sign, keyfunc by `kid`, `VerifyKeys()` |
| `auth/jwt_key_parsing_test.go` | delete (RSA parsing gone) or trim to ECDSA |
| `auth/config.go`, `auth/config_adapter.go` | `JWTConfig.Keyring`; drop `Secret`, `SigningMethod`, `KeyID`, RSA/ECDSA path fields and their env fallbacks |
| `internal/config/config.go`, `setting_defs_auth.go`, `process_env.go`, `config_test.go`, `migratable_settings_test.go` | `auth.jwt.keyring` / `TMI_JWT_KEYRING`; deprecated `secret`/`signing_method`; secret-reference list |
| `auth/handlers_openid.go` + `auth/jwks_test.go` | JWKS from the verify set; `ES256` in discovery |
| `auth/service_test_helpers.go` and every test building `JWTConfig{Secret: ...}` | use `testKeyring(t)` |
| `api-schema/tmi-openapi.json:14465,14862` | `HS256` -> `ES256`; then `make validate-openapi && make generate-api` |
| `internal/rotator/jwt_keyring.go` (new) + test | `JWTKeyringRotation` |
| `cmd/rotator/main.go` | register the rotation first; `TMI_ROTATOR_JWT_PREVIOUS_GRACE` |
| `deployments/k8s/dev/server.yml`, `server-oracle.yml`, `aws/patches/server-config.yaml` | `TMI_JWT_KEYRING` from `tmi-secrets` |
| `scripts/lib/deploy.py`, `scripts/deploy-aws.sh` | trigger the rotator bootstrap Job after the overlay |
| `terraform/modules/secrets/aws/*`, `terraform/modules/kubernetes/aws/*`, `terraform/environments/aws-public/main.tf` | drop `jwt_secret` everywhere |
| `test/integration/tlsgen` (PR 6) and `scripts/run-integration-tests.py` | per-run keyring in `secrets.env` |
| `test/integration/workflows/jwt_es256_test.go` (new) | tokens carry `kid`/`ES256`; JWKS verifies them; HS256 token is 401; rotation under load |
| `auth/README.md`, wiki | docs |

---

### Task 1: Keyring document

**Files:**
- Create: `auth/jwt_keyring.go`
- Test: `auth/jwt_keyring_test.go`

**Interfaces (Produces):**

```go
package auth

type JWTSigningKey struct {
	KID        string `json:"kid"`
	PrivatePEM string `json:"private_pem"`
}
type JWTVerifyKey struct {
	KID       string `json:"kid"`
	PublicPEM string `json:"public_pem"`
}
type JWTKeyring struct {
	Signing JWTSigningKey  `json:"signing"`
	Verify  []JWTVerifyKey `json:"verify"`
}

func GenerateJWTKeyPair() (JWTSigningKey, JWTVerifyKey, error)      // P-256, kid = thumbprint
func NewJWTKeyring() (*JWTKeyring, error)                            // one fresh pair
func ParseJWTKeyring(doc string) (*JWTKeyring, error)                 // validates everything
func (k *JWTKeyring) String() string                                  // compact JSON
func (k *JWTKeyring) SigningPrivateKey() (*ecdsa.PrivateKey, error)
func (k *JWTKeyring) VerifyPublicKeys() (map[string]*ecdsa.PublicKey, error)
func JWKThumbprint(pub *ecdsa.PublicKey) (string, error)              // RFC 7638, base64url(SHA-256)
```

- [ ] **Step 1: Write the failing tests**

```go
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewJWTKeyring_RoundTrip(t *testing.T) {
	kr, err := NewJWTKeyring()
	require.NoError(t, err)
	require.Len(t, kr.Verify, 1)
	require.Equal(t, kr.Signing.KID, kr.Verify[0].KID)
	doc := kr.String()
	require.False(t, strings.Contains(doc, "\n"), "compact single line")
	back, err := ParseJWTKeyring(doc)
	require.NoError(t, err)
	require.Equal(t, kr.Signing.KID, back.Signing.KID)
	priv, err := back.SigningPrivateKey()
	require.NoError(t, err)
	pubs, err := back.VerifyPublicKeys()
	require.NoError(t, err)
	require.True(t, priv.PublicKey.Equal(pubs[kr.Signing.KID]))
	tp, err := JWKThumbprint(&priv.PublicKey)
	require.NoError(t, err)
	require.Equal(t, kr.Signing.KID, tp)
	require.Len(t, tp, 43, "base64url SHA-256 without padding")
}

func TestJWKThumbprint_MatchesRFC7638Vector(t *testing.T) {
	// RFC 7638 has only an RSA vector; pin ours against an independent computation.
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tp, err := JWKThumbprint(&priv.PublicKey)
	require.NoError(t, err)
	// Recompute by hand: canonical members in lexicographic order, no whitespace.
	x := base64URLEncode(priv.PublicKey.X.FillBytes(make([]byte, 32)))
	y := base64URLEncode(priv.PublicKey.Y.FillBytes(make([]byte, 32)))
	canon := `{"crv":"P-256","kty":"EC","x":"` + x + `","y":"` + y + `"}`
	require.Equal(t, sha256B64URL([]byte(canon)), tp)
}

func TestParseKeyring_SigningKidMustBeVerifiable(t *testing.T) {
	kr, _ := NewJWTKeyring()
	kr.Verify = nil
	_, err := ParseJWTKeyring(kr.String())
	require.ErrorContains(t, err, "signing key")
}

func TestParseKeyring_RejectsWrongKidCurveAndGarbage(t *testing.T) {
	kr, _ := NewJWTKeyring()
	kr.Verify[0].KID = "wrong"
	kr.Signing.KID = "wrong"
	_, err := ParseJWTKeyring(kr.String())
	require.ErrorContains(t, err, "thumbprint")

	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	sk, vk, err := keyPairFrom(p384)
	require.NoError(t, err)
	bad := &JWTKeyring{Signing: sk, Verify: []JWTVerifyKey{vk}}
	_, err = ParseJWTKeyring(bad.String())
	require.ErrorContains(t, err, "P-256")

	_, err = ParseJWTKeyring("")
	require.Error(t, err)
	_, err = ParseJWTKeyring("{not json")
	require.Error(t, err)
}

func TestParseKeyring_TrimsAndTolerates(t *testing.T) {
	kr, _ := NewJWTKeyring()
	doc := "  " + strings.ReplaceAll(kr.String(), `\n`, `\r\n`) + "\r\n"
	back, err := ParseJWTKeyring(doc)
	require.NoError(t, err)
	require.Equal(t, kr.Signing.KID, back.Signing.KID)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(back.String()), &m))
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestNewJWTKeyring count1=true`
Expected: `undefined: NewJWTKeyring`.

- [ ] **Step 3: Implement `auth/jwt_keyring.go`**

```go
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// JWTSigningKey is the private half the server signs with.
// SEM@<sha>: signing key entry of the JWT keyring: kid and PKCS8 PEM (pure)
type JWTSigningKey struct {
	KID        string `json:"kid"`
	PrivatePEM string `json:"private_pem"`
}

// JWTVerifyKey is one public key clients and the server verify with.
// SEM@<sha>: verify key entry of the JWT keyring: kid and PKIX PEM (pure)
type JWTVerifyKey struct {
	KID       string `json:"kid"`
	PublicPEM string `json:"public_pem"`
}

// JWTKeyring is the TMI_JWT_KEYRING document (#965): ES256 only.
// SEM@<sha>: rotatable set of JWT keys: one signing key plus the verify set (pure)
type JWTKeyring struct {
	Signing JWTSigningKey  `json:"signing"`
	Verify  []JWTVerifyKey `json:"verify"`
}

// GenerateJWTKeyPair makes a fresh P-256 pair; kid is the RFC 7638 thumbprint.
// SEM@<sha>: generate a P-256 key pair encoded as keyring entries (pure)
func GenerateJWTKeyPair() (JWTSigningKey, JWTVerifyKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return JWTSigningKey{}, JWTVerifyKey{}, fmt.Errorf("generate ECDSA key: %w", err)
	}
	return keyPairFrom(priv)
}

// SEM@<sha>: encode an ECDSA private key as keyring signing and verify entries (pure)
func keyPairFrom(priv *ecdsa.PrivateKey) (JWTSigningKey, JWTVerifyKey, error) {
	kid, err := JWKThumbprint(&priv.PublicKey)
	if err != nil {
		return JWTSigningKey{}, JWTVerifyKey{}, err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return JWTSigningKey{}, JWTVerifyKey{}, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return JWTSigningKey{}, JWTVerifyKey{}, err
	}
	return JWTSigningKey{KID: kid, PrivatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))},
		JWTVerifyKey{KID: kid, PublicPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))}, nil
}

// NewJWTKeyring returns a keyring with one fresh pair.
// SEM@<sha>: build a single-key JWT keyring (pure)
func NewJWTKeyring() (*JWTKeyring, error) {
	sk, vk, err := GenerateJWTKeyPair()
	if err != nil {
		return nil, err
	}
	return &JWTKeyring{Signing: sk, Verify: []JWTVerifyKey{vk}}, nil
}

// String renders compact JSON (one line; PEM newlines are JSON-escaped).
// SEM@<sha>: serialize the keyring as compact JSON (pure)
func (k *JWTKeyring) String() string {
	b, _ := json.Marshal(k)
	return string(b)
}

// ParseJWTKeyring parses and validates a keyring document.
// SEM@<sha>: parse and validate a JWT keyring document: P-256, thumbprint kids, verifiable signing key (pure)
func ParseJWTKeyring(doc string) (*JWTKeyring, error) {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return nil, errors.New("jwt keyring is empty")
	}
	var k JWTKeyring
	if err := json.Unmarshal([]byte(doc), &k); err != nil {
		return nil, fmt.Errorf("jwt keyring is not valid JSON: %w", err)
	}
	if len(k.Verify) == 0 {
		return nil, errors.New("jwt keyring has no verify keys; the signing key must be verifiable")
	}
	priv, err := k.SigningPrivateKey()
	if err != nil {
		return nil, err
	}
	pubs, err := k.VerifyPublicKeys()
	if err != nil {
		return nil, err
	}
	tp, err := JWKThumbprint(&priv.PublicKey)
	if err != nil {
		return nil, err
	}
	if tp != k.Signing.KID {
		return nil, fmt.Errorf("jwt keyring signing kid %q is not the key's thumbprint", k.Signing.KID)
	}
	pub, ok := pubs[k.Signing.KID]
	if !ok || !pub.Equal(&priv.PublicKey) {
		return nil, errors.New("jwt keyring signing key is not in the verify set")
	}
	return &k, nil
}

// SEM@<sha>: decode the signing key PEM as a P-256 private key (pure)
func (k *JWTKeyring) SigningPrivateKey() (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(k.Signing.PrivatePEM))
	if block == nil {
		return nil, errors.New("jwt keyring signing key: no PEM block")
	}
	var key any
	var err error
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("jwt keyring signing key: unsupported PEM type %s", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("jwt keyring signing key: %w", err)
	}
	priv, ok := key.(*ecdsa.PrivateKey)
	if !ok || priv.Curve != elliptic.P256() {
		return nil, errors.New("jwt keyring signing key must be ECDSA P-256")
	}
	return priv, nil
}

// SEM@<sha>: decode every verify key PEM, keyed by kid, checking curve and thumbprint (pure)
func (k *JWTKeyring) VerifyPublicKeys() (map[string]*ecdsa.PublicKey, error) {
	out := make(map[string]*ecdsa.PublicKey, len(k.Verify))
	for _, v := range k.Verify {
		block, _ := pem.Decode([]byte(v.PublicPEM))
		if block == nil || block.Type != "PUBLIC KEY" {
			return nil, fmt.Errorf("jwt keyring verify key %q: no PUBLIC KEY PEM block", v.KID)
		}
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("jwt keyring verify key %q: %w", v.KID, err)
		}
		pub, ok := key.(*ecdsa.PublicKey)
		if !ok || pub.Curve != elliptic.P256() {
			return nil, fmt.Errorf("jwt keyring verify key %q must be ECDSA P-256", v.KID)
		}
		tp, err := JWKThumbprint(pub)
		if err != nil {
			return nil, err
		}
		if tp != v.KID {
			return nil, fmt.Errorf("jwt keyring verify kid %q is not the key's thumbprint", v.KID)
		}
		if _, dup := out[v.KID]; dup {
			return nil, fmt.Errorf("jwt keyring verify kid %q listed twice", v.KID)
		}
		out[v.KID] = pub
	}
	return out, nil
}

// JWKThumbprint implements RFC 7638 for EC keys: SHA-256 over the canonical
// JSON {"crv","kty","x","y"} in lexicographic member order, base64url unpadded.
// SEM@<sha>: compute the RFC 7638 JWK thumbprint of a P-256 public key (pure)
func JWKThumbprint(pub *ecdsa.PublicKey) (string, error) {
	if pub.Curve != elliptic.P256() {
		return "", errors.New("thumbprint: only P-256 is supported")
	}
	x := base64URLEncode(pub.X.FillBytes(make([]byte, 32)))
	y := base64URLEncode(pub.Y.FillBytes(make([]byte, 32)))
	canon := `{"crv":"P-256","kty":"EC","x":"` + x + `","y":"` + y + `"}`
	return sha256B64URL([]byte(canon)), nil
}

// SEM@<sha>: base64url-encode the SHA-256 of bytes (pure)
func sha256B64URL(b []byte) string {
	h := sha256.Sum256(b)
	return base64.RawURLEncoding.EncodeToString(h[:])
}
```

`base64URLEncode` already exists in `auth/handlers_openid.go`.

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestNewJWTKeyring count1=true`, `make test-unit name=TestJWKThumbprint count1=true`, `make test-unit name=TestParseKeyring count1=true`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add auth/jwt_keyring.go auth/jwt_keyring_test.go
git commit -m "feat(auth): JWT keyring document with RFC 7638 kids (#965)"
```

---

### Task 2: Key manager on the keyring

**Files:**
- Rewrite: `auth/jwt_key_manager.go`
- Rewrite: `auth/jwt_key_manager_test.go`; delete `auth/jwt_key_parsing_test.go` (RSA/PEM parsing helpers are removed) after moving any ECDSA-relevant assertion into the new test
- Modify: `auth/config.go:57-74` (`JWTConfig`), `auth/config.go:186-200` (env defaults), `auth/config.go:430-470` (validation), `auth/config_adapter.go:35-41`
- Modify: `auth/service_test_helpers.go:43,101` and every test listed by `rg -ln "SigningMethod:|Secret:\s*\"[^\"]*\",?$|JWTConfig\{" auth cmd api internal --type go`

**Interfaces:**
- Produces: `JWTConfig.Keyring string` (replaces `Secret`, `SigningMethod`, `KeyID`, the four RSA and four ECDSA fields); `NewJWTKeyManager(config JWTConfig) (*JWTKeyManager, error)` (same name); `(*JWTKeyManager).CreateToken(claims jwt.Claims) (string, error)` sets `kid`; `(*JWTKeyManager).VerifyToken(tokenString string, claims jwt.Claims) (*jwt.Token, error)`; new `(*JWTKeyManager).VerifyKeys() []JWTPublicKey` where `type JWTPublicKey struct { KID string; Key *ecdsa.PublicKey }`, sorted by KID; `(*JWTKeyManager).SigningKID() string`. Removed: `GetPublicKey`, `GetSigningMethod`, `loadHMACKeys/loadRSAKeys/loadECDSAKeys/getKeyData`, `parseRSA*`, `parseECDSA*`.
- Test helper (in `auth/service_test_helpers.go`, non-test file already used by tests): `func testKeyringJSON(t testing.TB) string` -> `NewJWTKeyring().String()`, `require.NoError`.

- [ ] **Step 1: Write the failing tests** (`auth/jwt_key_manager_test.go`, replacing the file)

```go
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) (*JWTKeyManager, *JWTKeyring) {
	t.Helper()
	kr, err := NewJWTKeyring()
	require.NoError(t, err)
	m, err := NewJWTKeyManager(JWTConfig{Keyring: kr.String(), ExpirationSeconds: 60})
	require.NoError(t, err)
	return m, kr
}

func claimsFor(sub string) *jwt.RegisteredClaims {
	return &jwt.RegisteredClaims{Subject: sub, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}
}

func TestKeyManager_SignsWithKidAndES256(t *testing.T) {
	m, kr := newTestManager(t)
	tok, err := m.CreateToken(claimsFor("alice"))
	require.NoError(t, err)
	parsed, _, err := jwt.NewParser().ParseUnverified(tok, &jwt.RegisteredClaims{})
	require.NoError(t, err)
	require.Equal(t, "ES256", parsed.Header["alg"])
	require.Equal(t, kr.Signing.KID, parsed.Header["kid"])
	out := &jwt.RegisteredClaims{}
	_, err = m.VerifyToken(tok, out)
	require.NoError(t, err)
	require.Equal(t, "alice", out.Subject)
	require.Equal(t, kr.Signing.KID, m.SigningKID())
	require.Len(t, m.VerifyKeys(), 1)
}

func TestKeyManager_RequiresKeyring(t *testing.T) {
	_, err := NewJWTKeyManager(JWTConfig{ExpirationSeconds: 60})
	require.ErrorContains(t, err, "keyring")
}

func TestVerifyToken_RejectsOtherAlgs(t *testing.T) {
	m, _ := newTestManager(t)
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsFor("x"))
	hs.Header["kid"] = m.SigningKID()
	s, _ := hs.SignedString([]byte("secret"))
	_, err := m.VerifyToken(s, &jwt.RegisteredClaims{})
	require.Error(t, err)
	none := jwt.NewWithClaims(jwt.SigningMethodNone, claimsFor("x"))
	s, _ = none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	_, err = m.VerifyToken(s, &jwt.RegisteredClaims{})
	require.Error(t, err)
}

func TestVerifyToken_UnknownOrMissingKid(t *testing.T) {
	m, _ := newTestManager(t)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claimsFor("x"))
	s, _ := tok.SignedString(other) // no kid header
	_, err := m.VerifyToken(s, &jwt.RegisteredClaims{})
	require.ErrorContains(t, err, "kid")
	tok.Header["kid"] = "nope"
	s, _ = tok.SignedString(other)
	_, err = m.VerifyToken(s, &jwt.RegisteredClaims{})
	require.ErrorContains(t, err, "kid")
}

func TestVerifyToken_WrongKeyForKid(t *testing.T) {
	m, _ := newTestManager(t)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claimsFor("x"))
	tok.Header["kid"] = m.SigningKID()
	s, _ := tok.SignedString(other)
	_, err := m.VerifyToken(s, &jwt.RegisteredClaims{})
	require.Error(t, err)
}

func TestVerifyToken_AcceptsStagedOldKey(t *testing.T) {
	// Rotation: sign with A, then keyring signs with B but still verifies A.
	mA, krA := newTestManager(t)
	tokA, _ := mA.CreateToken(claimsFor("alice"))
	skB, vkB, err := GenerateJWTKeyPair()
	require.NoError(t, err)
	krB := &JWTKeyring{Signing: skB, Verify: []JWTVerifyKey{krA.Verify[0], vkB}}
	mB, err := NewJWTKeyManager(JWTConfig{Keyring: krB.String(), ExpirationSeconds: 60})
	require.NoError(t, err)
	_, err = mB.VerifyToken(tokA, &jwt.RegisteredClaims{})
	require.NoError(t, err)
	require.Len(t, mB.VerifyKeys(), 2)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestKeyManager count1=true`
Expected: unknown field `Keyring`.

- [ ] **Step 3: Rewrite `auth/jwt_key_manager.go`**

```go
package auth

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"sort"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ericfitz/tmi/internal/slogging"
)

// JWTPublicKey is one entry of the verify set, for JWKS.
// SEM@<sha>: kid plus P-256 public key of a verify entry (pure)
type JWTPublicKey struct {
	KID string
	Key *ecdsa.PublicKey
}

// JWTKeyManager signs with the keyring's signing key and verifies by kid.
// SEM@<sha>: ES256 signer and kid-dispatched verifier over the JWT keyring (pure)
type JWTKeyManager struct {
	signingKID string
	signingKey *ecdsa.PrivateKey
	verify     map[string]*ecdsa.PublicKey
}

var es256Only = jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()})

// NewJWTKeyManager parses config.Keyring; there is no default key.
// SEM@<sha>: build a JWT key manager from the configured keyring document, refusing to run without one
func NewJWTKeyManager(config JWTConfig) (*JWTKeyManager, error) {
	logger := slogging.Get()
	if config.Keyring == "" {
		return nil, errors.New("jwt keyring is required (TMI_JWT_KEYRING); tmi-rotator bootstraps it on its first run")
	}
	kr, err := ParseJWTKeyring(config.Keyring)
	if err != nil {
		return nil, err
	}
	priv, err := kr.SigningPrivateKey()
	if err != nil {
		return nil, err
	}
	pubs, err := kr.VerifyPublicKeys()
	if err != nil {
		return nil, err
	}
	logger.Info("JWT key manager initialized alg=ES256 signing_kid=%s verify_keys=%d", kr.Signing.KID, len(pubs))
	return &JWTKeyManager{signingKID: kr.Signing.KID, signingKey: priv, verify: pubs}, nil
}

// SEM@<sha>: sign claims with ES256 and the signing key, stamping its kid (pure)
func (m *JWTKeyManager) CreateToken(claims jwt.Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = m.signingKID
	s, err := token.SignedString(m.signingKey)
	if err != nil {
		slogging.Get().Error("Failed to sign JWT error=%v", err)
		return "", err
	}
	return s, nil
}

// VerifyToken accepts only ES256 tokens whose kid names a verify key.
// SEM@<sha>: validate an ES256 JWT by its kid against the verify set; reject other algs or unknown kids (pure)
func (m *JWTKeyManager) VerifyToken(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token has no kid header")
		}
		pub, ok := m.verify[kid]
		if !ok {
			return nil, fmt.Errorf("unknown kid")
		}
		return pub, nil
	}, es256Only)
	if err != nil {
		slogging.Get().Warn("JWT verification failed error=%v", err)
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("token is invalid")
	}
	return token, nil
}

// SEM@<sha>: return the kid tokens are currently signed with (pure)
func (m *JWTKeyManager) SigningKID() string { return m.signingKID }

// SEM@<sha>: list the verify set sorted by kid, for the JWKS endpoint (pure)
func (m *JWTKeyManager) VerifyKeys() []JWTPublicKey {
	out := make([]JWTPublicKey, 0, len(m.verify))
	for kid, k := range m.verify {
		out = append(out, JWTPublicKey{KID: kid, Key: k})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KID < out[j].KID })
	return out
}
```

Do not log the `unknown kid` value at Warn on every request from a stale client burst; the `Warn` above logs the wrapped error, which contains no kid: keep the message `unknown kid`.

- [ ] **Step 4: `auth/config.go`**

`JWTConfig` becomes:

```go
type JWTConfig struct {
	Keyring             string // TMI_JWT_KEYRING document (auth/jwt_keyring.go); ES256 only
	ExpirationSeconds   int
	RefreshTokenDays    int
	SessionLifetimeDays int
}
```

Env defaults (line ~190): `Keyring: envutil.Get("TMI_JWT_KEYRING", "")`; delete the `Secret`, `SigningMethod`, `KeyID`, `JWT_RSA_*`, `JWT_ECDSA_*` lines and the `signingMethodHS256/RS256/ES256` constants if nothing else uses them (`rg -n "signingMethod" auth`). Validation (line ~430): replace the signing-method switch and the "JWT secret must not match" block with:

```go
	if _, err := ParseJWTKeyring(c.JWT.Keyring); err != nil {
		logger.Error("JWT keyring invalid error=%v", err)
		return fmt.Errorf("jwt keyring: %w", err)
	}
```

`auth/config_adapter.go:35`: `JWT: JWTConfig{Keyring: unified.Auth.JWT.Keyring, ExpirationSeconds: ..., RefreshTokenDays: ..., SessionLifetimeDays: ...}`.

`auth/service_test_helpers.go`: add

```go
// testKeyringJSON returns a fresh single-key keyring document for tests.
// SEM@<sha>: build a throwaway JWT keyring document for tests (pure)
func testKeyringJSON(t testing.TB) string {
	t.Helper()
	kr, err := NewJWTKeyring()
	require.NoError(t, err)
	return kr.String()
}
```

and replace `Secret: "...", SigningMethod: "HS256"` at lines 43 and 101 with `Keyring: testKeyringJSON(t)`. Do the same in every file the `rg` in the Files list names (tests outside package `auth` use `auth.NewJWTKeyring()` inline). `auth/jwks_test.go` is rewritten in Task 3. `cmd/server/startup_checks_test.go` and `internal/config/*_test.go` reference `Auth.JWT.Secret`: change to `Keyring` after Task 4 lands (do Tasks 2-4 in one build-green sequence; commit at the end of Task 4 if the tree does not compile in between).

- [ ] **Step 5: Run**

Run: `make test-unit name=TestKeyManager count1=true`, `make test-unit name=TestVerifyToken count1=true`, then `make build-server` (expect config package errors until Task 4).

- [ ] **Step 6: Commit** (once Task 4 makes the tree build, or now if it builds)

```bash
git add auth/jwt_key_manager.go auth/jwt_key_manager_test.go auth/config.go auth/config_adapter.go auth/service_test_helpers.go
git rm auth/jwt_key_parsing_test.go
git commit -m "feat(auth)!: sign and verify JWTs with ES256 from the keyring, dispatch by kid (#965)"
```

---
### Task 3: JWKS and discovery from the verify set

**Files:**
- Modify: `auth/handlers_openid.go:78,137-260` (`IDTokenSigningAlgValuesSupported`, `JWK`, `createJWKFromPublicKey`, `GetJWKS`; delete `intToBytes` and the RSA branch)
- Rewrite: `auth/jwks_test.go`
- Modify: `api-schema/tmi-openapi.json:14465` (enum/example `HS256` -> `ES256`) and `:14862`

**Interfaces:**
- Consumes: `(*JWTKeyManager).VerifyKeys() []JWTPublicKey`.
- Produces: `createJWKFromPublicKey(kid string, key *ecdsa.PublicKey) (*JWK, error)`; JWKS response `{"keys":[{"kty":"EC","use":"sig","key_ops":["verify"],"kid":"...","alg":"ES256","crv":"P-256","x":"...","y":"..."}]}`, one entry per verify key, sorted by kid.

- [ ] **Step 1: Write the failing test** (`auth/jwks_test.go`)

```go
package auth

import (
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestGetJWKS_PublishesEveryVerifyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	skB, vkB, _ := GenerateJWTKeyPair()
	krA, _ := NewJWTKeyring()
	kr := &JWTKeyring{Signing: skB, Verify: []JWTVerifyKey{krA.Verify[0], vkB}}
	svc := newTestServiceWithKeyring(t, kr.String()) // helper next to the existing service test helpers
	h := &Handlers{service: svc}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	h.GetJWKS(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))

	var body JWKSResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Keys, 2)
	for _, k := range body.Keys {
		require.Equal(t, "EC", k.KeyType)
		require.Equal(t, "P-256", k.Curve)
		require.Equal(t, "ES256", k.Algorithm)
		require.Equal(t, "sig", k.Use)
		require.NotEmpty(t, k.X)
		require.NotEmpty(t, k.Y)
	}

	// A token signed by the server verifies against the published JWK.
	tok, err := svc.keyManager.CreateToken(claimsFor("alice"))
	require.NoError(t, err)
	parsed, err := jwt.Parse(tok, func(tk *jwt.Token) (any, error) {
		kid := tk.Header["kid"].(string)
		for _, k := range body.Keys {
			if k.KID == kid {
				return ecdsaFromJWK(t, k), nil
			}
		}
		return nil, jwt.ErrTokenUnverifiable
	}, jwt.WithValidMethods([]string{"ES256"}))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
}

func ecdsaFromJWK(t *testing.T, k JWK) *ecdsa.PublicKey {
	t.Helper()
	x, err := base64URLDecode(k.X)
	require.NoError(t, err)
	y, err := base64URLDecode(k.Y)
	require.NoError(t, err)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
	require.NoError(t, err)
	return pub
}
```

Add `base64URLDecode` (`base64.RawURLEncoding.DecodeString`) next to `base64URLEncode` and import `crypto/elliptic`. If `ecdsa.ParseUncompressedPublicKey` is not in this Go version, use `elliptic.Unmarshal` (deprecated but present) or build the key from `big.Int`s. `newTestServiceWithKeyring` wraps the existing `service_test_helpers.go` constructor with an explicit keyring (add it there).

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestGetJWKS count1=true`
Expected: fails (RSA-era `createJWKFromPublicKey` signature / `GetPublicKey` missing).

- [ ] **Step 3: Implement**

`handlers_openid.go`:

```go
// createJWKFromPublicKey renders one verify key as a JWK.
// SEM@<sha>: convert a P-256 public key and its kid to a signature-use JWK (pure)
func createJWKFromPublicKey(kid string, key *ecdsa.PublicKey) (*JWK, error) {
	if key.Curve.Params().Name != "P-256" {
		return nil, fmt.Errorf("unsupported ECDSA curve: %s", key.Curve.Params().Name)
	}
	return &JWK{
		KeyType: "EC", Use: "sig", KeyOps: []string{"verify"}, KeyID: kid, Algorithm: "ES256", Curve: "P-256",
		X: base64URLEncode(key.X.FillBytes(make([]byte, 32))),
		Y: base64URLEncode(key.Y.FillBytes(make([]byte, 32))),
	}, nil
}

// SEM@<sha>: handle GET /.well-known/jwks.json with every verify key of the keyring
func (h *Handlers) GetJWKS(c *gin.Context) {
	jwks := JWKSResponse{Keys: []JWK{}}
	for _, vk := range h.service.keyManager.VerifyKeys() {
		jwk, err := createJWKFromPublicKey(vk.KID, vk.Key)
		if err != nil {
			slogging.Get().WithContext(c).Error("JWKS: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create JWK"})
			return
		}
		jwks.Keys = append(jwks.Keys, *jwk)
	}
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, jwks)
}
```

Remove `N`/`E` from `JWK`, `intToBytes`, and the `crypto/rsa` import. Line 78: `IDTokenSigningAlgValuesSupported: []string{"ES256"}`; grep the file for the other two discovery structs and set `ES256` wherever `HS256` appears (`rg -n "HS256" auth/`). OpenAPI: at `api-schema/tmi-openapi.json:14465` and `:14862` replace `HS256` with `ES256` (use `jq` or a targeted edit; back the file up first per the CLI-edit rule and delete the backup after `make validate-openapi` passes). Then `make validate-openapi && make generate-api`.

- [ ] **Step 4: Run**

Run: `make test-unit name=TestGetJWKS count1=true`, `make test-unit name=TestOIDC count1=true` (discovery tests, if any assert `HS256`; update them).

- [ ] **Step 5: Commit**

```bash
git add auth/handlers_openid.go auth/jwks_test.go auth/service_test_helpers.go api-schema/tmi-openapi.json api/api.go
git commit -m "feat(auth): publish every verify key at /.well-known/jwks.json (#965)"
```

---

### Task 4: Bootstrap config surface (`internal/config`)

**Files:**
- Modify: `internal/config/config.go:170-177` (`JWTConfig`), `:973` and `:1119-1135` (`validateJWTSecret`), `:1051` (secret-reference list), `internal/config/setting_defs_auth.go:19-36`, `internal/config/process_env.go` (`TMI_JWT_KEY_ID` entry), `cmd/server/main.go:1965-1990` (comment), `cmd/server/startup_checks.go` if it references `Auth.JWT.Secret`
- Tests: `internal/config/config_test.go`, `internal/config/migratable_settings_test.go`, `internal/config/secret_reference_test.go`, `cmd/server/startup_checks_test.go` (replace `Secret:` with `Keyring:`)
- Regenerate: `config-example.yml`, `config-reference.md`

- [ ] **Step 1: Write the failing test** (append to `internal/config/config_test.go`)

```go
func TestJWTKeyring_ConfigSurface(t *testing.T) {
	t.Setenv("TMI_JWT_KEYRING", `{"signing":{"kid":"k","private_pem":"x"},"verify":[]}`)
	t.Setenv("TMI_DATABASE_URL", "postgres://u:p@localhost:5432/d?sslmode=disable")
	cfg, err := Load("")
	require.NoError(t, err, "Load does not validate the keyring; auth.NewService does")
	require.Equal(t, `{"signing":{"kid":"k","private_pem":"x"},"verify":[]}`, cfg.Auth.JWT.Keyring)

	// Deprecated keys are tolerated and reported, not fatal.
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yml")
	require.NoError(t, os.WriteFile(path, []byte("auth:\n  jwt:\n    secret: old\n    signing_method: HS256\n"), 0o600))
	cfg, err = Load(path)
	require.NoError(t, err)
	require.Equal(t, "old", cfg.Auth.JWT.DeprecatedSecret)
}

func TestSecretReferences_IncludeJWTKeyring(t *testing.T) {
	c := &Config{}
	c.Auth.JWT.Keyring = "file:///etc/tmi-jwt/keyring.json"
	fields := c.bootstrapSecretFields() // the slice at config.go:1051, extracted into a method if it is inline
	found := false
	for _, f := range fields {
		if f.name == "auth.jwt.keyring" {
			found = true
		}
	}
	require.True(t, found)
}
```

Adapt the second test to however the list at `config.go:1051` is shaped (`sed -n 1036,1060p internal/config/config.go`).

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestJWTKeyring_ConfigSurface count1=true`
Expected: unknown field `Keyring`.

- [ ] **Step 3: Implement**

`config.go` `JWTConfig`:

```go
type JWTConfig struct {
	// Keyring is the ES256 keyring document (auth/jwt_keyring.go), one JSON
	// value; tmi-rotator writes it into tmi-secrets. Supports file:// and
	// env:// references like the other bootstrap secrets.
	Keyring string `yaml:"keyring" env:"TMI_JWT_KEYRING"`
	// DeprecatedSecret and DeprecatedSigningMethod accept the pre-#965 keys so
	// an old config file still loads; they are ignored and logged once.
	DeprecatedSecret        string `yaml:"secret" env:"TMI_JWT_SECRET"`
	DeprecatedSigningMethod string `yaml:"signing_method" env:"TMI_JWT_SIGNING_METHOD"`
	ExpirationSeconds       int    `yaml:"expiration_seconds" env:"TMI_JWT_EXPIRATION_SECONDS"`
	RefreshTokenDays        int    `yaml:"refresh_token_days" env:"TMI_REFRESH_TOKEN_DAYS"`
	SessionLifetimeDays     int    `yaml:"session_lifetime_days" env:"TMI_SESSION_LIFETIME_DAYS"`
}
```

Delete `validateJWTSecret` and its call at `:973` (and its use inside `validateJWT`); the keyring is validated where it is consumed (`auth.NewService` -> `NewJWTKeyManager`), so `dbtool` and other config consumers no longer need a JWT value. In `loadConfig` (or `Load`), after env overrides: if `DeprecatedSecret != "" || DeprecatedSigningMethod != ""` log `slogging.Get().Warn("auth.jwt.secret / auth.jwt.signing_method are ignored since #965 (ES256 keyring); remove them from the config")`. In the secret-reference list at `:1051` replace `{"auth.jwt.secret", &c.Auth.JWT.Secret}` with `{"auth.jwt.keyring", &c.Auth.JWT.Keyring}`.

`setting_defs_auth.go`: the `auth.jwt.secret` entry becomes

```go
	{
		Key:         "auth.jwt.keyring",
		Class:       bootstrapClass(true, VisibilityInternal, true),
		Type:        "string",
		Description: "JWT ES256 keyring document (signing key + verify set); written by tmi-rotator",
		YAMLPath:    "auth.jwt.keyring",
		EnvVar:      "TMI_JWT_KEYRING",
		Get:         func(c *Config) string { return c.Auth.JWT.Keyring },
	},
```

and the `auth.jwt.signing_method` entry is deleted (the registry must not advertise a no-op). `process_env.go`: delete the `TMI_JWT_KEY_ID` entry; add `{Name: "TMI_JWT_SECRET", Binary: "server", Secret: true, Purpose: "Deprecated since #965: ignored with a warning; use TMI_JWT_KEYRING"}` only if the allowlist test requires every `env:` tag to be documented (`make test-unit name=TestRepoTMIEnvTokens count1=true` tells you). `cmd/server/main.go:1969,1987` comments: `Auth.JWT.Secret` -> `Auth.JWT.Keyring`. `startup_checks.go`: `rg -n "JWT.Secret" cmd/server` and switch to `Keyring`.

Then `make generate-config-example && make generate-config-docs`.

- [ ] **Step 4: Run**

Run: `make build-server`, `make test-unit name=TestJWTKeyring_ConfigSurface count1=true`, `make test-unit name=TestConfig count1=true`, `make test-unit` (whole suite: every test touched in Task 2 must compile), `make lint`.
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add internal/config cmd/server config-example.yml config-reference.md
git commit -m "feat(config)!: replace auth.jwt.secret with the auth.jwt.keyring bootstrap secret (#965)"
```

---

### Task 5: `JWTKeyringRotation`

**Files:**
- Create: `internal/rotator/jwt_keyring.go`
- Test: `internal/rotator/jwt_keyring_test.go`
- Modify: `cmd/rotator/main.go` (`TMI_ROTATOR_JWT_PREVIOUS_GRACE`, register first), `internal/config/process_env.go` (+ `make generate-config-docs`), `scripts/rotate-secret.py` (`VALID` gains `jwt-keyring`)

**Interfaces:**
- Consumes (PR 1): `Env`, `Env.Transition`, `Env.WaitServerRolled`, `AnnPhase`, `AnnPromotedAt`, `AnnRotatedAt`, `MemorySecretStore`, `NewFakeRolloutWaiter`, `testEnv` (test helper in `rotator_test.go`).
- Consumes (Task 1): `auth.ParseJWTKeyring`, `auth.NewJWTKeyring`, `auth.GenerateJWTKeyPair`, `auth.JWTKeyring.String`.
- Produces: `NewJWTKeyringRotation(previousGrace time.Duration) *JWTKeyringRotation`, `Name() == "jwt-keyring"`, Secret key `JWTKeyringKey = "TMI_JWT_KEYRING"`, `ErrGraceTooShort`.

Phases (one per run; each `Run` returns after at most one Secret data write):

| phase | condition | write | next |
|---|---|---|---|
| `""`, no keyring | bootstrap | keyring {signing NEW, verify [NEW]}; rotated-at = now | done (no rollout wait: the server may not be running yet) |
| `""`, keyring present | due/forced | verify += NEW (signing unchanged); `TMI_JWT_KEYRING_NEXT` = NEW's signing entry | `staged` |
| `staged` | server rolled since stage | signing = NEW from `TMI_JWT_KEYRING_NEXT` (then deleted), verify unchanged, promoted-at = now, rotated-at = now | `promoted` |
| `promoted` | server rolled since promote AND now >= promoted-at + grace | verify = [signing only] | done (rotated-at stays = promoted-at) |

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/auth"
)

func TestJWTKeyringRotation_BootstrapWritesSingleKey(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	r := NewJWTKeyringRotation(24 * time.Hour)
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	kr, err := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
	require.NoError(t, err)
	require.Len(t, kr.Verify, 1)
	require.Empty(t, s.Annotations[AnnPhase+"jwt-keyring"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnRotatedAt+"jwt-keyring"])
	require.Equal(t, 1, st.DataWrites)
}

func TestJWTKeyringRotation_OnePhasePerRun(t *testing.T) {
	kr0, _ := auth.NewJWTKeyring()
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{JWTKeyringKey: kr0.String()}, Annotations: map[string]string{}})
	r := NewJWTKeyringRotation(24 * time.Hour)

	// Run 1: stage.
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	kr1, _ := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
	require.Equal(t, kr0.Signing.KID, kr1.Signing.KID, "still signing with the old key")
	require.Len(t, kr1.Verify, 2)
	require.Equal(t, "staged", s.Annotations[AnnPhase+"jwt-keyring"])
	require.Equal(t, 1, st.DataWrites)

	// Run 2: promote.
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), "tmi-secrets")
	kr2, _ := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
	require.NotEqual(t, kr0.Signing.KID, kr2.Signing.KID)
	require.Len(t, kr2.Verify, 2)
	require.Equal(t, "promoted", s.Annotations[AnnPhase+"jwt-keyring"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnPromotedAt+"jwt-keyring"])
	require.Equal(t, 2, st.DataWrites)

	// Run 3, same day: parked (grace not elapsed).
	require.NoError(t, r.Run(context.Background(), env))
	require.Equal(t, 2, st.DataWrites)

	// Run 4, next day: drop.
	env.Now = func() time.Time { return time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC) }
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), "tmi-secrets")
	kr3, _ := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
	require.Len(t, kr3.Verify, 1)
	require.Equal(t, kr2.Signing.KID, kr3.Signing.KID)
	require.Empty(t, s.Annotations[AnnPhase+"jwt-keyring"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnRotatedAt+"jwt-keyring"], "rotation date is the promotion date")
	require.Equal(t, 3, st.DataWrites)
}

func TestJWTKeyringRotation_StageWaitsForRollBeforePromote(t *testing.T) {
	kr0, _ := auth.NewJWTKeyring()
	skB, vkB, _ := auth.GenerateJWTKeyPair()
	nextJSON, _ := json.Marshal(skB)
	staged := &auth.JWTKeyring{Signing: kr0.Signing, Verify: []auth.JWTVerifyKey{kr0.Verify[0], vkB}}
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{JWTKeyringKey: staged.String(), JWTKeyringNextKey: string(nextJSON)},
		Annotations: map[string]string{AnnPhase + "jwt-keyring": "staged", AnnGeneration + "jwt-keyring": "7"}})
	// DataWrites 0 <= 7: the server has not rolled; promote must not happen.
	err := NewJWTKeyringRotation(24 * time.Hour).Run(context.Background(), env)
	require.Error(t, err)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "staged", s.Annotations[AnnPhase+"jwt-keyring"])
}

func TestJWTKeyringRotation_GraceFloor(t *testing.T) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	err := NewJWTKeyringRotation(30 * time.Minute).Run(context.Background(), env)
	require.ErrorIs(t, err, ErrGraceTooShort)
}
```

The staged keyring only holds NEW's public key, so the stage write also stores NEW's signing entry under `TMI_JWT_KEYRING_NEXT` (same data write, no extra roll); promote moves it into `signing` and deletes it. Add to the one-phase test after run 1: `require.NotEmpty(t, s.Data[JWTKeyringNextKey])` and after run 2: `_, has := s.Data[JWTKeyringNextKey]; require.False(t, has)`.

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestJWTKeyringRotation count1=true`
Expected: `undefined: NewJWTKeyringRotation`.

- [ ] **Step 3: Implement `internal/rotator/jwt_keyring.go`**

```go
package rotator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	JWTKeyringKey     = "TMI_JWT_KEYRING"
	JWTKeyringNextKey = "TMI_JWT_KEYRING_NEXT" // pending signing key between stage and promote
	jwtRotationName   = "jwt-keyring"
	jwtPhaseStaged    = "staged"
	jwtPhasePromoted  = "promoted"
	minJWTGrace       = time.Hour
)

// ErrGraceTooShort rejects a previous-key grace below the default access-token lifetime.
var ErrGraceTooShort = errors.New("jwt previous-key grace must be at least 1h (longer than auth.jwt.expiration_seconds)")

// JWTKeyringRotation rotates the ES256 keyring one phase per run: stage, promote, drop.
// SEM@<sha>: phased JWT keyring rotation advancing one phase per run
type JWTKeyringRotation struct{ previousGrace time.Duration }

// SEM@<sha>: build a JWTKeyringRotation (pure)
func NewJWTKeyringRotation(previousGrace time.Duration) *JWTKeyringRotation {
	return &JWTKeyringRotation{previousGrace: previousGrace}
}

// SEM@<sha>: return the rotation name (pure)
func (r *JWTKeyringRotation) Name() string { return jwtRotationName }

// SEM@<sha>: advance the JWT keyring rotation by one phase from its recorded state
func (r *JWTKeyringRotation) Run(ctx context.Context, env *Env) error {
	if r.previousGrace < minJWTGrace {
		return ErrGraceTooShort
	}
	logger := slogging.Get()
	s, err := env.Secrets.Get(ctx, env.SecretName)
	if err != nil {
		return err
	}
	name := r.Name()
	switch phase := s.Annotations[AnnPhase+name]; phase {
	case "":
		if s.Data[JWTKeyringKey] == "" {
			kr, err := auth.NewJWTKeyring()
			if err != nil {
				return err
			}
			logger.Info("JWT keyring bootstrapped kid=%s", kr.Signing.KID)
			return env.Transition(ctx, name, "", func(s *Secret) { s.Data[JWTKeyringKey] = kr.String() })
		}
		kr, err := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
		if err != nil {
			return err
		}
		if len(kr.Verify) != 1 {
			return fmt.Errorf("keyring has %d verify keys; finish or clean up the previous rotation first", len(kr.Verify))
		}
		sk, vk, err := auth.GenerateJWTKeyPair()
		if err != nil {
			return err
		}
		next, _ := json.Marshal(sk)
		kr.Verify = append(kr.Verify, vk)
		logger.Info("JWT key staged kid=%s", vk.KID)
		return env.Transition(ctx, name, jwtPhaseStaged, func(s *Secret) {
			s.Data[JWTKeyringKey] = kr.String()
			s.Data[JWTKeyringNextKey] = string(next)
		})

	case jwtPhaseStaged:
		if err := env.WaitServerRolled(ctx, s, name); err != nil {
			return fmt.Errorf("staged key rollout: %w", err)
		}
		kr, err := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
		if err != nil {
			return err
		}
		var next auth.JWTSigningKey
		if err := json.Unmarshal([]byte(s.Data[JWTKeyringNextKey]), &next); err != nil || next.KID == "" {
			return fmt.Errorf("%s missing or invalid; cannot promote", JWTKeyringNextKey)
		}
		kr.Signing = next
		if _, err := auth.ParseJWTKeyring(kr.String()); err != nil {
			return fmt.Errorf("promoted keyring invalid: %w", err)
		}
		now := env.Now().UTC().Format(time.RFC3339)
		logger.Info("JWT key promoted kid=%s", next.KID)
		return env.Transition(ctx, name, jwtPhasePromoted, func(s *Secret) {
			s.Data[JWTKeyringKey] = kr.String()
			delete(s.Data, JWTKeyringNextKey)
			s.Annotations[AnnPromotedAt+name] = now
			s.Annotations[AnnRotatedAt+name] = now
		})

	case jwtPhasePromoted:
		if err := env.WaitServerRolled(ctx, s, name); err != nil {
			return fmt.Errorf("promoted key rollout: %w", err)
		}
		promotedAt, err := time.Parse(time.RFC3339, s.Annotations[AnnPromotedAt+name])
		if err != nil {
			return fmt.Errorf("missing promoted-at annotation for %s", name)
		}
		if env.Now().Before(promotedAt.Add(r.previousGrace)) {
			logger.Info("JWT previous key kept until %s", promotedAt.Add(r.previousGrace).Format(time.RFC3339))
			return nil
		}
		kr, err := auth.ParseJWTKeyring(s.Data[JWTKeyringKey])
		if err != nil {
			return err
		}
		var keep []auth.JWTVerifyKey
		for _, v := range kr.Verify {
			if v.KID == kr.Signing.KID {
				keep = append(keep, v)
			}
		}
		kr.Verify = keep
		logger.Info("JWT old verify keys dropped; remaining kid=%s", kr.Signing.KID)
		return env.Transition(ctx, name, "", func(s *Secret) {
			s.Data[JWTKeyringKey] = kr.String()
			delete(s.Annotations, AnnPromotedAt+name)
			s.Annotations[AnnRotatedAt+name] = promotedAt.UTC().Format(time.RFC3339)
		})

	default:
		return fmt.Errorf("unknown %s phase %q", name, phase)
	}
}
```

`cmd/rotator/main.go`: add `JWTPreviousGrace time.Duration` to `options` from `TMI_ROTATOR_JWT_PREVIOUS_GRACE` (default `24h`), and put `rotator.NewJWTKeyringRotation(o.JWTPreviousGrace)` FIRST in the `rotations` slice (bootstrap must precede anything that waits for a server roll). Because the rotator reads the Secret before connecting to Redis/DB, nothing else changes. `process_env.go`: add `TMI_ROTATOR_JWT_PREVIOUS_GRACE` (Binary `rotator`) and `{Name: "TMI_JWT_KEYRING_NEXT", Binary: "rotator", Secret: true, Purpose: "Secret data key written by tmi-rotator between stage and promote; never read from the environment"}`: `TestRepoTMIEnvTokens_AreAllDocumented` scans every non-test `.go` file for `TMI_*` tokens, Secret-key literals included. Then `make generate-config-docs`. `scripts/rotate-secret.py`: `VALID` gains `"jwt-keyring"`. `deployments/k8s/dev/rotator.yml` header comment: mention the JWT keyring.

- [ ] **Step 4: Run**

Run: `make test-unit name=TestJWTKeyringRotation count1=true`, `make test-unit name=TestLoadOptions count1=true`, `make test-unit name=TestRepoTMIEnvTokens count1=true`, `make build-rotator`, `make lint`.

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/jwt_keyring.go internal/rotator/jwt_keyring_test.go cmd/rotator/main.go internal/config/process_env.go config-reference.md scripts/rotate-secret.py deployments/k8s/dev/rotator.yml
git commit -m "feat(rotator): stage, promote and drop JWT signing keys (#965)"
```

---

### Task 6: Manifests, bootstrap trigger, Terraform, harness

**Files:**
- Modify: `deployments/k8s/dev/server.yml`, `deployments/k8s/dev/server-oracle.yml`, `deployments/k8s/dev/aws/patches/server-config.yaml`
- Modify: `scripts/lib/deploy.py` (`start()`), `scripts/deploy-aws.sh` (`apply_overlay` -> bootstrap)
- Modify: `terraform/modules/secrets/aws/{main,outputs}.tf`, `terraform/modules/kubernetes/aws/{k8s_resources,variables}.tf`, `terraform/environments/aws-public/main.tf`
- Modify: `test/integration/tlsgen/main.go` (PR 6) and `scripts/run-integration-tests.py`
- Modify: `deployments/k8s/dev/aws/README.md`, `auth/README.md`

- [ ] **Step 1: Server env**

In `server.yml` (and the `$patch: replace` list in `aws/patches/server-config.yaml`, and `server-oracle.yml`), replace the `TMI_JWT_SECRET` secretKeyRef (AWS patch) / add (base):

```yaml
            # ES256 keyring (#965). tmi-rotator writes it: the first run of the
            # CronJob bootstraps it and deploy.py/deploy-aws.sh trigger that run,
            # so a fresh cluster's server restarts once the key exists.
            - name: TMI_JWT_KEYRING
              valueFrom: { secretKeyRef: { name: tmi-secrets, key: TMI_JWT_KEYRING } }
```

Local dev previously took the JWT secret from `config-development.yml`; it is now ignored (Task 4 warning).

- [ ] **Step 2: Bootstrap trigger**

`scripts/lib/deploy.py`, new function called from `start()` right after `apply_overlay(...)` and before the `rollout restart` calls:

```python
def bootstrap_secrets_via_rotator() -> None:
    """Run tmi-rotator once now if tmi-secrets lacks the JWT keyring (#965).
    The Job also rotates anything due; on a fresh cluster that is every
    secret, which is what we want (the seeds leave Terraform/dev files)."""
    if _secret_has_key("tmi-secrets", "TMI_JWT_KEYRING"):
        return
    job = f"tmi-rotator-bootstrap-{int(time.time())}"
    kubectl(["-n", NS, "create", "job", job, "--from=cronjob/tmi-rotator"])
    kubectl(["-n", NS, "wait", "--for=condition=complete", f"job/{job}", "--timeout=20m"])
    log_success("tmi-rotator bootstrapped tmi-secrets (JWT keyring)")
```

`scripts/deploy-aws.sh`: the same three commands as a `bootstrap_secrets()` function called in `main()` right after `apply_overlay`, guarded by `kubectl -n "${NAMESPACE}" get secret tmi-secrets -o jsonpath='{.data.TMI_JWT_KEYRING}' | grep -q .`.

- [ ] **Step 3: Terraform**

Delete `random_password.jwt_secret`, `aws_secretsmanager_secret.jwt_secret` + version, outputs `jwt_secret_arn/_name/jwt_secret`, the `jwt_secret` entry of `secret_arns`; delete `variable "jwt_secret"` (`terraform/modules/kubernetes/aws/variables.tf:160`), the `jwt_secret = module.secrets.jwt_secret` line in `aws-public/main.tf:331`, and the `TMI_JWT_SECRET` line in `kubernetes_secret_v1.tmi` data. `secret_arns` now holds only `db_credentials` (PR 3 removes that and the IRSA policy statement together). `AWS_PROFILE=tmi terraform validate` in `terraform/environments/aws-public`; read `terraform plan` (expected: one Secrets Manager secret destroyed, `tmi-secrets` shows no data diff thanks to `ignore_changes`).

- [ ] **Step 4: Integration harness**

Extend PR 6's `test/integration/tlsgen/main.go` to append `TMI_JWT_KEYRING=<compact JSON>` to `secrets.env` on generation (import `github.com/ericfitz/tmi/auth`, call `auth.NewJWTKeyring().String()`); no quoting: `docker --env-file` takes the value verbatim after the first `=`. `scripts/run-integration-tests.py` needs no change if the server container already gets `--env-file secrets.env` (PR 6); confirm with `rg -n "env-file" scripts/run-integration-tests.py`. Regenerate the harness dir once (`rm -rf .local/test-tls`) so the new key appears.

- [ ] **Step 5: Docs**

`auth/README.md`: replace the HS256/`TMI_JWT_SECRET` paragraph with the keyring contract (document shape, `kid` = RFC 7638, JWKS URL, rotation phases, "no HS256 verify path"). `deployments/k8s/dev/aws/README.md`: `TMI_JWT_SECRET` -> `TMI_JWT_KEYRING`, rotator-owned.

- [ ] **Step 6: Commit**

```bash
git add deployments/k8s scripts/lib/deploy.py scripts/deploy-aws.sh terraform test/integration/tlsgen auth/README.md
git commit -m "feat(deploy): JWT keyring from tmi-secrets, bootstrapped by tmi-rotator (#965)"
```

---

### Task 7: Integration tests

**Files:**
- Create: `test/integration/workflows/jwt_es256_test.go`

- [ ] **Step 1: Write the tests**

```go
package workflows

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/test/integration/framework"
)

func TestJWTES256Integration_TokenVerifiesAgainstJWKS(t *testing.T) {
	serverURL := os.Getenv("TMI_SERVER_URL")
	tokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "authenticate")

	parsed, _, err := jwt.NewParser().ParseUnverified(tokens.AccessToken, jwt.MapClaims{})
	framework.AssertNoError(t, err, "parse")
	if parsed.Header["alg"] != "ES256" || parsed.Header["kid"] == "" {
		t.Fatalf("expected ES256 with kid, got %v", parsed.Header)
	}

	anon, _ := framework.NewUnauthenticatedClient(serverURL)
	resp, err := anon.Do(framework.Request{Method: http.MethodGet, Path: "/.well-known/jwks.json"})
	framework.AssertNoError(t, err, "jwks")
	framework.AssertStatusOK(t, resp)
	var jwks auth.JWKSResponse
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &jwks), "jwks json")
	_, err = jwt.Parse(tokens.AccessToken, func(tk *jwt.Token) (any, error) {
		for _, k := range jwks.Keys {
			if k.KID == tk.Header["kid"] {
				x, _ := base64.RawURLEncoding.DecodeString(k.X)
				y, _ := base64.RawURLEncoding.DecodeString(k.Y)
				return ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
			}
		}
		return nil, jwt.ErrTokenUnverifiable
	}, jwt.WithValidMethods([]string{"ES256"}))
	framework.AssertNoError(t, err, "verify against JWKS")
}

func TestJWTES256Integration_HS256TokenIsRejected(t *testing.T) {
	serverURL := os.Getenv("TMI_SERVER_URL")
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "alice", "exp": time.Now().Add(time.Hour).Unix()})
	s, _ := tok.SignedString([]byte("legacy-secret"))
	client, _ := framework.NewClient(serverURL, &framework.OAuthTokens{AccessToken: s})
	resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
	framework.AssertNoError(t, err, "request")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("HS256 token must be 401, got %d", resp.StatusCode)
	}
}

// The Docker-run server cannot reload its keyring, so the rotation itself is
// exercised against a MemorySecretStore only: it proves the phase machine and
// that a token signed before the rotation still verifies against the staged
// keyring. The live cutover is verified on the k3s cluster (plan Task 8).
func TestJWTES256Integration_RotationKeepsOldTokensValid(t *testing.T) {
	kr0, _ := auth.NewJWTKeyring()
	m0, _ := auth.NewJWTKeyManager(auth.JWTConfig{Keyring: kr0.String(), ExpirationSeconds: 60})
	old, _ := m0.CreateToken(&jwt.RegisteredClaims{Subject: "x", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))})

	st := rotator.NewMemorySecretStore(&rotator.Secret{Name: "tmi-secrets", Data: map[string]string{rotator.JWTKeyringKey: kr0.String()}, Annotations: map[string]string{}})
	env := &rotator.Env{Secrets: st, Rollouts: rotator.NewFakeRolloutWaiter(st, "tmi-server"), SecretName: "tmi-secrets", ServerDeployment: "tmi-server", RolloutTimeout: time.Second, Now: time.Now}
	r := rotator.NewJWTKeyringRotation(time.Hour)
	var calls int32
	for i := 0; i < 2; i++ { // stage, promote
		framework.AssertNoError(t, r.Run(context.Background(), env), "run")
		atomic.AddInt32(&calls, 1)
		s, _ := st.Get(context.Background(), "tmi-secrets")
		m, err := auth.NewJWTKeyManager(auth.JWTConfig{Keyring: s.Data[rotator.JWTKeyringKey], ExpirationSeconds: 60})
		framework.AssertNoError(t, err, "keyring loads")
		_, err = m.VerifyToken(old, &jwt.RegisteredClaims{})
		framework.AssertNoError(t, err, "old token still valid after phase")
	}
}
```

Adjust `framework.OAuthTokens`/`resp.Body` field names to the framework's real types (`rg -n "type OAuthTokens|type Response" test/integration/framework/*.go`).

- [ ] **Step 2: Run**

`cd test/integration && go mod tidy`; `make test-integration`. Expected: the three tests execute and pass; the rest of the suite is unchanged (every test authenticates through the OAuth stub, which stores opaque tokens).

- [ ] **Step 3: Commit**

```bash
git add test/integration
git commit -m "test(integration): ES256 tokens, JWKS verification and HS256 rejection (#965)"
```

---

### Task 8: Cutover verification, consumers, PR

- [ ] **Step 1: Consumers before merge (spec).** On Agentbus, post to tmi-mcp and the addons channel: "TMI JWTs move to ES256 with `kid`; old tokens 401 once, refresh continues; JWKS at `/.well-known/jwks.json`; anyone verifying locally must use JWKS." Wait for acks or 24h.
- [ ] **Step 2: k3s cutover.** With the pre-PR server running and a tmi-ux session open (browser), deploy this branch (`CLUSTER=k3s make dev-up`; needs `.local/k3s.json`, #998). Expected: the rotator bootstrap Job completes, the server comes up, the open tmi-ux tab hits 401 once and refreshes silently. Then open a diagram for collaboration: the WebSocket handshake with the refreshed token must connect. If the WS path does not refresh on 401 (spec's open item), stop and ask: "This appears to be a client bug. Would you like me to file a bug against tmi-ux?" and use `/file-client-bug` if yes.
- [ ] **Step 3: Rotation on k3s.** `make rotate-secret name=jwt-keyring` three times (stage, promote; the third parks on grace), with `TMI_ROTATOR_JWT_PREVIOUS_GRACE=1h` temporarily set to `1h` and the clock allowed to pass, or wait a day for the CronJob. After promote, an old tmi-ux session keeps working without re-login; after drop, `/.well-known/jwks.json` has one key.
- [ ] **Step 4: Gates.** `make lint`, `make build-server`, `make test-unit`, `make test-integration`, `make validate-openapi`, `security-review` skill.
- [ ] **Step 5: PR.** Push; `gh pr create --title "feat(auth)!: sign JWTs with ES256 from a rotatable JWKS keyring (#965)"` with the body listing the cutover behaviour, the consumer acks, the k3s results and `Refs #965`; attribution trailer.
- [ ] **Step 6: After merge.** Ping `dm/tmi-wiki` on Agentbus: 14 wiki pages document HS256 / `TMI_JWT_SECRET` (`rg -l "HS256|TMI_JWT_SECRET" /Users/efitz/Projects/tmi.wiki`); hand over the keyring contract from `auth/README.md` and the rotation phases for the `Secret-Rotation.md` runbook (PR 1 Task 13).

## Self-review notes (writer)

- Spec §3 coverage: keyring document and `kid` (Tasks 1-2), JWKS (Task 3), config replacement and no default key (Task 4), rotation phases (Task 5), HS256 cutover and consumer checks (Tasks 7-8), devenv/integration keyring generation (Task 6).
- `testEnv` is defined in PR 1's `rotator_test.go`; PR 2's tests reuse it (same package).
- `JWTKeyringNextKey` was added while writing Task 5; the phase table's "stage" row writes it too.
