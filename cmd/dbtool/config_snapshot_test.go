package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/test/testdb"
	"gopkg.in/yaml.v3"
)

// seedSettingWithOrigin inserts a system_settings row with a given origin;
// origin "" stores NULL.
func seedSettingWithOrigin(t *testing.T, db *testdb.TestDB, key, value, typ, origin string) {
	t.Helper()
	s := models.SystemSetting{
		SettingKey:  models.DBVarchar(key),
		Value:       models.DBText(value),
		SettingType: models.DBVarchar(typ),
	}
	if origin != "" {
		s.Origin = models.NullableDBVarchar{String: origin, Valid: true}
	}
	if err := db.DB().Create(&s).Error; err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

func loadSetting(t *testing.T, db *testdb.TestDB, key string) (models.SystemSetting, bool) {
	t.Helper()
	var s models.SystemSetting
	res := db.DB().Where("setting_key = ?", key).Limit(1).Find(&s)
	if res.Error != nil {
		t.Fatalf("load %s: %v", key, res.Error)
	}
	return s, res.RowsAffected > 0
}

func writeSnapshotFile(t *testing.T, body string, withMarker bool) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "snap.yaml")
	content := body
	if withMarker {
		content = snapshotMarkerLine + "\n" + body
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// #1182: exported settings nest by YAML path, so a key whose path differs from
// its setting key (extraction.async_enabled lives at
// content_extractors.async_enabled) round-trips through the config loader.
func TestBuildNestedConfig_NestsByYAMLPath(t *testing.T) {
	root := buildNestedConfig([]exportRow{
		{Key: "extraction.async_enabled", Value: "true", Type: "bool"},
	})
	if _, ok := root["extraction"]; ok {
		t.Fatalf("nested under the setting key, want the YAML path: %#v", root)
	}
	ce, ok := root["content_extractors"].(map[string]any)
	if !ok || ce["async_enabled"] != true {
		t.Fatalf("content_extractors.async_enabled missing: %#v", root)
	}
}

func TestExportedFile_RoundTripsPathDifferingKey(t *testing.T) {
	out := filepath.Join(t.TempDir(), "e.yaml")
	if err := writeExportedConfig([]exportRow{{Key: "extraction.async_enabled", Value: "true", Type: "bool"}}, out, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadSettingsFileOnly(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range cfg.GetMigratableSettings() {
		if s.Key == "extraction.async_enabled" {
			if !s.Explicit || s.Value != "true" {
				t.Errorf("extraction.async_enabled = %q explicit=%v, want true/explicit", s.Value, s.Explicit)
			}
			return
		}
	}
	t.Fatal("extraction.async_enabled not found")
}

func TestRunConfigExport_ExplicitOnlyFiltersAndMarks(t *testing.T) {
	db, cfgPath := newExportTestDB(t)
	seedSettingWithOrigin(t, db, "operator.name", "Eric", "string", models.SystemSettingOriginExplicit)
	seedSettingWithOrigin(t, db, "websocket.inactivity_timeout_seconds", "300", "int", models.SystemSettingOriginSeeded)
	seedSettingWithOrigin(t, db, "operator.contact", "x@example.com", "string", "") // NULL origin
	seedSettingWithOrigin(t, db, "ui.default_theme", "dark", "string", "bogus")     // unexpected origin

	out := filepath.Join(t.TempDir(), "snap.yaml")
	if err := runConfigExport(db, cfgPath, out, true, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(data), snapshotMarkerLine+"\n") {
		t.Errorf("file does not start with the format marker:\n%s", data)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if op, ok := parsed["operator"].(map[string]any); !ok || op["name"] != "Eric" || len(op) != 1 {
		t.Errorf("want only operator.name, got %#v", parsed["operator"])
	}
	for _, k := range []string{"websocket", "ui"} {
		if _, ok := parsed[k]; ok {
			t.Errorf("non-explicit row exported under %q: %#v", k, parsed)
		}
	}
}

func TestRunConfigExport_DefaultModeHasNoMarker(t *testing.T) {
	db, cfgPath := newExportTestDB(t)
	seedSettingWithOrigin(t, db, "operator.name", "Eric", "string", models.SystemSettingOriginSeeded)
	out := filepath.Join(t.TempDir(), "e.yaml")
	if err := runConfigExport(db, cfgPath, out, true, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), snapshotMarkerLine) {
		t.Errorf("full export carries the snapshot marker:\n%s", data)
	}
	if !strings.Contains(string(data), "name: Eric") {
		t.Errorf("full export dropped a seeded row:\n%s", data)
	}
}

func TestRunSnapshotRestore_RefusesFileWithoutMarker(t *testing.T) {
	db, _ := newExportTestDB(t)
	seedSettingWithOrigin(t, db, "operator.name", "Seeded", "string", models.SystemSettingOriginSeeded)
	p := writeSnapshotFile(t, "operator:\n  name: Eric\n", false)

	err := runSnapshotRestore(db, p, false)
	if err == nil {
		t.Fatal("expected refusal for a file without the marker")
	}
	if !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), "dev-config-snapshot") {
		t.Errorf("error should name the file and the remedy, got: %v", err)
	}
	got, _ := loadSetting(t, db, "operator.name")
	if string(got.Value) != "Seeded" || got.IsExplicit() {
		t.Errorf("refused restore modified the database: %q explicit=%v", got.Value, got.IsExplicit())
	}
}

func TestRunSnapshotRestore_ReplacesSeededSkipsExplicitCreatesMissing(t *testing.T) {
	db, _ := newExportTestDB(t)
	seedSettingWithOrigin(t, db, "websocket.inactivity_timeout_seconds", "300", "int", models.SystemSettingOriginSeeded)
	seedSettingWithOrigin(t, db, "operator.name", "Live", "string", models.SystemSettingOriginExplicit)
	seedSettingWithOrigin(t, db, "operator.contact", "old", "string", "") // NULL origin counts as not explicit
	seedSettingWithOrigin(t, db, "ui.default_theme", "light", "string", models.SystemSettingOriginSeeded)

	p := writeSnapshotFile(t, "websocket:\n  inactivity_timeout_seconds: 600\n"+
		"operator:\n  name: Snapshot\n  contact: new\n"+
		"content_extractors:\n  async_enabled: true\n", true)
	if err := runSnapshotRestore(db, p, false); err != nil {
		t.Fatal(err)
	}

	ws, _ := loadSetting(t, db, "websocket.inactivity_timeout_seconds")
	if string(ws.Value) != "600" || !ws.IsExplicit() {
		t.Errorf("seeded row not replaced as explicit: %q explicit=%v", ws.Value, ws.IsExplicit())
	}
	nm, _ := loadSetting(t, db, "operator.name")
	if string(nm.Value) != "Live" {
		t.Errorf("explicit row was overwritten: %q", nm.Value)
	}
	ct, _ := loadSetting(t, db, "operator.contact")
	if string(ct.Value) != "new" || !ct.IsExplicit() {
		t.Errorf("NULL-origin row not replaced: %q explicit=%v", ct.Value, ct.IsExplicit())
	}
	ax, ok := loadSetting(t, db, "extraction.async_enabled")
	if !ok || string(ax.Value) != "true" || !ax.IsExplicit() {
		t.Errorf("missing key not created as explicit: ok=%v %q", ok, ax.Value)
	}
	// A seeded row the file does not name is untouched.
	th, _ := loadSetting(t, db, "ui.default_theme")
	if string(th.Value) != "light" || th.IsExplicit() {
		t.Errorf("unnamed row changed: %q explicit=%v", th.Value, th.IsExplicit())
	}
	var n int64
	db.DB().Model(&models.SystemSetting{}).Count(&n)
	if n != 5 {
		t.Errorf("row count = %d, want 5 (only the file's keys written)", n)
	}
}

// Struct defaults and host environment must not leak into a restore.
func TestRunSnapshotRestore_WritesOnlyFileKeysIgnoringEnv(t *testing.T) {
	db, _ := newExportTestDB(t)
	t.Setenv("TMI_OPERATOR_NAME", "FromHostEnv")
	t.Setenv("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS", "999")
	p := writeSnapshotFile(t, "operator:\n  name: FromSnapshot\n", true)
	if err := runSnapshotRestore(db, p, false); err != nil {
		t.Fatal(err)
	}
	nm, _ := loadSetting(t, db, "operator.name")
	if string(nm.Value) != "FromSnapshot" {
		t.Errorf("operator.name = %q, want the snapshot value", nm.Value)
	}
	if _, ok := loadSetting(t, db, "websocket.inactivity_timeout_seconds"); ok {
		t.Error("a key absent from the file was written from env/defaults")
	}
}

func TestRunSnapshotRestore_EmptySnapshotIsNoOp(t *testing.T) {
	db, _ := newExportTestDB(t)
	seedSettingWithOrigin(t, db, "operator.name", "Seeded", "string", models.SystemSettingOriginSeeded)
	p := writeSnapshotFile(t, "", true)
	if err := runSnapshotRestore(db, p, false); err != nil {
		t.Fatalf("empty snapshot must be a no-op, got: %v", err)
	}
	nm, _ := loadSetting(t, db, "operator.name")
	if string(nm.Value) != "Seeded" {
		t.Errorf("empty restore changed a row: %q", nm.Value)
	}
}

func TestRunSnapshotRestore_SecondRunIsNoOp(t *testing.T) {
	db, _ := newExportTestDB(t)
	seedSettingWithOrigin(t, db, "operator.name", "Seeded", "string", models.SystemSettingOriginSeeded)
	p := writeSnapshotFile(t, "operator:\n  name: Snap\n", true)
	for i := 0; i < 2; i++ {
		if err := runSnapshotRestore(db, p, false); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	nm, _ := loadSetting(t, db, "operator.name")
	if string(nm.Value) != "Snap" || !nm.IsExplicit() {
		t.Errorf("after two restores: %q explicit=%v", nm.Value, nm.IsExplicit())
	}
}

func TestRunSnapshotRestore_DryRunWritesNothing(t *testing.T) {
	db, _ := newExportTestDB(t)
	p := writeSnapshotFile(t, "operator:\n  name: Snap\n", true)
	if err := runSnapshotRestore(db, p, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadSetting(t, db, "operator.name"); ok {
		t.Error("dry run wrote a row")
	}
}

// Export (explicit-only) then restore into a different database round-trips
// the explicit rows with their origin.
func TestSnapshotExportRestoreRoundTrip(t *testing.T) {
	src, cfgPath := newExportTestDB(t)
	seedSettingWithOrigin(t, src, "operator.name", "Eric", "string", models.SystemSettingOriginExplicit)
	seedSettingWithOrigin(t, src, "extraction.async_enabled", "true", "bool", models.SystemSettingOriginExplicit)
	seedSettingWithOrigin(t, src, "websocket.inactivity_timeout_seconds", "300", "int", models.SystemSettingOriginSeeded)
	out := filepath.Join(t.TempDir(), "snap.yaml")
	if err := runConfigExport(src, cfgPath, out, true, true); err != nil {
		t.Fatal(err)
	}

	dst, _ := newExportTestDB(t)
	seedSettingWithOrigin(t, dst, "operator.name", "Seeded", "string", models.SystemSettingOriginSeeded)
	if err := runSnapshotRestore(dst, out, false); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"operator.name": "Eric", "extraction.async_enabled": "true"} {
		got, ok := loadSetting(t, dst, k)
		if !ok || string(got.Value) != want || !got.IsExplicit() {
			t.Errorf("%s: ok=%v value=%q explicit=%v, want %q explicit", k, ok, got.Value, got.IsExplicit(), want)
		}
	}
	if _, ok := loadSetting(t, dst, "websocket.inactivity_timeout_seconds"); ok {
		t.Error("seeded source row leaked into the snapshot")
	}
}

func TestDispatchRejectsExplicitOnlyWithoutExport(t *testing.T) {
	db, _ := newExportTestDB(t)
	err := dispatchOperation(db, nil, 1, cliFlags{importConfig: true, explicitOnly: true, inputFile: "x"})
	if err == nil || !strings.Contains(err.Error(), "--explicit-only") {
		t.Errorf("want an --explicit-only error, got %v", err)
	}
}

func TestDispatchRestoreSnapshotRejectsOverwriteAndMissingFile(t *testing.T) {
	db, _ := newExportTestDB(t)
	if err := dispatchOperation(db, nil, 1, cliFlags{restoreSnapshot: true}); err == nil || !strings.Contains(err.Error(), "--input-file") {
		t.Errorf("want --input-file error, got %v", err)
	}
	if err := dispatchOperation(db, nil, 1, cliFlags{restoreSnapshot: true, inputFile: "x", overwrite: true}); err == nil || !strings.Contains(err.Error(), "--overwrite") {
		t.Errorf("want --overwrite error, got %v", err)
	}
}
