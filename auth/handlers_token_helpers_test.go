package auth

import (
	"strings"
	"testing"
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
}
