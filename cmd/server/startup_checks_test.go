package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---

// capturedLog records a single log call.
type capturedLog struct {
	level string // "DEBUG", "INFO", "WARN", "ERROR"
	msg   string
}

// testLogger captures log calls for assertion.
type testLogger struct {
	mu   sync.Mutex
	logs []capturedLog
}

func (l *testLogger) Debug(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, capturedLog{level: "DEBUG", msg: fmt.Sprintf(format, args...)})
}

func (l *testLogger) Info(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, capturedLog{level: "INFO", msg: fmt.Sprintf(format, args...)})
}

func (l *testLogger) Warn(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, capturedLog{level: "WARN", msg: fmt.Sprintf(format, args...)})
}

func (l *testLogger) Error(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, capturedLog{level: "ERROR", msg: fmt.Sprintf(format, args...)})
}

// logsAtLevel returns all log messages at the given level.
func (l *testLogger) logsAtLevel(level string) []capturedLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []capturedLog
	for _, entry := range l.logs {
		if entry.level == level {
			out = append(out, entry)
		}
	}
	return out
}

// allLogs returns a copy of all captured log entries.
func (l *testLogger) allLogs() []capturedLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]capturedLog, len(l.logs))
	copy(out, l.logs)
	return out
}

// mockSecretKeyGetter is a simple in-memory implementation of secretKeyGetter.
type mockSecretKeyGetter struct {
	// store maps key → value; absent key means "not found in DB".
	store map[string]string
	// errKeys maps key → error to return for that key.
	errKeys map[string]error
	// origins maps key → Origin value (models.SystemSettingOriginSeeded or
	// models.SystemSettingOriginExplicit). Absent means NULL, which
	// SystemSetting.IsExplicit() treats as NOT explicit (fail-safe).
	origins map[string]string
}

func newMockSecretKeyGetter(values map[string]string) *mockSecretKeyGetter {
	return &mockSecretKeyGetter{
		store:   values,
		errKeys: make(map[string]error),
		origins: make(map[string]string),
	}
}

func (m *mockSecretKeyGetter) Get(_ context.Context, key string) (*models.SystemSetting, error) {
	if err, ok := m.errKeys[key]; ok {
		return nil, err
	}
	val, ok := m.store[key]
	if !ok {
		return nil, nil // not found
	}
	setting := &models.SystemSetting{SettingKey: models.DBVarchar(key), Value: models.DBText(val)}
	if origin, ok := m.origins[key]; ok {
		setting.Origin = models.NullableDBVarchar{String: origin, Valid: true}
	}
	return setting, nil
}

// listErr, when set, makes List fail — the divergence check must then skip
// silently rather than abort startup.
func (m *mockSecretKeyGetter) List(_ context.Context) ([]models.SystemSetting, error) {
	if err, ok := m.errKeys[listErrSentinelKey]; ok {
		return nil, err
	}
	out := make([]models.SystemSetting, 0, len(m.store))
	for key, val := range m.store {
		s := models.SystemSetting{SettingKey: models.DBVarchar(key), Value: models.DBText(val)}
		if origin, ok := m.origins[key]; ok {
			s.Origin = models.NullableDBVarchar{String: origin, Valid: true}
		}
		out = append(out, s)
	}
	return out, nil
}

// listErrSentinelKey is a reserved errKeys entry meaning "fail the whole List
// call" rather than one key, since List has no per-key error path.
const listErrSentinelKey = "\x00list"

// minimalConfigWithBuildMode builds the smallest *config.Config that produces
// a known set of Secret-classified keys and the desired build mode.
// We use auth.jwt.secret and database.redis.password as two well-known Secret keys.
func minimalConfigWithBuildMode(buildMode string) *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			BuildMode: buildMode,
			JWT: config.JWTConfig{
				Secret:            "supersecret",
				ExpirationSeconds: 3600,
				SigningMethod:     "HS256",
			},
		},
		Database: config.DatabaseConfig{
			URL: "postgres://localhost/tmi",
			Redis: config.RedisConfig{
				Host:     "localhost",
				Port:     "6379",
				Password: "redispass",
			},
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			LogDir: "logs",
		},
		Secrets: config.SecretsConfig{
			Provider:   "env",
			VaultToken: "vaulttoken",
		},
	}
}

