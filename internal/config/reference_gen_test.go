package config

import (
	"strings"
	"testing"
)

func TestGenerateReferenceMarkdown_HasBothCategoryTables(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	for _, want := range []string{"## Bootstrap settings", "## Operational settings"} {
		if !strings.Contains(s, want) {
			t.Errorf("reference missing section %q", want)
		}
	}
}

func TestGenerateReferenceMarkdown_BootstrapKeyHasEnvVar(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	// server.port is bootstrap and overridable by TMI_SERVER_PORT.
	if !strings.Contains(string(out), "`TMI_SERVER_PORT`") {
		t.Error("reference missing env var TMI_SERVER_PORT for server.port")
	}
}

func TestGenerateReferenceMarkdown_NeverLeaksSecretDefault(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	cfg := getDefaultConfig()
	for _, ms := range cfg.GetMigratableSettings() {
		if !ms.Class.Secret {
			continue
		}
		// A secret's real default value must never appear in the reference.
		if ms.Value != "" && strings.Contains(s, "`"+ms.Value+"`") {
			t.Errorf("secret %q default value leaked into reference", ms.Key)
		}
		// Its row must render the masked placeholder.
		for _, line := range strings.Split(s, "\n") {
			if strings.Contains(line, "`"+ms.Key+"`") && !strings.Contains(line, "_(secret)_") {
				t.Errorf("secret %q row not masked: %q", ms.Key, line)
			}
		}
	}
}
