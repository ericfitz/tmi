package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestClassifySAMLProcessError_CrossProviderConflict pins that the /saml/acs
// boundary surfaces a cross-provider email conflict (#465/#469) as a distinct,
// actionable 409 mirroring the OAuth path — both for the bare sentinel and for
// the error as actually wrapped by ProcessSAMLResponse (fmt.Errorf("failed to
// process user: %w", err)), so the errors.Is detection survives the wrapping.
func TestClassifySAMLProcessError_CrossProviderConflict(t *testing.T) {
	cases := map[string]error{
		"bare sentinel":  errCrossProviderConflict,
		"wrapped as ACS": fmt.Errorf("failed to process user: %w", errCrossProviderConflict),
		"double wrapped": fmt.Errorf("outer: %w", fmt.Errorf("failed to process user: %w", errCrossProviderConflict)),
	}
	for name, err := range cases {
		status, code, msg := classifySAMLProcessError(err)
		if status != http.StatusConflict {
			t.Errorf("%s: status = %d, want %d", name, status, http.StatusConflict)
		}
		if code != "account_conflict" {
			t.Errorf("%s: code = %q, want %q", name, code, "account_conflict")
		}
		// The message must be actionable: name neither the secret existence
		// detail nor a generic failure, but tell the user what to do.
		if !strings.Contains(strings.ToLower(msg), "different sign-in provider") ||
			!strings.Contains(strings.ToLower(msg), "link") {
			t.Errorf("%s: msg = %q, want actionable conflict guidance", name, msg)
		}
		if strings.HasPrefix(msg, "Authentication failed") {
			t.Errorf("%s: msg = %q, want the actionable message, not the generic one", name, msg)
		}
	}
}

// TestClassifySAMLProcessError_GenericFailure pins that every non-conflict
// processing error stays a generic, non-disclosing 401.
func TestClassifySAMLProcessError_GenericFailure(t *testing.T) {
	cases := map[string]error{
		"parse failure":      errors.New("failed to parse SAML response: bad signature"),
		"token-gen failure":  fmt.Errorf("failed to generate tokens: %w", errors.New("redis down")),
		"unverified email":   errUnverifiedEmailMatch,
		"extract user error": errors.New("failed to extract user info: missing NameID"),
	}
	for name, err := range cases {
		status, code, msg := classifySAMLProcessError(err)
		if status != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", name, status, http.StatusUnauthorized)
		}
		if code != "saml_error" {
			t.Errorf("%s: code = %q, want %q", name, code, "saml_error")
		}
		if !strings.HasPrefix(msg, "Authentication failed") {
			t.Errorf("%s: msg = %q, want generic 'Authentication failed' prefix", name, msg)
		}
		if code == "account_conflict" {
			t.Errorf("%s: non-conflict error must not be classified as account_conflict", name)
		}
	}
}
