package workflows

import (
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestSurveyCreateReturnsStoredTimestampsIntegration verifies that creating a
// survey and a survey response returns the created_at and modified_at the
// database holds, matching a subsequent GET (#1153: the create responses
// echoed GORM-generated timestamps carrying sub-microsecond digits).
func TestSurveyCreateReturnsStoredTimestampsIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// assertCreateMatchesGet compares a create response's timestamps with a GET.
	assertCreateMatchesGet := func(t *testing.T, created map[string]any, path string) {
		t.Helper()
		resp, err := client.Do(framework.Request{Method: "GET", Path: path})
		framework.AssertNoError(t, err, "GET after create failed")
		framework.AssertStatusCode(t, resp, 200)
		fetched := decodeObject(t, resp.Body)
		for _, field := range []string{"created_at", "modified_at"} {
			got, want := parseTimestamp(t, created, field), parseTimestamp(t, fetched, field)
			if !got.Equal(want) {
				t.Fatalf("%s: create response %s %v, GET returns %v", path, field, got, want)
			}
			if got.Nanosecond()%int(time.Microsecond) != 0 {
				t.Fatalf("%s: create response %s %v carries sub-microsecond digits", path, field, got)
			}
		}
	}

	resp, err := client.Do(framework.Request{
		Method: "POST",
		Path:   "/admin/surveys",
		Body: framework.NewSurveyFixture().
			WithName(framework.UniqueName("Survey Timestamps Test")).
			WithVersion(framework.UniqueName("v")).
			WithStatus("active"),
	})
	framework.AssertNoError(t, err, "Failed to create survey")
	framework.AssertStatusCreated(t, resp)
	survey := decodeObject(t, resp.Body)
	surveyID := survey["id"].(string)
	surveyPath := "/admin/surveys/" + surveyID
	assertCreateMatchesGet(t, survey, surveyPath)
	t.Logf("✓ survey create returned stored timestamps")

	resp, err = client.Do(framework.Request{
		Method: "POST",
		Path:   "/intake/survey_responses",
		Body:   framework.NewSurveyResponseFixture(surveyID),
	})
	framework.AssertNoError(t, err, "Failed to create survey response")
	framework.AssertStatusCreated(t, resp)
	response := decodeObject(t, resp.Body)
	responsePath := "/intake/survey_responses/" + response["id"].(string)
	assertCreateMatchesGet(t, response, responsePath)
	t.Logf("✓ survey response create returned stored timestamps")

	// Best-effort cleanup: the response first, so the survey has no dependents.
	for _, path := range []string{responsePath, surveyPath} {
		if resp, err := client.Do(framework.Request{Method: "DELETE", Path: path}); err != nil || resp.StatusCode != 204 {
			t.Logf("cleanup of %s did not return 204 (err=%v)", path, err)
		}
	}
}
