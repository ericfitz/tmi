package main

import (
	"context"
	"fmt"
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
}

func newMockSecretKeyGetter(values map[string]string) *mockSecretKeyGetter {
	return &mockSecretKeyGetter{
		store:   values,
		errKeys: make(map[string]error),
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
	return &models.SystemSetting{SettingKey: key, Value: val}, nil
}

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
