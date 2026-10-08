package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/crypto"
	"gopkg.in/yaml.v3"
)

// newTestEncryptor installs a settings key in the env (so runConfigExport's
// secrets provider finds it) and returns a matching encryptor.
func newTestEncryptor(t *testing.T) *crypto.SettingsEncryptor {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", hex.EncodeToString(key))
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// #1033: the settings service encrypts every value at rest, so typed
// non-secret settings (bool/int) are ENC: strings in the DB. Export must
// decrypt them to typed plaintext and the result must re-import cleanly.
func TestRunConfigExport_DecryptsNonSecretTypedValues_RoundTrip(t *testing.T) {
	db, cfgPath := newExportTestDB(t)
	enc := newTestEncryptor(t)

	encBool, err := enc.Encrypt("true")
	if err != nil {
		t.Fatal(err)
	}
	encInt, err := enc.Encrypt("300")
	if err != nil {
		t.Fatal(err)
	}
	seedSystemSetting(t, db, "features.saml_enabled", encBool, "bool")
	seedSystemSetting(t, db, "websocket.inactivity_timeout_seconds", encInt, "int")

	out := filepath.Join(t.TempDir(), "export.yml")
	if err := runConfigExport(db, cfgPath, out, true); err != nil {
		t.Fatalf("runConfigExport: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ENC:") {
		t.Fatalf("export contains ciphertext:\n%s", data)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	feat, ok := parsed["features"].(map[string]any)
	if !ok || feat["saml_enabled"] != true {
		t.Errorf("features.saml_enabled not typed plaintext: %#v", parsed)
	}
	ws, ok := parsed["websocket"].(map[string]any)
	if !ok || ws["inactivity_timeout_seconds"] != 300 {
		t.Errorf("websocket.inactivity_timeout_seconds not typed plaintext: %#v", parsed)
	}

	// Round trip: the exported file imports into a fresh DB.
	db2, _ := newExportTestDB(t)
	if err := runConfigSeed(db2, out, filepath.Join(t.TempDir(), "migrated.yml"), true, false, false); err != nil {
		t.Fatalf("re-import failed: %v", err)
	}
	var got models.SystemSetting
	if err := db2.DB().Where("setting_key = ?", "websocket.inactivity_timeout_seconds").First(&got).Error; err != nil {
		t.Fatalf("setting not imported: %v", err)
	}
}

// #1033: a non-secret ENC value with no decryptor is skipped (key warned,
// never the value), not written as ciphertext.
func TestRunConfigExport_SkipsEncryptedNonSecretWhenNoEncryptor(t *testing.T) {
	db, cfgPath := newExportTestDB(t)
	t.Setenv("TMI_SECRET_SETTINGS_ENCRYPTION_KEY", "")

	seedSystemSetting(t, db, "features.saml_enabled", "ENC:v1:1:1700000000:c29tZS1jaXBoZXJ0ZXh0Cg==", "bool")
	seedSystemSetting(t, db, "operator.name", "Eric", "string")

	out := filepath.Join(t.TempDir(), "export.yml")
	if err := runConfigExport(db, cfgPath, out, true); err != nil {
		t.Fatalf("runConfigExport: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ENC:") {
		t.Errorf("ciphertext written to export:\n%s", data)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["features"]; ok {
		t.Errorf("encrypted setting should be skipped: %#v", parsed)
	}
	if op, ok := parsed["operator"].(map[string]any); !ok || op["name"] != "Eric" {
		t.Errorf("plain setting missing: %#v", parsed)
	}
}

// #1033: importing a stale snapshot that carries ciphertext fails fast,
// naming every offending key and never a value.
func TestRunConfigSeed_RejectsEncryptedValues(t *testing.T) {
	db, _ := newExportTestDB(t)
	cipher := "ENC:v1:1:1700000000:c2VjcmV0LWNpcGhlcnRleHQ="
	src := filepath.Join(t.TempDir(), "snap.yml")
	body := "features:\n  saml_enabled: " + cipher + "\n" +
		"websocket:\n  inactivity_timeout_seconds: " + cipher + "\n" +
		"operator:\n  name: Eric\n"
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runConfigSeed(db, src, "", true, false, false)
	if err == nil {
		t.Fatal("expected error importing encrypted values")
	}
	msg := err.Error()
	for _, want := range []string{"features.saml_enabled", "websocket.inactivity_timeout_seconds", "dev-config-snapshot"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, "c2VjcmV0") || strings.Contains(msg, "operator.name") {
		t.Errorf("error leaks a value or lists a clean key: %s", msg)
	}
	var n int64
	db.DB().Model(&models.SystemSetting{}).Count(&n)
	if n != 0 {
		t.Errorf("nothing should be written on rejection, got %d rows", n)
	}
}