// --- helpers ---

// disabledEncryptor returns a *crypto.SettingsEncryptor with IsEnabled() == false
// by creating one without a valid key (the constructor returns a disabled one
// when no key is found, but for tests we use NewSettingsEncryptorFromKeys with a
// nil key path). Since we cannot construct a disabled encryptor via the public API
// without a secrets provider, we pass nil instead — the function treats nil the
// same as disabled.
func disabledEncryptor() *crypto.SettingsEncryptor {
	return nil // nil encryptor ⟹ IsEnabled() short-circuit triggers warn path
}

// enabledEncryptor returns a properly initialised encryptor (encryption ON).
func enabledEncryptor(t *testing.T) *crypto.SettingsEncryptor {
	t.Helper()
	key := make([]byte, 32) // all-zero key — valid for tests
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	require.NoError(t, err)
	return enc
}

// --- tests ---

// TestWarnIfPlaintextSecretsAtRest_DevBuildPlaintextSecrets verifies that when:
//   - encryption is OFF (disabled encryptor)
//   - at least one Secret-classified key has a non-empty value in the DB
//   - build mode is "dev"
//
// the function emits exactly ONE WARN-level log naming the condition and remediation,
// and does not emit any ERROR-level log.
func TestWarnIfPlaintextSecretsAtRest_DevBuildPlaintextSecrets(t *testing.T) {
	cfg := minimalConfigWithBuildMode("dev")
	secretKeys := secretClassifiedKeys(cfg)
	require.NotEmpty(t, secretKeys, "test requires at least one Secret-classified key")

	// Put a non-empty value in DB for the first secret key.
	store := map[string]string{
		secretKeys[0]: "some-plaintext-value",
	}

	log := &testLogger{}
	svc := newMockSecretKeyGetter(store)

	warnIfPlaintextSecretsAtRest(context.Background(), disabledEncryptor(), svc, cfg, log)

	warns := log.logsAtLevel("WARN")
	errors := log.logsAtLevel("ERROR")

	assert.Len(t, warns, 1, "expected exactly one WARN log entry")
	assert.Empty(t, errors, "expected no ERROR log entries in dev build")

	if len(warns) == 1 {
		msg := warns[0].msg
		assert.Contains(t, msg, "SECURITY WARNING", "warn message should name the condition")
		assert.Contains(t, msg, "TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "warn message should name the remediation env var")
		assert.Contains(t, msg, "/admin/settings/reencrypt", "warn message should name the remediation endpoint")
		assert.Contains(t, msg, secretKeys[0], "warn message should name the affected key")
	}
}

// TestWarnIfPlaintextSecretsAtRest_ProductionBuildPlaintextSecrets verifies that when:
//   - encryption is OFF
//   - at least one Secret-classified key has a non-empty value in the DB
//   - build mode is "production"
//
// the function emits exactly ONE ERROR-level log and no WARN-level log,
// and the function returns normally (server does not abort).
func TestWarnIfPlaintextSecretsAtRest_ProductionBuildPlaintextSecrets(t *testing.T) {
	cfg := minimalConfigWithBuildMode("production")
	secretKeys := secretClassifiedKeys(cfg)
	require.NotEmpty(t, secretKeys)

	store := map[string]string{
		secretKeys[0]: "some-plaintext-value",
	}

	log := &testLogger{}
	svc := newMockSecretKeyGetter(store)

	// Function must return normally — production build must NOT fail to start.
	warnIfPlaintextSecretsAtRest(context.Background(), disabledEncryptor(), svc, cfg, log)

	warns := log.logsAtLevel("WARN")
	errors := log.logsAtLevel("ERROR")

	assert.Empty(t, warns, "expected no WARN logs in production build (uses ERROR instead)")
	assert.Len(t, errors, 1, "expected exactly one ERROR log entry in production build")

	if len(errors) == 1 {
		msg := errors[0].msg
		assert.Contains(t, msg, "SECURITY WARNING", "error message should name the condition")
		assert.Contains(t, msg, "TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "error message should name the remediation env var")
		assert.Contains(t, msg, "/admin/settings/reencrypt", "error message should name the remediation endpoint")
		assert.Contains(t, msg, secretKeys[0], "error message should name the affected key")
	}
}

// TestWarnIfPlaintextSecretsAtRest_EncryptionEnabled verifies that when encryption
// is ON, no warning is emitted regardless of what the DB contains.
func TestWarnIfPlaintextSecretsAtRest_EncryptionEnabled(t *testing.T) {
	cfg := minimalConfigWithBuildMode("production") // worst case
	secretKeys := secretClassifiedKeys(cfg)
	require.NotEmpty(t, secretKeys)

	// All secret keys have values — but encryption IS enabled.
	store := make(map[string]string)
	for _, k := range secretKeys {
		store[k] = "some-value"
	}

	log := &testLogger{}
	svc := newMockSecretKeyGetter(store)
	enc := enabledEncryptor(t)

	warnIfPlaintextSecretsAtRest(context.Background(), enc, svc, cfg, log)

	all := log.allLogs()
	assert.Empty(t, all, "expected no log output when encryption is enabled")
}

// TestWarnIfPlaintextSecretsAtRest_NoSecretsStored verifies that when encryption is
// OFF but no Secret-classified key has a value in the DB, no warning is emitted.
func TestWarnIfPlaintextSecretsAtRest_NoSecretsStored(t *testing.T) {
	cfg := minimalConfigWithBuildMode("production") // worst case build mode

	// Empty store — no secret keys have values.
	log := &testLogger{}
	svc := newMockSecretKeyGetter(map[string]string{})

	warnIfPlaintextSecretsAtRest(context.Background(), disabledEncryptor(), svc, cfg, log)

	warns := log.logsAtLevel("WARN")
	errors := log.logsAtLevel("ERROR")

	assert.Empty(t, warns, "expected no WARN when no secrets are stored")
	assert.Empty(t, errors, "expected no ERROR when no secrets are stored")
}

// TestWarnIfPlaintextSecretsAtRest_AggregatesMultipleKeys verifies that when
// multiple Secret-classified keys have plaintext values, all key names appear in
// a SINGLE warning line (not one per key).
func TestWarnIfPlaintextSecretsAtRest_AggregatesMultipleKeys(t *testing.T) {
	cfg := minimalConfigWithBuildMode("dev")
	secretKeys := secretClassifiedKeys(cfg)
	require.GreaterOrEqual(t, len(secretKeys), 2, "test requires at least two Secret-classified keys")

	// Put values for the first two secret keys.
	store := map[string]string{
		secretKeys[0]: "value-for-key-0",
		secretKeys[1]: "value-for-key-1",
	}

	log := &testLogger{}
	svc := newMockSecretKeyGetter(store)

	warnIfPlaintextSecretsAtRest(context.Background(), disabledEncryptor(), svc, cfg, log)

	warns := log.logsAtLevel("WARN")
	assert.Len(t, warns, 1, "must emit exactly ONE warning line regardless of how many keys are affected")

	if len(warns) == 1 {
		msg := warns[0].msg
		// Both key names must appear in the single line.
		assert.True(t, strings.Contains(msg, secretKeys[0]),
			"single warning must name first affected key")
		assert.True(t, strings.Contains(msg, secretKeys[1]),
			"single warning must name second affected key")
	}
}

// --- warnIfConfigDatabaseDiverges tests ---

// minimalOperationalConfig builds the smallest *config.Config needed to exercise
// warnIfConfigDatabaseDiverges. websocketTimeoutSeconds feeds the operational key
// "websocket.inactivity_timeout_seconds"; timmyLLMAPIKey feeds the operational,
// Secret-classified key "timmy.llm_api_key". Both are left at their struct
// defaults ("not explicit") unless the caller also sets the matching env var via
// t.Setenv, since Explicit is computed from Source=="environment" || file-key
// presence, not from the field being non-zero.
func minimalOperationalConfig(websocketTimeoutSeconds int, timmyLLMAPIKey string) *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			BuildMode: "dev",
			JWT: config.JWTConfig{
				Secret:            "supersecret",
				ExpirationSeconds: 3600,
				SigningMethod:     "HS256",
			},
		},
		Database: config.DatabaseConfig{
			URL: "postgres://localhost/tmi",
		},
		Logging: config.LoggingConfig{
			Level:  "info",
			LogDir: "logs",
		},
		Secrets: config.SecretsConfig{
			Provider: "env",
		},
		WebSocket: config.WebSocketConfig{
			InactivityTimeoutSeconds: websocketTimeoutSeconds,
		},
		Timmy: config.TimmyConfig{
			LLMAPIKey: timmyLLMAPIKey,
		},
	}
}

