package config

import (
	"os"
	"sort"
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

// SEM@2b405dc298a9b163f65c46256419a34afb630280: verify the config reference masks every secret-classed default
func TestGenerateReferenceMarkdown_NeverLeaksSecretDefault(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	cfg := getDefaultConfig()
	cfg.Server.TLSSubjectName = "localhost"
	for _, ms := range referenceSettings(cfg) {
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

func TestGenerateReferenceMarkdown_HasPrecedenceSection(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"## Precedence",
		"config explicit",
		"DB explicit",
		"auth.oauth.providers.*",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("reference missing precedence prose %q", want)
		}
	}
}

func TestGenerateReferenceMarkdown_PrecedenceColumnByCategory(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)

	// server.port is bootstrap: config/env only, never DB-backed.
	bootstrapLine := findRowContaining(t, s, "`server.port`")
	if !strings.Contains(bootstrapLine, "config/env only") {
		t.Errorf("bootstrap key server.port: want precedence cell %q, got row %q", "config/env only", bootstrapLine)
	}

	// auth.auto_promote_first_user is operational: DB wins unless config is explicit.
	operationalLine := findRowContaining(t, s, "`auth.auto_promote_first_user`")
	if !strings.Contains(operationalLine, "db unless config explicit") {
		t.Errorf("operational key auth.auto_promote_first_user: want precedence cell %q, got row %q",
			"db unless config explicit", operationalLine)
	}
}

func findRowContaining(t *testing.T, doc, keyCell string) string {
	t.Helper()
	for _, line := range strings.Split(doc, "\n") {
		if strings.Contains(line, keyCell) {
			return line
		}
	}
	t.Fatalf("no table row found containing %q", keyCell)
	return ""
}

// TestGenerateReferenceMarkdown_CoversEveryRegistryEnvVar is the allowlist-
// completeness guardrail: config-reference.md doubles as this project's
// TMI_* environment-variable allowlist, so every env var the registry
// declares must be named in the generated reference. It checks the whole
// registry, not the GetMigratableSettings projection — that projection
// omits empty-valued settings for database-seeding reasons (see
// OmitWhenEmpty on SettingDef) that do not apply to documentation, and a
// setting with an empty default is exactly the one an operator needs to be
// told about (#810). There is deliberately no exception list.
// SEM@2b405dc298a9b163f65c46256419a34afb630280: verify the config reference names every registry env var
func TestGenerateReferenceMarkdown_CoversEveryRegistryEnvVar(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)

	var missing []string
	for _, d := range AllSettingDefs() {
		if d.EnvVar == "" {
			continue
		}
		if !strings.Contains(s, "`"+d.EnvVar+"`") {
			missing = append(missing, d.EnvVar)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("generated reference is missing %d registry env vars (it doubles as the TMI_* allowlist): %v", len(missing), missing)
	}
}

// TestGenerateReferenceMarkdown_DocumentsEveryConfigDeliveredDef pins the
// three omission classes #810 found: an empty default (server.base_url), a
// conditional on another field (server.tls_cert_file, previously skipped
// because the default config has TLS off), and a key deliberately excluded
// from the settings path (content_token_encryption_key, in
// ExpectedMigratableKeysSkipped). All three are real config/env settings
// and must be documented.
// SEM@2b405dc298a9b163f65c46256419a34afb630280: verify the config reference has one row per config-delivered registry setting
func TestGenerateReferenceMarkdown_DocumentsEveryConfigDeliveredDef(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)

	for _, key := range []string{"server.base_url", "server.tls_cert_file", "content_token_encryption_key", "ssrf.webhook.allowlist"} {
		if !strings.Contains(s, "| `"+key+"` |") {
			t.Errorf("reference has no row for %s", key)
		}
	}

	var rows int
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "| `") && !strings.HasPrefix(line, "| `TMI_") && !strings.HasPrefix(line, "| `SAML_") {
			rows++
		}
	}
	var want int
	for _, d := range AllSettingDefs() {
		if d.Get != nil {
			want++
		}
	}
	if rows != want {
		t.Errorf("reference renders %d setting rows, registry has %d config-delivered defs", rows, want)
	}
}

