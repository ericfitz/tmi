package workflows

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestClientCredentialsCRUD covers the following OpenAPI operations:
// - POST /me/client_credentials (createCurrentUserClientCredential)
// - GET /me/client_credentials (listCurrentUserClientCredentials)
// - DELETE /me/client_credentials/{id} (deleteCurrentUserClientCredential)
//
// Total: 3 operations
func TestClientCredentialsCRUD(t *testing.T) {
	// Setup
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Ensure OAuth stub is running
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	// Authenticate as admin: the POST /me/client_credentials handler restricts
	// credential creation to administrators and security reviewers. A fresh
	// random user (UniqueUserID) is neither, so creation would 403 and every
	// downstream CRUD subtest would cascade-fail.
	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Admin authentication failed")

	// Create client
	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	var credentialID string
	var clientID string

	t.Run("ListEmptyCredentials", func(t *testing.T) {
		// Test that listing returns 200 OK with empty array for new user
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list credentials for new user")
		framework.AssertStatusOK(t, resp)

		// Validate response is a wrapped object with pagination
		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		// Should be empty for a fresh user
		if len(response.Credentials) != 0 {
			t.Logf("Note: User already has %d credentials from previous tests", len(response.Credentials))
		}

		t.Log("✓ Empty credential list returns 200 OK with wrapped response")
	})

	t.Run("CreateClientCredential", func(t *testing.T) {
		// Set expiration 1 year from now
		expiresAt := time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339)

		fixture := map[string]interface{}{
			"name":        "Integration Test Credential",
			"description": "Created by integration test suite",
			"expires_at":  expiresAt,
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create client credential")
		framework.AssertStatusCreated(t, resp)

		// Parse response
		var credential map[string]interface{}
		err = json.Unmarshal(resp.Body, &credential)
		framework.AssertNoError(t, err, "Failed to parse credential response")

		// Extract and validate ID
		credentialID = framework.ExtractID(t, resp, "id")
		framework.AssertValidUUID(t, resp, "id")

		// Validate client_id is present
		if cid, ok := credential["client_id"].(string); ok {
			clientID = cid
			if clientID == "" {
				t.Error("client_id is empty")
			}
		} else {
			t.Error("client_id not found in response")
		}

		// Validate client_secret is present (only returned on creation)
		if secret, ok := credential["client_secret"].(string); ok {
			if secret == "" {
				t.Error("client_secret is empty")
			}
		} else {
			t.Error("client_secret not found in response")
		}

		// Validate other fields
		framework.AssertJSONField(t, resp, "name", "Integration Test Credential")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertValidTimestamp(t, resp, "created_at")

		// Save to workflow state
		client.SaveState("credential_id", credentialID)
		client.SaveState("client_id", clientID)

		t.Logf("✓ Created client credential: %s (client_id: %s)", credentialID, clientID)
	})

	t.Run("ListClientCredentials", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list client credentials")
		framework.AssertStatusOK(t, resp)

		// Validate response is a wrapped object with pagination
		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		// Should contain our created credential
		found := false
		for _, cred := range response.Credentials {
			if id, ok := cred["id"].(string); ok && id == credentialID {
				found = true

				// Verify client_secret is NOT returned in list
				if _, hasSecret := cred["client_secret"]; hasSecret {
					t.Error("client_secret should not be returned in list response")
				}

				// Verify other fields are present
				if _, ok := cred["client_id"]; !ok {
					t.Error("client_id should be present in list response")
				}
				if _, ok := cred["name"]; !ok {
					t.Error("name should be present in list response")
				}
				if _, ok := cred["is_active"]; !ok {
					t.Error("is_active should be present in list response")
				}
				break
			}
		}
		if !found {
			t.Errorf("Expected to find credential %s in list", credentialID)
		}

		t.Logf("✓ Listed %d client credentials (total: %d, limit: %d, offset: %d)", len(response.Credentials), response.Total, response.Limit, response.Offset)
	})

	t.Run("CreateSecondCredential", func(t *testing.T) {
		// Create a second credential to test multiple credentials
		fixture := map[string]interface{}{
			"name": "Second Test Credential",
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create second credential")
		framework.AssertStatusCreated(t, resp)

		secondID := framework.ExtractID(t, resp, "id")
		client.SaveState("second_credential_id", secondID)

		t.Logf("✓ Created second credential: %s", secondID)
	})

	t.Run("ListShowsMultipleCredentials", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list credentials")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		if len(response.Credentials) < 2 {
			t.Errorf("Expected at least 2 credentials, got %d", len(response.Credentials))
		}

		t.Logf("✓ Listed %d credentials (multiple)", len(response.Credentials))
	})

	t.Run("DeleteSecondCredential", func(t *testing.T) {
		secondID, err := client.GetStateString("second_credential_id")
		framework.AssertNoError(t, err, "Failed to get second credential ID from state")

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/" + secondID,
		})
		framework.AssertNoError(t, err, "Failed to delete second credential")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("✓ Deleted second credential: %s", secondID)
	})

	t.Run("DeleteClientCredential", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/" + credentialID,
		})
		framework.AssertNoError(t, err, "Failed to delete client credential")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("✓ Deleted client credential: %s", credentialID)
	})

	t.Run("VerifyCredentialDeleted", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Failed to list credentials after deletion")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Credentials []map[string]interface{} `json:"credentials"`
			Total       int                      `json:"total"`
			Limit       int                      `json:"limit"`
			Offset      int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse credentials response")

		// Verify deleted credential is not in list
		for _, cred := range response.Credentials {
			if id, ok := cred["id"].(string); ok && id == credentialID {
				t.Errorf("Deleted credential %s should not be in list", credentialID)
			}
		}

		t.Log("✓ Verified credential was deleted")
	})

	t.Run("DeleteNonExistentCredential", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/00000000-0000-0000-0000-000000000000",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ 404 handling validated for non-existent credential")
	})

	t.Run("CreateCredential_MassAssignmentPrevention", func(t *testing.T) {
		// Try to send extra fields that should be rejected
		fixture := map[string]interface{}{
			"name":           "Mass Assignment Test",
			"organizationId": "attacker-org", // This should be rejected
		}

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		// Should return 400 Bad Request due to unknown field
		if resp.StatusCode != 400 {
			t.Errorf("Expected 400 for mass assignment attempt, got %d", resp.StatusCode)
		}

		t.Log("✓ Mass assignment prevention validated")
	})

	t.Run("Unauthorized_NoToken", func(t *testing.T) {
		// Test without authentication token
		noAuthClient, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := noAuthClient.Do(framework.Request{
			Method: "GET",
			Path:   "/me/client_credentials",
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")

		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized without token, got %d", resp.StatusCode)
		}

		t.Log("✓ Unauthorized access properly rejected")
	})

	t.Log("✓ All client credentials tests completed successfully")
}