// TestWarnIfConfigDatabaseDiverges_ExplicitConfigDiffersFromDB verifies that an
// explicit config value disagreeing with the DB row produces exactly one WARN
// naming the affected key.
func TestWarnIfConfigDatabaseDiverges_ExplicitConfigDiffersFromDB(t *testing.T) {
	t.Setenv("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS", "300")
	cfg := minimalOperationalConfig(300, "")

	svc := newMockSecretKeyGetter(map[string]string{
		"websocket.inactivity_timeout_seconds": "600",
	})

	log := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

	warns := log.logsAtLevel("WARN")
	require.Len(t, warns, 1, "expected exactly one WARN log entry")
	assert.Contains(t, warns[0].msg, "websocket.inactivity_timeout_seconds")
	assert.Contains(t, warns[0].msg, "300")
	assert.Contains(t, warns[0].msg, "600")
}

// TestWarnIfConfigDatabaseDiverges_ExplicitConfigMatchesDB verifies that no warning
// is emitted when the explicit config value and the DB row agree.
func TestWarnIfConfigDatabaseDiverges_ExplicitConfigMatchesDB(t *testing.T) {
	t.Setenv("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS", "300")
	cfg := minimalOperationalConfig(300, "")

	svc := newMockSecretKeyGetter(map[string]string{
		"websocket.inactivity_timeout_seconds": "300",
	})

	log := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

	assert.Empty(t, log.allLogs(), "expected no log output when config and database agree")
}

