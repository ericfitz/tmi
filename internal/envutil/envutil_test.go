package envutil

import (
	"testing"
)

func TestScanPrefixedMap(t *testing.T) {
	prefix := "TEST_SCANPREFIX_CLAIMS_"
	t.Setenv(prefix+"SUBJECT_CLAIM", "sub")
	t.Setenv(prefix+"EMAIL_CLAIM", "email")
	t.Setenv(prefix+"EMAIL_VERIFIED_CLAIM", "email_verified")
	// A var under a sibling prefix must not leak in.
	t.Setenv("TEST_SCANPREFIX_OTHER_X", "nope")

	got := ScanPrefixedMap(prefix)

	want := map[string]string{
		"subject_claim":        "sub",
		"email_claim":          "email",
		"email_verified_claim": "email_verified",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %#v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["x"]; ok {
		t.Errorf("sibling-prefix var leaked into result: %#v", got)
	}
}

func TestScanPrefixedMap_NoMatches(t *testing.T) {
	got := ScanPrefixedMap("TEST_SCANPREFIX_NONE_")
	if got == nil {
		t.Fatal("expected non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %#v", got)
	}
}
