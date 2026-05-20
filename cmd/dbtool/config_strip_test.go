package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestStripOperationalKeys_RemovesOperationalKeepsBootstrap pins #420's
// in-place rewrite contract: operational keys disappear, bootstrap keys
// stay verbatim (no type coercion, no password redaction, no quoting
// changes that would break config.Load).
func TestStripOperationalKeys_RemovesOperationalKeepsBootstrap(t *testing.T) {
	yamlIn := `# top comment preserved
server:
  port: "8080"
  read_timeout: 5s
database:
  url: postgres://u:p@h:5432/db
auth:
  build_mode: dev
  jwt:
    secret: SUPER-SECRET
    signing_method: HS256
    expiration_seconds: 3600
  cookie:
    enabled: true
    domain: localhost
features:
  saml_enabled: false
operator:
  name: TMI
  contact: ops@example
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(yamlIn), 0o600); err != nil {
		t.Fatal(err)
	}

	size, err := stripOperationalKeys(path)
	if err != nil {
		t.Fatalf("stripOperationalKeys: %v", err)
	}
	if size <= 0 {
		t.Errorf("size %d, want > 0", size)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	// Bootstrap must survive.
	for _, must := range []string{
		"server:",
		"port",
		`"8080"`, // string-quoted port stays string-quoted
		"5s",     // duration stays as-is
		"database:",
		"postgres://u:p@h:5432/db", // NO password redaction
		"auth:",
		"build_mode: dev",
		"jwt:",
		"secret: SUPER-SECRET",
		"signing_method: HS256",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("stripped output missing bootstrap fragment %q\n--- got ---\n%s", must, got)
		}
	}

	// Operational must be gone.
	for _, gone := range []string{
		"expiration_seconds",
		"cookie",
		"saml_enabled",
		"features",
		"operator",
	} {
		if strings.Contains(got, gone) {
			t.Errorf("stripped output still contains operational fragment %q\n--- got ---\n%s", gone, got)
		}
	}

	// The result must parse as YAML.
	var probe map[string]any
	if err := yaml.Unmarshal(out, &probe); err != nil {
		t.Fatalf("stripped output is not valid YAML: %v", err)
	}
}

// TestStripOperationalKeys_EmptyParentsRemoved ensures that pruning a
// mapping that has only operational children leaves no empty `key: {}`
// stub behind.
func TestStripOperationalKeys_EmptyParentsRemoved(t *testing.T) {
	// `operator` is entirely operational. After stripping, the parent
	// mapping should be gone, not "operator: {}".
	yamlIn := `operator:
  name: TMI
  contact: ops@example
auth:
  build_mode: dev
`
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yml")
	_ = os.WriteFile(path, []byte(yamlIn), 0o600)

	if _, err := stripOperationalKeys(path); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	got := string(out)
	if strings.Contains(got, "operator") {
		t.Errorf("operator subtree should be gone; got:\n%s", got)
	}
	if !strings.Contains(got, "auth:") {
		t.Errorf("auth subtree should remain; got:\n%s", got)
	}
}

// TestBackupConfigFile_WritesContentVerbatim pins the backup contract:
// content is identical to the source, and the path ends with .bak.
func TestBackupConfigFile_WritesContentVerbatim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yml")
	contents := []byte("server:\n  port: 8080\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	bak, err := backupConfigFile(path)
	if err != nil {
		t.Fatalf("backupConfigFile: %v", err)
	}
	if !strings.HasSuffix(bak, ".bak") {
		t.Errorf("backup path %q does not end with .bak", bak)
	}
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(contents) {
		t.Errorf("backup content differs:\ngot:  %q\nwant: %q", got, contents)
	}
}

// TestLooksLikeYAMLPath covers the extension-based gate for in-place
// rewrite eligibility.
func TestLooksLikeYAMLPath(t *testing.T) {
	cases := map[string]bool{
		"config.yml":         true,
		"config.yaml":        true,
		"CONFIG.YAML":        true,
		"path/to/config.yml": true,
		"config.json":        false,
		"config":             false,
		"config.yml.bak":     false,
	}
	for in, want := range cases {
		if got := looksLikeYAMLPath(in); got != want {
			t.Errorf("looksLikeYAMLPath(%q) = %v, want %v", in, got, want)
		}
	}
}