// TestClientCredentialsCrossUserIsolation exercises the #367 acceptance
// criterion that user A cannot reach user B's /me/client_credentials. The
// route carries x-tmi-authz: { ownership: "none" } so AuthzMiddleware passes
// any authenticated caller through; the IDOR defense lives in the handler,
// which calls service.Delete(ctx, credID, ownerUUID) and surfaces a 404 if
// the credential isn't owned by the JWT subject.
func TestClientCredentialsCrossUserIsolation(t *testing.T) {
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

	// Step 1: Alice (admin) creates a credential. Only admins can create
	// client credentials per the existing handler restriction; that's fine
	// for this test — the IDOR concern is on DELETE which is open to any
	// authenticated user.
	aliceTokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Alice (admin) authentication failed")
	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")

	expiresAt := time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339)
	createResp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/me/client_credentials",
		Body: map[string]interface{}{
			"name":       "Cross-User IDOR Test Credential",
			"expires_at": expiresAt,
		},
	})
	framework.AssertNoError(t, err, "Alice failed to create credential")
	framework.AssertStatusCreated(t, createResp)

	credentialID := framework.ExtractID(t, createResp, "id")
	t.Logf("Alice created credential: %s", credentialID)

	defer func() {
		// Best-effort cleanup as Alice (the owner).
		_, _ = aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/me/client_credentials/" + credentialID,
		})
	}()

	// Step 2: Bob (a different user, non-admin) tries to DELETE Alice's
	// credential by its ID. The handler must return 404 because the
	// credential isn't owned by Bob's JWT subject. A 200/204 here would
	// be a critical IDOR bug.
	bobUserID := framework.UniqueUserID()
	bobTokens, err := framework.AuthenticateUser(bobUserID)
	framework.AssertNoError(t, err, "Bob (random user) authentication failed")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	bobDeleteResp, err := bobClient.Do(framework.Request{
		Method: "DELETE",
		Path:   "/me/client_credentials/" + credentialID,
	})
	framework.AssertNoError(t, err, "Bob's request to DELETE Alice's credential errored unexpectedly")

	if bobDeleteResp.StatusCode != 404 {
		t.Fatalf("CRITICAL: Bob deleted Alice's credential! Expected 404 (not owned by Bob), got %d. Body: %s",
			bobDeleteResp.StatusCode, string(bobDeleteResp.Body))
	}
	t.Log("✓ Bob cannot DELETE Alice's credential (404 — handler scopes by owner UUID)")

	// Step 3: Confirm Alice can still see her credential (Bob's failed
	// attempt didn't somehow corrupt state).
	listResp, err := aliceClient.Do(framework.Request{
		Method: "GET",
		Path:   "/me/client_credentials",
	})
	framework.AssertNoError(t, err, "Alice failed to list credentials")
	framework.AssertStatusOK(t, listResp)

	var listResponse struct {
		Credentials []map[string]interface{} `json:"credentials"`
	}
	if err := json.Unmarshal(listResp.Body, &listResponse); err != nil {
		t.Fatalf("Failed to parse Alice's credential list: %v", err)
	}

	found := false
	for _, cred := range listResponse.Credentials {
		if id, _ := cred["id"].(string); id == credentialID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Alice's credential %s missing from her own list after Bob's failed delete", credentialID)
	}
	t.Logf("✓ Alice's credential survived Bob's IDOR attempt")

	// Step 4: Bob's own credential list is empty (or at least does not
	// include Alice's credential ID). This is a secondary check that the
	// list-scope is also subject-bound.
	bobListResp, err := bobClient.Do(framework.Request{
		Method: "GET",
		Path:   "/me/client_credentials",
	})
	framework.AssertNoError(t, err, "Bob failed to list his own credentials")
	framework.AssertStatusOK(t, bobListResp)

	var bobList struct {
		Credentials []map[string]interface{} `json:"credentials"`
	}
	if err := json.Unmarshal(bobListResp.Body, &bobList); err != nil {
		t.Fatalf("Failed to parse Bob's credential list: %v", err)
	}
	for _, cred := range bobList.Credentials {
		if id, _ := cred["id"].(string); id == credentialID {
			t.Fatalf("CRITICAL: Bob's GET /me/client_credentials includes Alice's credential %s", id)
		}
	}
	t.Log("✓ Bob's credential list does not include Alice's credential (subject-scoped query)")
}

