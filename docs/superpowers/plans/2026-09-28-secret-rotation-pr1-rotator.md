# Secret Rotation PR 1: tmi-rotator, Redis password, settings key (#965) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the in-cluster `tmi-rotator` CronJob and use it to rotate the Redis password and the settings-encryption key on schedule, with the key id carried in the `ENC:` envelope and a resumable `ReEncryptAll`.

**Architecture:** A new `internal/rotator` package holds phase state in annotations on the Kubernetes Secret `tmi-secrets` and advances each rotation one idempotent phase at a time (add new credential, write Secret, wait for Stakater Reloader to roll `tmi-server`, retire the old credential). `cmd/rotator` is a second binary in the server image, run daily by a CronJob in every overlay. `internal/crypto.SettingsEncryptor` becomes a keyring that selects the decryption key by the id in the envelope; `SettingsService.ReEncryptAll` becomes batched and resumable. Terraform seeds `tmi-secrets` once and then ignores its data.

**Tech Stack:** Go 1.26, client-go v0.37 (already in go.mod), go-redis v9 (`ACL SETUSER`), GORM, kustomize overlays, Terraform (AWS), uv Python for scripts.

**Spec:** `docs/superpowers/specs/2026-09-28-secret-rotation-design.md` (sections 1, 2, 5). Depends on PR 6: `docs/superpowers/specs/2026-09-28-redis-nats-tls-design.md`.

## Open questions (resolved 2026-09-28)

HUMAN DECISIONS (Eric, 2026-09-28): (A) item 4 is OUT of #965: webhook secrets (plaintext `WebhookSubscription.Secret`) and content-token key rotation get a separate issue; PR 1 re-encrypts `system_settings` only. (B) item 7 is IN scope: Redis persistence (PVC + AOF) is Task 5 of this plan, for every cluster flavour that runs in-cluster Redis. All other items keep the defaults stated below.

HUMAN DECISIONS (Eric, 2026-09-29): single EBS CSI controller replica (TMI runs stateful single-instance pods); Task 5 step 5 sets `controller.replicaCount = 1`.

Nothing here changes an approved decision; each item is a place where the spec text is infeasible or silent, and the plan records the choice it makes so it can be overturned cheaply.

