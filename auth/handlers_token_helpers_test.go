package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEmptySubjectError_BodyShape(t *testing.T) {
	body, _ := emptySubjectError("github", "alice@example.com")

	errCode, ok := body["error"].(string)
	if !ok || errCode != "provider_response_invalid" {
		t.Errorf("error code = %v, want \"provider_response_invalid\"", body["error"])
	}
	desc, ok := body["error_description"].(string)
	if !ok || !strings.Contains(desc, "incomplete profile data") {
		t.Errorf("error_description = %v, want to mention incomplete profile data", body["error_description"])
	}
	// The user-facing body must NOT leak internal terms — operators get the
	// detail in logs, end users get a generic message via the API.
	if strings.Contains(desc, "provider_user_id") || strings.Contains(desc, "subject_claim") || strings.Contains(desc, "OAUTH_PROVIDERS") {
		t.Errorf("error_description leaked internal terms: %s", desc)
	}
}

func TestEmptySubjectError_LogMessage(t *testing.T) {
	_, msg := emptySubjectError("badprovider", "bob@example.com")

	// The log message must include the provider ID, the env var to set, and
	// the issue reference for operators trying to fix the config.
	for _, want := range []string{"badprovider", "BADPROVIDER", "subject_claim", "OAUTH_PROVIDERS_BADPROVIDER_USERINFO_CLAIMS_SUBJECT_CLAIM", "#288"} {
		if !strings.Contains(msg, want) {
			t.Errorf("log message missing %q; got: %s", want, msg)
		}
	}

	// Per #294, the default subject-claim path must be reported live from
	// DefaultClaimMappings rather than hardcoded; assert the message
	// includes whatever the live default currently is, in the form
	// `subject_claim_path_default=<value>`.
	wantDefault := "subject_claim_path_default=" + DefaultClaimMappings["subject_claim"]
	if !strings.Contains(msg, wantDefault) {
		t.Errorf("log message missing live default %q; got: %s", wantDefault, msg)
	}
}

// TestEmptySubjectError_LogMessage_TracksDefaultMappingChanges guards against
// the log message drifting back into hardcoded literals (#294). If
// DefaultClaimMappings["subject_claim"] is changed, the log must reflect the
// new value automatically.
func TestEmptySubjectError_LogMessage_TracksDefaultMappingChanges(t *testing.T) {
	original := DefaultClaimMappings["subject_claim"]
	t.Cleanup(func() { DefaultClaimMappings["subject_claim"] = original })

	DefaultClaimMappings["subject_claim"] = "user_id" // hypothetical future change
	_, msg := emptySubjectError("p", "u@x")
	if !strings.Contains(msg, "subject_claim_path_default=user_id") {
		t.Errorf("log message did not pick up updated default; got: %s", msg)
	}
	if strings.Contains(msg, "subject_claim_path_default=sub") {
		t.Errorf("log message still references stale literal `sub`; got: %s", msg)
	}
}

// leakySentinel is an error string designed to contain every category of
// detail we forbid in client-facing OAuth response bodies (#295): an internal
// IP, a private hostname, a stack-trace fragment, and a library-internal
// term. If any of these substrings show up in a response body, the helper is
// leaking and the test fails loud.
const leakySentinel = "dial tcp 10.0.0.1:443: connection refused while contacting graph.internal.example: panic: runtime error: invalid memory address [github.com/some-lib/internals.func1+0xab]"

func bodiesUnderTest(t *testing.T) []gin.H {
	t.Helper()
	leaky := errors.New(leakySentinel)
	bodies := []gin.H{}

	body, _ := codeExchangeError("github", "abcdefghij", leaky)
	bodies = append(bodies, body)
	body, _ = userInfoFetchError("github", leaky)
	bodies = append(bodies, body)
	body, _ = userPersistError("github", leaky)
	bodies = append(bodies, body)
	body, _ = tokenIssuanceError("alice@example.com", leaky)
	bodies = append(bodies, body)
	body, _ = codeVerifierFormatError(leaky)
	bodies = append(bodies, body)
	body, _ = refreshTokenError(leaky)
	bodies = append(bodies, body)
	return bodies
}

// TestErrorHelpers_BodiesDoNotLeakInternals is the load-bearing assertion for
// #295: even when the upstream error contains internal IPs, hostnames, and
// stack-trace fragments, none of those substrings may appear in any field of
// the response body. The detail is for the server log only.
func TestErrorHelpers_BodiesDoNotLeakInternals(t *testing.T) {
	leakyTerms := []string{
		"10.0.0.1",               // internal IP
		"graph.internal.example", // internal hostname
		"panic:",                 // stack-trace fragment
		"github.com/some-lib",    // library internals
		"connection refused",     // raw transport error
		"runtime error",          // Go runtime detail
	}
	for _, body := range bodiesUnderTest(t) {
		// Concatenate every string field for substring checks; a body is a
		// flat gin.H, so this works without recursion.
		var combined strings.Builder
		for _, v := range body {
			if s, ok := v.(string); ok {
				combined.WriteString(s)
				combined.WriteString("\n")
			}
		}
		got := combined.String()
		for _, term := range leakyTerms {
			if strings.Contains(got, term) {
				t.Errorf("response body leaks %q: %s", term, got)
			}
		}
	}
}

// TestErrorHelpers_BodiesUseOAuthErrorCodes verifies that every helper
// returns a body shaped per RFC 6749 §5.2 (`error` + `error_description`).
// Extension codes (`provider_unreachable`, `provider_response_invalid`) are
// fine alongside the spec codes.
func TestErrorHelpers_BodiesUseOAuthErrorCodes(t *testing.T) {
	allowedCodes := map[string]bool{
		// Spec codes (RFC 6749 §5.2).
		"invalid_request": true,
		"invalid_client":  true,
		"invalid_grant":   true,
		"server_error":    true,
		// TMI extension codes.
		"provider_unreachable":      true,
		"provider_response_invalid": true,
		"account_conflict":          true,
		"email_not_verified":        true,
	}
	for _, body := range bodiesUnderTest(t) {
		code, ok := body["error"].(string)
		if !ok || code == "" {
			t.Errorf("body missing string `error` field: %v", body)
			continue
		}
		if !allowedCodes[code] {
			t.Errorf("body uses unexpected error code %q (extend allowedCodes if intentional)", code)
		}
		desc, ok := body["error_description"].(string)
		if !ok || desc == "" {
			t.Errorf("body missing string `error_description` field: %v", body)
		}
	}
}

// TestErrorHelpers_LogMessagesContainErrDetail confirms the inverse: the log
// message MUST retain the raw err so operators can diagnose. If we ever stop
// logging the underlying cause, debugging becomes impossible.
func TestErrorHelpers_LogMessagesContainErrDetail(t *testing.T) {
	leaky := errors.New(leakySentinel)
	cases := []struct {
		name string
		msg  string
	}{
		{"codeExchangeError", mustMsg(codeExchangeError("github", "abcdefghij", leaky))},
		{"userInfoFetchError", mustMsg(userInfoFetchError("github", leaky))},
		{"userPersistError", mustMsg(userPersistError("github", leaky))},
		{"tokenIssuanceError", mustMsg(tokenIssuanceError("alice@example.com", leaky))},
		{"codeVerifierFormatError", mustMsg(codeVerifierFormatError(leaky))},
		{"refreshTokenError", mustMsg(refreshTokenError(leaky))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.msg, leakySentinel) {
				t.Errorf("log message must retain raw err detail; got: %s", tc.msg)
			}
		})
	}
}

func mustMsg(_ gin.H, msg string) string { return msg }
