package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
)

// TestRunConfigSeed_RejectsInvalidTimmySetting pins #1108: --import-config must
// apply the settings service's semantic validators before writing, and write
// nothing (not even the valid sibling settings) when one value is invalid.
func TestRunConfigSeed_RejectsInvalidTimmySetting(t *testing.T) {
	db, _ := newExportTestDB(t)

	inputPath := filepath.Join(t.TempDir(), "settings.yml")
	yamlContent := "operator:\n  name: \"Eric\"\ntimmy:\n  llm_provider: \"bogus\"\n"
	if err := os.WriteFile(inputPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runConfigSeed(db, inputPath, "", true, false, false)
	if err == nil {
		t.Fatal("runConfigSeed: expected error for invalid timmy.llm_provider, got nil")
	}
	if !strings.Contains(err.Error(), "timmy.llm_provider") {
		t.Errorf("error should name the offending key, got: %v", err)
	}

	var n int64
	if cErr := db.DB().Model(&models.SystemSetting{}).Count(&n).Error; cErr != nil {
		t.Fatal(cErr)
	}
	if n != 0 {
		t.Errorf("system_settings has %d rows after rejected import, want 0 (nothing partial)", n)
	}
}

// TestRunConfigSeed_AcceptsValidTimmySetting guards against over-rejection.
func TestRunConfigSeed_AcceptsValidTimmySetting(t *testing.T) {
	db, _ := newExportTestDB(t)

	inputPath := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(inputPath, []byte("timmy:\n  llm_provider: \"openai\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigSeed(db, inputPath, "", true, false, false); err != nil {
		t.Fatalf("runConfigSeed: %v", err)
	}
	var s models.SystemSetting
	if err := db.DB().Where("setting_key = ?", "timmy.llm_provider").First(&s).Error; err != nil {
		t.Fatalf("expected timmy.llm_provider row: %v", err)
	}
}

// TestSeedSetting_RejectsInvalidTimmySetting covers the hand-authored seed path.
func TestSeedSetting_RejectsInvalidTimmySetting(t *testing.T) {
	db, _ := newExportTestDB(t)

	entry := SeedEntry{Kind: kindSetting, Ref: "r", Data: map[string]any{
		"key": "timmy.llm_provider", "value": "bogus", "type": "string",
	}}
	_, err := seedSetting(db, entry)
	if err == nil {
		t.Fatal("seedSetting: expected error for invalid timmy.llm_provider, got nil")
	}
	if !strings.Contains(err.Error(), "timmy.llm_provider") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
	var n int64
	if cErr := db.DB().Model(&models.SystemSetting{}).Count(&n).Error; cErr != nil {
		t.Fatal(cErr)
	}
	if n != 0 {
		t.Errorf("system_settings has %d rows after rejected seed, want 0", n)
	}
}

// TestSeedSetting_ProductionRejectsDumpFlag checks the build-mode-dependent
// validator is evaluated against TMI_BUILD_MODE.
func TestSeedSetting_ProductionRejectsDumpFlag(t *testing.T) {
	db, _ := newExportTestDB(t)
	t.Setenv("TMI_BUILD_MODE", "production")

	entry := SeedEntry{Kind: kindSetting, Ref: "r", Data: map[string]any{
		"key": "timmy.dump_extracted_text_to_note", "value": "true", "type": "bool",
	}}
	if _, err := seedSetting(db, entry); err == nil {
		t.Fatal("seedSetting: expected error enabling dump flag in production, got nil")
	}
}

// TestValidateSeedSettings_RejectsBadEntryAmongValid pins the whole-list
// pre-pass: one invalid setting entry fails the batch and names its key.
func TestValidateSeedSettings_RejectsBadEntryAmongValid(t *testing.T) {
	entries := []SeedEntry{
		{Kind: kindSetting, Data: map[string]any{"key": "operator.name", "value": "x", "type": "string"}},
		{Kind: kindSetting, Data: map[string]any{"key": "timmy.llm_provider", "value": "bogus", "type": "string"}},
		{Kind: kindUser, Data: map[string]any{"key": "timmy.llm_provider", "value": "bogus"}},
	}
	err := validateSeedSettings(entries)
	if err == nil || !strings.Contains(err.Error(), "timmy.llm_provider") {
		t.Fatalf("expected error naming timmy.llm_provider, got %v", err)
	}
	if err := validateSeedSettings(entries[:1]); err != nil {
		t.Errorf("valid entries rejected: %v", err)
	}
}

// TestRunConfigSeed_RejectsNegativeMaxTokens covers timmy.llm_max_tokens, the
// third key the startup Timmy check validates.
func TestRunConfigSeed_RejectsNegativeMaxTokens(t *testing.T) {
	db, _ := newExportTestDB(t)
	inputPath := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(inputPath, []byte("timmy:\n  llm_max_tokens: -3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runConfigSeed(db, inputPath, "", true, false, false)
	if err == nil || !strings.Contains(err.Error(), "timmy.llm_max_tokens") {
		t.Fatalf("expected error naming timmy.llm_max_tokens, got %v", err)
	}
}

// TestRunConfigSeed_DryRunRejectsInvalid pins that --dry-run reports validation
// failures (non-zero) and writes nothing.
func TestRunConfigSeed_DryRunRejectsInvalid(t *testing.T) {
	db, _ := newExportTestDB(t)
	inputPath := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(inputPath, []byte("timmy:\n  llm_provider: \"bogus\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runConfigSeed(db, inputPath, "", true, true, false)
	if err == nil || !strings.Contains(err.Error(), "timmy.llm_provider") {
		t.Fatalf("dry-run: expected error naming timmy.llm_provider, got %v", err)
	}
	var n int64
	if cErr := db.DB().Model(&models.SystemSetting{}).Count(&n).Error; cErr != nil {
		t.Fatal(cErr)
	}
	if n != 0 {
		t.Errorf("dry-run wrote %d rows, want 0", n)
	}
}
