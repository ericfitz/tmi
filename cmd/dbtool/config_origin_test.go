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