// TestGenerateReferenceMarkdown_CoversEveryProcessEnvVar: variables read
// straight from the process environment (the SSRF overrides, workers, admin
// bootstrap, cloud logging, secret prefixes) never pass through the
// registry, so the reference — the TMI_* allowlist — must render the
// hand-maintained ProcessEnvVars table in full (#810).
// SEM@4077bab0af75093efb1660aa34801b2c752c9c0f: verify the config reference lists every process-environment env var and pattern
func TestGenerateReferenceMarkdown_CoversEveryProcessEnvVar(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "## Process environment") {
		t.Fatal("reference missing section \"## Process environment\"")
	}
	for _, p := range ProcessEnvVars() {
		if !strings.Contains(s, "`"+p.Name+"`") {
			t.Errorf("process env var %s missing from the reference", p.Name)
		}
	}
	for _, want := range []string{"### server", "### workers", "### Prefix patterns"} {
		if !strings.Contains(s, want) {
			t.Errorf("reference missing process-environment group %q", want)
		}
	}
}

func TestConfigReferenceFile_MatchesRegistry(t *testing.T) {
	generated, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}
	onDisk, err := os.ReadFile("../../config-reference.md")
	if err != nil {
		t.Fatalf("read config-reference.md: %v", err)
	}
	strip := func(b []byte) string {
		var keep []string
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "GENERATED by") {
				continue
			}
			keep = append(keep, line)
		}
		return strings.Join(keep, "\n")
	}
	if strip(generated) != strip(onDisk) {
		t.Error("config-reference.md is stale — run `make generate-config-docs`")
	}
}

// TestGenerateReferenceMarkdown_MutabilityComesFromRegistry pins the fix for
// the reference table reporting every operational setting as "hot".
//
// GetMigratableSettings assigns Class via classificationFor(key), which does
// not carry the per-entry withMutability(...) overrides. Rendering
// s.Class.Mutability directly therefore printed the pre-audit default for
// every row. The generator resolves the value from the registry def instead.
//
// Watched to fail: reverting declaredMutability to return
// s.Class.Mutability.String() makes this test report 0 static rows.
func TestGenerateReferenceMarkdown_MutabilityComesFromRegistry(t *testing.T) {
	out, err := GenerateReferenceMarkdown()
	if err != nil {
		t.Fatalf("GenerateReferenceMarkdown: %v", err)
	}

	var static, hot int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		switch {
		case strings.Contains(line, "| static |"):
			static++
		case strings.Contains(line, "| hot |"):
			hot++
		}
	}

	if static+hot == 0 {
		t.Fatal("no operational rows rendered a mutability cell")
	}
	// The registry audit set most operational settings to Static. If the
	// generator regresses to the projected classification, every row reads
	// "hot" and static drops to zero.
	if static == 0 {
		t.Errorf("every operational row reports hot (%d rows) — the generator is "+
			"rendering the projected classification instead of the registry "+
			"declaration; see declaredMutability", hot)
	}
	if static <= hot {
		t.Errorf("expected static to dominate per the registry audit, got static=%d hot=%d", static, hot)
	}

	// Cross-check one traced case end to end: features.saml_enabled gates a
	// manager built once at boot, so it must render static.
	d, ok := DefFor("features.saml_enabled")
	if !ok {
		t.Fatal("features.saml_enabled missing from the registry")
	}
	if d.Class.Mutability != MutabilityStatic {
		t.Errorf("features.saml_enabled declared %s, want static", d.Class.Mutability)
	}
	if !strings.Contains(string(out), "`features.saml_enabled`") {
		t.Fatal("features.saml_enabled not present in the reference table")
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "`features.saml_enabled`") && !strings.Contains(line, "| static |") {
			t.Errorf("features.saml_enabled row does not report static: %s", line)
		}
	}
}
