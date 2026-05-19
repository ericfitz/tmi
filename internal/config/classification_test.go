package config

import "testing"

func TestConfigClass_String(t *testing.T) {
	if CategoryBootstrap.String() != "bootstrap" {
		t.Errorf("CategoryBootstrap.String() = %q, want %q", CategoryBootstrap.String(), "bootstrap")
	}
	if CategoryOperational.String() != "operational" {
		t.Errorf("CategoryOperational.String() = %q, want %q", CategoryOperational.String(), "operational")
	}
}

func TestConfigClass_ZeroValueIsUnclassified(t *testing.T) {
	var c ConfigClass
	if c.Category != CategoryUnclassified {
		t.Errorf("zero ConfigClass.Category = %v, want CategoryUnclassified", c.Category)
	}
}

func TestGetMigratableSettings_EveryKeyClassified(t *testing.T) {
	c := getDefaultConfig()
	settings := c.GetMigratableSettings()
	if len(settings) == 0 {
		t.Fatal("GetMigratableSettings returned no settings")
	}
	for _, s := range settings {
		if s.Class.Category == CategoryUnclassified {
			t.Errorf("setting %q is unclassified — add it to the classification registry", s.Key)
		}
	}
}

func TestGetMigratableSettings_PassesValidationSuite(t *testing.T) {
	c := getDefaultConfig()
	if err := ValidateClassifications(c.GetMigratableSettings()); err != nil {
		t.Fatalf("default config fails classification validation:\n%v", err)
	}
}