// TestWarnIfConfigDatabaseDiverges_NonExplicitConfigSilent verifies that a struct
// DEFAULT (not explicitly set via env or file) disagreeing with the DB is not
// warned about — that disagreement is normal and expected.
func TestWarnIfConfigDatabaseDiverges_NonExplicitConfigSilent(t *testing.T) {
	// No env var set for TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS, so Explicit==false
	// even though the field itself is non-zero.
	cfg := minimalOperationalConfig(200, "")

	svc := newMockSecretKeyGetter(map[string]string{
		"websocket.inactivity_timeout_seconds": "999",
	})

	log := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

	assert.Empty(t, log.allLogs(), "expected no log output for a non-explicit config value")
}

// TestWarnIfConfigDatabaseDiverges_BootstrapCategorySilent verifies that a
// CategoryBootstrap key (never database-backed) is skipped even when explicit and
// even when a DB row happens to exist and disagree.
func TestWarnIfConfigDatabaseDiverges_BootstrapCategorySilent(t *testing.T) {
	t.Setenv("TMI_DATABASE_URL", "postgres://explicit-host/tmi")
	cfg := minimalOperationalConfig(0, "")
	cfg.Database.URL = "postgres://explicit-host/tmi"

	svc := newMockSecretKeyGetter(map[string]string{
		"database.url": "postgres://some-other-host/tmi",
	})

	log := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

	assert.Empty(t, log.allLogs(), "bootstrap-category keys must never be warned about")
}

