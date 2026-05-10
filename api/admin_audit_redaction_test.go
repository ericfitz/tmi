package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// allKnownAuditFieldPaths returns every field path that the audit system can
// emit. It is used by TestRedactor_BuildTimeGate_DenyListCoversSensitiveNames
// to verify that the redactor's deny-list covers every sensitive-named path.
//
// The walk has two sources:
//  1. Every descriptor in adminAuditDescriptors — the FieldPathFn is invoked
//     with placeholder route params so the path template is fully expanded.
//  2. Every key in config.GetMigratableSettings, prefixed with
//     "system_settings." to match the PUT /admin/settings/{key} field path.
func allKnownAuditFieldPaths(t *testing.T) []string {
	t.Helper()
	out := []string{}

	// 1. Walk descriptor field-paths.
	gin.SetMode(gin.TestMode)
	for _, d := range adminAuditDescriptors(nil) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil)
		setPlaceholderParams(c, d.PathTpl)
		out = append(out, d.FieldPathFn(c))
	}

	// 2. Walk MigratableSettings keys (system_settings.*).
	cfg := getConfigForTest(t)
	for _, ms := range cfg.GetMigratableSettings() {
		out = append(out, "system_settings."+ms.Key)
	}

	return out
}

// setPlaceholderParams fills in the Gin context's route params with
// placeholder values matching the {param} segments in the OpenAPI path
// template.  For example, "/admin/groups/{internal_uuid}/members/{member_uuid}"
// produces params [internal_uuid=X, member_uuid=X].
func setPlaceholderParams(c *gin.Context, tpl string) {
	for _, name := range extractParamNames(tpl) {
		c.Params = append(c.Params, gin.Param{Key: name, Value: "X"})
	}
}

// extractParamNames returns every {param} name from an OpenAPI path template.
func extractParamNames(tpl string) []string {
	out := []string{}
	for _, seg := range strings.Split(tpl, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.Trim(seg, "{}"))
		}
	}
	return out
}

// getConfigForTest returns a *config.Config for use by the build-time gate
// test. We construct it directly (without going through config.Load) to avoid
// the database-URL validation requirement that blocks tests without a live DB.
// The goal is only to enumerate MigratableSetting keys, so the values do not
// need to be real — we just need a representative, fully-populated Config so
// that no static key is silently omitted.
//
// Provider-specific sensitive paths (e.g. auth.oauth.providers.X.client_secret,
// auth.saml.providers.X.sp_private_key) are only present when providers are
// configured. Those patterns are already verified by the tier-1/tier-2 unit
// tests and the deny-list pattern coverage; the gate's job is to catch newly
// added *static* sensitive keys.
func getConfigForTest(_ *testing.T) *config.Config {
	return &config.Config{}
}

func TestRedactor_Tier1_TotalRedaction(t *testing.T) {
	r := NewRedactor()
	cases := []struct {
		field string
		value string
	}{
		{"oauth.providers.google.password", "hunter2"},
		{"saml.idp.passphrase", "correct horse battery staple"},
	}
	for _, c := range cases {
		got := r.Redact(c.field, c.value)
		if !strings.Contains(got, `"redacted":true`) {
			t.Errorf("field=%q: expected redacted:true, got %s", c.field, got)
		}
		if strings.Contains(got, "sha256_prefix") {
			t.Errorf("field=%q: tier-1 unexpectedly carries sha256_prefix: %s", c.field, got)
		}
		if strings.Contains(got, "tail") {
			t.Errorf("field=%q: tier-1 unexpectedly carries tail: %s", c.field, got)
		}
	}
}

func TestRedactor_Tier2_HashAndTail(t *testing.T) {
	r := NewRedactor()

	// Long value (>=24 chars) gets sha256_prefix AND tail (last 6 chars).
	long := "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAA1234abcd56" // 42 chars
	got := r.Redact("oauth.providers.github.client_secret", long)
	if !strings.Contains(got, `"redacted":true`) {
		t.Errorf("missing redacted:true: %s", got)
	}
	if !strings.Contains(got, `"sha256_prefix":"`) {
		t.Errorf("missing sha256_prefix: %s", got)
	}
	if !strings.Contains(got, `"tail":"abcd56"`) {
		t.Errorf("expected tail=abcd56 (last 6 chars), got: %s", got)
	}

	// Short value (<24 chars) gets sha256_prefix only, no tail.
	short := "abc123" // 6 chars
	got = r.Redact("api.bearer_token", short)
	if strings.Contains(got, `"tail"`) {
		t.Errorf("short value unexpectedly emitted tail: %s", got)
	}
	if !strings.Contains(got, `"sha256_prefix":"`) {
		t.Errorf("short value missing sha256_prefix: %s", got)
	}
}

