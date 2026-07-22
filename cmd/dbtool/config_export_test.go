package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/test/testdb"
	"gopkg.in/yaml.v3"
)

// exercises buildNestedConfig: dotted keys become nested maps, values typed by setting_type
func TestBuildNestedConfig(t *testing.T) {
	rows := []exportRow{
		{Key: "operator.name", Value: "Eric", Type: "string"},
		{Key: "extraction.async_enabled", Value: "true", Type: "bool"},
		{Key: "websocket.inactivity_timeout_seconds", Value: "300", Type: "int"},
	}
	root := buildNestedConfig(rows)

	op, ok := root["operator"].(map[string]any)
	if !ok {
		t.Fatalf("operator not nested: %#v", root)
	}
	if op["name"] != "Eric" {
		t.Errorf("operator.name = %v", op["name"])
	}
	ex := root["extraction"].(map[string]any)
	if ex["async_enabled"] != true {
		t.Errorf("bool not coerced: %v", ex["async_enabled"])
	}
	ws := root["websocket"].(map[string]any)
	if ws["inactivity_timeout_seconds"] != 300 {
		t.Errorf("int not coerced: %v", ws["inactivity_timeout_seconds"])
	}
}

// exercises writeExportedConfig: file round-trips through YAML
func TestWriteExportedConfig(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "export.yml")
	rows := []exportRow{{Key: "operator.name", Value: "Eric", Type: "string"}}
	if err := writeExportedConfig(rows, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	if parsed["operator"].(map[string]any)["name"] != "Eric" {
		t.Errorf("round-trip failed: %#v", parsed)
	}
}

// exercises buildNestedConfig's "json" case: a json-typed setting value must
// decode into a real structure (slice/map), not be emitted as a YAML string
// scalar — a string scalar fails to unmarshal into config.Config's typed
// []AdministratorConfig field on --import-config.
func TestBuildNestedConfig_JSONValue(t *testing.T) {
	rows := []exportRow{
		{Key: "administrators", Value: `[{"provider":"tmi","provider_id":"alice","subject_type":"user"}]`, Type: "json"},
	}
	root := buildNestedConfig(rows)

	admins, ok := root["administrators"].([]any)
	if !ok {
		t.Fatalf("administrators not decoded as slice: %#v", root["administrators"])
	}
	if len(admins) != 1 {
		t.Fatalf("expected 1 administrator entry, got %d: %#v", len(admins), admins)
	}
	entry, ok := admins[0].(map[string]any)
	if !ok {
		t.Fatalf("administrator entry not a map: %#v", admins[0])
	}
	if entry["provider"] != "tmi" || entry["provider_id"] != "alice" || entry["subject_type"] != "user" {
		t.Errorf("administrator entry = %#v", entry)
	}
}

// exercises buildNestedConfig's fallback: an invalid json-typed value keeps
// the raw string instead of aborting the export.
func TestBuildNestedConfig_InvalidJSONFallsBackToRawString(t *testing.T) {
	rows := []exportRow{
		{Key: "administrators", Value: `not-valid-json`, Type: "json"},
	}
	root := buildNestedConfig(rows)
	if root["administrators"] != "not-valid-json" {
		t.Errorf("expected raw string fallback, got %#v", root["administrators"])
	}
}