// TestWarnIfConfigDatabaseDiverges_SecretRedacted verifies that a Secret-classified
// key that diverges is warned about, but that neither the config value nor the DB
// value ever appears in the logged output.
func TestWarnIfConfigDatabaseDiverges_SecretRedacted(t *testing.T) {
	const configValue = "sk-config-secret-value"
	const dbValue = "sk-database-secret-value"

	t.Setenv("TMI_TIMMY_LLM_API_KEY", configValue)
	cfg := minimalOperationalConfig(0, configValue)

	svc := newMockSecretKeyGetter(map[string]string{
		"timmy.llm_api_key": dbValue,
	})

	log := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

	warns := log.logsAtLevel("WARN")
	require.Len(t, warns, 1, "expected exactly one WARN log entry")
	assert.Contains(t, warns[0].msg, "timmy.llm_api_key", "warning should name the affected key")
	assert.NotContains(t, warns[0].msg, configValue, "config secret value must never be logged")
	assert.NotContains(t, warns[0].msg, dbValue, "database secret value must never be logged")

	// Belt-and-suspenders: check every captured log line, not just the one WARN.
	for _, entry := range log.allLogs() {
		assert.NotContains(t, entry.msg, configValue)
		assert.NotContains(t, entry.msg, dbValue)
	}
}

// TestWarnIfConfigDatabaseDiverges_DBReadErrorSilent verifies that a DB read error
// for a given key is skipped (best-effort) rather than panicking or warning.
func TestWarnIfConfigDatabaseDiverges_DBReadErrorSilent(t *testing.T) {
	t.Setenv("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS", "300")
	cfg := minimalOperationalConfig(300, "")

	svc := newMockSecretKeyGetter(map[string]string{})
	svc.errKeys["websocket.inactivity_timeout_seconds"] = fmt.Errorf("connection refused")

	log := &testLogger{}
	require.NotPanics(t, func() {
		warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)
	})

	assert.Empty(t, log.logsAtLevel("WARN"), "a DB read error must not produce a warning")
}

// TestWarnIfConfigDatabaseDiverges_AbsentOrEmptyRowSilent verifies that an absent
// DB row and an empty-value DB row are both treated as "nothing to compare" and
// produce no warning.
func TestWarnIfConfigDatabaseDiverges_AbsentOrEmptyRowSilent(t *testing.T) {
	t.Setenv("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS", "300")
	cfg := minimalOperationalConfig(300, "")

	// Absent row: no entry in the store at all.
	svc := newMockSecretKeyGetter(map[string]string{})

	log := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)
	assert.Empty(t, log.allLogs(), "an absent DB row must not be warned about")

	// Empty-value row.
	svc2 := newMockSecretKeyGetter(map[string]string{
		"websocket.inactivity_timeout_seconds": "",
	})
	log2 := &testLogger{}
	warnIfConfigDatabaseDiverges(context.Background(), svc2, cfg, log2)
	assert.Empty(t, log2.allLogs(), "an empty DB row value must not be warned about")
}

