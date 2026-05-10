package api

import (
	"strings"
	"testing"
)

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
	// Heuristic: any field path containing one of these substrings MUST
	// match a deny-pattern. This is the build-time gate.
	suspectSubstrings := []string{"secret", "key", "password", "token", "credential", "private", "auth"}
	knownPaths := []string{
		"system_settings.oauth.providers.google.client_secret",
		"system_settings.oauth.providers.github.client_secret",
		"system_settings.oauth.providers.microsoft.client_secret",
		"system_settings.saml.idp.signing_key",
		"system_settings.jwt.signing_key",
		"system_settings.encryption.master_key",
		"system_settings.api.bearer_token",
		"system_settings.user.password",
		"system_settings.saml.idp.passphrase",
		"system_settings.auth.step_up_window_seconds", // verbatim — sensitive substring "auth" is intentional false positive
	}

	r := NewRedactor()
	for _, p := range knownPaths {
		lower := strings.ToLower(p)
		flagged := false
		for _, s := range suspectSubstrings {
			if strings.Contains(lower, s) {
				flagged = true
				break
			}
		}
		if !flagged {
			continue
		}
		// Special case: paths containing "auth" but no other sensitive substring
		// should not necessarily redact — "auth.step_up_window_seconds" is
		// a window-length integer, not a secret. Skip if "auth" was the only match.
		// We test this by also checking the OTHER substrings.
		other := false
		for _, s := range []string{"secret", "key", "password", "passphrase", "token", "credential", "private"} {
			if strings.Contains(lower, s) {
				other = true
				break
			}
		}
		if !other {
			continue
		}
		// This path looks sensitive (and not just "auth"). The redactor MUST cover it.
		probe := strings.Repeat("A", 36)
		out := r.Redact(p, probe)
		if out == probe {
			t.Errorf("deny-list misses sensitive-named field %q", p)
		}
	}
}