// TestClientCredentialsDeniedOnAdminRoutes pins the #399 invariant end to
// end: a service-account (client-credentials) token is categorically denied
// on /admin/* with 403, even when the credential's owner is an
// administrator. Covers all five /admin/settings operations plus one write
// per remaining /admin sub-area.
func TestClientCredentialsDeniedOnAdminRoutes(t *testing.T) {
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

	// Owner is an administrator (framework.AuthenticateAdmin authenticates with
	// login hint "test-admin" and promotes that user) — the denial must hold anyway.
	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Admin authentication failed")
	adminClient, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// 1. Create a client credential as the admin.
	createResp, err := adminClient.Do(framework.Request{
		Method: "POST",
		Path:   "/me/client_credentials",
		Body: map[string]interface{}{
			"name": "cc-admin-denial-test",
		},
	})
	framework.AssertNoError(t, err, "create client credential")
	if createResp.StatusCode != 201 {
		t.Fatalf("create credential: got %d, want 201: %s", createResp.StatusCode, string(createResp.Body))
	}
	var cred struct {
		ID           string `json:"id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	framework.AssertNoError(t, json.Unmarshal(createResp.Body, &cred), "parse credential")
	t.Cleanup(func() {
		_, _ = adminClient.Do(framework.Request{Method: "DELETE", Path: "/me/client_credentials/" + cred.ID})
	})

	// 2. Exchange for a service-account access token.
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", cred.ClientID)
	form.Set("client_secret", cred.ClientSecret)
	resp, err := http.Post(serverURL+"/oauth2/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	framework.AssertNoError(t, err, "POST /oauth2/token")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("token exchange: got %d, want 200: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	framework.AssertNoError(t, json.Unmarshal(body, &tok), "parse token response")

	// 3. Every admin call with the CC token must return 403 (not 401, not 404).
	// All request bodies must be schema-valid: OpenAPI request validation runs
	// before the authz middleware, so a malformed body would be rejected with
	// 400 before authz can return 403. Bodies do not need to be semantically
	// valid (e.g., a group that already exists is fine — authz denies first).
	adminCalls := []struct {
		method string
		path   string
		body   map[string]interface{}
	}{
		// all five /admin/settings operations
		{"GET", "/admin/settings", nil},
		{"GET", "/admin/settings/test.key", nil},
		// SystemSettingUpdate: field is "type", not "setting_type"
		{"PUT", "/admin/settings/test.key", map[string]interface{}{"value": "x", "type": "string"}},
		{"DELETE", "/admin/settings/test.key", nil},
		{"POST", "/admin/settings/reencrypt", nil},
		// one representative write per remaining sub-area
		// CreateAdminGroupRequest requires both "group_name" (identifier) and "name" (display)
		{"POST", "/admin/groups", map[string]interface{}{"group_name": "cc-denial-test", "name": "CC Denial Test"}},
		{"DELETE", "/admin/users/00000000-0000-0000-0000-000000000000", nil},
		// UserQuotaUpdate requires max_requests_per_minute
		{"PUT", "/admin/quotas/users/00000000-0000-0000-0000-000000000000", map[string]interface{}{"max_requests_per_minute": 60}},
		// Correct path is /admin/webhooks/subscriptions; WebhookSubscriptionInput requires name, url, events
		{"POST", "/admin/webhooks/subscriptions", map[string]interface{}{
			"name":   "cc-denial-test-webhook",
			"url":    "https://example.com/webhook",
			"events": []string{"threat_model.created"},
		}},
		// SurveyBase (via SurveyInput allOf) requires name, version, survey_json
		{"POST", "/admin/surveys", map[string]interface{}{
			"name":    "cc-denial-test-survey",
			"version": "v1.0-cc-test",
			"survey_json": map[string]interface{}{
				"pages": []interface{}{
					map[string]interface{}{
						"name":     "page1",
						"elements": []interface{}{},
					},
				},
			},
		}},
	}

	// Disable OpenAPI response validation: these endpoints do declare 403 Error
	// response schemas, but this test asserts status codes only — response-body
	// validation adds nothing here and a validator error from any spec/body
	// mismatch would mask the status-code assertion we actually care about.
	ccClient, err := framework.NewClient(serverURL,
		&framework.OAuthTokens{AccessToken: tok.AccessToken},
		framework.WithValidation(false))
	framework.AssertNoError(t, err, "create CC client")

	for _, call := range adminCalls {
		call := call // capture loop variable
		t.Run(call.method+" "+call.path, func(t *testing.T) {
			resp, err := ccClient.Do(framework.Request{
				Method: call.method,
				Path:   call.path,
				Body:   call.body,
			})
			framework.AssertNoError(t, err, "request failed")
			if resp.StatusCode != 403 {
				t.Errorf("got %d, want 403 (categorical service-account denial); body: %s",
					resp.StatusCode, string(resp.Body))
			}
		})
	}
}
