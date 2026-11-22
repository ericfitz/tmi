package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ericfitz/tmi/api/models"
)

// TestRunConfigSeed_StampsExplicitOrigin pins #794: dbtool --import-config
// (runConfigSeed) marks every row it writes explicit, on both the create and
// the update path, since an operator running the import deliberately put
// these values in the database.
func TestRunConfigSeed_StampsExplicitOrigin(t *testing.T) {
	db, _ := newExportTestDB(t)

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "settings.yml")
	yamlContent := "operator:\n  name: \"Eric\"\n"
	if err := os.WriteFile(inputPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create path.
	if err := runConfigSeed(db, inputPath, "", false, false, false); err != nil {
		t.Fatalf("runConfigSeed (create): %v", err)
	}

	var created models.SystemSetting
	if err := db.DB().Where("setting_key = ?", "operator.name").First(&created).Error; err != nil {
		t.Fatalf("load created setting: %v", err)
	}
	if !created.Origin.Valid || created.Origin.String != models.SystemSettingOriginExplicit {
		t.Errorf("created row: Origin = (valid=%v, %q), want %q", created.Origin.Valid, created.Origin.String, models.SystemSettingOriginExplicit)
	}
	if !created.IsExplicit() {
		t.Error("created row: IsExplicit() = false, want true")
	}

	// Update path: re-run with --overwrite against a changed value, and
	// against a row whose origin was seeded, to prove the update path
	// promotes it too.
	if err := db.DB().Model(&created).Updates(map[string]any{
		"origin": models.SystemSettingOriginSeeded,
	}).Error; err != nil {
		t.Fatalf("reset origin to seeded for update-path test: %v", err)
	}

	yamlContent = "operator:\n  name: \"Someone Else\"\n"
	if err := os.WriteFile(inputPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigSeed(db, inputPath, "", true, false, false); err != nil {
		t.Fatalf("runConfigSeed (update): %v", err)
	}

	var updated models.SystemSetting
	if err := db.DB().Where("setting_key = ?", "operator.name").First(&updated).Error; err != nil {
		t.Fatalf("load updated setting: %v", err)
	}
	if !updated.Origin.Valid || updated.Origin.String != models.SystemSettingOriginExplicit {
		t.Errorf("updated row: Origin = (valid=%v, %q), want %q", updated.Origin.Valid, updated.Origin.String, models.SystemSettingOriginExplicit)
	}
	if string(updated.Value) != "Someone Else" {
		t.Errorf("updated row: Value = %q, want %q (sanity check the update actually ran)", updated.Value, "Someone Else")
	}
}

// TestRunConfigSeed_DefaultImportPreservesExplicitRows pins #1205: without
// --overwrite, --import-config leaves an existing explicit row unchanged (an
// upgrade deploy must not change chosen configuration), replaces a seeded
// row (a fresh deploy's server seeds defaults before the import runs, so the
// operator's file must still take effect), and creates a missing row.
func TestRunConfigSeed_DefaultImportPreservesExplicitRows(t *testing.T) {
	db, _ := newExportTestDB(t)

	seed := func(key, value, origin string) {
		t.Helper()
		row := models.SystemSetting{
			SettingKey:  models.DBVarchar(key),
			Value:       models.DBText(value),
			SettingType: models.DBVarchar("string"),
			Origin:      models.NullableDBVarchar{String: origin, Valid: true},
		}
		if err := db.DB().Create(&row).Error; err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	seed("operator.name", "Chosen Operator", models.SystemSettingOriginExplicit)
	seed("operator.contact", "default-contact", models.SystemSettingOriginSeeded)

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "settings.yml")
	yamlContent := "operator:\n  name: \"File Operator\"\n  contact: \"file-contact\"\n  jurisdiction: \"file-jurisdiction\"\n"
	if err := os.WriteFile(inputPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := runConfigSeed(db, inputPath, "", false, false, false); err != nil {
		t.Fatalf("runConfigSeed: %v", err)
	}

	load := func(key string) models.SystemSetting {
		t.Helper()
		var s models.SystemSetting
		if err := db.DB().Where("setting_key = ?", key).First(&s).Error; err != nil {
			t.Fatalf("load %s: %v", key, err)
		}
		return s
	}

	if got := load("operator.name"); string(got.Value) != "Chosen Operator" || !got.IsExplicit() {
		t.Errorf("explicit row: Value = %q explicit=%v, want unchanged %q explicit", got.Value, got.IsExplicit(), "Chosen Operator")
	}
	if got := load("operator.contact"); string(got.Value) != "file-contact" || !got.IsExplicit() {
		t.Errorf("seeded row: Value = %q explicit=%v, want replaced %q explicit", got.Value, got.IsExplicit(), "file-contact")
	}
	if got := load("operator.jurisdiction"); string(got.Value) != "file-jurisdiction" || !got.IsExplicit() {
		t.Errorf("missing row: Value = %q explicit=%v, want created %q explicit", got.Value, got.IsExplicit(), "file-jurisdiction")
	}
}

// TestRunConfigSeed_OverwriteReplacesExplicitRows pins the opt-in side of
// #1205: with --overwrite, an existing explicit row is replaced too.
func TestRunConfigSeed_OverwriteReplacesExplicitRows(t *testing.T) {
	db, _ := newExportTestDB(t)

	row := models.SystemSetting{
		SettingKey:  models.DBVarchar("operator.name"),
		Value:       models.DBText("Chosen Operator"),
		SettingType: models.DBVarchar("string"),
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
	}
	if err := db.DB().Create(&row).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	inputPath := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(inputPath, []byte("operator:\n  name: \"File Operator\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigSeed(db, inputPath, "", true, false, false); err != nil {
		t.Fatalf("runConfigSeed --overwrite: %v", err)
	}

	var got models.SystemSetting
	if err := db.DB().Where("setting_key = ?", "operator.name").First(&got).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(got.Value) != "File Operator" || !got.IsExplicit() {
		t.Errorf("Value = %q explicit=%v, want replaced %q explicit", got.Value, got.IsExplicit(), "File Operator")
	}
}
