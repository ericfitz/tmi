package errcode

import (
	"regexp"
	"testing"
)

func TestSetsAreDisjointAndSnakeCase(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	seen := map[Code]string{}
	for name, set := range map[string][]Code{"rest": REST(), "details": Details()} {
		for _, c := range set {
			if !re.MatchString(string(c)) {
				t.Errorf("%s: %q is not snake_case", name, c)
			}
			if prev, dup := seen[c]; dup {
				t.Errorf("%q in both %s and %s", c, prev, name)
			}
			seen[c] = name
		}
	}
	if len(REST()) != 23 {
		t.Errorf("REST has %d codes, want 23", len(REST()))
	}
	if !IsProtocol(ServerError) || !IsREST(ServerError) {
		t.Error("server_error must be in both tiers")
	}
	if IsREST(InvalidRequest) {
		t.Error("invalid_request is protocol-only")
	}
}

func TestForStatusAlwaysReturnsRESTCode(t *testing.T) {
	for status := 400; status < 600; status++ {
		if c := ForStatus(status); !IsREST(c) {
			t.Errorf("ForStatus(%d) = %q, not a REST code", status, c)
		}
	}
	if ForStatus(404) != NotFound || ForStatus(418) != InvalidInput || ForStatus(502) != ServerError {
		t.Error("unexpected ForStatus mapping")
	}
}

// TestRESTLegacyExceptionsAreExactlyTwo pins the only non-Tier-1 members of the
// REST enum: domain codes kept at top level because tmi-ux branches on them.
func TestRESTLegacyExceptionsAreExactlyTwo(t *testing.T) {
	legacy := map[Code]bool{DetailFeatureNotAvailable: true, DetailContentTokenProviderNotConfigured: true}
	tier1 := 0
	for _, c := range REST() {
		if !legacy[c] {
			tier1++
		}
	}
	if tier1 != 21 {
		t.Errorf("REST has %d non-legacy codes, want 23", tier1)
	}
	for c := range legacy {
		if !IsREST(c) {
			t.Errorf("%q must be in the REST enum", c)
		}
	}
}