0. **Resolved while planning:** `TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID` is not seeded by Terraform and is `optional: true` on the server (an existing `tmi-secrets` never gains keys from Terraform once `ignore_changes` is on; a missing id means 1), so nothing wedges on the first AWS apply.
1. **Forced rotation command.** `kubectl create job --from=cronjob/tmi-rotator` cannot set an env var, so the spec's literal command is infeasible. The plan adds `make rotate-secret name=<rotation>` (`scripts/rotate-secret.py`: dry-run the CronJob's job template, inject `ROTATE=<name>`, apply). Same effect, one wrapper.
2. **100-day alarm shape.** A CloudWatch alarm cannot look back 100 days (period x evaluation periods is capped at one day). The plan has the rotator log `secret=<name> age_days=<n>` on every run; a log metric filter extracts `age_days` and the alarm fires on `Maximum > 100` over one day with `treat_missing_data = breaching` (a CronJob that stops running is also an incident).
3. **`ReEncryptAll` atomicity (#845 reversal, FYI).** The spec's resumable, per-row transactions replace the single-transaction pass chosen in #845. The handler's "rolled back, nothing changed" 503 wording and the code comments are updated to "partial progress is kept; retry finishes it".
4. **"Webhook secrets included" is not true today.** `WebhookSubscription.Secret` (`api/models/models.go:661`) is stored in plaintext (varchar 128) and content tokens use a separate `TMI_CONTENT_TOKEN_ENCRYPTION_KEY`. Under the settings key, the only `ENC:` column is `system_settings.value`; Redis holds the rest (all with TTLs). This plan re-encrypts `system_settings` only. Encrypting webhook secrets, or rotating the content-token key, would be a new feature. **Resolved: out of scope (decision A); separate issue to be filed.**
5. **Secrets Manager removal is split per PR.** Removing the DB and JWT copies in PR 1 would break `deploy-aws.sh import_config` (DB credentials) and PR 2's not-yet-existing keyring. PR 1 removes the Redis and settings-key copies; PR 2 removes JWT; PR 3 removes DB credentials.
6. **Oracle dev overlay (`docker-desktop-oracle`).** The rotator is not wired into it in PR 1: the Oracle server reads `TMI_DATABASE_URL` from `tmi-oracle-db`, not `tmi-secrets`, and PR 3 does the Oracle wiring anyway. `ReEncryptAll` on Oracle is covered by `make test-integration-oci`.
7. **Redis persistence.** PR 6 defers it to #965 and the #965 spec only notes it. **Resolved: IN scope (decision B); new Task 5** (PVC + AOF `everysec`, `Recreate`, longhorn/gp3 patches, EBS CSI addon in Terraform; ACL state deliberately not persisted, the `default` user is rebuilt from `tmi-secrets` at every start).
8. **Previous-key grace period.** The Redis `ENC:` TTLs are operator-configurable (`auth.jwt.refresh_token_days`, `auth.jwt.session_lifetime_days`, default 7 days). The plan uses a fixed `TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE` (default `192h` = 8 days) rather than reading the live settings; the runbook says to raise it if those settings exceed 7 days.

## Post-PR 6 state (verified 2026-09-29 against `main` 3aee8c76, after #991/#995/#998)

- `deployments/k8s/dev/redis.yml` starts Redis with `--tls-port 6379`, `--requirepass $(REDIS_PASSWORD)` from `tmi-secrets/TMI_REDIS_PASSWORD`, and carries `secret.reloader.stakater.com/reload: "redis-tls"` (NOT `auto`), so Redis never rolls when `tmi-secrets` changes.
- `deployments/k8s/dev/server.yml` carries `reloader.stakater.com/auto: "true"` and reads `TMI_REDIS_PASSWORD`, `TMI_REDIS_TLS_ENABLED=true`, `TMI_REDIS_TLS_CA_FILE=/etc/tmi-redis-tls/ca.crt` (Secret `redis-tls` mounted at `/etc/tmi-redis-tls`).
- `scripts/lib/deploy.py` creates `tmi-secrets` with a random `TMI_REDIS_PASSWORD` on docker-desktop/k3s when absent (PR 6 plan names it `ensure_redis_password_secret()`). It must merge, never replace: this plan adds keys to the same function.
- `auth/db.RedisConfig` gained `TLSEnabled bool` and `TLSCAFile string`; `internal/tlsconfig.Load(caFile, certFile, keyFile string) (*tls.Config, error)` exists.
- `deployments/k8s/platform/reloader.yml` is applied by `deploy.py apply_platform_base` and `deploy-aws.sh apply_platform_base`, except where a Helm-managed Reloader (or cert-manager) already exists, which is reused instead (PR 6 as merged; the k3s lab cluster is such a cluster, so Task 14's Reloader check runs against the Helm install there).
- Reloader rolls a Deployment when the **data** of a referenced Secret changes; annotation-only changes do not roll (confirmed on k3s in Task 14 before relying on it).

## Global Constraints

- **Make targets only.** `make build-server`, `make build-rotator` (new), `make lint`, `make test-unit name=TestX count1=true`, `make test-integration`, `make validate-openapi`, `make generate-api`, `make generate-config-docs`. Never `go test`/`go run` directly.
- **Logging:** only `github.com/ericfitz/tmi/internal/slogging` (`Get()`, `Info/Warn/Error`, `InfoCtx(ctx, msg, attrs...)` for structured fields). Never log a secret value; log key names, ids, phases, counts. `scripts/check-sensitive-log-args.py` runs in `make lint`.
- **Secret safety:** no secret value in argv, env of an operator shell, logs, or chat. Scripts use `umask 077` files and `kubectl --from-file` / `--patch-file`.
- **SEM markers:** every new function/method/type gets `// SEM@<sha>: <intent>` (one line, canonical verb, no mechanism); changed functions get their description updated (keep the sha, tooling refreshes it).
- **Terraform owns** namespace, ConfigMap, Secrets, IRSA SA, and (Task 5) the EBS CSI addon and `gp3` StorageClass; **kustomize overlays own workloads**, including the new CronJob, ServiceAccount, Role, RoleBinding and the `redis-data` PVC.
- **Redis data lifecycle:** the `redis-data` PVC survives `make dev-down` (like the Postgres PVC and `tmi-secrets`) and is deleted with the namespace on `make dev-nuke`. Never delete it while `tmi-secrets` stays, or vice versa: `ENC:` entries need both.
- **Oracle review is mandatory** before the PR is reported complete (Task 13): `ReEncryptAll` touches `system_settings`.
- **Branch:** `feat/965-secret-rotation` (worktree `/Users/efitz/Projects/tmi-965`), rebased on `main` 3aee8c76 (PR 6 merged as #991; #995 removed Tilt; #998 moved lab details to untracked `.local/`). `main` is PR-only.
- **No lab or deployment identifiers in tracked files (#998):** hostnames, IPs, kube context names, registry hosts and personal domains live in `.local/k3s.json` / `.local/aws-deploy.json`; plans, manifests and scripts say "the k3s cluster" and read those files. `CLUSTER=k3s make dev-up` needs `.local/k3s.json` (schema: `deployments/k8s/dev/k3s/k3s.json.example`).
- **Commits:** conventional commits; the PR title is `feat(rotator): scheduled rotation of the Redis password and settings key (#965)`. Every commit ends with:

  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01SzKuNwXFRA31NbzpvwV5Nk
  ```

- **Secret key names (confirmed, spec asked to confirm):** the existing `internal/secrets.SecretKeys` names are reused, so the env vars are `TMI_SECRET_SETTINGS_ENCRYPTION_KEY`, `TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID`, `TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY`, and the new `TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID`. There is no `_KEY_ID`.

## Review Focus

Inputs the spec implies but no test would otherwise exercise, most likely to bite first:

1. A forced run (`ROTATE=redis-password`) overlapping the nightly run: both read `tmi-secrets`, both write. Expected: the second write fails with a conflict and exits non-zero; nothing is lost. (Test in Task 3: `MemorySecretStore` conflict; Task 4: `KubeSecretStore` conflict via `PrependReactor`.)
2. Redis restarted mid-rotation: its ACL is in-memory (the AOF from Task 5 does not persist ACL changes), so `NEW` is gone and `--requirepass` is whatever the Secret held at restart. Expected: resume re-adds `NEW` before waiting. (Task 6 test `TestRedisPasswordRotation_ResumeReAddsNewPassword`.)
3. A `system_settings` row whose ciphertext no key can open (legacy id, wrong key). Expected: reported in `settingErrors`, skipped, never re-selected in a loop, everything else still re-encrypted. (Task 7 test `TestReEncryptAll_UndecryptableRowIsSkippedOnce`.)
4. `tmi.dev/rotate-every.<name>` unparsable (`"3 months"`). Expected: warn, fall back to `90d`, never treat as "due now". (Task 3 test `TestParseRotateEvery`.)
5. A Redis `ENC:` entry encrypted under a dropped key id. Expected: `Decrypt` fails cleanly, the caller treats the entry as missing (existing behaviour: `RedisDB.Get` returns an error), never a panic or a 500 on a hot path. (Task 1 test `TestDecrypt_UnknownIDAfterDrop`; the auth handlers already map a missing refresh token to 401.)

---

## File map

| File | Change |
|---|---|
| `internal/crypto/settings_encryptor.go` | keyring by id: `previousID`, `NewSettingsEncryptorFromKeyring`, `CurrentPrefix`, `ContextIDOf`, id-selecting `Decrypt` |
| `internal/secrets/provider.go` | `SecretKeys.SettingsEncryptionPreviousContextID` |
| `internal/secrets/file_provider.go` (new) | `file` provider: one file per secret key in a directory (for `deploy-aws.sh import_config`) |
| `internal/config/config.go` | `SecretsConfig.FileDir` (`TMI_SECRETS_FILE_DIR`) |
| `api/settings_service.go` | batched, resumable `ReEncryptAll`; `reEncryptOne`; `CountValuesWithContextID` |
| `api/config_handlers.go` | 503 wording for partial progress |
| `api-schema/tmi-openapi.json` | `reencryptSystemSettings` description |
| `internal/rotator/*.go` (new) | `Secret`, `SecretStore`, `MemorySecretStore`, `KubeSecretStore`, `RolloutWaiter`, `KubeRolloutWaiter`, `FakeRolloutWaiter`, `Env`, `Rotation`, `Run`, `RedisPasswordRotation`, `SettingsKeyRotation`, key generation, schedule parsing |
| `cmd/rotator/main.go` (new) | wiring: env, kube client, Redis, DB, `rotator.Run`, exit code |
| `scripts/build-server.py`, `Makefile`, `Dockerfile.server`, `Dockerfile.server-oracle` | build `tmi-rotator` into the server image |
| `deployments/k8s/dev/redis.yml` | AOF (`appendonly yes`, `appendfsync everysec`, `--dir /data`), `Recreate`, `fsGroup`, `/data` volume; new `PersistentVolumeClaim` `redis-data` |
| `deployments/k8s/dev/k3s/patches/redis-storageclass.yaml`, `deployments/k8s/dev/aws/patches/redis-storageclass.yaml` (new); the two `kustomization.yaml` | `storageClassName` longhorn / gp3 on `redis-data` |
| `terraform/modules/kubernetes/aws/main.tf` | `aws-ebs-csi-driver` addon, IRSA role `ebs-csi`, `kubernetes_storage_class_v1.gp3` |
| `deployments/k8s/dev/aws/README.md` | "Redis persistence" paragraph |
| `deployments/k8s/dev/rotator.yml` (new) | ServiceAccount, Role, RoleBinding, CronJob |
| `deployments/k8s/dev/networkpolicy-redis.yml`, `deployments/k8s/dev/k3s/networkpolicy-k3s.yml` | admit `app: tmi-rotator` to Redis and (k3s) Postgres |
| `deployments/k8s/dev/server.yml`, `deployments/k8s/dev/aws/patches/server-config.yaml` | settings-key env from `tmi-secrets` (four keys, previous pair optional) |
| `deployments/k8s/dev/{docker-desktop,k3s,aws}/kustomization.yaml` | add `../rotator.yml` |
| `scripts/lib/deploy.py` | seed settings key (id 1) into `tmi-secrets` if absent; keep `tmi-secrets` (and the `redis-data` PVC) on `dev-down`; PR 6 docstring wording |
| `scripts/rotate-secret.py` (new), `Makefile` | `make rotate-secret name=<rotation>` |
| `terraform/modules/kubernetes/aws/k8s_resources.tf` | `ignore_changes = [data]`, seed `_CONTEXT_ID = "1"` |
| `terraform/modules/secrets/aws/{main,outputs}.tf` | drop the Redis and settings-key Secrets Manager secrets |
| `terraform/environments/aws-public/main.tf` | log metric filter + alarm |
| `scripts/deploy-aws.sh` | `import_config` reads the settings keyring from `tmi-secrets` via the `file` provider |
| `internal/config/process_env.go`, `config-reference.md` | rotator env vars |
| wiki `Security-Operations.md` (or new `Secret-Rotation.md`) | runbook |

---

### Task 1: Settings encryptor keyring by id

**Files:**
- Modify: `internal/crypto/settings_encryptor.go`
- Modify: `internal/secrets/provider.go:100-131`
- Test: `internal/crypto/settings_encryptor_test.go`

**Interfaces:**
- Produces: `NewSettingsEncryptorFromKeyring(current []byte, currentID int, previous []byte, previousID int) (*SettingsEncryptor, error)`, `(*SettingsEncryptor).CurrentPrefix() string`, `ContextIDOf(value string) (int, bool)`, `secrets.SecretKeys.SettingsEncryptionPreviousContextID = "settings_encryption_previous_context_id"`. `NewSettingsEncryptorFromKeys` is unchanged (previousID 0 = unknown, trial decrypt).

- [ ] **Step 1: Write the failing tests** (append to `internal/crypto/settings_encryptor_test.go`)

```go
func TestKeyring_DecryptSelectsByID(t *testing.T) {
	k1 := bytes.Repeat([]byte{1}, 32)
	k2 := bytes.Repeat([]byte{2}, 32)
	old, err := NewSettingsEncryptorFromKeyring(k1, 1, nil, 0)
	require.NoError(t, err)
	v1, err := old.Encrypt("secret-one")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(v1, "ENC:v1:1:"))

	cur, err := NewSettingsEncryptorFromKeyring(k2, 2, k1, 1)
	require.NoError(t, err)
	require.Equal(t, "ENC:v1:2:", cur.CurrentPrefix())
	got, err := cur.Decrypt(v1)
	require.NoError(t, err)
	require.Equal(t, "secret-one", got)

	v2, err := cur.Encrypt("secret-two")
	require.NoError(t, err)
	id, ok := ContextIDOf(v2)
	require.True(t, ok)
	require.Equal(t, 2, id)
	_, ok = ContextIDOf("plaintext")
	require.False(t, ok)
}

func TestKeyring_LegacyIDFallsBackToTrial(t *testing.T) {
	k1 := bytes.Repeat([]byte{1}, 32)
	k2 := bytes.Repeat([]byte{2}, 32)
	// Value written with k1 but labelled with an id the keyring does not know (legacy data).
	writer, _ := NewSettingsEncryptorFromKeyring(k1, 7, nil, 0)
	v, _ := writer.Encrypt("legacy")
	reader, _ := NewSettingsEncryptorFromKeyring(k2, 2, k1, 1)
	got, err := reader.Decrypt(v)
	require.NoError(t, err)
	require.Equal(t, "legacy", got)
}

func TestDecrypt_UnknownIDAfterDrop(t *testing.T) {
	k1 := bytes.Repeat([]byte{1}, 32)
	k2 := bytes.Repeat([]byte{2}, 32)
	writer, _ := NewSettingsEncryptorFromKeyring(k1, 1, nil, 0)
	v, _ := writer.Encrypt("gone")
	reader, _ := NewSettingsEncryptorFromKeyring(k2, 2, nil, 0) // k1 dropped
	_, err := reader.Decrypt(v)
	require.Error(t, err)
}
```

Add `"bytes"` and `"strings"` to the test imports if missing.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name=TestKeyring count1=true`
Expected: compile error `undefined: NewSettingsEncryptorFromKeyring`.

- [ ] **Step 3: Implement the keyring**

In `internal/crypto/settings_encryptor.go`:

```go
type SettingsEncryptor struct {
	currentKey  []byte
	previousKey []byte // nil if no previous key configured
	previousID  int    // 0 when unknown (legacy config) or no previous key
	context     EncryptionContext
	enabled     bool
}
```

In `NewSettingsEncryptor`, after loading the previous key:

```go
	if enc.previousKey != nil {
		if pidStr, err := provider.GetSecret(ctx, secrets.SecretKeys.SettingsEncryptionPreviousContextID); err == nil {
			if parsed, err := strconv.Atoi(strings.TrimSpace(pidStr)); err == nil && parsed > 0 {
				enc.previousID = parsed
			} else {
				logger.Warn("Invalid settings encryption previous context ID, previous key will be selected by trial decrypt")
			}
		}
	}
```

Add:

```go
// NewSettingsEncryptorFromKeyring builds an encryptor from raw keys with explicit ids.
// previousID 0 means "unknown" (selected by trial decrypt only).
// SEM@<sha>: build a SettingsEncryptor from current and previous key bytes with their ids (pure)
func NewSettingsEncryptorFromKeyring(current []byte, currentID int, previous []byte, previousID int) (*SettingsEncryptor, error) {
	enc, err := NewSettingsEncryptorFromKeys(current, previous, currentID)
	if err != nil {
		return nil, err
	}
	if previous != nil && previousID > 0 {
		enc.previousID = previousID
	}
	return enc, nil
}

// CurrentPrefix returns the envelope prefix values written by this encryptor carry.
// SEM@<sha>: return the ENC:v1:<id>: prefix of the current key (pure)
func (e *SettingsEncryptor) CurrentPrefix() string {
	return fmt.Sprintf("ENC:v1:%d:", e.context.ContextID)
}

// ContextIDOf parses the key id out of an ENC:v1 envelope.
// SEM@<sha>: parse the key id from an encrypted setting envelope; false for plaintext or malformed (pure)
func ContextIDOf(value string) (int, bool) {
	if !IsEncrypted(value) {
		return 0, false
	}
	parts := strings.SplitN(value, ":", 5)
	if len(parts) != 5 || parts[1] != "v1" {
		return 0, false
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
```

Replace the key-selection block of `Decrypt` (after base64 decode) with:

```go
	id, _ := strconv.Atoi(parts[2])
	// Select by id; unknown ids (legacy data) fall back to trying every key.
	order := []struct {
		key []byte
		id  int
	}{{e.currentKey, e.context.ContextID}, {e.previousKey, e.previousID}}
	if id == e.previousID && e.previousKey != nil {
		order[0], order[1] = order[1], order[0]
	}
	for _, k := range order {
		if k.key == nil {
			continue
		}
		if plaintext, err := decryptAESGCM(k.key, data); err == nil {
			if k.id != e.context.ContextID {
				slogging.Get().Debug("Decrypted setting with a non-current key id=%d", k.id)
			}
			return string(plaintext), nil
		}
	}
	return "", fmt.Errorf("decryption failed: value could not be decrypted with current or previous key")
```

Update the SEM description of `Decrypt` to `decrypt an ENC-prefixed setting value selecting the key by envelope id, trial fallback for unknown ids (pure)`.

In `internal/secrets/provider.go` add `SettingsEncryptionPreviousContextID string` to the struct and `SettingsEncryptionPreviousContextID: "settings_encryption_previous_context_id",` to the literal.

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestKeyring count1=true` then `make test-unit name=TestDecrypt_UnknownIDAfterDrop count1=true` then `make test-unit name=TestSettingsEncryptor count1=true`
Expected: all PASS (existing encryptor tests still pass: behaviour for previousID 0 is unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/crypto/settings_encryptor.go internal/crypto/settings_encryptor_test.go internal/secrets/provider.go
git commit -m "feat(crypto): select the settings encryption key by envelope id (#965)"
```

---

### Task 2: `file` secrets provider

Needed by `deploy-aws.sh import_config` (Task 11) so the settings keyring reaches `dbtool` from files, never env or argv.

**Files:**
- Create: `internal/secrets/file_provider.go`
- Modify: `internal/secrets/provider.go:44-80` (`ProviderTypeFile`, `NewProvider` case)
- Modify: `internal/config/config.go:340-351` (`SecretsConfig.FileDir`)
- Test: `internal/secrets/file_provider_test.go`

**Interfaces:**
- Produces: `NewFileProvider(dir string) *FileProvider`; config `secrets.file_dir` / `TMI_SECRETS_FILE_DIR`; provider type string `"file"`. `GetSecret(key)` reads `<dir>/<key>` (key exactly as in `SecretKeys`, e.g. `settings_encryption_key`), trims one trailing newline.

- [ ] **Step 1: Write the failing test**

```go
package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileProvider_ReadsOneFilePerKey(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings_encryption_key"), []byte("abc\n"), 0o600))
	p := NewFileProvider(dir)
	v, err := p.GetSecret(context.Background(), "settings_encryption_key")
	require.NoError(t, err)
	require.Equal(t, "abc", v)
	_, err = p.GetSecret(context.Background(), "missing")
	require.True(t, errors.Is(err, ErrSecretNotFound))
	_, err = p.GetSecret(context.Background(), "../etc/passwd")
	require.Error(t, err)
	keys, err := p.ListSecrets(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"settings_encryption_key"}, keys)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `make test-unit name=TestFileProvider count1=true`
Expected: `undefined: NewFileProvider`.

- [ ] **Step 3: Implement**

`internal/secrets/file_provider.go`:

```go
package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileProvider reads one secret per file from a directory, the shape of a
// Kubernetes Secret volume or a umask-077 staging directory.
// SEM@<sha>: secrets provider that reads each secret from <dir>/<key> (reads files)
type FileProvider struct {
	dir string
}

// SEM@<sha>: build a directory-backed secrets provider (pure)
func NewFileProvider(dir string) *FileProvider {
	return &FileProvider{dir: dir}
}

// SEM@<sha>: fetch a secret from the file named after its key, rejecting path traversal
func (p *FileProvider) GetSecret(_ context.Context, key string) (string, error) {
	if key == "" || key != filepath.Base(key) {
		return "", fmt.Errorf("%w: invalid secret key", ErrInvalidConfig)
	}
	data, err := os.ReadFile(filepath.Join(p.dir, key)) // #nosec G304 -- key is a validated single path element
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

// SEM@<sha>: list secret keys as the regular file names in the directory
func (p *FileProvider) ListSecrets(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets directory: %w", err)
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			keys = append(keys, e.Name())
		}
	}
	return keys, nil
}

// SEM@<sha>: return the provider type name for the file provider (pure)
func (p *FileProvider) Name() string { return string(ProviderTypeFile) }

// SEM@<sha>: no-op close for the file provider (pure)
func (p *FileProvider) Close() error { return nil }
```

In `provider.go`: add `ProviderTypeFile ProviderType = "file"` and, in `NewProvider`:

```go
	case ProviderTypeFile:
		if cfg.FileDir == "" {
			return nil, fmt.Errorf("%w: file secrets provider requires file_dir", ErrInvalidConfig)
		}
		return NewFileProvider(cfg.FileDir), nil
```

In `internal/config/config.go` `SecretsConfig`, after `Provider`:

```go
	// Directory holding one file per secret key (provider "file").
	FileDir string `yaml:"file_dir" env:"TMI_SECRETS_FILE_DIR"`
```

Update the `Provider` comment to `// "env" (default), "file", "vault", "aws"`.

- [ ] **Step 4: Run tests, regenerate docs**

Run: `make test-unit name=TestFileProvider count1=true` (PASS), then `make generate-config-example` and `make generate-config-docs` (the registry tests compare `config-reference.md` / `config-example.yml` against the code; commit the regenerated files), then `make test-unit name=TestConfig count1=true`.

- [ ] **Step 5: Commit**

```bash
git add internal/secrets/file_provider.go internal/secrets/file_provider_test.go internal/secrets/provider.go internal/config/config.go config-reference.md config-example.yml
git commit -m "feat(secrets): add a directory-backed file secrets provider (#965)"
```

---

### Task 3: `internal/rotator` core: Secret state, scheduling, phases

**Files:**
- Create: `internal/rotator/secret.go`, `internal/rotator/secret_mem.go`, `internal/rotator/schedule.go`, `internal/rotator/rotator.go`, `internal/rotator/keygen.go`
- Test: `internal/rotator/schedule_test.go`, `internal/rotator/rotator_test.go`, `internal/rotator/secret_mem_test.go`

**Interfaces (Produces; PR 2 and PR 3 add rotations against these exact names):**

```go
package rotator

// Secret is a snapshot of one Kubernetes Secret: decoded data, annotations, and
// the resourceVersion used for optimistic concurrency.
type Secret struct {
	Name            string
	Data            map[string]string
	Annotations     map[string]string
	ResourceVersion string
}

var ErrConflict = errors.New("secret changed since it was read")

type SecretStore interface {
	Get(ctx context.Context, name string) (*Secret, error)
	// Update replaces data and annotations; returns ErrConflict when s.ResourceVersion is stale.
	Update(ctx context.Context, s *Secret) error
}

type RolloutWaiter interface {
	Generation(ctx context.Context, deployment string) (int64, error)
	// WaitRolled returns once the Deployment's generation exceeds since and its rollout is complete.
	WaitRolled(ctx context.Context, deployment string, since int64, timeout time.Duration) error
}

type Env struct {
	Secrets          SecretStore
	Rollouts         RolloutWaiter
	SecretName       string        // "tmi-secrets"
	ServerDeployment string        // "tmi-server"
	RolloutTimeout   time.Duration // 10m
	Now              func() time.Time
}

type Rotation interface {
	Name() string
	// Run resumes from the phase recorded on the Secret and returns when the
	// rotation is complete or parked (waiting on a later run). Idempotent.
	Run(ctx context.Context, env *Env) error
}

// Annotation keys (suffix = Rotation.Name()).
const (
	AnnRotatedAt  = "tmi.dev/rotated-at."
	AnnEvery      = "tmi.dev/rotate-every."
	AnnPhase      = "tmi.dev/rotation-phase."
	AnnGeneration = "tmi.dev/rotation-generation."
)

func Run(ctx context.Context, env *Env, rotations []Rotation, force string) error
func (e *Env) Transition(ctx context.Context, name, nextPhase string, mutate func(s *Secret)) error
func (e *Env) WaitServerRolled(ctx context.Context, s *Secret, name string) error
func NewPassword() (string, error)  // 32 chars [A-Za-z0-9], crypto/rand
func NewHexKey() (string, error)    // 64 hex chars (32 bytes)
func ParseRotateEvery(v string) (time.Duration, error) // "90d", "12h", ...
func IsDue(s *Secret, name string, now time.Time) bool
```

- [ ] **Step 1: Write the failing tests**

`internal/rotator/schedule_test.go`:

```go
package rotator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRotateEvery(t *testing.T) {
	d, err := ParseRotateEvery("90d")
	require.NoError(t, err)
	require.Equal(t, 90*24*time.Hour, d)
	d, err = ParseRotateEvery("36h")
	require.NoError(t, err)
	require.Equal(t, 36*time.Hour, d)
	_, err = ParseRotateEvery("3 months")
	require.Error(t, err)
	_, err = ParseRotateEvery("0d")
	require.Error(t, err)
}

func TestIsDue(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s := &Secret{Annotations: map[string]string{}}
	require.True(t, IsDue(s, "x", now), "never rotated is due")
	s.Annotations[AnnRotatedAt+"x"] = now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	require.False(t, IsDue(s, "x", now), "10 days old, default 90d")
	s.Annotations[AnnEvery+"x"] = "7d"
	require.True(t, IsDue(s, "x", now))
	s.Annotations[AnnEvery+"x"] = "3 months" // unparsable: warn and use the default
	require.False(t, IsDue(s, "x", now))
	s.Annotations[AnnRotatedAt+"x"] = "garbage"
	require.True(t, IsDue(s, "x", now), "unparsable rotated-at is due")
}
```

`internal/rotator/secret_mem_test.go`:

```go
package rotator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemorySecretStore_ConflictOnStaleVersion(t *testing.T) {
	st := NewMemorySecretStore(&Secret{Name: "tmi-secrets", Data: map[string]string{"A": "1"}})
	ctx := context.Background()
	a, err := st.Get(ctx, "tmi-secrets")
	require.NoError(t, err)
	b, err := st.Get(ctx, "tmi-secrets")
	require.NoError(t, err)
	a.Data["A"] = "2"
	require.NoError(t, st.Update(ctx, a))
	b.Data["A"] = "3"
	err = st.Update(ctx, b)
	require.True(t, errors.Is(err, ErrConflict))
	cur, _ := st.Get(ctx, "tmi-secrets")
	require.Equal(t, "2", cur.Data["A"])
	require.Equal(t, 1, st.DataWrites, "annotation-only updates do not count as data writes")
}
```

`internal/rotator/rotator_test.go`:

```go
package rotator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRotation struct {
	name string
	runs int
	err  error
}

func (f *fakeRotation) Name() string { return f.name }
func (f *fakeRotation) Run(context.Context, *Env) error {
	f.runs++
	return f.err
}

func testEnv(s *Secret) (*Env, *MemorySecretStore) {
	st := NewMemorySecretStore(s)
	return &Env{
		Secrets:          st,
		Rollouts:         NewFakeRolloutWaiter(st, "tmi-server"),
		SecretName:       "tmi-secrets",
		ServerDeployment: "tmi-server",
		RolloutTimeout:   time.Second,
		Now:              func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) },
	}, st
}

func TestRun_SkipsNotDue_RunsForcedAndInProgress(t *testing.T) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{
		AnnRotatedAt + "fresh":  "2026-09-27T00:00:00Z",
		AnnRotatedAt + "forced": "2026-09-27T00:00:00Z",
		AnnRotatedAt + "stuck":  "2026-09-27T00:00:00Z",
		AnnPhase + "stuck":      "swapped",
	}})
	fresh, forced, stuck := &fakeRotation{name: "fresh"}, &fakeRotation{name: "forced"}, &fakeRotation{name: "stuck"}
	require.NoError(t, Run(context.Background(), env, []Rotation{fresh, forced, stuck}, "forced"))
	require.Equal(t, 0, fresh.runs)
	require.Equal(t, 1, forced.runs)
	require.Equal(t, 1, stuck.runs, "a recorded phase resumes even when not due")
}

func TestRun_FailureIsReportedAfterOthersRun(t *testing.T) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	bad := &fakeRotation{name: "bad", err: errors.New("boom")}
	good := &fakeRotation{name: "good"}
	err := Run(context.Background(), env, []Rotation{bad, good}, "")
	require.Error(t, err)
	require.Equal(t, 1, good.runs)
}

func TestTransition_WritesPhaseAndGenerationAtomically(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{"K": "old"}, Annotations: map[string]string{}})
	err := env.Transition(context.Background(), "k", "swapped", func(s *Secret) { s.Data["K"] = "new" })
	require.NoError(t, err)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "new", s.Data["K"])
	require.Equal(t, "swapped", s.Annotations[AnnPhase+"k"])
	require.Equal(t, "0", s.Annotations[AnnGeneration+"k"], "generation before the write")
	require.NoError(t, env.WaitServerRolled(context.Background(), s, "k"))

	// Completing a rotation clears the phase and stamps rotated-at.
	err = env.Transition(context.Background(), "k", "", nil)
	require.NoError(t, err)
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"k"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnRotatedAt+"k"])
}

func TestNewPasswordAndHexKey(t *testing.T) {
	p, err := NewPassword()
	require.NoError(t, err)
	require.Len(t, p, 32)
	k, err := NewHexKey()
	require.NoError(t, err)
	require.Len(t, k, 64)
	q, _ := NewPassword()
	require.NotEqual(t, p, q)
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `make test-unit name=TestParseRotateEvery count1=true`
Expected: package does not compile (`undefined`).

- [ ] **Step 3: Implement `secret.go`**

```go
// Package rotator rotates the high-value secrets in tmi-secrets one idempotent
// phase at a time, recording progress in Secret annotations (#965).
package rotator

import (
	"context"
	"errors"
	"time"
)

// Secret is a snapshot of one Kubernetes Secret with decoded data.
// SEM@<sha>: snapshot of a Kubernetes Secret's data, annotations and resourceVersion (pure)
type Secret struct {
	Name            string
	Data            map[string]string
	Annotations     map[string]string
	ResourceVersion string
}

// ErrConflict is returned by SecretStore.Update when the Secret changed after it was read.
var ErrConflict = errors.New("secret changed since it was read")

// SecretStore reads and writes whole Secrets with optimistic concurrency.
// SEM@<sha>: read/update a Kubernetes Secret with resourceVersion conflict detection
type SecretStore interface {
	Get(ctx context.Context, name string) (*Secret, error)
	Update(ctx context.Context, s *Secret) error
}

// RolloutWaiter observes Deployment rollouts triggered by Secret changes (Reloader).
// SEM@<sha>: observe a Deployment's generation and wait for a rollout past it
type RolloutWaiter interface {
	Generation(ctx context.Context, deployment string) (int64, error)
	WaitRolled(ctx context.Context, deployment string, since int64, timeout time.Duration) error
}

// Clone returns a deep copy so a store can hand out independent snapshots.
// SEM@<sha>: deep-copy a Secret snapshot (pure)
func (s *Secret) Clone() *Secret {
	c := &Secret{Name: s.Name, ResourceVersion: s.ResourceVersion, Data: map[string]string{}, Annotations: map[string]string{}}
	for k, v := range s.Data {
		c.Data[k] = v
	}
	for k, v := range s.Annotations {
		c.Annotations[k] = v
	}
	return c
}
```

- [ ] **Step 4: Implement `secret_mem.go`** (non-test file: integration tests reuse it)

```go
package rotator

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// MemorySecretStore is an in-memory SecretStore for tests. It bumps
// resourceVersion on every Update and counts data writes so a FakeRolloutWaiter
// can imitate Reloader (which rolls on data changes only).
// SEM@<sha>: in-memory SecretStore with resourceVersion conflicts and data-write counting (mutates shared state)
type MemorySecretStore struct {
	mu         sync.Mutex
	secrets    map[string]*Secret
	version    int
	DataWrites int
}

// SEM@<sha>: build a MemorySecretStore seeded with the given Secrets (pure)
func NewMemorySecretStore(seed ...*Secret) *MemorySecretStore {
	st := &MemorySecretStore{secrets: map[string]*Secret{}}
	for _, s := range seed {
		c := s.Clone()
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		c.ResourceVersion = "0"
		st.secrets[c.Name] = c
	}
	return st
}

// SEM@<sha>: return a copy of the named Secret or a not-found error
func (m *MemorySecretStore) Get(_ context.Context, name string) (*Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.secrets[name]
	if !ok {
		return nil, fmt.Errorf("secret %q not found", name)
	}
	return s.Clone(), nil
}

// SEM@<sha>: store the Secret if its resourceVersion is current, else ErrConflict
func (m *MemorySecretStore) Update(_ context.Context, s *Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.secrets[s.Name]
	if !ok {
		return fmt.Errorf("secret %q not found", s.Name)
	}
	if cur.ResourceVersion != s.ResourceVersion {
		return ErrConflict
	}
	if !equalMaps(cur.Data, s.Data) {
		m.DataWrites++
	}
	m.version++
	c := s.Clone()
	c.ResourceVersion = strconv.Itoa(m.version)
	m.secrets[s.Name] = c
	return nil
}

// SEM@<sha>: compare two string maps for equality (pure)
func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// FakeRolloutWaiter reports a generation equal to the store's data-write count,
// so every data write "rolls" the server immediately.
// SEM@<sha>: RolloutWaiter fake whose generation tracks MemorySecretStore data writes
type FakeRolloutWaiter struct {
	store      *MemorySecretStore
	deployment string
	Waits      int
}

// SEM@<sha>: build a FakeRolloutWaiter bound to a MemorySecretStore (pure)
func NewFakeRolloutWaiter(store *MemorySecretStore, deployment string) *FakeRolloutWaiter {
	return &FakeRolloutWaiter{store: store, deployment: deployment}
}

// SEM@<sha>: report the fake Deployment generation (data writes so far)
func (f *FakeRolloutWaiter) Generation(_ context.Context, deployment string) (int64, error) {
	if deployment != f.deployment {
		return 0, fmt.Errorf("unknown deployment %q", deployment)
	}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	return int64(f.store.DataWrites), nil
}

// SEM@<sha>: succeed when a data write happened after since, else time out
func (f *FakeRolloutWaiter) WaitRolled(ctx context.Context, deployment string, since int64, _ time.Duration) error {
	f.Waits++
	gen, err := f.Generation(ctx, deployment)
	if err != nil {
		return err
	}
	if gen <= since {
		return fmt.Errorf("rollout of %s did not start (generation %d <= %d)", deployment, gen, since)
	}
	return nil
}
```

- [ ] **Step 5: Implement `schedule.go`**

```go
package rotator

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	AnnRotatedAt  = "tmi.dev/rotated-at."
	AnnEvery      = "tmi.dev/rotate-every."
	AnnPhase      = "tmi.dev/rotation-phase."
	AnnGeneration = "tmi.dev/rotation-generation."

	DefaultRotateEvery = 90 * 24 * time.Hour
)

// ParseRotateEvery accepts "<n>d" or any Go duration; zero or negative is an error.
// SEM@<sha>: parse a rotation interval written as days or a Go duration (pure)
func ParseRotateEvery(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid rotate-every %q", v)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid rotate-every %q", v)
	}
	return d, nil
}

// IsDue reports whether the named rotation should start now: never rotated, an
// unreadable rotated-at, or older than rotate-every (default 90d; an unparsable
// interval is logged and falls back to the default, never to "now").
// SEM@<sha>: decide whether a secret's scheduled rotation is due from its annotations (pure)
func IsDue(s *Secret, name string, now time.Time) bool {
	last, ok := s.Annotations[AnnRotatedAt+name]
	if !ok {
		return true
	}
	at, err := time.Parse(time.RFC3339, last)
	if err != nil {
		slogging.Get().Warn("Unreadable rotated-at annotation for %s; treating as due", name)
		return true
	}
	every := DefaultRotateEvery
	if v, ok := s.Annotations[AnnEvery+name]; ok {
		if d, err := ParseRotateEvery(v); err == nil {
			every = d
		} else {
			slogging.Get().Warn("Unreadable rotate-every annotation for %s; using default %s", name, DefaultRotateEvery)
		}
	}
	return now.Sub(at) >= every
}

// AgeDays returns whole days since the last rotation, or -1 if never rotated.
// SEM@<sha>: compute days since a secret's last recorded rotation (pure)
func AgeDays(s *Secret, name string, now time.Time) int {
	at, err := time.Parse(time.RFC3339, s.Annotations[AnnRotatedAt+name])
	if err != nil {
		return -1
	}
	return int(now.Sub(at).Hours() / 24)
}
```

- [ ] **Step 6: Implement `keygen.go`**

```go
package rotator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// NewPassword returns a 32-character alphanumeric password (URL- and shell-safe,
// so it needs no escaping in TMI_DATABASE_URL or redis-cli).
// SEM@<sha>: generate a random 32-char alphanumeric credential (pure)
func NewPassword() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random source failed: %w", err)
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(out), nil
}

// NewHexKey returns 32 random bytes as 64 hex characters (AES-256 key).
// SEM@<sha>: generate a random 32-byte key as hex text (pure)
func NewHexKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random source failed: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
```

(`int(b) % 62` has a bias of 4/256 on the first 8 letters; acceptable for a 32-char credential with ~190 bits, and simpler than rejection sampling. Note it in a comment.)

- [ ] **Step 7: Implement `rotator.go`**

```go
package rotator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Env is everything a Rotation needs from the cluster.
// SEM@<sha>: cluster handles and settings shared by every rotation (pure)
type Env struct {
	Secrets          SecretStore
	Rollouts         RolloutWaiter
	SecretName       string
	ServerDeployment string
	RolloutTimeout   time.Duration
	Now              func() time.Time
}

// Rotation is one secret's phased, resumable rotation.
// SEM@<sha>: phased, idempotent rotation of one named secret
type Rotation interface {
	Name() string
	Run(ctx context.Context, env *Env) error
}

// Run evaluates every rotation: forced, in progress (phase annotation set), or
// due. Each rotation runs even if an earlier one failed; the first error is returned.
// SEM@<sha>: run every forced, in-progress or due rotation and report the first failure
func Run(ctx context.Context, env *Env, rotations []Rotation, force string) error {
	logger := slogging.Get()
	var firstErr error
	for _, r := range rotations {
		s, err := env.Secrets.Get(ctx, env.SecretName)
		if err != nil {
			return fmt.Errorf("read %s: %w", env.SecretName, err)
		}
		phase := s.Annotations[AnnPhase+r.Name()]
		now := env.Now()
		logger.InfoCtx(ctx, "rotation status",
			slog.String("secret", r.Name()),
			slog.Int("age_days", AgeDays(s, r.Name(), now)),
			slog.String("phase", phase))
		switch {
		case force == r.Name():
			logger.Info("Rotation forced secret=%s", r.Name())
		case phase != "":
			logger.Info("Rotation resuming secret=%s phase=%s", r.Name(), phase)
		case IsDue(s, r.Name(), now):
			logger.Info("Rotation due secret=%s", r.Name())
		default:
			continue
		}
		if err := r.Run(ctx, env); err != nil {
			logger.Error("Rotation failed secret=%s error=%v", r.Name(), err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", r.Name(), err)
			}
			continue
		}
		logger.Info("Rotation step complete secret=%s", r.Name())
	}
	return firstErr
}

// Transition reads the Secret, applies mutate, records the phase (or, for
// nextPhase == "", clears it and stamps rotated-at) together with the server
// Deployment's generation before the write, and writes everything in one Update.
// A conflict means another run touched the Secret: fail, do not retry.
// SEM@<sha>: apply a rotation phase change to the Secret atomically with its bookkeeping annotations
func (e *Env) Transition(ctx context.Context, name, nextPhase string, mutate func(s *Secret)) error {
	s, err := e.Secrets.Get(ctx, e.SecretName)
	if err != nil {
		return err
	}
	gen, err := e.Rollouts.Generation(ctx, e.ServerDeployment)
	if err != nil {
		return fmt.Errorf("read %s generation: %w", e.ServerDeployment, err)
	}
	s.Annotations[AnnGeneration+name] = strconv.FormatInt(gen, 10)
	if nextPhase == "" {
		delete(s.Annotations, AnnPhase+name)
		s.Annotations[AnnRotatedAt+name] = e.Now().UTC().Format(time.RFC3339)
	} else {
		s.Annotations[AnnPhase+name] = nextPhase
	}
	// mutate runs last so a completing write may override rotated-at
	// (the settings-key drop keeps the promotion date as the rotation date).
	if mutate != nil {
		mutate(s)
	}
	if err := e.Secrets.Update(ctx, s); err != nil {
		if errors.Is(err, ErrConflict) {
			return fmt.Errorf("%s phase %q: %w (another rotator run is active?)", name, nextPhase, err)
		}
		return err
	}
	slogging.Get().Info("Rotation phase recorded secret=%s phase=%q", name, nextPhase)
	return nil
}

// WaitServerRolled waits for the server rollout caused by the last Transition of name.
// SEM@<sha>: wait for the server Deployment to roll past the generation recorded for a rotation
func (e *Env) WaitServerRolled(ctx context.Context, s *Secret, name string) error {
	since, err := strconv.ParseInt(s.Annotations[AnnGeneration+name], 10, 64)
	if err != nil {
		return fmt.Errorf("missing rotation-generation annotation for %s", name)
	}
	return e.Rollouts.WaitRolled(ctx, e.ServerDeployment, since, e.RolloutTimeout)
}
```

- [ ] **Step 8: Run the tests**

Run: `make test-unit name=TestParseRotateEvery count1=true`, `make test-unit name=TestIsDue count1=true`, `make test-unit name=TestMemorySecretStore count1=true`, `make test-unit name=TestRun_ count1=true`, `make test-unit name=TestTransition count1=true`, `make test-unit name=TestNewPasswordAndHexKey count1=true`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/rotator/
git commit -m "feat(rotator): phase state on tmi-secrets, scheduling and fakes (#965)"
```

---
### Task 4: Kubernetes-backed `SecretStore` and `RolloutWaiter`

**Files:**
- Create: `internal/rotator/kube.go`
- Test: `internal/rotator/kube_test.go`

**Interfaces:**
- Produces: `NewKubeSecretStore(cs kubernetes.Interface, namespace string) *KubeSecretStore`, `NewKubeRolloutWaiter(cs kubernetes.Interface, namespace string) *KubeRolloutWaiter`. Both satisfy the Task 3 interfaces. `k8s.io/client-go/kubernetes/fake` is in the module already (client-go v0.37.1); run `go mod tidy` via `make lint` if `go.sum` needs the fake package's deps.

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestKubeSecretStore_RoundTripDecodesData(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-secrets", Namespace: "tmi-platform", ResourceVersion: "1",
			Annotations: map[string]string{"tmi.dev/rotated-at.x": "2026-01-01T00:00:00Z"}},
		Data: map[string][]byte{"A": []byte("one")},
	})
	st := NewKubeSecretStore(cs, "tmi-platform")
	s, err := st.Get(context.Background(), "tmi-secrets")
	require.NoError(t, err)
	require.Equal(t, "one", s.Data["A"])
	require.Equal(t, "2026-01-01T00:00:00Z", s.Annotations["tmi.dev/rotated-at.x"])
	s.Data["B"] = "two"
	delete(s.Data, "A")
	require.NoError(t, st.Update(context.Background(), s))
	got, _ := cs.CoreV1().Secrets("tmi-platform").Get(context.Background(), "tmi-secrets", metav1.GetOptions{})
	require.Equal(t, []byte("two"), got.Data["B"])
	_, hasA := got.Data["A"]
	require.False(t, hasA)
}

func TestKubeSecretStore_ConflictMapsToErrConflict(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tmi-secrets", Namespace: "tmi-platform", ResourceVersion: "1"}})
	cs.PrependReactor("update", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, "tmi-secrets", errors.New("stale"))
	})
	st := NewKubeSecretStore(cs, "tmi-platform")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	err := st.Update(context.Background(), s)
	require.True(t, errors.Is(err, ErrConflict))
}

func TestKubeRolloutWaiter_WaitsForGenerationThenAvailability(t *testing.T) {
	one := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-server", Namespace: "tmi-platform", Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
	cs := fake.NewClientset(dep)
	w := NewKubeRolloutWaiter(cs, "tmi-platform")
	w.Poll = 5 * time.Millisecond
	gen, err := w.Generation(context.Background(), "tmi-server")
	require.NoError(t, err)
	require.EqualValues(t, 3, gen)

	// Generation has not moved: times out.
	err = w.WaitRolled(context.Background(), "tmi-server", 3, 30*time.Millisecond)
	require.Error(t, err)

	// Reloader bumped the template (generation 4); status catches up after a moment.
	dep.Generation = 4
	dep.Status.ObservedGeneration = 3
	dep.Status.AvailableReplicas = 0
	_, _ = cs.AppsV1().Deployments("tmi-platform").Update(context.Background(), dep, metav1.UpdateOptions{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		dep.Status.ObservedGeneration = 4
		dep.Status.AvailableReplicas = 1
		dep.Status.UpdatedReplicas = 1
		_, _ = cs.AppsV1().Deployments("tmi-platform").UpdateStatus(context.Background(), dep, metav1.UpdateOptions{})
	}()
	require.NoError(t, w.WaitRolled(context.Background(), "tmi-server", 3, 2*time.Second))
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestKubeSecretStore count1=true`
Expected: `undefined: NewKubeSecretStore`.

- [ ] **Step 3: Implement `kube.go`**

```go
package rotator

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubeSecretStore is the SecretStore for a real cluster.
// SEM@<sha>: SecretStore over the Kubernetes API with resourceVersion-checked updates
type KubeSecretStore struct {
	cs        kubernetes.Interface
	namespace string
}

// SEM@<sha>: build a KubeSecretStore for one namespace (pure)
func NewKubeSecretStore(cs kubernetes.Interface, namespace string) *KubeSecretStore {
	return &KubeSecretStore{cs: cs, namespace: namespace}
}

// SEM@<sha>: fetch a Secret and decode its data into strings
func (k *KubeSecretStore) Get(ctx context.Context, name string) (*Secret, error) {
	obj, err := k.cs.CoreV1().Secrets(k.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", k.namespace, name, err)
	}
	s := &Secret{Name: name, ResourceVersion: obj.ResourceVersion, Data: map[string]string{}, Annotations: map[string]string{}}
	for key, v := range obj.Data {
		s.Data[key] = string(v)
	}
	for key, v := range obj.Annotations {
		s.Annotations[key] = v
	}
	return s, nil
}

// SEM@<sha>: write a Secret's data and annotations back, mapping a 409 to ErrConflict
func (k *KubeSecretStore) Update(ctx context.Context, s *Secret) error {
	obj, err := k.cs.CoreV1().Secrets(k.namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get secret %s/%s: %w", k.namespace, s.Name, err)
	}
	if obj.ResourceVersion != s.ResourceVersion {
		return ErrConflict
	}
	obj.Data = map[string][]byte{}
	for key, v := range s.Data {
		obj.Data[key] = []byte(v)
	}
	obj.StringData = nil
	obj.Annotations = map[string]string{}
	for key, v := range s.Annotations {
		obj.Annotations[key] = v
	}
	if _, err := k.cs.CoreV1().Secrets(k.namespace).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return ErrConflict
		}
		return fmt.Errorf("update secret %s/%s: %w", k.namespace, s.Name, err)
	}
	return nil
}

// KubeRolloutWaiter polls a Deployment until Reloader's rollout completes.
// SEM@<sha>: RolloutWaiter that polls Deployment generation and replica status
type KubeRolloutWaiter struct {
	cs        kubernetes.Interface
	namespace string
	Poll      time.Duration
}

// SEM@<sha>: build a KubeRolloutWaiter polling every 5s (pure)
func NewKubeRolloutWaiter(cs kubernetes.Interface, namespace string) *KubeRolloutWaiter {
	return &KubeRolloutWaiter{cs: cs, namespace: namespace, Poll: 5 * time.Second}
}

// SEM@<sha>: read a Deployment's metadata.generation
func (w *KubeRolloutWaiter) Generation(ctx context.Context, deployment string) (int64, error) {
	d, err := w.cs.AppsV1().Deployments(w.namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("get deployment %s/%s: %w", w.namespace, deployment, err)
	}
	return d.Generation, nil
}

// WaitRolled first waits for the generation to pass since (Reloader has patched
// the pod template), then for the rollout to complete. A Recreate Deployment
// dips to zero available replicas in between; that is progress, not failure.
// SEM@<sha>: block until a Deployment rolled past a generation and is fully available, or time out
func (w *KubeRolloutWaiter) WaitRolled(ctx context.Context, deployment string, since int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		d, err := w.cs.AppsV1().Deployments(w.namespace).Get(ctx, deployment, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get deployment %s/%s: %w", w.namespace, deployment, err)
		}
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		rolled := d.Generation > since &&
			d.Status.ObservedGeneration >= d.Generation &&
			d.Status.UpdatedReplicas == want &&
			d.Status.AvailableReplicas == want &&
			d.Status.Replicas == want
		if rolled {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("deployment %s did not finish rolling within %s (generation %d, since %d, available %d/%d)",
				deployment, timeout, d.Generation, since, d.Status.AvailableReplicas, want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.Poll):
		}
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestKube count1=true`
Expected: PASS. (If the fake clientset does not persist `UpdateStatus` in this client-go version, use `Update` in the goroutine instead; the waiter reads the whole object either way.)

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/kube.go internal/rotator/kube_test.go go.mod go.sum
git commit -m "feat(rotator): Kubernetes SecretStore and Deployment rollout waiter (#965)"
```

---

### Task 5: Redis persistence (PVC + AOF) so rotations and rolls keep sessions

**Files:**
- Modify: `deployments/k8s/dev/redis.yml` (post-PR 6 Deployment: AOF args, `/data` volume, `Recreate`; new `PersistentVolumeClaim`)
- Create: `deployments/k8s/dev/k3s/patches/redis-storageclass.yaml`, `deployments/k8s/dev/aws/patches/redis-storageclass.yaml`
- Modify: `deployments/k8s/dev/k3s/kustomization.yaml`, `deployments/k8s/dev/aws/kustomization.yaml` (patch entries)
- Modify: `terraform/modules/kubernetes/aws/main.tf` (EBS CSI addon + IRSA role, `gp3` StorageClass)
- Modify: `deployments/k8s/dev/aws/README.md` (new "Redis persistence" paragraph), `scripts/lib/deploy.py` (comment in PR 6's `ensure_redis_password_secret()` docstring)

**Interfaces:**
- Consumes: PR 6's `redis.yml` (TLS + `--requirepass $(REDIS_PASSWORD)` from `tmi-secrets`, Reloader annotation `secret.reloader.stakater.com/reload: "redis-tls"`).
- Produces: PVC `redis-data` in `tmi-platform`, mounted at `/data`; Redis keeps `ENC:` sessions and refresh tokens across any restart (cert renewal roll, image roll, crash, `dev-down`/`dev-up`). Task 6's rotation relies on the restart semantics documented in step 2 (`default` user rebuilt from the Secret). Nothing in Go changes.

Why now: Task 6 rotates the Redis password and PR 6 rolls Redis on cert renewal; without persistence every roll logs every user out. `docker-desktop-oracle` includes `../redis.yml`, so it gets the PVC from the base with no overlay change.

Why a Deployment and not a StatefulSet: everything targets `kind: Deployment` / `deploy/redis` today (the docker-desktop `redis-pullpolicy.yaml` patch, `scripts/cats-prep.py` `REDIS_DEPLOYMENT`, `deploy.py`'s `delete deploy,svc ... redis`, `scripts/lib/devstatus.py` `_WANT`, PR 6's verification, Task 14 step 3). One replica with one named PVC is the same thing a StatefulSet would give, without touching any of those. `strategy: Recreate` is required: the PVC is `ReadWriteOnce`, and on the two-node EKS cluster a `RollingUpdate` surge pod scheduled to the other node could never attach the EBS volume, so the roll would hang. Recreate costs a few seconds of Redis downtime per roll (the server's go-redis client reconnects; the same trade PR 6 accepted for `tmi-server`).

- [ ] **Step 1: `deployments/k8s/dev/redis.yml`**

Replace the Deployment (the Service is unchanged). This is PR 6 Task 6 step 1's manifest with the changes marked `# PR 1 (#965)`; if PR 6 landed with different flags, keep PR 6's flags and apply only the marked lines.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: tmi-platform
  annotations:
    # Roll ONLY when the TLS Secret is re-issued (cert-manager renewal, about
    # every 60 days). Not `auto`: that would also roll on every tmi-secrets
    # change (#965 rotations). Data survives a roll (AOF on redis-data, #965
    # PR 1), but each roll is still a few seconds of Redis downtime.
    secret.reloader.stakater.com/reload: "redis-tls"
spec:
  replicas: 1
  # PR 1 (#965): redis-data is ReadWriteOnce. A RollingUpdate surge pod on the
  # other EKS node could never attach the volume and the roll would hang;
  # Recreate stops the old pod first.
  strategy:
    type: Recreate
  selector:
    matchLabels: { app: redis }
  template:
    metadata:
      labels: { app: redis }
    spec:
      # PR 1 (#965): the PVC is mounted with this group so the non-root redis
      # user (uid/gid 65532 in cgr.dev/chainguard/redis, verified 2026-09-28; step 3 re-checks) can
      # write /data. redis:7-alpine (k3s image remap) starts as root and chowns
      # /data to its own redis user, so the group is harmless there.
      securityContext:
        fsGroup: 65532
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
          #
          # Persistence (#965 PR 1): append-only file on the redis-data PVC,
          # fsync once per second (at most one second of writes lost on a
          # crash; a clean stop loses nothing). --save "" stays: no RDB
          # snapshots on top of the AOF (Redis 7 keeps its own base file in
          # appendonlydir). ACL changes are NOT in the AOF: the default user's
          # password is rebuilt from --requirepass at every start, which is
          # exactly what tmi-rotator relies on for password rotation (#965).
          args:
            - "--save"
            - ""
            - "--appendonly"
            - "yes"
            - "--appendfsync"
            - "everysec"
            - "--dir"
            - "/data"
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
            - name: data
              mountPath: /data
          resources:
            requests: { cpu: 50m, memory: 64Mi }
            limits: { cpu: 500m, memory: 256Mi }
      volumes:
        - name: tls
          secret:
            secretName: redis-tls
        - name: data
          persistentVolumeClaim:
            claimName: redis-data
---
# Redis data (#965 PR 1): sessions, refresh tokens and cached authorization
# decisions, all ENC:-encrypted under the settings key and all with TTLs.
# 1Gi is far above the 256Mi memory limit; the AOF rewrite needs about 2x the
# dataset on disk. No storageClassName here: docker-desktop uses its default
# (hostpath) class; the k3s and aws overlays patch in longhorn / gp3.
#
# Lifecycle: `make dev-down` deletes the redis Deployment and Service but not
# this claim (same as the Postgres PVC), so sessions survive a dev-down /
# dev-up. `make dev-nuke` deletes the namespace and the claim with it, which
# is consistent: tmi-secrets goes at the same time, and ENC: entries are
# unreadable without it anyway.
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: redis-data
  namespace: tmi-platform
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 1Gi
```

Note: `Dockerfile.redis` (the `tmi-redis` image the AWS overlay uses) has `CMD ["--appendonly", "yes", ...]`, but Kubernetes `args` replaces CMD entirely, so the manifest above is the only place the flags matter.

- [ ] **Step 2: Resolve ACL persistence (design note; no code)**

Redis does not write `ACL SETUSER` to the AOF; ACL state persists only through `CONFIG REWRITE` or an `aclfile` plus `ACL SAVE`. This plan uses **neither**, and the rotation stays correct because:

- tmi-rotator (Task 6) creates no ACL users. It only adds a password hash to the `default` user (`ACL SETUSER default >NEW`) and later removes one (`ACL SETUSER default !<sha256>`).
- At every start Redis rebuilds `default` from `--requirepass $(REDIS_PASSWORD)`. The kubelet resolves `$(REDIS_PASSWORD)` from `tmi-secrets` when it starts the container (a fresh pod after a roll, and also a crash restart of the container inside the same pod: env is built on every container start). So after any restart `default` accepts exactly the Secret's current password, which is NEW from the rotation's first Secret write onward.
- The `swapped` phase re-runs `ACL SETUSER default >NEW` idempotently before waiting for the server roll (Task 6 test `TestRedisPasswordRotation_ResumeReAddsNewPassword`), so a restart between the two phases changes nothing.
- Accepted cost: if Redis restarts mid-rotation, OLD is gone, and any `tmi-server` pod Reloader has not yet replaced fails auth (`WRONGPASS`) until the roll completes (minutes; the rotator's rollout wait covers it). An `aclfile` would avoid that window but makes the Secret and the file two sources of truth for `default`'s password, needs a writable file plus `ACL SAVE` from the rotator, and conflicts with `--requirepass`; rejected.

Step 6 verifies the restart behaviour on a real cluster.

- [ ] **Step 3: Confirm the redis uid in the chainguard image**

```bash
docker pull cgr.dev/chainguard/redis:latest
docker image inspect cgr.dev/chainguard/redis:latest --format '{{.Config.User}}'
c=$(docker create cgr.dev/chainguard/redis:latest); docker cp "$c:/etc/passwd" - | tar -xO | rg '^redis'; docker rm "$c" >/dev/null
```

Expected: `redis` (or `65532`), and the passwd line `redis:x:65532:65532:...`. If the uid differs, set `fsGroup` in step 1 to that value.

- [ ] **Step 4: k3s and aws overlay patches (storage class)**

`deployments/k8s/dev/k3s/patches/redis-storageclass.yaml`:

```yaml
# Redis data on longhorn, like k3s/postgres.yml. The base PVC has no
# storageClassName so docker-desktop can use its default; k3s's default
# (local-path) pins the pod to one node, longhorn does not.
- op: add
  path: /spec/storageClassName
  value: longhorn
```

`deployments/k8s/dev/aws/patches/redis-storageclass.yaml`:

```yaml
# Redis data on EBS gp3 via the ebs.csi.aws.com StorageClass that
# terraform/modules/kubernetes/aws/main.tf creates (kubernetes_storage_class_v1
# "gp3", volumeBindingMode WaitForFirstConsumer so the volume lands in the
# AZ of whichever node schedules the pod).
- op: add
  path: /spec/storageClassName
  value: gp3
```

Add to `deployments/k8s/dev/k3s/kustomization.yaml` and `deployments/k8s/dev/aws/kustomization.yaml` under `patches:`:

```yaml
  - path: patches/redis-storageclass.yaml
    target:
      kind: PersistentVolumeClaim
      name: redis-data
```

- [ ] **Step 5: Terraform: EBS CSI driver addon, IRSA role, `gp3` StorageClass**

The module manages only `vpc-cni`, `kube-proxy` and `coredns`; there is no EBS CSI driver and no StorageClass anywhere in `terraform/`, so on EKS 1.36 a PVC stays `Pending` forever. Add to `terraform/modules/kubernetes/aws/main.tf`, after the `coredns` addon:

```hcl
# ============================================================================
# EBS CSI driver (#965 PR 1): backs the redis-data PVC. Nothing else in the
# platform claims storage (Postgres is RDS, NATS JetStream is an emptyDir).
# ============================================================================

data "aws_eks_addon_version" "ebs_csi" {
  addon_name         = "aws-ebs-csi-driver"
  kubernetes_version = var.kubernetes_version
}

resource "aws_iam_role" "ebs_csi" {
  name = "${var.name_prefix}-ebs-csi-driver"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRoleWithWebIdentity"
        Effect = "Allow"
        Principal = {
          Federated = local.oidc_provider_arn
        }
        Condition = {
          StringEquals = {
            "${local.oidc_provider_url}:aud" = "sts.amazonaws.com"
            # The addon creates this ServiceAccount itself.
            "${local.oidc_provider_url}:sub" = "system:serviceaccount:kube-system:ebs-csi-controller-sa"
          }
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "ebs_csi" {
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy"
  role       = aws_iam_role.ebs_csi.name
}

resource "aws_eks_addon" "ebs_csi" {
  cluster_name             = aws_eks_cluster.tmi.name
  addon_name               = "aws-ebs-csi-driver"
  addon_version            = data.aws_eks_addon_version.ebs_csi.version
  service_account_role_arn = aws_iam_role.ebs_csi.arn

  # One controller replica (Eric, 2026-09-29): TMI runs stateful
  # single-instance pods and the two-node group is close to its pod ceiling.
  # Upstream default is 2; the schema key is controller.replicaCount.
  configuration_values = jsonencode({ controller = { replicaCount = 1 } })

  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "OVERWRITE"

  tags = var.tags

  depends_on = [aws_eks_node_group.tmi, aws_iam_role_policy_attachment.ebs_csi]
}

# gp3 is cheaper than the legacy gp2 class EKS creates by default and lets
# the volume be encrypted at rest with the account's default EBS key.
# WaitForFirstConsumer: the volume is created in the AZ of the node that
# schedules the pod (the node group spans two AZs).
resource "kubernetes_storage_class_v1" "gp3" {
  metadata {
    name = "gp3"
  }
  storage_provisioner    = "ebs.csi.aws.com"
  reclaim_policy         = "Delete"
  volume_binding_mode    = "WaitForFirstConsumer"
  allow_volume_expansion = true
  parameters = {
    type      = "gp3"
    encrypted = "true"
  }

  depends_on = [aws_eks_addon.ebs_csi]
}
```

Pod-ceiling check (the module's comment on the two-node group explains that a t3.medium tops out at 17 pods and the platform sits at 16 at rest): with `controller.replicaCount = 1` the addon adds one `ebs-csi-controller` pod plus one `ebs-csi-node` DaemonSet pod per node. The DaemonSet stays on both nodes: nothing pins Redis to a node (`redis.yml` has no `nodeSelector`/`affinity`; `WaitForFirstConsumer` + `ReadWriteOnce` + `Recreate` let the scheduler pick, and the node agent must run wherever the volume attaches), so do not set `node.nodeSelector`. Total addon pods = 3; the platform goes from 16 to 19 pods at rest against a 34-pod ceiling (2 nodes x 17), leaving surge room for one rolling workload at a time.

The key was verified on 2026-09-29 against the public add-on catalogue (`describe-addon-configuration`, `aws-ebs-csi-driver` `v1.66.0-eksbuild.1` for EKS 1.36): `properties.controller.properties.replicaCount` is an integer, default 2, minimum 1. `data.aws_eks_addon_version` may resolve a newer build, so re-check before `terraform plan`:

```bash
cd /Users/efitz/Projects/tmi-965/terraform/environments/aws-public
AWS_PROFILE=tmi aws eks describe-addon-configuration --addon-name aws-ebs-csi-driver \
  --addon-version "$(AWS_PROFILE=tmi aws eks describe-addon-versions --addon-name aws-ebs-csi-driver --kubernetes-version 1.36 --query 'addons[0].addonVersions[0].addonVersion' --output text)" \
  --query configurationSchema --output text | jq '.properties.controller.properties | keys'
```

Expected: `replicaCount` is in the list. If a newer build dropped or renamed it, stop and report; do not remove `configuration_values` (that silently reverts to 2 replicas). After Eric's deferred apply, verify `kubectl -n kube-system get deploy ebs-csi-controller` shows `1/1` and `kubectl -n kube-system get ds ebs-csi-node` shows `2/2` (record both in the PR body at the deferred AWS deploy).

Then `AWS_PROFILE=tmi terraform init -backend-config=backend.hcl && AWS_PROFILE=tmi terraform validate && AWS_PROFILE=tmi terraform plan` from `terraform/environments/aws-public`; expected: 4 to add (`aws_iam_role.ebs_csi`, `aws_iam_role_policy_attachment.ebs_csi`, `aws_eks_addon.ebs_csi`, `kubernetes_storage_class_v1.gp3`), nothing changed or destroyed. Do **not** apply; Eric applies at the deferred AWS deploy (same rule as PR 6 Task 6 step 6). Add a paragraph "Redis persistence" to `deployments/k8s/dev/aws/README.md` next to the "NATS storage class" section: the base PVC, the `gp3` patch, the Terraform addon/StorageClass, and that the first `kubectl apply -k` after the Terraform apply creates the volume (about a minute before Redis is Ready).

In `scripts/lib/deploy.py`, add to the `ensure_redis_password_secret()` docstring (as merged in PR 6 it says nothing about persistence): "`dev-nuke` deletes the namespace, which takes tmi-secrets AND the redis-data PVC together, so the next start() regenerates the password against an empty Redis; `dev-down` keeps both."

- [ ] **Step 6: Verify on docker-desktop: render, AOF on, session survives a Redis restart**

```bash
cd /Users/efitz/Projects/tmi-965
for o in docker-desktop docker-desktop-oracle k3s aws; do
  echo "== $o"
  kubectl kustomize --load-restrictor LoadRestrictionsNone deployments/k8s/dev/$o > /tmp/pr1-$o.yml
  rg -c 'kind: PersistentVolumeClaim' /tmp/pr1-$o.yml          # 1
  rg -c -- '--appendonly' /tmp/pr1-$o.yml                        # 1 (followed by "yes")
  rg -n 'storageClassName' /tmp/pr1-$o.yml || echo "default class"   # k3s: longhorn; aws: gp3; docker-desktop*: default class
  rg -c 'type: Recreate' /tmp/pr1-$o.yml                         # 2 (tmi-server already uses Recreate; redis is the second)
done
make dev-up
kubectl -n tmi-platform get pvc redis-data                      # STATUS Bound
kubectl -n tmi-platform exec deploy/redis -- sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --tls --cacert /tls/ca.crt INFO persistence' | rg 'aof_enabled|aof_last_write_status|aof_last_bgrewrite_status'
```

Expected: `aof_enabled:1`, `aof_last_write_status:ok`, `aof_last_bgrewrite_status:ok`. A `Permission denied` on `/data/appendonlydir` in `kubectl -n tmi-platform logs deploy/redis` means the `fsGroup` from step 3 is wrong.

Session survival (no secret on any command line):

```bash
make start-oauth-stub
curl -s -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid":"alice"}'
# wait for the flow, then:
curl -s "http://localhost:8079/creds?userid=alice" | jq -r '.refresh_token' > "$SCRATCH/alice.rt"   # SCRATCH = the scratchpad dir
kubectl -n tmi-platform rollout restart deploy/redis && kubectl -n tmi-platform rollout status deploy/redis --timeout=120s
curl -s -o /dev/null -w '%{http_code}\n' -X POST http://localhost:8079/refresh -H 'Content-Type: application/json' -d "{\"userid\":\"alice\",\"refresh_token\":\"$(cat "$SCRATCH/alice.rt")\"}"
rm "$SCRATCH/alice.rt"
```

Expected: `200` (the refresh token persisted through the pod replacement). Before this task the same sequence returns `401`. Then the crash-restart case from step 2 (container restart inside the same pod, env re-resolved):

```bash
kubectl -n tmi-platform exec deploy/redis -- sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --tls --cacert /tls/ca.crt SHUTDOWN'
kubectl -n tmi-platform get pod -l app=redis      # RESTARTS 1, same pod name, Running
kubectl -n tmi-platform exec deploy/redis -- sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --tls --cacert /tls/ca.crt DBSIZE'
```

Expected: the pod restarts in place, the password from `tmi-secrets` is accepted and `DBSIZE` is non-zero (the AOF was replayed; `SHUTDOWN` without `NOSAVE` flushes it first). Finally `make dev-down && make dev-up` and repeat the `/refresh` call with a fresh token taken before the down: `200`.

- [ ] **Step 7: Commit**

```bash
cd /Users/efitz/Projects/tmi-965
git add deployments/k8s/dev/redis.yml deployments/k8s/dev/k3s deployments/k8s/dev/aws terraform/modules/kubernetes/aws/main.tf scripts/lib/deploy.py
git commit -m "feat(deploy): persist Redis on a PVC with AOF so rotations keep sessions (#965)

redis-data PVC (default class on docker-desktop, longhorn on k3s, gp3 on
AWS via a new EBS CSI addon + StorageClass in Terraform), appendonly yes
with appendfsync everysec, Recreate strategy for the RWO volume. ACL
changes are not persisted by design: the default user is rebuilt from
--requirepass (tmi-secrets) at every start, which is what the password
rotation relies on."
```

---

### Task 6: Redis password rotation

**Files:**
- Create: `internal/rotator/redis_password.go`
- Test: `internal/rotator/redis_password_test.go`

**Interfaces:**
- Produces: `RedisACL` interface `{ AddPassword(ctx, pw string) error; RemovePasswordHash(ctx, sha256hex string) error }`, `NewGoRedisACL(client *redis.Client) *GoRedisACL`, `NewRedisPasswordRotation(acl RedisACL) *RedisPasswordRotation` with `Name() == "redis-password"`. Secret data key `TMI_REDIS_PASSWORD`. Annotation `tmi.dev/rotation-retire.redis-password` = SHA-256 hex of the password being retired (a hash, not a value; needed so the completion write is annotation-only and does not roll the server again).

Phases:

| phase | on entry | writes | next |
|---|---|---|---|
| `""` | generate NEW; `ACL SETUSER default >NEW` | `TMI_REDIS_PASSWORD=NEW`, retire = sha256(OLD) | `swapped` |
| `swapped` | `ACL SETUSER default >NEW` again (idempotent; covers a Redis restart in between); wait server rolled; `ACL SETUSER default !<retire>` | annotations only: phase cleared, rotated-at, retire removed | done |

Why this is safe: Redis keeps a set of password hashes per user, so adding NEW while OLD still works means no client is ever locked out; the server pods restart (Reloader) with NEW from the Secret; only then is OLD removed. A Redis restart reads `--requirepass` from whatever the Secret holds, which is NEW from the first write onward.

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeACL struct {
	added   []string
	removed []string
}

func (f *fakeACL) AddPassword(_ context.Context, pw string) error { f.added = append(f.added, pw); return nil }
func (f *fakeACL) RemovePasswordHash(_ context.Context, h string) error {
	f.removed = append(f.removed, h)
	return nil
}

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestRedisPasswordRotation_FullCycle(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{"TMI_REDIS_PASSWORD": "old"}, Annotations: map[string]string{}})
	acl := &fakeACL{}
	r := NewRedisPasswordRotation(acl)
	require.NoError(t, r.Run(context.Background(), env))

	s, _ := st.Get(context.Background(), "tmi-secrets")
	newPw := s.Data["TMI_REDIS_PASSWORD"]
	require.NotEqual(t, "old", newPw)
	require.Len(t, newPw, 32)
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
	require.NotEmpty(t, s.Annotations[AnnRotatedAt+"redis-password"])
	_, retireLeft := s.Annotations[AnnRetire+"redis-password"]
	require.False(t, retireLeft)
	require.Equal(t, []string{newPw, newPw}, acl.added, "added before the swap and re-added on resume")
	require.Equal(t, []string{sha("old")}, acl.removed)
	require.Equal(t, 1, st.DataWrites, "exactly one server roll per rotation")
}

func TestRedisPasswordRotation_ResumeReAddsNewPassword(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "0"}})
	st.DataWrites = 1 // the swap write already happened and the server rolled
	acl := &fakeACL{}
	require.NoError(t, NewRedisPasswordRotation(acl).Run(context.Background(), env))
	require.Equal(t, []string{"new"}, acl.added)
	require.Equal(t, []string{sha("old")}, acl.removed)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
}

func TestRedisPasswordRotation_RolloutTimeoutKeepsPhase(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "5"}})
	// DataWrites (0) <= since (5): the fake waiter reports "did not roll".
	acl := &fakeACL{}
	err := NewRedisPasswordRotation(acl).Run(context.Background(), env)
	require.Error(t, err)
	require.Empty(t, acl.removed, "old password must not be retired before the rollout")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "swapped", s.Annotations[AnnPhase+"redis-password"])
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestRedisPasswordRotation count1=true`
Expected: `undefined: NewRedisPasswordRotation`.

- [ ] **Step 3: Implement**

```go
package rotator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	RedisPasswordKey   = "TMI_REDIS_PASSWORD"
	AnnRetire          = "tmi.dev/rotation-retire."
	redisRotationName  = "redis-password"
	redisPhaseSwapped  = "swapped"
)

// RedisACL manages the passwords accepted for the default Redis user.
// SEM@<sha>: add or retire passwords on the Redis default user
type RedisACL interface {
	AddPassword(ctx context.Context, pw string) error
	RemovePasswordHash(ctx context.Context, sha256hex string) error
}

// GoRedisACL implements RedisACL with ACL SETUSER.
// SEM@<sha>: RedisACL over a go-redis client using ACL SETUSER
type GoRedisACL struct{ client *redis.Client }

// SEM@<sha>: wrap a go-redis client as a RedisACL (pure)
func NewGoRedisACL(client *redis.Client) *GoRedisACL { return &GoRedisACL{client: client} }

// SEM@<sha>: add a password to the default user (ACL SETUSER default >pw); idempotent
func (a *GoRedisACL) AddPassword(ctx context.Context, pw string) error {
	return a.client.Do(ctx, "ACL", "SETUSER", "default", ">"+pw).Err()
}

// SEM@<sha>: remove a password from the default user by its SHA-256 (ACL SETUSER default !hash); idempotent
func (a *GoRedisACL) RemovePasswordHash(ctx context.Context, sha256hex string) error {
	return a.client.Do(ctx, "ACL", "SETUSER", "default", "!"+sha256hex).Err()
}

// RedisPasswordRotation rotates TMI_REDIS_PASSWORD without a Redis restart.
// SEM@<sha>: phased rotation of the Redis password: add new, swap Secret, roll server, retire old
type RedisPasswordRotation struct{ acl RedisACL }

// SEM@<sha>: build a RedisPasswordRotation over a RedisACL (pure)
func NewRedisPasswordRotation(acl RedisACL) *RedisPasswordRotation {
	return &RedisPasswordRotation{acl: acl}
}

// SEM@<sha>: return the rotation name used in annotations and ROTATE (pure)
func (r *RedisPasswordRotation) Name() string { return redisRotationName }

// SEM@<sha>: advance the Redis password rotation from its recorded phase to completion
func (r *RedisPasswordRotation) Run(ctx context.Context, env *Env) error {
	logger := slogging.Get()
	s, err := env.Secrets.Get(ctx, env.SecretName)
	if err != nil {
		return err
	}
	phase := s.Annotations[AnnPhase+r.Name()]

	if phase == "" {
		old := s.Data[RedisPasswordKey]
		if old == "" {
			return fmt.Errorf("%s has no %s to rotate", env.SecretName, RedisPasswordKey)
		}
		next, err := NewPassword()
		if err != nil {
			return err
		}
		if err := r.acl.AddPassword(ctx, next); err != nil {
			return fmt.Errorf("add new Redis password: %w", err)
		}
		logger.Info("Redis accepts the new password; swapping the Secret")
		if err := env.Transition(ctx, r.Name(), redisPhaseSwapped, func(s *Secret) {
			s.Data[RedisPasswordKey] = next
			s.Annotations[AnnRetire+r.Name()] = sha256Hex(old)
		}); err != nil {
			return err
		}
		if s, err = env.Secrets.Get(ctx, env.SecretName); err != nil {
			return err
		}
		phase = redisPhaseSwapped
	}

	if phase != redisPhaseSwapped {
		return fmt.Errorf("unknown %s phase %q", r.Name(), phase)
	}
	// Idempotent: a Redis restart between runs would have dropped the in-memory ACL entry.
	if err := r.acl.AddPassword(ctx, s.Data[RedisPasswordKey]); err != nil {
		return fmt.Errorf("re-add new Redis password: %w", err)
	}
	if err := env.WaitServerRolled(ctx, s, r.Name()); err != nil {
		return fmt.Errorf("waiting for %s to pick up the new Redis password: %w", env.ServerDeployment, err)
	}
	if retire := s.Annotations[AnnRetire+r.Name()]; retire != "" {
		if err := r.acl.RemovePasswordHash(ctx, retire); err != nil {
			return fmt.Errorf("retire old Redis password: %w", err)
		}
		logger.Info("Old Redis password retired")
	}
	return env.Transition(ctx, r.Name(), "", func(s *Secret) {
		delete(s.Annotations, AnnRetire+r.Name())
	})
}

// SEM@<sha>: hex SHA-256 of a string (pure)
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
```

- [ ] **Step 4: Run the tests**

Run: `make test-unit name=TestRedisPasswordRotation count1=true`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/redis_password.go internal/rotator/redis_password_test.go
git commit -m "feat(rotator): rotate the Redis password via ACL SETUSER without a restart (#965)"
```

---
### Task 7: Batched, resumable `ReEncryptAll`

**Files:**
- Modify: `api/settings_service.go:689-790` (`ReEncryptAll`, new `reEncryptOne`, `CountValuesWithContextID`)
- Modify: `api/server.go:36` (interface gains `CountValuesWithContextID`), `api/config_handlers.go:843-900` (503 wording), `api/config_handlers_test.go` and `api/runtime_config_reader_adapter_test.go` (mock/fake gain the new method)
- Modify: `api-schema/tmi-openapi.json` (`reencryptSystemSettings` description, 503 description)
- Test: `api/settings_service_test.go`

**Interfaces:**
- Produces: `(*SettingsService).ReEncryptAll(ctx) (int, []SettingError, error)` (same signature; now commits per row and returns the count committed so far on a DB failure); `(*SettingsService).CountValuesWithContextID(ctx, id int) (int64, error)`; `SettingsServiceInterface.CountValuesWithContextID`. `reEncryptBatchSize = 100`.
- Consumes: `crypto.(*SettingsEncryptor).CurrentPrefix()` (Task 1).

- [ ] **Step 1: Write the failing tests** (append to `api/settings_service_test.go`; `setupSettingsTestDB` already gives a SQLite `*gorm.DB`)

```go
func seedEncrypted(t *testing.T, gormDB *gorm.DB, enc *crypto.SettingsEncryptor, key, plaintext string) {
	t.Helper()
	v, err := enc.Encrypt(plaintext)
	require.NoError(t, err)
	require.NoError(t, gormDB.Create(&models.SystemSetting{SettingKey: models.DBVarchar(key), Value: models.DBText(v), SettingType: models.SystemSettingTypeString, ModifiedAt: time.Now()}).Error)
}

func TestReEncryptAll_ResumesAndOnlyTouchesStaleRows(t *testing.T) {
	gormDB := setupSettingsTestDB(t)
	k1, k2 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	old, _ := crypto.NewSettingsEncryptorFromKeyring(k1, 1, nil, 0)
	cur, _ := crypto.NewSettingsEncryptorFromKeyring(k2, 2, k1, 1)
	for i := 0; i < 250; i++ {
		seedEncrypted(t, gormDB, old, fmt.Sprintf("k.%03d", i), fmt.Sprintf("v%d", i))
	}
	seedEncrypted(t, gormDB, cur, "already.current", "fresh")
	require.NoError(t, gormDB.Create(&models.SystemSetting{SettingKey: "plain", Value: "text", SettingType: models.SystemSettingTypeString, ModifiedAt: time.Now()}).Error)

	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(cur)
	n, errs, err := svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	require.Empty(t, errs)
	require.Equal(t, 251, n, "250 stale rows + 1 plaintext; the current row is untouched")

	stale, err := svc.CountValuesWithContextID(context.Background(), 1)
	require.NoError(t, err)
	require.Zero(t, stale)
	var row models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "k.000").First(&row).Error)
	require.True(t, strings.HasPrefix(string(row.Value), "ENC:v1:2:"))
	got, err := cur.Decrypt(string(row.Value))
	require.NoError(t, err)
	require.Equal(t, "v0", got)

	// Second pass is a no-op.
	n, _, err = svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestReEncryptAll_UndecryptableRowIsSkippedOnce(t *testing.T) {
	gormDB := setupSettingsTestDB(t)
	k1, k2, k3 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32)
	stranger, _ := crypto.NewSettingsEncryptorFromKeyring(k3, 9, nil, 0)
	old, _ := crypto.NewSettingsEncryptorFromKeyring(k1, 1, nil, 0)
	cur, _ := crypto.NewSettingsEncryptorFromKeyring(k2, 2, k1, 1)
	seedEncrypted(t, gormDB, stranger, "bad", "x")
	seedEncrypted(t, gormDB, old, "good", "y")

	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(cur)
	n, errs, err := svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Len(t, errs, 1)
	require.Equal(t, "bad", errs[0].Key)
}

func TestReEncryptAll_PreservesAuditFields(t *testing.T) {
	gormDB := setupSettingsTestDB(t)
	k1, k2 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)
	old, _ := crypto.NewSettingsEncryptorFromKeyring(k1, 1, nil, 0)
	cur, _ := crypto.NewSettingsEncryptorFromKeyring(k2, 2, k1, 1)
	v, _ := old.Encrypt("s")
	then := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	require.NoError(t, gormDB.Create(&models.SystemSetting{SettingKey: "a", Value: models.DBText(v), SettingType: models.SystemSettingTypeString, ModifiedAt: then, ModifiedBy: models.NullableDBVarchar("someone")}).Error)
	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(cur)
	_, _, err := svc.ReEncryptAll(context.Background())
	require.NoError(t, err)
	var row models.SystemSetting
	require.NoError(t, gormDB.Where("setting_key = ?", "a").First(&row).Error)
	require.Equal(t, then.Unix(), row.ModifiedAt.Unix())
	require.Equal(t, models.NullableDBVarchar("someone"), row.ModifiedBy)
}
```

If an existing test (`TestReEncryptAll*` from #805/#845) asserts the whole pass rolls back on a write failure, rewrite it to assert the rows before the failure stay re-encrypted and `n` counts them (the #845 reversal, Open question 3). Check with `rg -n "ReEncryptAll" api/settings_service_test.go`.

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestReEncryptAll count1=true`
Expected: `undefined: CountValuesWithContextID` / the resume test fails because the current row is counted.

- [ ] **Step 3: Implement**

Replace `ReEncryptAll` and its doc comment in `api/settings_service.go`:

```go
// reEncryptBatchSize bounds one SELECT of stale keys; the pass loops until none remain.
const reEncryptBatchSize = 100

// maxUnreadableSettings caps the NOT IN exclusion list (Oracle allows 1000).
const maxUnreadableSettings = 900

// errSettingUnreadable marks a row that cannot be decrypted or re-encrypted:
// reported, skipped, never a database failure.
var errSettingUnreadable = errors.New("setting unreadable")

// ReEncryptAll re-encrypts every system_settings value not already under the
// current key id. Each row is its own short transaction (#965): a crash or a
// database error leaves a readable mix of old-id and current-id rows and the
// next call finishes the job, because the selection is by envelope prefix, not
// by a full-table pass. Only the value column is written (#805): modified_at and
// modified_by are left as they were. Rows no key can open are reported in
// []SettingError and excluded from later batches so the loop always terminates.
// Returns the number of rows committed so far, also on error.
// SEM@<sha>: re-encrypt stale setting rows under the current key id in resumable per-row transactions (writes DB)
func (s *SettingsService) ReEncryptAll(ctx context.Context) (int, []SettingError, error) {
	logger := slogging.Get()
	if s.encryptor == nil || !s.encryptor.IsEnabled() {
		return 0, nil, ErrEncryptionNotEnabled
	}
	prefix := s.encryptor.CurrentPrefix()
	var reencrypted int
	var settingErrors []SettingError
	var skip []string
	for {
		q := s.gormDB.WithContext(ctx).Model(&models.SystemSetting{}).
			Where("value NOT LIKE ?", prefix+"%").
			Order("setting_key").Limit(reEncryptBatchSize)
		if len(skip) > 0 {
			q = q.Where("setting_key NOT IN ?", skip)
		}
		var keys []string
		if err := q.Pluck("setting_key", &keys).Error; err != nil {
			return reencrypted, settingErrors, fmt.Errorf("failed to list settings for re-encryption: %w", err)
		}
		if len(keys) == 0 {
			break
		}
		for _, key := range keys {
			err := s.reEncryptOne(ctx, key)
			switch {
			case err == nil:
				reencrypted++
			case errors.Is(err, errSettingUnreadable):
				logger.Warn("Setting %s skipped during re-encryption: %v", key, err)
				settingErrors = append(settingErrors, SettingError{Key: key, Error: err.Error()})
				skip = append(skip, key)
				if len(skip) >= maxUnreadableSettings { // stays under Oracle's 1000-element IN list
					s.InvalidateAll(ctx)
					return reencrypted, settingErrors, fmt.Errorf("re-encryption stopped: %d settings are unreadable", len(skip))
				}
			default:
				logger.Error("Re-encryption stopped after %d rows: %v", reencrypted, err)
				s.InvalidateAll(ctx)
				return reencrypted, settingErrors, err
			}
		}
	}
	s.InvalidateAll(ctx)
	logger.Info("Re-encryption completed: %d re-encrypted, %d errors", reencrypted, len(settingErrors))
	return reencrypted, settingErrors, nil
}

// reEncryptOne rewrites one row's ciphertext under the current key inside its
// own transaction, holding a row lock on PostgreSQL/Oracle so a concurrent
// SettingsService.Set cannot be overwritten.
// SEM@<sha>: re-encrypt a single setting row under a row lock in one transaction (writes DB)
func (s *SettingsService) reEncryptOne(ctx context.Context, key string) error {
	return db.WithRetryableGormTransaction(ctx, s.gormDB, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
		q := tx.Where("setting_key = ?", key)
		if tx.Dialector.Name() != "sqlite" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var row models.SystemSetting
		if err := q.First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: setting no longer exists", errSettingUnreadable)
			}
			return fmt.Errorf("failed to read setting %s: %w", key, err)
		}
		plaintext, err := s.encryptor.Decrypt(string(row.Value))
		if err != nil {
			return fmt.Errorf("%w: %v", errSettingUnreadable, err)
		}
		encrypted, err := s.encryptor.Encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("%w: %v", errSettingUnreadable, err)
		}
		// UpdateColumn skips hooks: autoUpdateTime never moves modified_at (#805).
		res := tx.Model(&models.SystemSetting{}).Where("setting_key = ?", key).UpdateColumn("value", models.DBText(encrypted))
		if res.Error != nil {
			return fmt.Errorf("failed to save re-encrypted setting %s: %w", key, res.Error)
		}
		return nil
	})
}

// CountValuesWithContextID counts rows still encrypted under the given key id;
// the rotator uses it to decide when the previous key can be dropped.
// SEM@<sha>: count setting rows whose envelope carries a given key id (reads DB)
func (s *SettingsService) CountValuesWithContextID(ctx context.Context, id int) (int64, error) {
	var n int64
	err := s.gormDB.WithContext(ctx).Model(&models.SystemSetting{}).
		Where("value LIKE ?", fmt.Sprintf("ENC:v1:%d:%%", id)).Count(&n).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count settings under key id %d: %w", id, err)
	}
	return n, nil
}
```

Add `"gorm.io/gorm/clause"` to the imports. Oracle notes for the reviewer (Task 13): `value` is a CLOB on Oracle; `LIKE` on a CLOB is supported, `=` is not (which is why the guard is a row lock rather than `AND value = ?`); `NOT IN` lists are capped at `maxUnreadableSettings` (900) so they stay under Oracle's 1000-element limit.

`api/server.go:36`: add `CountValuesWithContextID(ctx context.Context, id int) (int64, error)` to `SettingsServiceInterface`; add a trivial implementation to `MockSettingsService` (`api/config_handlers_test.go`) and `fakeSettingsService` (`api/runtime_config_reader_adapter_test.go`).

`api/config_handlers.go` `ReencryptSystemSettings`: the `dberrors.ErrTransient` and generic branches now say `"Re-encryption stopped on a transient database error; rows already re-encrypted are kept, retry to finish"` (503) and `"Re-encryption stopped on a database error; rows already re-encrypted are kept"` (500). Update the two comments above them (no more "rolled back").

`api-schema/tmi-openapi.json` `reencryptSystemSettings` description: append `" The pass is resumable: each row is committed on its own, so a failure part-way keeps the rows already converted and a retry finishes the rest."` Update the 503 response description to match. Then `make validate-openapi` and `make generate-api`.

- [ ] **Step 4: Run tests, lint, build**

Run: `make test-unit name=TestReEncryptAll count1=true`, `make test-unit name=TestReencrypt count1=true` (handler tests), `make lint`, `make build-server`
Expected: PASS, lint clean (`check-oracle-*` scripts included).

- [ ] **Step 5: Commit**

```bash
git add api/settings_service.go api/settings_service_test.go api/server.go api/config_handlers.go api/config_handlers_test.go api/runtime_config_reader_adapter_test.go api-schema/tmi-openapi.json api/api.go
git commit -m "feat(settings): make ReEncryptAll batched and resumable by envelope key id (#965)"
```

---

### Task 8: Settings-key rotation

**Files:**
- Create: `internal/rotator/settings_key.go`
- Test: `internal/rotator/settings_key_test.go`

**Interfaces:**
- Produces: `SettingsStore` interface `{ ReEncrypt(ctx, keyring Keyring) (int, error); CountWithID(ctx, keyring Keyring, id int) (int64, error) }`; `Keyring{CurrentKeyHex string; CurrentID int; PreviousKeyHex string; PreviousID int}`; `KeyringFromSecret(s *Secret) (Keyring, error)`; `NewSettingsKeyRotation(store SettingsStore, previousGrace time.Duration) *SettingsKeyRotation` with `Name() == "settings-key"`. `NewGormSettingsStore(gormDB *gorm.DB, redis *db.RedisDB) *GormSettingsStore` (wraps `api.SettingsService`).
- Secret data keys: `TMI_SECRET_SETTINGS_ENCRYPTION_KEY`, `TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID` (missing = `1`), `TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY`, `TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID`. Annotation `tmi.dev/rotation-promoted-at.settings-key`.

Phases:

| phase | on entry | writes (one Update) | next |
|---|---|---|---|
| `""` | generate NEW, `newID = curID+1` | previous = NEW/newID (current unchanged) | `staged` |
| `staged` | wait server rolled (every pod can now read both ids) | current = NEW/newID, previous = OLD/oldID | `promoted` |
| `promoted` | wait server rolled (every pod now writes newID); `ReEncrypt` in-process | annotations only: promoted-at = now, rotated-at = now | `reencrypted` |
| `reencrypted` | if now < promoted-at + grace: log and stop (parked, not an error). Else if `CountWithID(oldID) > 0`: `ReEncrypt` again and stop. Else: | previous pair deleted (data change: one more roll) | done |

`rotated-at` is stamped at `promoted` so the schedule counts from the key swap; the phase stays set until the drop, and `Run` resumes it daily.

- [ ] **Step 1: Write the failing tests**

```go
package rotator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeSettings struct {
	rows      map[int]int64 // rows per key id
	reencrypt int
	lastKr    Keyring
}

func (f *fakeSettings) ReEncrypt(_ context.Context, kr Keyring) (int, error) {
	f.reencrypt++
	f.lastKr = kr
	var moved int64
	for id, n := range f.rows {
		if id != kr.CurrentID {
			moved += n
			delete(f.rows, id)
		}
	}
	f.rows[kr.CurrentID] += moved
	return int(moved), nil
}

func (f *fakeSettings) CountWithID(_ context.Context, _ Keyring, id int) (int64, error) { return f.rows[id], nil }

func settingsSecret() *Secret {
	return &Secret{Name: "tmi-secrets", Data: map[string]string{
		"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "0000000000000000000000000000000000000000000000000000000000000001",
		"TMI_DATABASE_URL":                   "postgres://x",
	}, Annotations: map[string]string{}}
}

func TestSettingsKeyRotation_StageThenPromoteThenReencrypt(t *testing.T) {
	env, st := testEnv(settingsSecret())
	fs := &fakeSettings{rows: map[int]int64{1: 40}}
	r := NewSettingsKeyRotation(fs, 8*24*time.Hour)
	require.NoError(t, r.Run(context.Background(), env))

	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
	kr, err := KeyringFromSecret(s)
	require.NoError(t, err)
	require.Equal(t, 2, kr.CurrentID)
	require.Equal(t, 1, kr.PreviousID)
	require.Len(t, kr.CurrentKeyHex, 64)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000001", kr.PreviousKeyHex)
	require.Equal(t, 2, st.DataWrites, "stage and promote each roll the server once")
	require.Equal(t, 1, fs.reencrypt)
	require.EqualValues(t, 40, fs.rows[2])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnRotatedAt+"settings-key"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnPromotedAt+"settings-key"])
}

func TestSettingsKeyRotation_DropWaitsForGraceAndZeroRows(t *testing.T) {
	env, st := testEnv(settingsSecret())
	fs := &fakeSettings{rows: map[int]int64{1: 3}}
	r := NewSettingsKeyRotation(fs, 8*24*time.Hour)
	require.NoError(t, r.Run(context.Background(), env)) // -> reencrypted
	writes := st.DataWrites

	// Same day: parked.
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
	require.Equal(t, writes, st.DataWrites)

	// Grace elapsed but a row under the old id reappeared (restored backup): re-encrypt, still parked.
	env.Now = func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }
	fs.rows[1] = 1
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
	require.Zero(t, fs.rows[1])

	// Next run: drop.
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"settings-key"])
	_, hasPrev := s.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"]
	require.False(t, hasPrev)
	_, hasPrevID := s.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"]
	require.False(t, hasPrevID)
	require.Equal(t, writes+1, st.DataWrites)
}

func TestSettingsKeyRotation_ResumeFromStaged(t *testing.T) {
	sec := settingsSecret()
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"] = "0000000000000000000000000000000000000000000000000000000000000002"
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"] = "2"
	sec.Annotations[AnnPhase+"settings-key"] = "staged"
	sec.Annotations[AnnGeneration+"settings-key"] = "0"
	env, st := testEnv(sec)
	st.DataWrites = 1
	fs := &fakeSettings{rows: map[int]int64{1: 2}}
	require.NoError(t, NewSettingsKeyRotation(fs, time.Hour).Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	kr, _ := KeyringFromSecret(s)
	require.Equal(t, 2, kr.CurrentID)
	require.Equal(t, "0000000000000000000000000000000000000000000000000000000000000002", kr.CurrentKeyHex)
	require.Equal(t, 1, kr.PreviousID)
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
}

func TestKeyringFromSecret_Validation(t *testing.T) {
	_, err := KeyringFromSecret(&Secret{Data: map[string]string{}})
	require.Error(t, err, "no key")
	_, err = KeyringFromSecret(&Secret{Data: map[string]string{"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "zz"}})
	require.Error(t, err, "not 64 hex chars")
	kr, err := KeyringFromSecret(&Secret{Data: map[string]string{"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "0000000000000000000000000000000000000000000000000000000000000001"}})
	require.NoError(t, err)
	require.Equal(t, 1, kr.CurrentID, "missing context id defaults to 1")
	require.Zero(t, kr.PreviousID)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestSettingsKeyRotation count1=true`
Expected: `undefined: NewSettingsKeyRotation`.

- [ ] **Step 3: Implement `settings_key.go`**

```go
package rotator

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	SettingsKeyKey        = "TMI_SECRET_SETTINGS_ENCRYPTION_KEY"
	SettingsKeyIDKey      = "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"
	SettingsPrevKeyKey    = "TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"
	SettingsPrevKeyIDKey  = "TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"
	AnnPromotedAt         = "tmi.dev/rotation-promoted-at."
	settingsRotationName  = "settings-key"
	settingsPhaseStaged   = "staged"
	settingsPhasePromoted = "promoted"
	settingsPhaseReenc    = "reencrypted"
)

// Keyring is the settings-encryption key material held in tmi-secrets.
// SEM@<sha>: current and previous settings encryption keys with their ids (pure)
type Keyring struct {
	CurrentKeyHex  string
	CurrentID      int
	PreviousKeyHex string
	PreviousID     int
}

// KeyringFromSecret reads and validates the keyring; a missing context id is 1
// (the id every pre-rotation value carries).
// SEM@<sha>: parse and validate the settings keyring from Secret data (pure)
func KeyringFromSecret(s *Secret) (Keyring, error) {
	kr := Keyring{CurrentKeyHex: s.Data[SettingsKeyKey], CurrentID: 1, PreviousKeyHex: s.Data[SettingsPrevKeyKey]}
	if err := checkHexKey(kr.CurrentKeyHex); err != nil {
		return Keyring{}, fmt.Errorf("%s: %w", SettingsKeyKey, err)
	}
	if v := s.Data[SettingsKeyIDKey]; v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id <= 0 {
			return Keyring{}, fmt.Errorf("%s is not a positive integer", SettingsKeyIDKey)
		}
		kr.CurrentID = id
	}
	if kr.PreviousKeyHex != "" {
		if err := checkHexKey(kr.PreviousKeyHex); err != nil {
			return Keyring{}, fmt.Errorf("%s: %w", SettingsPrevKeyKey, err)
		}
		id, err := strconv.Atoi(s.Data[SettingsPrevKeyIDKey])
		if err != nil || id <= 0 {
			return Keyring{}, fmt.Errorf("%s is not a positive integer", SettingsPrevKeyIDKey)
		}
		kr.PreviousID = id
	}
	return kr, nil
}

// SEM@<sha>: validate a 64-hex-char key string (pure)
func checkHexKey(v string) error {
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("must be 64 hex characters")
	}
	return nil
}

// Encryptor builds the crypto keyring for this Keyring.
// SEM@<sha>: build a SettingsEncryptor from a Keyring (pure)
func (k Keyring) Encryptor() (*crypto.SettingsEncryptor, error) {
	cur, _ := hex.DecodeString(k.CurrentKeyHex)
	var prev []byte
	if k.PreviousKeyHex != "" {
		prev, _ = hex.DecodeString(k.PreviousKeyHex)
	}
	return crypto.NewSettingsEncryptorFromKeyring(cur, k.CurrentID, prev, k.PreviousID)
}

// SettingsStore is the database side of the settings-key rotation.
// SEM@<sha>: re-encrypt settings under a keyring and count rows per key id
type SettingsStore interface {
	ReEncrypt(ctx context.Context, kr Keyring) (int, error)
	CountWithID(ctx context.Context, kr Keyring, id int) (int64, error)
}

// GormSettingsStore runs the real SettingsService against the database.
// SEM@<sha>: SettingsStore backed by api.SettingsService over GORM (writes DB)
type GormSettingsStore struct {
	gormDB *gorm.DB
	redis  *db.RedisDB
}

// SEM@<sha>: build a GormSettingsStore (pure)
func NewGormSettingsStore(gormDB *gorm.DB, redis *db.RedisDB) *GormSettingsStore {
	return &GormSettingsStore{gormDB: gormDB, redis: redis}
}

// SEM@<sha>: build a SettingsService for a keyring (pure)
func (g *GormSettingsStore) service(kr Keyring) (*api.SettingsService, error) {
	enc, err := kr.Encryptor()
	if err != nil {
		return nil, err
	}
	svc := api.NewSettingsService(g.gormDB, g.redis)
	svc.SetEncryptor(enc)
	return svc, nil
}

// SEM@<sha>: re-encrypt every stale settings row under the keyring's current key (writes DB)
func (g *GormSettingsStore) ReEncrypt(ctx context.Context, kr Keyring) (int, error) {
	svc, err := g.service(kr)
	if err != nil {
		return 0, err
	}
	n, rowErrs, err := svc.ReEncryptAll(ctx)
	for _, e := range rowErrs {
		slogging.Get().Warn("Setting %s could not be re-encrypted: %s", e.Key, e.Error)
	}
	if err != nil {
		return n, err
	}
	if len(rowErrs) > 0 {
		return n, fmt.Errorf("%d settings could not be re-encrypted (see log)", len(rowErrs))
	}
	return n, nil
}

// SEM@<sha>: count settings rows still under a key id (reads DB)
func (g *GormSettingsStore) CountWithID(ctx context.Context, kr Keyring, id int) (int64, error) {
	svc, err := g.service(kr)
	if err != nil {
		return 0, err
	}
	return svc.CountValuesWithContextID(ctx, id)
}

// SettingsKeyRotation rotates the settings-encryption key: stage, promote,
// re-encrypt, and (after previousGrace, once nothing references the old id) drop.
// SEM@<sha>: phased rotation of the settings encryption key with deferred drop of the previous key
type SettingsKeyRotation struct {
	store         SettingsStore
	previousGrace time.Duration
}

// SEM@<sha>: build a SettingsKeyRotation (pure)
func NewSettingsKeyRotation(store SettingsStore, previousGrace time.Duration) *SettingsKeyRotation {
	return &SettingsKeyRotation{store: store, previousGrace: previousGrace}
}

// SEM@<sha>: return the rotation name (pure)
func (r *SettingsKeyRotation) Name() string { return settingsRotationName }

// SEM@<sha>: advance the settings-key rotation from its recorded phase
func (r *SettingsKeyRotation) Run(ctx context.Context, env *Env) error {
	logger := slogging.Get()
	s, err := env.Secrets.Get(ctx, env.SecretName)
	if err != nil {
		return err
	}
	kr, err := KeyringFromSecret(s)
	if err != nil {
		return err
	}
	name := r.Name()
	phase := s.Annotations[AnnPhase+name]

	if phase == "" {
		if kr.PreviousID != 0 {
			return fmt.Errorf("previous key id %d still present; refusing to start a new rotation", kr.PreviousID)
		}
		next, err := NewHexKey()
		if err != nil {
			return err
		}
		newID := kr.CurrentID + 1
		if err := env.Transition(ctx, name, settingsPhaseStaged, func(s *Secret) {
			s.Data[SettingsPrevKeyKey] = next
			s.Data[SettingsPrevKeyIDKey] = strconv.Itoa(newID)
		}); err != nil {
			return err
		}
		logger.Info("Settings key staged id=%d", newID)
		if s, err = env.Secrets.Get(ctx, env.SecretName); err != nil {
			return err
		}
		phase = settingsPhaseStaged
	}

	if phase == settingsPhaseStaged {
		if err := env.WaitServerRolled(ctx, s, name); err != nil {
			return fmt.Errorf("staged key rollout: %w", err)
		}
		kr, _ := KeyringFromSecret(s)
		if err := env.Transition(ctx, name, settingsPhasePromoted, func(s *Secret) {
			s.Data[SettingsKeyKey] = kr.PreviousKeyHex
			s.Data[SettingsKeyIDKey] = strconv.Itoa(kr.PreviousID)
			s.Data[SettingsPrevKeyKey] = kr.CurrentKeyHex
			s.Data[SettingsPrevKeyIDKey] = strconv.Itoa(kr.CurrentID)
		}); err != nil {
			return err
		}
		logger.Info("Settings key promoted id=%d previous_id=%d", kr.PreviousID, kr.CurrentID)
		if s, err = env.Secrets.Get(ctx, env.SecretName); err != nil {
			return err
		}
		phase = settingsPhasePromoted
	}

	if phase == settingsPhasePromoted {
		if err := env.WaitServerRolled(ctx, s, name); err != nil {
			return fmt.Errorf("promoted key rollout: %w", err)
		}
		kr, err := KeyringFromSecret(s)
		if err != nil {
			return err
		}
		n, err := r.store.ReEncrypt(ctx, kr)
		if err != nil {
			return fmt.Errorf("re-encrypt after %d rows: %w", n, err)
		}
		logger.Info("Settings re-encrypted rows=%d id=%d", n, kr.CurrentID)
		now := env.Now().UTC().Format(time.RFC3339)
		if err := env.Transition(ctx, name, settingsPhaseReenc, func(s *Secret) {
			s.Annotations[AnnPromotedAt+name] = now
			s.Annotations[AnnRotatedAt+name] = now
		}); err != nil {
			return err
		}
		return nil // the drop is decided by a later run
	}

	if phase != settingsPhaseReenc {
		return fmt.Errorf("unknown %s phase %q", name, phase)
	}
	promotedAt, err := time.Parse(time.RFC3339, s.Annotations[AnnPromotedAt+name])
	if err != nil {
		return fmt.Errorf("missing promoted-at annotation for %s", name)
	}
	if env.Now().Before(promotedAt.Add(r.previousGrace)) {
		logger.Info("Settings previous key kept until %s (Redis entries under the old id may still be live)", promotedAt.Add(r.previousGrace).Format(time.RFC3339))
		return nil
	}
	remaining, err := r.store.CountWithID(ctx, kr, kr.PreviousID)
	if err != nil {
		return err
	}
	if remaining > 0 {
		n, err := r.store.ReEncrypt(ctx, kr)
		if err != nil {
			return fmt.Errorf("re-encrypt %d remaining rows: %w", remaining, err)
		}
		logger.Info("Settings re-encrypted late rows=%d; previous key kept until the next run", n)
		return nil
	}
	if err := env.Transition(ctx, name, "", func(s *Secret) {
		delete(s.Data, SettingsPrevKeyKey)
		delete(s.Data, SettingsPrevKeyIDKey)
		delete(s.Annotations, AnnPromotedAt+name)
		s.Annotations[AnnRotatedAt+name] = promotedAt.UTC().Format(time.RFC3339)
	}); err != nil {
		return err
	}
	logger.Info("Settings previous key dropped id=%d", kr.PreviousID)
	return nil
}
```

`Transition` with `nextPhase == ""` stamps `rotated-at` with `Now()` and then runs `mutate` (Task 3), so the drop's mutate re-stamps it with the promotion time and the 90-day clock does not restart.

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestSettingsKeyRotation count1=true`, `make test-unit name=TestKeyringFromSecret count1=true`, `make test-unit name=TestTransition count1=true`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rotator/settings_key.go internal/rotator/settings_key_test.go
git commit -m "feat(rotator): stage, promote, re-encrypt and drop the settings encryption key (#965)"
```

---
### Task 9: `cmd/rotator` binary, build targets, image

**Files:**
- Create: `cmd/rotator/main.go`
- Modify: `scripts/build-server.py:40-80` (COMPONENTS), `Makefile:129-160` (`build-rotator`), `Dockerfile.server:30-56`, `Dockerfile.server-oracle:105-175`
- Modify: `internal/config/process_env.go` (rotator env vars), then `make generate-config-docs`
- Test: `cmd/rotator/main_test.go` (env parsing only)

**Interfaces:**
- Consumes: `rotator.Run`, `rotator.NewKubeSecretStore`, `rotator.NewKubeRolloutWaiter`, `rotator.NewRedisPasswordRotation`, `rotator.NewGoRedisACL`, `rotator.NewSettingsKeyRotation`, `rotator.NewGormSettingsStore`, `db.NewRedisDB(db.RedisConfig{... TLSEnabled, TLSCAFile})` (PR 6), `db.ParseDatabaseURL`, `db.NewGormDB`.
- Produces: env contract (all read only here):

| Var | Default | Purpose |
|---|---|---|
| `ROTATE` | `""` | force one rotation by name |
| `TMI_ROTATOR_NAMESPACE` | `tmi-platform` | namespace of the Secret and Deployment |
| `TMI_ROTATOR_SECRET` | `tmi-secrets` | Secret holding the rotating values |
| `TMI_ROTATOR_SERVER_DEPLOYMENT` | `tmi-server` | Deployment Reloader rolls |
| `TMI_ROTATOR_ROLLOUT_TIMEOUT` | `10m` | per-phase rollout wait |
| `TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE` | `192h` | wait before dropping the previous settings key |
| `TMI_REDIS_HOST`, `TMI_REDIS_PORT`, `TMI_REDIS_DB` | `redis`, `6379`, `0` | Redis endpoint (password comes from the Secret) |
| `TMI_REDIS_TLS_ENABLED`, `TMI_REDIS_TLS_CA_FILE` | PR 6 | Redis TLS |
| `TMI_DATABASE_URL` | from the Secret's `TMI_DATABASE_URL` | override for clusters where the URL is not in tmi-secrets |
| `TMI_ORACLE_WALLET_LOCATION` | unset | passed through to `db.GormConfig` when set |

- [ ] **Step 1: Write the failing test** (`cmd/rotator/main_test.go`)

```go
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadOptions_DefaultsAndOverrides(t *testing.T) {
	o, err := loadOptions(func(string) string { return "" })
	require.NoError(t, err)
	require.Equal(t, "tmi-platform", o.Namespace)
	require.Equal(t, "tmi-secrets", o.SecretName)
	require.Equal(t, "tmi-server", o.ServerDeployment)
	require.Equal(t, 10*time.Minute, o.RolloutTimeout)
	require.Equal(t, 192*time.Hour, o.SettingsPreviousGrace)
	require.Equal(t, "redis", o.RedisHost)
	require.Equal(t, "6379", o.RedisPort)

	env := map[string]string{"ROTATE": "redis-password", "TMI_ROTATOR_ROLLOUT_TIMEOUT": "30s", "TMI_REDIS_DB": "1"}
	o, err = loadOptions(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, "redis-password", o.Force)
	require.Equal(t, 30*time.Second, o.RolloutTimeout)
	require.Equal(t, 1, o.RedisDB)

	_, err = loadOptions(func(k string) string { return map[string]string{"TMI_ROTATOR_ROLLOUT_TIMEOUT": "soon"}[k] })
	require.Error(t, err)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestLoadOptions count1=true`
Expected: `undefined: loadOptions`.

- [ ] **Step 3: Implement `cmd/rotator/main.go`**

```go
// Command rotator rotates the high-value secrets in tmi-secrets (#965). It runs
// as a CronJob and exits non-zero when any rotation fails, leaving its phase
// annotation for the next run to resume.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/internal/slogging"
)

type options struct {
	Force                 string
	Namespace             string
	SecretName            string
	ServerDeployment      string
	RolloutTimeout        time.Duration
	SettingsPreviousGrace time.Duration
	RedisHost, RedisPort  string
	RedisDB               int
	RedisTLSEnabled       bool
	RedisTLSCAFile        string
	DatabaseURL           string
	OracleWallet          string
}

// SEM@<sha>: read rotator settings from the environment with defaults (pure)
func loadOptions(getenv func(string) string) (options, error) {
	get := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return def
	}
	o := options{
		Force:            getenv("ROTATE"),
		Namespace:        get("TMI_ROTATOR_NAMESPACE", "tmi-platform"),
		SecretName:       get("TMI_ROTATOR_SECRET", "tmi-secrets"),
		ServerDeployment: get("TMI_ROTATOR_SERVER_DEPLOYMENT", "tmi-server"),
		RedisHost:        get("TMI_REDIS_HOST", "redis"),
		RedisPort:        get("TMI_REDIS_PORT", "6379"),
		RedisTLSEnabled:  get("TMI_REDIS_TLS_ENABLED", "false") == "true",
		RedisTLSCAFile:   getenv("TMI_REDIS_TLS_CA_FILE"),
		DatabaseURL:      getenv("TMI_DATABASE_URL"),
		OracleWallet:     getenv("TMI_ORACLE_WALLET_LOCATION"),
	}
	var err error
	if o.RolloutTimeout, err = time.ParseDuration(get("TMI_ROTATOR_ROLLOUT_TIMEOUT", "10m")); err != nil {
		return o, fmt.Errorf("TMI_ROTATOR_ROLLOUT_TIMEOUT: %w", err)
	}
	if o.SettingsPreviousGrace, err = time.ParseDuration(get("TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE", "192h")); err != nil {
		return o, fmt.Errorf("TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE: %w", err)
	}
	if o.RedisDB, err = strconv.Atoi(get("TMI_REDIS_DB", "0")); err != nil {
		return o, fmt.Errorf("TMI_REDIS_DB: %w", err)
	}
	return o, nil
}

func main() {
	os.Exit(run())
}

// SEM@<sha>: wire cluster, Redis and DB clients and run every rotation; return the exit code
func run() int {
	logger := slogging.Get()
	o, err := loadOptions(os.Getenv)
	if err != nil {
		logger.Error("Invalid rotator configuration: %v", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*o.RolloutTimeout+5*time.Minute)
	defer cancel()

	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		logger.Error("Kubernetes client config: %v", err)
		return 2
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.Error("Kubernetes client: %v", err)
		return 2
	}
	secrets := rotator.NewKubeSecretStore(cs, o.Namespace)
	env := &rotator.Env{
		Secrets:          secrets,
		Rollouts:         rotator.NewKubeRolloutWaiter(cs, o.Namespace),
		SecretName:       o.SecretName,
		ServerDeployment: o.ServerDeployment,
		RolloutTimeout:   o.RolloutTimeout,
		Now:              time.Now,
	}

	// Redis: the password is whatever the Secret holds right now (never this
	// pod's env, which may predate a swap).
	sec, err := secrets.Get(ctx, o.SecretName)
	if err != nil {
		logger.Error("Read %s: %v", o.SecretName, err)
		return 2
	}
	redisDB, err := db.NewRedisDB(db.RedisConfig{
		Host: o.RedisHost, Port: o.RedisPort, DB: o.RedisDB,
		Password:   sec.Data[rotator.RedisPasswordKey],
		TLSEnabled: o.RedisTLSEnabled, TLSCAFile: o.RedisTLSCAFile,
	})
	if err != nil {
		logger.Error("Redis connection: %v", err)
		return 2
	}
	defer func() { _ = redisDB.Close() }()

	dbURL := o.DatabaseURL
	if dbURL == "" {
		dbURL = sec.Data["TMI_DATABASE_URL"]
	}
	gormCfg, err := db.ParseDatabaseURL(dbURL)
	if err != nil {
		logger.Error("Database URL: %v", err)
		return 2
	}
	if o.OracleWallet != "" {
		gormCfg.OracleWalletLocation = o.OracleWallet
	}
	gormDB, err := db.NewGormDB(*gormCfg)
	if err != nil {
		logger.Error("Database connection: %v", err)
		return 2
	}
	defer func() { _ = gormDB.Close() }()

	rotations := []rotator.Rotation{
		rotator.NewRedisPasswordRotation(rotator.NewGoRedisACL(redisDB.GetClient())),
		rotator.NewSettingsKeyRotation(rotator.NewGormSettingsStore(gormDB.DB(), redisDB), o.SettingsPreviousGrace),
	}
	if err := rotator.Run(ctx, env, rotations, o.Force); err != nil {
		logger.Error("Rotation run failed: %v", err)
		return 1
	}
	logger.Info("Rotation run complete")
	return 0
}
```

Check the exact field name for the wallet in `db.GormConfig` (`rg -n "Wallet" auth/db/gorm.go`) and the `Close` method names on `*db.RedisDB` / `*db.GormDB` (`rg -n "func (.*) Close" auth/db/redis.go auth/db/gorm.go`); adjust.

- [ ] **Step 4: Build wiring**

`scripts/build-server.py` COMPONENTS: add

```python
    "rotator": {
        "package": "github.com/ericfitz/tmi/cmd/rotator",
        "output": "bin/tmi-rotator",
        "description": "secret rotator (#965)",
    },
```

(match the exact dict shape of the neighbouring entries) and list it in the `--component` help text.

`Makefile` after `build-worker-probe`:

```make
build-rotator:  ## Build the tmi-rotator secret rotation binary (#965)
	@uv run scripts/build-server.py --component rotator
```

Add `build-rotator` to the `.PHONY` line above.

`Dockerfile.server`: after the `go build ... ./cmd/server` RUN, add a second build with the same flags:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -tags "${BUILD_TAGS}" -ldflags "-s -w" -trimpath -buildmode=exe -o tmi-rotator ./cmd/rotator
```

and after `COPY --from=builder /app/tmiserver /tmiserver` add `COPY --from=builder /app/tmi-rotator /tmi-rotator`. Same two edits in `Dockerfile.server-oracle` (keep its `CGO_ENABLED=1 ... -tags oracle` flags and `GOARCH=${TARGETARCH}`). The image ENTRYPOINT stays `/tmiserver`; the CronJob overrides `command`.

`internal/config/process_env.go`: append one `ProcessEnvVar` per row of the table above with `Binary: "rotator"` (e.g. `{Name: "TMI_ROTATOR_NAMESPACE", Binary: "rotator", Purpose: "Namespace of the Secret and Deployment the rotator manages (default tmi-platform)"}`; `ROTATE` gets `Purpose: "Force one rotation by name (redis-password, settings-key) for a manually created Job"`). Do not redeclare names already present (`TMI_REDIS_*`, `TMI_DATABASE_URL`, `TMI_ORACLE_WALLET_LOCATION`); the test rejects duplicates. Then `make generate-config-docs`.

- [ ] **Step 5: Verify**

Run: `make test-unit name=TestLoadOptions count1=true`, `make test-unit name=TestProcessEnvVars count1=true`, `make build-rotator`, `make build-server`, `make lint`, and `make build-server-container` (confirms the Dockerfile builds; then `docker run --rm --entrypoint /tmi-rotator tmi-server:dev` must fail fast with "Kubernetes client config", exit 2, not "not found").
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/rotator scripts/build-server.py Makefile Dockerfile.server Dockerfile.server-oracle internal/config/process_env.go config-reference.md
git commit -m "feat(rotator): tmi-rotator binary built into the server image (#965)"
```

---

### Task 10: Manifests, dev-cluster seeding, forced runs

**Files:**
- Create: `deployments/k8s/dev/rotator.yml`
- Modify: `deployments/k8s/dev/networkpolicy-redis.yml:26-42`, `deployments/k8s/dev/k3s/networkpolicy-k3s.yml:9-25`
- Modify: `deployments/k8s/dev/server.yml` (env), `deployments/k8s/dev/aws/patches/server-config.yaml` (env list is `$patch: replace`, so repeat the entries there)
- Modify: `deployments/k8s/dev/docker-desktop/kustomization.yaml`, `deployments/k8s/dev/k3s/kustomization.yaml`, `deployments/k8s/dev/aws/kustomization.yaml` (`resources: - ../rotator.yml`)
- Modify: `scripts/lib/deploy.py` (`ensure_redis_password_secret()` from PR 6 gains the settings key; `dev-down` keeps `tmi-secrets`), `scripts/lib/tests/test_deploy.py` (tests for the new seeding function; `make test-dev-scripts` runs them)
- Create: `deployments/k8s/dev/docker-desktop/patches/rotator-pullpolicy.yaml` (+ `docker-desktop/kustomization.yaml` patch entry)
- Create: `scripts/rotate-secret.py`; Modify: `Makefile` (`rotate-secret`)

- [ ] **Step 1: `deployments/k8s/dev/rotator.yml`**

```yaml
# tmi-rotator (#965): rotates TMI_REDIS_PASSWORD and the settings encryption
# key in Secret/tmi-secrets on a schedule, one idempotent phase per run, and
# relies on Stakater Reloader (PR 6) to roll tmi-server after each Secret write.
# Terraform seeds tmi-secrets and then ignores its data; this CronJob is the
# only writer. Force a run: `make rotate-secret name=redis-password`.
apiVersion: v1
kind: ServiceAccount
metadata:
  name: tmi-rotator
  namespace: tmi-platform
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: tmi-rotator
  namespace: tmi-platform
rules:
  # get/update on the two named Secrets only. No list/watch: those verbs
  # cannot be restricted by resourceName and would expose every Secret.
  - apiGroups: [""]
    resources: ["secrets"]
    resourceNames: ["tmi-secrets", "tmi-rotator-admin"]
    verbs: ["get", "update"]
  - apiGroups: ["apps"]
    resources: ["deployments"]
    resourceNames: ["tmi-server"]
    verbs: ["get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tmi-rotator
  namespace: tmi-platform
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: tmi-rotator
subjects:
  - kind: ServiceAccount
    name: tmi-rotator
    namespace: tmi-platform
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: tmi-rotator
  namespace: tmi-platform
spec:
  schedule: "17 3 * * *"
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 5
  jobTemplate:
    spec:
      backoffLimit: 0          # a failed phase is resumed tomorrow, not retried in a loop
      activeDeadlineSeconds: 2400
      template:
        metadata:
          labels: { app: tmi-rotator }
        spec:
          serviceAccountName: tmi-rotator
          restartPolicy: Never
          containers:
            - name: rotator
              image: localhost:5000/tmi-server:dev
              imagePullPolicy: Always
              command: ["/tmi-rotator"]
              env:
                - { name: TMI_ROTATOR_NAMESPACE, value: "tmi-platform" }
                - { name: TMI_REDIS_HOST, value: "redis" }
                - { name: TMI_REDIS_TLS_ENABLED, value: "true" }
                - { name: TMI_REDIS_TLS_CA_FILE, value: "/etc/tmi-redis-tls/ca.crt" }
                - { name: TMI_LOG_DIR, value: "/tmp/logs" }
              volumeMounts:
                - { name: redis-tls, mountPath: /etc/tmi-redis-tls, readOnly: true }
              resources:
                requests: { cpu: 50m, memory: 64Mi }
                limits: { cpu: 500m, memory: 256Mi }
          volumes:
            - name: redis-tls
              secret: { secretName: redis-tls }
```

The `resourceNames` on `deployments` with verb `get` is valid. The `images:` transformer of every overlay rewrites `localhost:5000/tmi-server` for CronJob pod templates too (kustomize handles `batch/v1 CronJob`): aws to `ECR_REGISTRY_PLACEHOLDER/...`, k3s to the tracked placeholder registry (`k3s-registry.invalid:30500`, #998) that `deploy.apply_overlay()` swaps for the real host from `.local/k3s.json` at apply time, docker-desktop to the bare `tmi-server` name.

docker-desktop imports images into the node by bare name, so `imagePullPolicy: Always` would make the Job pod `ImagePullBackOff` there (the server Deployment already needs `patches/server-pullpolicy.yaml` for the same reason). Add `deployments/k8s/dev/docker-desktop/patches/rotator-pullpolicy.yaml`:

```yaml
# Same reason as server-pullpolicy.yaml: the image is imported by bare name.
- op: replace
  path: /spec/jobTemplate/spec/template/spec/containers/0/imagePullPolicy
  value: IfNotPresent
```

and in `docker-desktop/kustomization.yaml` under `patches:`: `- path: patches/rotator-pullpolicy.yaml` with `target: { kind: CronJob, name: tmi-rotator }`. k3s and aws pull from a registry and keep `Always`.

- [ ] **Step 2: Network policies**

`networkpolicy-redis.yml` ingress `from:` gets a second selector:

```yaml
        - podSelector:
            matchLabels:
              app: tmi-rotator
```

Same addition in `k3s/networkpolicy-k3s.yml` `postgres-allow-tmi-server` (the rotator re-encrypts in-process). docker-desktop has no Postgres policy. On AWS, RDS is outside the cluster and the security group already admits the node group (verify: `rg -n "rds_security_group|ingress" terraform/modules/network/aws/main.tf`).

- [ ] **Step 3: Server env**

`deployments/k8s/dev/server.yml` env, after the PR 6 `TMI_REDIS_PASSWORD` entry:

```yaml
            # Settings-at-rest encryption keyring (#547, rotated by tmi-rotator #965).
            # Only TMI_SECRET_SETTINGS_ENCRYPTION_KEY is required: the id defaults to 1
            # and the previous pair exists only mid-rotation.
            - name: TMI_SECRET_SETTINGS_ENCRYPTION_KEY
              valueFrom: { secretKeyRef: { name: tmi-secrets, key: TMI_SECRET_SETTINGS_ENCRYPTION_KEY } }
            # optional: an existing AWS Secret never gains this key (Terraform
            # ignores data after the first apply) and a missing id means 1.
            - name: TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID
              valueFrom: { secretKeyRef: { name: tmi-secrets, key: TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID, optional: true } }
            - name: TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY
              valueFrom: { secretKeyRef: { name: tmi-secrets, key: TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY, optional: true } }
            - name: TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID
              valueFrom: { secretKeyRef: { name: tmi-secrets, key: TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID, optional: true } }
```

Same four entries in `aws/patches/server-config.yaml` (replace its single `TMI_SECRET_SETTINGS_ENCRYPTION_KEY` entry), and in `server-oracle.yml` (keep-in-sync contract). Add `../rotator.yml` to the three overlays' `resources:`.

- [ ] **Step 4: `scripts/lib/deploy.py`**

Extend PR 6's `ensure_redis_password_secret()` (rename to `ensure_tmi_secrets()` if that reads better; update its callers) so that after ensuring the Secret exists it patches in any missing keys without touching present ones:

```python
def _secret_has_key(name: str, key: str) -> bool:
    out = kubectl(["-n", NS, "get", "secret", name, "-o", f"jsonpath={{.data.{key}}}"], capture=True).stdout
    return bool(out.strip())


def ensure_settings_key_seeded() -> None:
    """Seed the settings-encryption keyring (id 1) into tmi-secrets on a dev
    cluster if absent. Values are written from umask-077 files via
    --patch-file; nothing is printed. tmi-rotator owns every later change."""
    if _secret_has_key("tmi-secrets", "TMI_SECRET_SETTINGS_ENCRYPTION_KEY"):
        return
    old = os.umask(0o077)
    try:
        with tempfile.TemporaryDirectory() as tmp:
            patch = Path(tmp) / "patch.json"
            key_b64 = base64.b64encode(secrets.token_hex(32).encode()).decode()
            id_b64 = base64.b64encode(b"1").decode()
            patch.write_text(json.dumps({"data": {
                "TMI_SECRET_SETTINGS_ENCRYPTION_KEY": key_b64,
                "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID": id_b64}}))
            kubectl(["-n", NS, "patch", "secret", "tmi-secrets", "--type=merge", f"--patch-file={patch}"], capture=True)
    finally:
        os.umask(old)
    log_success("Secret/tmi-secrets seeded with a settings encryption key (id 1)")
```

Call it from both `start()` and `restart()` right after their `ensure_redis_password_secret()` calls (PR 6 as merged has `restart()` re-run `ensure_redis_password_secret()` and `apply_platform_base()` too). Imports needed at the top of `deploy.py`: `base64` and `json` (`secrets` and `tempfile` are already imported). In `teardown()` (the `dev-down` cleanup), `tmi-secrets` must NOT be deleted while the Postgres PVC survives (encrypted rows would become unreadable), and the `redis-data` PVC (Task 5) is not in that list either, so the `ENC:` sessions it holds stay readable; add a comment next to the kept `tmi-oauth-providers` explaining both. `dev-nuke` deletes the namespace and the PVC together, which is consistent.

The server now reads the settings key from env, which beats `config-development.yml`; no change to that file.

- [ ] **Step 5: `scripts/rotate-secret.py` and `make rotate-secret`**

```python
#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.11"
# ///
"""Force one tmi-rotator rotation now (#965).

`kubectl create job --from=cronjob/...` cannot set env, so this renders the
CronJob's job template with --dry-run, injects ROTATE=<name>, applies it and
follows the logs. Usage: make rotate-secret name=redis-password
"""
import json
import subprocess
import sys
import time

NS = "tmi-platform"
VALID = {"redis-password", "settings-key"}


def main() -> int:
    if len(sys.argv) != 2 or sys.argv[1] not in VALID:
        print(f"usage: rotate-secret.py <{'|'.join(sorted(VALID))}>", file=sys.stderr)
        return 2
    name = sys.argv[1]
    job_name = f"tmi-rotator-{name}-{int(time.time())}"
    raw = subprocess.run(["kubectl", "-n", NS, "create", "job", job_name, "--from=cronjob/tmi-rotator",
                          "--dry-run=client", "-o", "json"], check=True, capture_output=True, text=True).stdout
    job = json.loads(raw)
    container = job["spec"]["template"]["spec"]["containers"][0]
    container.setdefault("env", []).append({"name": "ROTATE", "value": name})
    subprocess.run(["kubectl", "-n", NS, "apply", "-f", "-"], input=json.dumps(job), check=True, text=True)
    subprocess.run(["kubectl", "-n", NS, "wait", "--for=condition=complete", f"job/{job_name}", "--timeout=40m"], check=False)
    subprocess.run(["kubectl", "-n", NS, "logs", f"job/{job_name}"], check=False)
    return 0


if __name__ == "__main__":
    sys.exit(main())
```

Makefile (near `dev-status`):

```make
rotate-secret:  ## Force one tmi-rotator rotation now: make rotate-secret name=redis-password|settings-key
	@uv run scripts/rotate-secret.py $(name)
```

- [ ] **Step 6: Verify renders**

Run: `kubectl kustomize deployments/k8s/dev/docker-desktop | rg -n "tmi-rotator|CronJob|SETTINGS_ENCRYPTION" | head`, same for `k3s` and `aws` (both renders still carry their placeholder registries, `k3s-registry.invalid` and `ECR_REGISTRY_PLACEHOLDER`; that is fine, the deploy scripts substitute them). The docker-desktop render must show `imagePullPolicy: IfNotPresent` on the CronJob. `make lint` (which since #998 runs `ruff --select F` over `scripts/`, so `rotate-secret.py` must pass it) and `make test-dev-scripts`. `kubectl kustomize deployments/k8s/dev/aws | kubectl apply --dry-run=client -f -` must parse.
Expected: CronJob, Role, RoleBinding, ServiceAccount present in all three; the server env shows the four keyring entries, three of them `optional: true` (only the key itself is required).

- [ ] **Step 7: Commit**

```bash
git add deployments/k8s scripts/lib/deploy.py scripts/lib/tests/test_deploy.py scripts/rotate-secret.py Makefile
git commit -m "feat(deploy): tmi-rotator CronJob, RBAC, network policy and settings-key seeding (#965)"
```

---
### Task 11: Terraform hand-off, `deploy-aws.sh import_config`, CloudWatch alarm

**Files:**
- Modify: `terraform/modules/kubernetes/aws/k8s_resources.tf:207-240` (`kubernetes_secret_v1.tmi`)
- Modify: `terraform/modules/secrets/aws/main.tf`, `terraform/modules/secrets/aws/outputs.tf`
- Modify: `terraform/environments/aws-public/main.tf` (metric filter + alarm after `module "logging"`)
- Modify: `scripts/deploy-aws.sh:1078-1170` (`import_config`)
- Modify: `deployments/k8s/dev/aws/README.md` (secrets section: tmi-secrets is rotator-owned after the first apply)

No tests; verification is `terraform validate`/`plan` and a shell dry run.

- [ ] **Step 1: `kubernetes_secret_v1.tmi`**

```hcl
  data = {
    TMI_DATABASE_URL   = "postgresql://${var.db_username}:${urlencode(var.db_password)}@${var.db_host}:${var.db_port}/${var.db_name}?sslmode=require"
    TMI_JWT_SECRET     = var.jwt_secret
    TMI_REDIS_PASSWORD = var.redis_password
    TMI_SECRET_SETTINGS_ENCRYPTION_KEY = var.settings_encryption_key
    # No TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID seed: with ignore_changes
    # below an existing Secret would never receive it anyway, and a missing
    # id means 1 (internal/crypto, internal/rotator). The rotator writes it.
  }

  # #965: Terraform SEEDS these values on the first apply and never writes
  # them again. Secret/tmi-secrets is owned by the tmi-rotator CronJob
  # (deployments/k8s/dev/rotator.yml), which rewrites the data and records
  # its progress in annotations. Without ignore_changes every apply would
  # reset a rotated value to the Terraform seed and lock the server out of
  # Redis (or make every encrypted setting unreadable).
  lifecycle {
    ignore_changes = [data, metadata[0].annotations]
  }
```

Replace the long `TMI_SECRET_SETTINGS_ENCRYPTION_KEY` comment with a two-line pointer to the keyring env contract in `deployments/k8s/dev/server.yml`.

- [ ] **Step 2: Secrets Manager**

In `terraform/modules/secrets/aws/main.tf` delete `aws_secretsmanager_secret.redis_password`, `aws_secretsmanager_secret_version.redis_password`, `aws_secretsmanager_secret.settings_encryption_key` and its `_version`. Keep `random_password.redis_password` and `random_id.settings_encryption_key` (still the seed). In `outputs.tf` delete `redis_password_secret_arn/_name`, `settings_encryption_key_arn/_name` and the two map entries in `secret_arns`. `rg -n "redis_password_secret|settings_encryption_key_arn|settings_encryption_key_name" terraform/` must return nothing afterwards. The IRSA policy (`terraform/modules/kubernetes/aws/main.tf:695`) still has two ARNs (db, jwt) so `Resource` is non-empty.

Run: `cd terraform/environments/aws-public && AWS_PROFILE=tmi terraform init -backend=false && terraform validate` and, if a backend is configured on this machine, `AWS_PROFILE=tmi terraform plan` (read it: expected changes are two Secrets Manager secrets destroyed, `tmi-secrets` shows no data diff, nothing else). Do not apply; the AWS deploy waits for #968/#972 and PRs 2-3 (spec §5).

- [ ] **Step 3: `deploy-aws.sh import_config`**

Replace the Secrets Manager settings-key block with the `file` provider (Task 2). After the `dbtool-connect.yaml` heredoc's `database:` section, the `secrets:` block becomes:

```yaml
secrets:
  provider: "file"
  file_dir: "${tmp_dir}/keyring"
```

and before writing the YAML, materialise the keyring from the cluster under `umask 077`:

```bash
    # Settings keyring from tmi-secrets (#965): one file per secret key, the
    # names internal/secrets/provider.go SecretKeys uses. The previous pair is
    # only present mid-rotation; a missing key writes an empty file that the
    # provider treats as not found. Values go straight from kubectl to disk.
    mkdir -p "${tmp_dir}/keyring"
    local k
    for k in KEY CONTEXT_ID PREVIOUS_KEY PREVIOUS_CONTEXT_ID; do
        local lower
        lower="settings_encryption_$(echo "$k" | tr '[:upper:]' '[:lower:]')"
        kubectl -n "${NAMESPACE}" get secret tmi-secrets \
            -o "go-template={{index .data \"TMI_SECRET_SETTINGS_ENCRYPTION_${k}\" | base64decode}}" \
            > "${tmp_dir}/keyring/${lower}" 2>/dev/null || : > "${tmp_dir}/keyring/${lower}"
        [[ -s "${tmp_dir}/keyring/${lower}" ]] || rm -f "${tmp_dir}/keyring/${lower}"
    done
```

(`go-template` with a missing key prints `<no value>`; guard: if the file content is literally `<no value>`, remove it. Test the template against a Secret lacking the key before relying on it.) `mktemp -d` already runs inside `umask 077` in this function? It does not: wrap the `mkdir`/loop in the same `( umask 077; ... )` subshell pattern the script uses for the YAML, or set `umask 077` at the top of `import_config`. The DB credential fetch stays on Secrets Manager until PR 3.

Dry-run: `bash -n scripts/deploy-aws.sh`; `shellcheck scripts/deploy-aws.sh` if installed.

- [ ] **Step 4: CloudWatch metric filter and alarm** (`terraform/environments/aws-public/main.tf`, after `module "logging"`)

```hcl
# #965 / #968: the rotator logs one JSON line per secret per run with its age.
# A 100-day lookback is impossible in one alarm (period x evaluation periods
# is capped at one day), so the AGE is the metric: Maximum over a day > 100
# means a secret is stale, and missing data (no rotator run at all) also alarms.
data "aws_sns_topic" "security_alerts" {
  name = "tmi-security-alerts" # owned by terraform/environments/aws-persistent
}

resource "aws_cloudwatch_log_metric_filter" "secret_age" {
  name           = "tmi-secret-age-days"
  log_group_name = module.logging.log_group_name
  pattern        = "{ $.msg = \"rotation status\" && $.age_days >= 0 }"

  metric_transformation {
    name      = "SecretAgeDays"
    namespace = "TMI/Rotator"
    value     = "$.age_days"
    unit      = "Count"
  }
}

resource "aws_cloudwatch_metric_alarm" "secret_rotation_stale" {
  alarm_name          = "tmi-secret-rotation-stale"
  alarm_description   = "No successful rotation of a tmi-secrets value in 100 days, or the tmi-rotator CronJob stopped reporting"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = aws_cloudwatch_log_metric_filter.secret_age.metric_transformation[0].name
  namespace           = "TMI/Rotator"
  period              = 86400
  statistic           = "Maximum"
  threshold           = 100
  treat_missing_data  = "breaching"
  alarm_actions       = [data.aws_sns_topic.security_alerts.arn]
  ok_actions          = [data.aws_sns_topic.security_alerts.arn]
  tags                = local.common_tags
}
```

Verified: the Fluent Bit config in `terraform/modules/logging/aws/main.tf:190` has `Merge_Log On`, so the container's JSON line is merged into top-level fields and `$.msg` / `$.age_days` match (slog's default message key is `msg`; `internal/slogging/logger.go:240` `ReplaceAttr` does not rename it). Confirm the field names by running `make build-rotator` and `LOG_LEVEL=info ./bin/tmi-rotator` (it exits 2 before logging status; instead check `internal/slogging/logger.go:258` `JSONHandler` and its `msg` key with `rg -n "MessageKey|\"msg\"" internal/slogging/logger.go`); adjust `$.msg` if the key differs. Age `-1` (never rotated) is excluded by `>= 0` so a fresh cluster does not alarm on day 1; the first run rotates anyway.

- [ ] **Step 5: Commit**

```bash
git add terraform scripts/deploy-aws.sh deployments/k8s/dev/aws/README.md
git commit -m "feat(terraform): hand tmi-secrets to the rotator, drop redis/settings Secrets Manager copies, alarm on stale secrets (#965)"
```

---

### Task 12: Integration tests and harness settings key

**Files:**
- Modify: `scripts/run-integration-tests.py:330` (`start_test_server_container`; the harness NATS is `tmi-nats-itest` on 4223 and `ensure_redis` rejects a stale plaintext container, neither affects this task) and `test/integration/framework` env plumbing
- Create: `test/integration/workflows/secret_rotation_test.go`
- Modify: `test/integration/go.mod` (`go mod tidy` in `test/integration` if a new package is imported; memory: a stale module silently skips tests)

What is testable here (Docker-run server, no Kubernetes): the Redis password rotation end to end against the harness Redis with a `MemorySecretStore` and `FakeRolloutWaiter`, while a background loop hits the API; and `POST /admin/settings/reencrypt` while the loop runs. The stage/promote server rolls cannot happen in this harness; Task 14 covers them on the k3s cluster (spec §5).

- [ ] **Step 1: Harness gets a settings key**

In `start_test_server_container` add, next to the PR 6 Redis entries, `"-e", "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID=1"` and deliver the key itself via the PR 6 `--env-file` (`.local/test-tls/secrets.env`): extend PR 6's `tlsgen` to also write `TMI_SECRET_SETTINGS_ENCRYPTION_KEY=<64 hex>` into `secrets.env` (never argv). Without a key `POST /admin/settings/reencrypt` returns 409 and the settings-encryption paths are untested. Verify with `rg -n "SETTINGS_ENCRYPTION" scripts/run-integration-tests.py test/integration/tlsgen`.

- [ ] **Step 2: Write the test**

```go
package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/test/integration/framework"
)

// apiLoop calls GET /me every 50ms until stop is closed, counting non-2xx responses.
func apiLoop(t *testing.T, client *framework.IntegrationClient, stop <-chan struct{}) *int32 {
	t.Helper()
	var bad int32
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
			if err != nil || resp.StatusCode >= 500 || resp.StatusCode == http.StatusUnauthorized {
				atomic.AddInt32(&bad, 1)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return &bad
}

func TestSecretRotationIntegration_RedisPassword(t *testing.T) {
	serverURL := os.Getenv("TMI_SERVER_URL")
	framework.RequireIntegration(t) // whatever guard the sibling tests use; copy from settings_crud_test.go
	tokens, err := framework.AuthenticateUser("alice")
	framework.AssertNoError(t, err, "authenticate")
	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "client")

	opts, err := framework.RedisOptions() // PR 6 helper: host/port/db/password/TLS of the harness Redis
	framework.AssertNoError(t, err, "redis options")
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	oldPassword := opts.Password

	st := rotator.NewMemorySecretStore(&rotator.Secret{Name: "tmi-secrets", Data: map[string]string{"TMI_REDIS_PASSWORD": oldPassword}, Annotations: map[string]string{}})
	env := &rotator.Env{Secrets: st, Rollouts: rotator.NewFakeRolloutWaiter(st, "tmi-server"), SecretName: "tmi-secrets", ServerDeployment: "tmi-server", RolloutTimeout: time.Second, Now: time.Now}

	// Registered BEFORE the rotation: a failure mid-way must not leave the
	// harness Redis on a password the rest of the suite does not know.
	t.Cleanup(func() {
		s, _ := st.Get(context.Background(), "tmi-secrets")
		if pw := s.Data["TMI_REDIS_PASSWORD"]; pw != oldPassword {
			c := *opts
			c.Password = pw
			acl := rotator.NewGoRedisACL(redis.NewClient(&c))
			_ = acl.AddPassword(context.Background(), oldPassword)
			_ = acl.RemovePasswordHash(context.Background(), sha256Hex(pw))
		}
	})

	stop := make(chan struct{})
	bad := apiLoop(t, client, stop)
	rot := rotator.NewRedisPasswordRotation(rotator.NewGoRedisACL(rdb))
	err = rot.Run(context.Background(), env)
	close(stop)
	framework.AssertNoError(t, err, "rotation")
	if n := atomic.LoadInt32(bad); n != 0 {
		t.Fatalf("%d failed API calls during Redis password rotation", n)
	}

	s, _ := st.Get(context.Background(), "tmi-secrets")
	newPassword := s.Data["TMI_REDIS_PASSWORD"]
	newOpts := *opts
	newOpts.Password = newPassword
	if err := redis.NewClient(&newOpts).Ping(context.Background()).Err(); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
	oldOpts := *opts
	if err := redis.NewClient(&oldOpts).Ping(context.Background()).Err(); err == nil {
		t.Fatal("old password still accepted after rotation")
	}
	// The server's pooled connections stay authenticated; a fresh call still works.
	resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
	framework.AssertNoError(t, err, "post-rotation call")
	framework.AssertStatusOK(t, resp)

}

func sha256Hex(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestSecretRotationIntegration_ReencryptUnderLoad(t *testing.T) {
	serverURL := os.Getenv("TMI_SERVER_URL")
	admin, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "admin")
	adminClient, err := framework.NewClient(serverURL, admin)
	framework.AssertNoError(t, err, "admin client")
	user, _ := framework.AuthenticateUser("bob")
	userClient, _ := framework.NewClient(serverURL, user)

	stop := make(chan struct{})
	bad := apiLoop(t, userClient, stop)
	var last *framework.Response
	for i := 0; i < 3; i++ { // idempotent: a second and third pass are no-ops
		last, err = adminClient.Do(framework.Request{Method: http.MethodPost, Path: "/admin/settings/reencrypt"})
		framework.AssertNoError(t, err, fmt.Sprintf("reencrypt %d", i))
		framework.AssertStatusOK(t, last)
	}
	close(stop)
	if n := atomic.LoadInt32(bad); n != 0 {
		t.Fatalf("%d failed API calls during re-encryption", n)
	}
}
```

Adapt helper names to what `test/integration/framework` actually exports (`rg -n "^func " test/integration/framework/*.go`); the response type and the integration-guard idiom must match `settings_crud_test.go`. The test name contains `Integration` (runner selects `-run Integration`).

- [ ] **Step 3: Run**

Run: `cd test/integration && go mod tidy` (allowed: module maintenance, not a test run), then `make test-integration`.
Expected: both new tests pass; `SUMMARY` shows them executed (not skipped), zero failures.

- [ ] **Step 4: Commit**

```bash
git add scripts/run-integration-tests.py test/integration
git commit -m "test(integration): Redis password rotation and re-encryption under API load (#965)"
```

---

### Task 13: Oracle compatibility review

- [ ] **Step 1:** Invoke the `oracle-db-admin` skill with the diff of `api/settings_service.go`, `internal/rotator/settings_key.go`, and the notes from Task 7 (CLOB `LIKE`, no CLOB `=`, `NOT IN` cap, per-row `FOR UPDATE` transactions replacing the single SERIALIZABLE pass, the ORA-08177 history in memory `project_oracle_false_08177_recursive_txn`: per-row READ COMMITTED-style short transactions are the pattern that memory recommends).
- [ ] **Step 2:** Address the verdict: `APPROVED` -> note in the PR; `APPROVED WITH NOTES` -> fix easy items now, file follow-ups; `BLOCKING ISSUES` -> fix all or get Eric's explicit waiver.
- [ ] **Step 3:** `make test-integration-oci` if `scripts/oci-env.sh` is available (start tmiadb first: memory `project_oracle_adb_search_state_stale`).
- [ ] **Step 4:** Commit any fixes: `git commit -m "fix(settings): Oracle review follow-ups for resumable re-encryption (#965)"`.

---

### Task 14: Cluster verification on the k3s cluster, runbook, PR

- [ ] **Step 1: docker-desktop smoke**

`make dev-up`, then `make rotate-secret name=redis-password`. Expected in the job log: `Redis accepts the new password`, `Rotation phase recorded ... "swapped"`, a `tmi-server` roll (`kubectl -n tmi-platform get pods -w`), `Old Redis password retired`, exit 0. Then `curl -s localhost:8080/` is 200 and a login via the OAuth stub still works with the pre-rotation refresh token (sessions survive: `curl -X POST http://localhost:8079/refresh ...`).

- [ ] **Step 2: Reloader behaviour check (assumption from the header)**

`kubectl -n tmi-platform annotate secret tmi-secrets tmi.dev/probe=1 --overwrite` must NOT roll `tmi-server` (watch `kubectl get deploy tmi-server -o jsonpath='{.metadata.generation}'` for 60s). Remove the annotation. If it does roll, the Redis rotation still works but costs one extra roll; record it in the PR and keep going.

- [ ] **Step 3: k3s full pass**

`CLUSTER=k3s make dev-up` (needs `.local/k3s.json`, #998); `make rotate-secret name=settings-key` and watch two rolls; `kubectl -n tmi-platform get secret tmi-secrets -o jsonpath='{.metadata.annotations}'` shows `rotation-phase.settings-key: reencrypted`, `promoted-at`, `rotated-at`. `make test-integration` against the cluster passes. Then set `TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE=1s` on the CronJob (temporary `kubectl set env cronjob/tmi-rotator ...`), force `settings-key` again: expected `Settings previous key dropped id=1`, one roll, previous pair gone, server healthy, `GET /admin/settings` (admin) still decrypts every secret-classified setting. Revert the env.
- Forced run overlapping the nightly: start `make rotate-secret name=redis-password` twice in two terminals; one must exit non-zero with `another rotator run is active?`, the other completes; the Secret is consistent.
- Redis restart mid-rotation: force `redis-password`, and while `tmi-server` is rolling, `kubectl -n tmi-platform rollout restart deploy/redis`; the run may fail on the rollout wait; the next forced run resumes from `swapped`, re-adds the password and completes. Sessions created before the restart still refresh (`POST /refresh` on the OAuth stub returns 200): the AOF from Task 5 replayed them.

- [ ] **Step 4: Runbook**

In the wiki repo (`/Users/efitz/Projects/tmi.wiki`, per `.local/repos.json`) add `Secret-Rotation.md` and link it from `Security-Operations.md`: what rotates and how often (annotation table), forcing (`make rotate-secret`), reading phases, recovering a stuck phase (fix the cause, re-run; to abandon a Redis rotation in `swapped`: nothing to undo, the Secret already holds the working password), the grace period and when to raise it, the CloudWatch alarm, and a placeholder section for PR 2/PR 3 (JWT keyring, DB users) and the PR 6 CA rotation (trust old+new CA, reissue leaves, drop old). Commit and push the wiki separately.

- [ ] **Step 5: Gates**

`make lint`, `make build-server`, `make test-unit`, `make test-integration`, `make validate-openapi` (schema changed in Task 7). Then the `security-review` skill on the branch; stop and report if it finds issues.

- [ ] **Step 6: PR**

`git push -u origin feat/965-secret-rotation`; `gh pr create --title "feat(rotator): scheduled rotation of the Redis password and settings key (#965)" --body-file <file>` whose body lists: the phase tables, the Open questions above (marked resolved or still open), the Oracle verdict, the k3s verification results, and "Refs #965" (the issue closes after PR 3). End the body with the attribution trailer from the session reminder.

## Self-review notes (writer)

- Spec coverage: §1 rotator shape (Tasks 3, 4, 9, 10, 11), §2 Redis (Task 6; persistence Task 5, decision B), settings key ids (Task 1), keyring rollout (Task 8), resumable ReEncryptAll (Task 7), Terraform hand-off and `import_config` (Task 11), CloudWatch (Task 11), §5 unit/integration/cluster/runbook (Tasks 12, 14).
- Review Focus 5 (`TestDecrypt_UnknownIDAfterDrop`) lives in Task 1, not Task 7, and Review Focus 3 in Task 7.
- Renumbered 2026-09-28: Redis persistence inserted as Task 5; former Tasks 5-13 are now 6-14. Every `Task N` reference was re-checked by content.
