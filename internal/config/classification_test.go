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