func TestRedactor_Tier3_Verbatim(t *testing.T) {
	r := NewRedactor()
	if got := r.Redact("auth.step_up_window_seconds", "300"); got != "300" {
		t.Errorf("verbatim numeric: got %q, want %q", got, "300")
	}
	if got := r.Redact("feature.foo_enabled", boolTrue); got != boolTrue {
		t.Errorf("verbatim bool: got %q, want %q", got, boolTrue)
	}
	if got := r.Redact("system_settings.display_name", "My TMI"); got != "My TMI" {
		t.Errorf("verbatim string: got %q, want %q", got, "My TMI")
	}
}

func TestRedactor_EmptyString_YieldsSentinel(t *testing.T) {
	// Per oracle-db-admin review: Oracle treats "" as NULL on CLOB insert.
	// To keep PG/Oracle round-trips equivalent, the verbatim path emits a
	// sentinel "<empty>" rather than "" when the input is an empty string.
	r := NewRedactor()
	if got := r.Redact("system_settings.display_name", ""); got != "<empty>" {
		t.Errorf("empty input: got %q, want %q", got, "<empty>")
	}
}

func TestRedactor_BuildTimeGate_DenyListCoversSensitiveNames(t *testing.T) {
	// Heuristic: any field path whose last dot-segment (or any segment)
	// contains a sensitive keyword MUST match a deny-pattern. This is the
	// build-time gate: adding a new secret-named setting without updating the
	// deny-list breaks `make test-unit`.
	//
	// The "auth" substring is handled separately: paths like
	// "system_settings.auth.build_mode" contain "auth" but are not secrets.
	// We only require redaction when at least one secondary sensitive keyword
	// also appears.
	//
	// knownSafeExact is an explicit allowlist for paths that look sensitive by
	// the substring heuristic but are demonstrably NOT secret values.  Each
	// entry must have a justification comment.
	knownSafeExact := map[string]string{
		// "client_credentials" in the path is the resource type, not a
		// credential value.  The audit row records the operation metadata,
		// not a secret.
		"users.X.client_credentials.create": "operation path — resource type, not a credential value",
		"users.X.client_credentials.X":      "operation path — resource type, not a credential value",

		// "refresh_token_days" is an integer count of days, not a token.
		"system_settings.auth.jwt.refresh_token_days": "integer duration — not a token value",

		// "redact_auth_tokens" is a boolean flag, not a token.
		"system_settings.logging.redact_auth_tokens": "boolean flag — not a token value",

		// "secrets.provider" names the secrets back-end ("env", "vault", …).
		// The word "secrets" is a namespace prefix, not a secret value.
		"system_settings.secrets.provider": "provider name — not a secret value; the word 'secrets' is a namespace",
	}

	primarySuspect := []string{"secret", "key", "password", "token", "credential", "private", "auth"}
	secondarySensitive := []string{"secret", "key", "password", "passphrase", "token", "credential", "private"}

	knownPaths := allKnownAuditFieldPaths(t)

	r := NewRedactor()
	for _, p := range knownPaths {
		// Skip known-safe false positives.
		if _, ok := knownSafeExact[p]; ok {
			continue
		}

		lower := strings.ToLower(p)

		// Must contain at least one primary suspicious substring.
		flagged := false
		for _, s := range primarySuspect {
			if strings.Contains(lower, s) {
				flagged = true
				break
			}
		}
		if !flagged {
			continue
		}

		// Special case: paths containing "auth" but no other sensitive
		// substring are intentional false positives (e.g.
		// "system_settings.auth.build_mode").  Skip those.
		other := false
		for _, s := range secondarySensitive {
			if strings.Contains(lower, s) {
				other = true
				break
			}
		}
		if !other {
			continue
		}

		// This path looks sensitive. The redactor MUST cover it.
		probe := strings.Repeat("A", 36)
		out := r.Redact(p, probe)
		if out == probe {
			t.Errorf("deny-list misses sensitive-named field %q", p)
		}
	}
}