// TestWarnIfConfigDatabaseDiverges_WinnerReflectsDBOrigin verifies that the winner
// named in the message follows SystemSetting.IsExplicit(): a seeded (non-explicit)
// DB row loses to the explicit config value, while an explicit DB row wins.
func TestWarnIfConfigDatabaseDiverges_WinnerReflectsDBOrigin(t *testing.T) {
	t.Setenv("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS", "300")
	cfg := minimalOperationalConfig(300, "")

	t.Run("seeded DB row loses to config", func(t *testing.T) {
		svc := newMockSecretKeyGetter(map[string]string{
			"websocket.inactivity_timeout_seconds": "600",
		})
		svc.origins["websocket.inactivity_timeout_seconds"] = models.SystemSettingOriginSeeded

		log := &testLogger{}
		warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

		warns := log.logsAtLevel("WARN")
		require.Len(t, warns, 1)
		assert.Contains(t, warns[0].msg, "winner=config")
	})

	t.Run("explicit DB row wins", func(t *testing.T) {
		svc := newMockSecretKeyGetter(map[string]string{
			"websocket.inactivity_timeout_seconds": "600",
		})
		svc.origins["websocket.inactivity_timeout_seconds"] = models.SystemSettingOriginExplicit

		log := &testLogger{}
		warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

		warns := log.logsAtLevel("WARN")
		require.Len(t, warns, 1)
		assert.Contains(t, warns[0].msg, "winner=database")
	})
}

// TestSecretClassifiedKeys verifies that the helper returns a non-empty list
// for a realistic config (regression guard).
func TestSecretClassifiedKeys(t *testing.T) {
	cfg := minimalConfigWithBuildMode("dev")
	keys := secretClassifiedKeys(cfg)
	assert.NotEmpty(t, keys, "expected at least one Secret-classified key in a realistic config")

	// All keys should be non-empty strings.
	for _, k := range keys {
		assert.NotEmpty(t, k, "secret key name must not be empty")
	}
}

// TestWarnIfConfigDatabaseDiverges_RedactsProviderClientSecret pins the fix for
// the security review's one High finding on #794.
//
// Class.Secret is FALSE for the whole auth.oauth.providers. subtree — a blanket
// true there would mis-mask non-secret sub-keys like .client_id — so secrecy
// for .client_secret is carried only by the per-setting Secret flag. The
// divergence check originally redacted on Class.Secret alone, which meant a
// rotated OAuth client secret (the very case the warning exists to surface)
// printed the old AND new secret in plaintext into logs/tmi.log on every boot.
//
// SettingsService.List decrypts before returning, so the database side was
// plaintext too. Anyone with log access got a live credential.
func TestWarnIfConfigDatabaseDiverges_RedactsProviderClientSecret(t *testing.T) {
	const key = "auth.oauth.providers.google.client_secret"
	const configSecret = "config-side-client-secret-DO-NOT-LOG"
	const dbSecret = "database-side-client-secret-DO-NOT-LOG"

	// The key only counts as Explicit when the YAML file actually names it,
	// which is how a real operator reaches this path — so load a real config
	// file rather than hand-building a struct (explicitFileKeys is populated
	// only by the loader).
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(
		"auth:\n"+
			"  oauth:\n"+
			"    providers:\n"+
			"      google:\n"+
			"        id: google\n"+
			"        name: Google\n"+
			"        enabled: true\n"+
			"        client_id: google-client-id\n"+
			"        client_secret: "+configSecret+"\n"), 0o600))

	cfg, err := config.LoadSettingsSource(path)
	require.NoError(t, err)

	var found *config.MigratableSetting
	for _, ms := range cfg.GetMigratableSettings() {
		if ms.Key == key {
			s := ms
			found = &s
			break
		}
	}
	require.NotNil(t, found, "expected the loader to emit %s", key)
	require.True(t, found.Explicit, "a client_secret written in YAML must be Explicit, or this test is vacuous")
	require.True(t, found.IsSecret(), "client_secret must be treated as secret")

	log := &testLogger{}
	svc := newMockSecretKeyGetter(map[string]string{key: dbSecret})

	warnIfConfigDatabaseDiverges(context.Background(), svc, cfg, log)

	for _, entry := range log.allLogs() {
		assert.NotContains(t, entry.msg, configSecret,
			"the config-side client secret must never reach a log line")
		assert.NotContains(t, entry.msg, dbSecret,
			"the database-side client secret must never reach a log line")
	}
}