// end-to-end round-trip: the emitted YAML for a json-typed setting must
// parse into the same typed struct config.Load uses (internal/config's
// AdministratorConfig), not just into a generic map.
func TestWriteExportedConfig_JSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "export.yml")
	rows := []exportRow{
		{Key: "administrators", Value: `[{"provider":"tmi","provider_id":"alice","subject_type":"user"}]`, Type: "json"},
	}
	if err := writeExportedConfig(rows, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Administrators []config.AdministratorConfig `yaml:"administrators"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	if len(parsed.Administrators) != 1 {
		t.Fatalf("expected 1 administrator, got %d: %#v", len(parsed.Administrators), parsed.Administrators)
	}
	got := parsed.Administrators[0]
	if got.Provider != "tmi" || got.ProviderId != "alice" || got.SubjectType != "user" {
		t.Errorf("round-tripped administrator = %#v", got)
	}
}

// the generated file header must document keys known not to round-trip via
// --import-config (Important 2).
func TestWriteExportedConfig_HeaderDocumentsNonRoundTrippingKeys(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "export.yml")
	rows := []exportRow{{Key: "operator.name", Value: "Eric", Type: "string"}}
	if err := writeExportedConfig(rows, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	header := string(data)
	for key := range nonRoundTrippingKeys {
		if !strings.Contains(header, key) {
			t.Errorf("header missing documented non-round-tripping key %q:\n%s", key, header)
		}
	}
}

// newExportTestDB creates an in-memory SQLite-backed TestDB (single
// connection, so the in-memory database is shared across all queries within
// a test) and returns it along with the config file path used to open it —
// runConfigExport needs the path to reload MigratableSettings classification
// and the secrets provider config.
func newExportTestDB(t *testing.T) (*testdb.TestDB, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yml")
	cfgYAML := "database:\n" +
		"  url: \"sqlite://:memory:\"\n" +
		"  connection_pool:\n" +
		"    max_open_conns: 1\n" +
		"auth:\n" +
		"  build_mode: dev\n" +
		"  jwt:\n" +
		"    secret: test-secret\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := testdb.New(cfgPath)
	if err != nil {
		t.Fatalf("testdb.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if err := db.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return db, cfgPath
}

// seedSetting inserts a system_settings row directly, bypassing the API.
func seedSystemSetting(t *testing.T, db *testdb.TestDB, key, value, settingType string) {
	t.Helper()
	s := models.SystemSetting{
		SettingKey:  models.DBVarchar(key),
		Value:       models.DBText(value),
		SettingType: models.DBVarchar(settingType),
	}
	if err := db.DB().Create(&s).Error; err != nil {
		t.Fatalf("seed setting %s: %v", key, err)
	}
}

// (a) a secret setting is decrypted in the export output when a working
// encryptor is available.
func TestRunConfigExport_DecryptsSecretWhenEncryptorAvailable(t *testing.T) {
	db, cfgPath := newExportTestDB(t)

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", hex.EncodeToString(key))

	encryptor, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := encryptor.Encrypt("super-secret-value")
	if err != nil {
		t.Fatal(err)
	}

	// auth.jwt.secret is classified Secret: true by GetMigratableSettings.
	seedSystemSetting(t, db, "auth.jwt.secret", encrypted, "string")

	out := filepath.Join(t.TempDir(), "export.yml")
	if err := runConfigExport(db, cfgPath, out, true); err != nil {
		t.Fatalf("runConfigExport: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	auth, ok := parsed["auth"].(map[string]any)
	if !ok {
		t.Fatalf("auth key missing: %#v", parsed)
	}
	jwt, ok := auth["jwt"].(map[string]any)
	if !ok {
		t.Fatalf("auth.jwt missing: %#v", auth)
	}
	if jwt["secret"] != "super-secret-value" {
		t.Errorf("secret not decrypted: got %v", jwt["secret"])
	}
}

// (b) a secret setting is skipped (absent from output) with a warning when
// no decryptor is available — this is also a regression test for a bug
// where NewSettingsEncryptor's disabled-but-non-nil return (no key
// configured) made encryptor == nil never true, so Decrypt() hard-errored
// on an ENC:-prefixed value instead of skipping it.
func TestRunConfigExport_SkipsSecretWhenNoEncryptor(t *testing.T) {
	db, cfgPath := newExportTestDB(t)
	// Explicitly clear so an ambient env var can't make the encryptor enabled.
	t.Setenv("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "")

	seedSystemSetting(t, db, "auth.jwt.secret", "ENC:v1:1:1700000000:c29tZS1jaXBoZXJ0ZXh0Cg==", "string")
	seedSystemSetting(t, db, "operator.name", "Eric", "string")

	out := filepath.Join(t.TempDir(), "export.yml")
	if err := runConfigExport(db, cfgPath, out, true); err != nil {
		t.Fatalf("runConfigExport: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	if auth, ok := parsed["auth"]; ok {
		t.Errorf("secret setting should have been skipped, but auth key present: %#v", auth)
	}
	op, ok := parsed["operator"].(map[string]any)
	if !ok || op["name"] != "Eric" {
		t.Errorf("plain setting missing or wrong: %#v", parsed)
	}
}

// (c) plain (non-secret) settings of each coercible type are exported.
func TestRunConfigExport_ExportsPlainSettings(t *testing.T) {
	db, cfgPath := newExportTestDB(t)

	seedSystemSetting(t, db, "operator.name", "Eric", "string")
	seedSystemSetting(t, db, "websocket.inactivity_timeout_seconds", "300", "int")
	seedSystemSetting(t, db, "features.saml_enabled", "true", "bool")

	out := filepath.Join(t.TempDir(), "export.yml")
	if err := runConfigExport(db, cfgPath, out, true); err != nil {
		t.Fatalf("runConfigExport: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}

	op, ok := parsed["operator"].(map[string]any)
	if !ok || op["name"] != "Eric" {
		t.Errorf("operator.name missing or wrong: %#v", parsed)
	}
	ws, ok := parsed["websocket"].(map[string]any)
	if !ok || ws["inactivity_timeout_seconds"] != 300 {
		t.Errorf("websocket.inactivity_timeout_seconds missing or wrong: %#v", parsed)
	}
	feat, ok := parsed["features"].(map[string]any)
	if !ok || feat["saml_enabled"] != true {
		t.Errorf("features.saml_enabled missing or wrong: %#v", parsed)
	}
}
