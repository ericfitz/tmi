package auth

import (
	"context"
	"testing"
)

// stubRuntimeReader is a controllable RuntimeConfigReader for tests.
type stubRuntimeReader struct {
	allowList       []string
	allowListExists bool
	allowListErr    error
	samlEnabled     bool
	callbackURL     string
}

func (s *stubRuntimeReader) GetClientCallbackAllowList(_ context.Context) ([]string, bool, error) {
	return s.allowList, s.allowListExists, s.allowListErr
}
func (s *stubRuntimeReader) IsSAMLEnabled(_ context.Context) bool {
	return s.samlEnabled
}
func (s *stubRuntimeReader) GetOAuthCallbackURL(_ context.Context) string {
	return s.callbackURL
}

func TestHandlers_clientCallbackAllowList_PrefersRuntimeReader(t *testing.T) {
	h := &Handlers{
		config: Config{OAuth: OAuthConfig{ClientCallbackAllowList: []string{"http://yaml-fallback/"}}},
	}

	// Without a reader wired, the YAML snapshot is returned.
	if got := h.clientCallbackAllowList(context.Background()); len(got) != 1 || got[0] != "http://yaml-fallback/" {
		t.Errorf("nil reader: got %#v, want YAML snapshot", got)
	}

	// With a reader reporting exists=true and a list, the reader wins.
	h.SetRuntimeConfigReader(&stubRuntimeReader{
		allowList:       []string{"http://db-source/"},
		allowListExists: true,
	})
	if got := h.clientCallbackAllowList(context.Background()); len(got) != 1 || got[0] != "http://db-source/" {
		t.Errorf("reader present: got %#v, want DB source", got)
	}

	// With reader reporting exists=false (no DB row), fall back to YAML.
	h.SetRuntimeConfigReader(&stubRuntimeReader{allowListExists: false})
	if got := h.clientCallbackAllowList(context.Background()); len(got) != 1 || got[0] != "http://yaml-fallback/" {
		t.Errorf("reader exists=false: got %#v, want YAML fallback", got)
	}
}

// TestHandlers_clientCallbackAllowList_FailClosedOnCorruptRow pins the
// security contract: a DB row that exists but is unusable (corrupt JSON,
// read error, decryption failure) MUST return an empty allowlist, NOT the
// YAML snapshot. Silently falling back to YAML would defeat the
// open-redirect mitigation when an operator's allowlist is corrupted.
func TestHandlers_clientCallbackAllowList_FailClosedOnCorruptRow(t *testing.T) {
	h := &Handlers{
		config: Config{OAuth: OAuthConfig{ClientCallbackAllowList: []string{"http://yaml-should-not-leak/"}}},
	}
	h.SetRuntimeConfigReader(&stubRuntimeReader{
		allowListExists: true,
		allowListErr:    errFakeCorrupt,
	})
	got := h.clientCallbackAllowList(context.Background())
	if len(got) != 0 {
		t.Errorf("got %#v, want empty list (fail-closed) — YAML must not leak through a corrupt-row read", got)
	}
}

// errFakeCorrupt is a sentinel for fail-closed tests.
var errFakeCorrupt = newSimpleError("simulated row corruption")

type simpleError struct{ s string }

func (e *simpleError) Error() string { return e.s }

func newSimpleError(s string) error { return &simpleError{s} }

func TestHandlers_samlEnabled_PrefersRuntimeReader(t *testing.T) {
	// YAML says false, reader says true → reader wins.
	h := &Handlers{config: Config{SAML: SAMLConfig{Enabled: false}}}
	h.SetRuntimeConfigReader(&stubRuntimeReader{samlEnabled: true})
	if !h.samlEnabled(context.Background()) {
		t.Error("reader=true should win over YAML=false")
	}

	// No reader → YAML drives.
	h2 := &Handlers{config: Config{SAML: SAMLConfig{Enabled: true}}}
	if !h2.samlEnabled(context.Background()) {
		t.Error("YAML=true should pass through when no reader is wired")
	}
}

func TestHandlers_oauthCallbackURL_EmptyRuntimeValueFallsBackToYAML(t *testing.T) {
	h := &Handlers{
		config: Config{OAuth: OAuthConfig{CallbackURL: "http://yaml/cb"}},
	}
	// Reader returns empty → YAML wins (so a DB-row-missing dev environment still works).
	h.SetRuntimeConfigReader(&stubRuntimeReader{callbackURL: ""})
	if got := h.oauthCallbackURL(context.Background()); got != "http://yaml/cb" {
		t.Errorf("got %q, want YAML fallback http://yaml/cb", got)
	}
	// Reader returns non-empty → reader wins.
	h.SetRuntimeConfigReader(&stubRuntimeReader{callbackURL: "http://db/cb"})
	if got := h.oauthCallbackURL(context.Background()); got != "http://db/cb" {
		t.Errorf("got %q, want DB source http://db/cb", got)
	}
}
