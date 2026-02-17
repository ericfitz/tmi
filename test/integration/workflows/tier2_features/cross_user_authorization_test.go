package tier2_features

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestCrossUserResourceIsolation verifies that resources created by one user
// are not accessible by another user who has no role on that resource.
//
// Covers:
// - GET /threat_models/{id} (unauthorized user) -> 403
// - PUT /threat_models/{id} (unauthorized user) -> 403
// - DELETE /threat_models/{id} (unauthorized user) -> 403
// - GET /threat_models (listing does not include other users' private TMs)
// - GET /threat_models/{id}/threats/{threat_id} (unauthorized user) -> 403
func TestCrossUserResourceIsolation(t *testing.T) {
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

	aliceID := framework.UniqueUserID()
	bobID := framework.UniqueUserID()

	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Bob authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	// Alice creates a threat model (she is auto-assigned as owner)
	tmFixture := framework.NewThreatModelFixture().WithName("Alice Private TM")
	resp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat model")
	framework.AssertStatusCreated(t, resp)
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat model: %s", tmID)

	// Alice creates a threat under her TM
	threatFixture := map[string]interface{}{
		"name":        "Alice's Private Threat",
		"description": "Should not be visible to Bob",
		"severity":    "High",
		"status":      "Open",
	}
	resp, err = aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   fmt.Sprintf("/threat_models/%s/threats", tmID),
		Body:   threatFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat")
	framework.AssertStatusCreated(t, resp)
	threatID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat: %s", threatID)

	// Bob tries to access Alice's threat model - should be denied
	t.Run("Bob_Cannot_GET_ThreatModel", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied GET on Alice's threat model")
	})

	t.Run("Bob_Cannot_PUT_ThreatModel", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Hacked by Bob",
				"description": "This should not work",
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied PUT on Alice's threat model")
	})

	t.Run("Bob_Cannot_DELETE_ThreatModel", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied DELETE on Alice's threat model")
	})

	t.Run("Bob_List_Does_Not_Include_Alice_TM", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models",
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)

		var listResp struct {
			ThreatModels []map[string]interface{} `json:"threat_models"`
			Total        int                      `json:"total"`
		}
		err = json.Unmarshal(resp.Body, &listResp)
		framework.AssertNoError(t, err, "Failed to parse list response")

		for _, tm := range listResp.ThreatModels {
			if id, ok := tm["id"].(string); ok && id == tmID {
				t.Errorf("Bob's listing should NOT include Alice's private threat model %s", tmID)
			}
		}
		t.Logf("Bob's listing correctly excludes Alice's private threat model")
	})

	t.Run("Bob_Cannot_GET_Threat_SubResource", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", tmID, threatID),
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied GET on Alice's threat sub-resource")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Alice deleted threat model %s", tmID)
	})

	t.Log("All cross-user resource isolation tests completed successfully")
}

// TestRoleBasedAccessControl verifies the reader/writer/owner role hierarchy.
// A reader can GET but not PUT/DELETE. A writer can GET/PUT but not DELETE.
// Only the owner can DELETE.
//
// Covers:
// - Reader: GET allowed, PUT denied, DELETE denied
// - Writer: GET allowed, PUT allowed, DELETE denied
// - Role upgrade from reader to writer
func TestRoleBasedAccessControl(t *testing.T) {
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

	aliceID := framework.UniqueUserID()
	bobID := framework.UniqueUserID()

	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Bob authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	aliceEmail := aliceID + "@tmi.local"
	bobEmail := bobID + "@tmi.local"

	// Alice creates a threat model
	tmFixture := framework.NewThreatModelFixture().WithName("RBAC Test TM")
	resp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat model")
	framework.AssertStatusCreated(t, resp)
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat model: %s", tmID)

	// Alice adds Bob as reader
	t.Run("AddBobAsReader", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "RBAC Test TM",
				"description": "Testing role-based access control",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "reader",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Failed to add Bob as reader")
		framework.AssertStatusOK(t, resp)
		t.Logf("Alice added Bob as reader")
	})

	t.Run("Reader_Can_GET", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)
		framework.AssertJSONField(t, resp, "id", tmID)
		t.Logf("Bob (reader) can GET the threat model")
	})

	t.Run("Reader_Cannot_PUT", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "RBAC Test TM",
				"description": "Bob's unauthorized update attempt",
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob (reader) correctly denied PUT")
	})

	t.Run("Reader_Cannot_DELETE", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob (reader) correctly denied DELETE")
	})

	// Alice upgrades Bob to writer
	t.Run("UpgradeBobToWriter", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "RBAC Test TM",
				"description": "Testing role-based access control",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "writer",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Failed to upgrade Bob to writer")
		framework.AssertStatusOK(t, resp)
		t.Logf("Alice upgraded Bob to writer")
	})

	t.Run("Writer_Can_GET", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)
		t.Logf("Bob (writer) can GET the threat model")
	})

	t.Run("Writer_Can_PUT", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "RBAC Test TM - Updated by Writer",
				"description": "Bob updated this as a writer",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "writer",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)
		t.Logf("Bob (writer) can PUT (update) the threat model")
	})

	t.Run("Writer_Cannot_DELETE", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob (writer) correctly denied DELETE")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Alice deleted threat model %s", tmID)
	})

	t.Log("All role-based access control tests completed successfully")
}

// TestSubResourceAuthorizationInheritance verifies that sub-resources (threats)
// inherit authorization from their parent threat model. A user granted reader
// access to a TM can read threats but cannot create them.
//
// Covers:
// - GET /threat_models/{id}/threats/{threat_id} (reader access via inheritance)
// - GET /threat_models/{id}/threats (reader list access via inheritance)
// - POST /threat_models/{id}/threats (reader denied create)
func TestSubResourceAuthorizationInheritance(t *testing.T) {
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

	aliceID := framework.UniqueUserID()
	bobID := framework.UniqueUserID()

	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Bob authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	aliceEmail := aliceID + "@tmi.local"
	bobEmail := bobID + "@tmi.local"

	// Alice creates a threat model
	tmFixture := framework.NewThreatModelFixture().WithName("Sub-Resource Inheritance TM")
	resp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat model")
	framework.AssertStatusCreated(t, resp)
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat model: %s", tmID)

	// Alice creates a threat under her TM
	threatFixture := map[string]interface{}{
		"name":        "Inheritance Test Threat",
		"description": "Threat for sub-resource authorization inheritance testing",
		"severity":    "Medium",
		"status":      "Open",
	}
	resp, err = aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   fmt.Sprintf("/threat_models/%s/threats", tmID),
		Body:   threatFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat")
	framework.AssertStatusCreated(t, resp)
	threatID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat: %s", threatID)

	// Alice adds Bob as reader on the TM
	t.Run("AddBobAsReader", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Sub-Resource Inheritance TM",
				"description": "Testing sub-resource authorization inheritance",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "reader",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Failed to update authorization")
		framework.AssertStatusOK(t, resp)
		t.Logf("Alice added Bob as reader on threat model")
	})

	t.Run("Reader_Can_GET_Threat", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", tmID, threatID),
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)

		// Verify the threat data is returned correctly
		framework.AssertJSONField(t, resp, "id", threatID)
		framework.AssertJSONField(t, resp, "name", "Inheritance Test Threat")
		t.Logf("Bob (reader) can GET individual threat via inheritance")
	})

	t.Run("Reader_Can_List_Threats", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats", tmID),
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)

		var listResp struct {
			Threats []map[string]interface{} `json:"threats"`
			Total   int                      `json:"total"`
		}
		err = json.Unmarshal(resp.Body, &listResp)
		framework.AssertNoError(t, err, "Failed to parse threats list")

		found := false
		for _, threat := range listResp.Threats {
			if id, ok := threat["id"].(string); ok && id == threatID {
				found = true
				break
			}
		}
		framework.AssertTrue(t, found, "Expected to find threat in list via inherited reader access")
		t.Logf("Bob (reader) can list threats via inheritance")
	})

	t.Run("Reader_Cannot_POST_Threat", func(t *testing.T) {
		newThreat := map[string]interface{}{
			"name":        "Bob's Unauthorized Threat",
			"description": "Should not be created",
			"severity":    "Low",
			"status":      "Open",
		}
		resp, err := bobClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/threats", tmID),
			Body:   newThreat,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob (reader) correctly denied POST (create threat)")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Alice deleted threat model %s", tmID)
	})

	t.Log("All sub-resource authorization inheritance tests completed successfully")
}

// TestEveryonePseudoGroup verifies that the "everyone" pseudo-group correctly
// grants access to all authenticated users. When a threat model includes the
// "everyone" group with reader role, any authenticated user can read it but
// cannot write or delete.
//
// Covers:
// - "everyone" group grants read access to any authenticated user
// - "everyone" group with reader role does not grant write access
// - "everyone" group with reader role does not grant delete access
func TestEveryonePseudoGroup(t *testing.T) {
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

	aliceID := framework.UniqueUserID()
	bobID := framework.UniqueUserID()

	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Bob authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	aliceEmail := aliceID + "@tmi.local"

	// Alice creates a threat model
	tmFixture := framework.NewThreatModelFixture().WithName("Everyone Group TM")
	resp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat model")
	framework.AssertStatusCreated(t, resp)
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat model: %s", tmID)

	// First verify Bob has no access before adding everyone group
	t.Run("Bob_No_Access_Before_Everyone", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied before everyone group added")
	})

	// Alice adds "everyone" pseudo-group as reader
	t.Run("AddEveryoneAsReader", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Everyone Group TM",
				"description": "Testing everyone pseudo-group access",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "group",
						"provider":       "*",
						"provider_id":    "everyone",
						"role":           "reader",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Failed to add everyone group")
		framework.AssertStatusOK(t, resp)
		t.Logf("Alice added everyone group as reader")
	})

	t.Run("Everyone_Bob_Can_GET", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)

		// Verify the data is returned correctly
		framework.AssertJSONField(t, resp, "id", tmID)
		framework.AssertJSONField(t, resp, "name", "Everyone Group TM")
		t.Logf("Bob can GET via everyone group reader access")
	})

	t.Run("Everyone_Bob_Cannot_PUT", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Everyone Group TM",
				"description": "Bob's attempted update via everyone",
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied PUT (everyone is reader only)")
	})

	t.Run("Everyone_Bob_Cannot_DELETE", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied DELETE (everyone is reader only)")
	})

	// Test with a third user to confirm everyone really means everyone
	t.Run("Everyone_ThirdUser_Can_GET", func(t *testing.T) {
		charlieID := framework.UniqueUserID()
		charlieTokens, err := framework.AuthenticateUser(charlieID)
		framework.AssertNoError(t, err, "Charlie authentication failed")

		charlieClient, err := framework.NewClient(serverURL, charlieTokens)
		framework.AssertNoError(t, err, "Failed to create Charlie client")

		resp, err := charlieClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)
		t.Logf("Charlie (third user) can also GET via everyone group")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Alice deleted threat model %s", tmID)
	})

	t.Log("All everyone pseudo-group tests completed successfully")
}

// TestRoleEscalationPrevention verifies that users cannot escalate their own
// permissions, for example a writer cannot grant themselves owner access via PUT.
//
// Covers:
// - Writer cannot modify authorization to escalate own role
// - Writer cannot remove owner from authorization
func TestRoleEscalationPrevention(t *testing.T) {
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

	aliceID := framework.UniqueUserID()
	bobID := framework.UniqueUserID()

	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Bob authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	aliceEmail := aliceID + "@tmi.local"
	bobEmail := bobID + "@tmi.local"

	// Alice creates a TM and adds Bob as writer
	tmFixture := framework.NewThreatModelFixture().WithName("Escalation Prevention TM")
	resp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat model")
	framework.AssertStatusCreated(t, resp)
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat model: %s", tmID)

	// Add Bob as writer
	resp, err = aliceClient.Do(framework.Request{
		Method: "PUT",
		Path:   "/threat_models/" + tmID,
		Body: map[string]interface{}{
			"name":        "Escalation Prevention TM",
			"description": "Testing role escalation prevention",
			"authorization": []map[string]interface{}{
				{
					"principal_type": "user",
					"provider":       "tmi",
					"provider_id":    aliceEmail,
					"role":           "owner",
				},
				{
					"principal_type": "user",
					"provider":       "tmi",
					"provider_id":    bobEmail,
					"role":           "writer",
				},
			},
		},
	})
	framework.AssertNoError(t, err, "Failed to add Bob as writer")
	framework.AssertStatusOK(t, resp)
	t.Logf("Alice added Bob as writer")

	t.Run("Writer_Cannot_Escalate_To_Owner", func(t *testing.T) {
		// Bob (writer) attempts to escalate himself to owner
		resp, err := bobClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Escalation Prevention TM",
				"description": "Bob attempting escalation",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "owner",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		// The server should reject this - either 403 (forbidden) or 400 (bad request)
		// depending on how the authorization change validation is implemented
		if resp.StatusCode != 403 && resp.StatusCode != 400 {
			t.Errorf("Expected 403 or 400 for role escalation attempt, got %d\nBody: %s",
				resp.StatusCode, string(resp.Body))
		}
		t.Logf("Bob (writer) correctly prevented from escalating to owner (status: %d)", resp.StatusCode)
	})

	t.Run("Writer_Cannot_Remove_Owner", func(t *testing.T) {
		// Bob (writer) attempts to remove Alice (owner) from authorization
		resp, err := bobClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Escalation Prevention TM",
				"description": "Bob attempting to remove owner",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "owner",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Request failed")
		// The server should reject this
		if resp.StatusCode != 403 && resp.StatusCode != 400 {
			t.Errorf("Expected 403 or 400 for owner removal attempt, got %d\nBody: %s",
				resp.StatusCode, string(resp.Body))
		}
		t.Logf("Bob (writer) correctly prevented from removing owner (status: %d)", resp.StatusCode)
	})

	// Verify Alice is still owner after escalation attempts
	t.Run("Owner_Unchanged_After_Escalation_Attempts", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)

		var tmData map[string]interface{}
		err = json.Unmarshal(resp.Body, &tmData)
		framework.AssertNoError(t, err, "Failed to parse threat model")

		// Verify authorization still contains Alice as owner
		authz, ok := tmData["authorization"].([]interface{})
		if !ok {
			t.Fatal("Expected authorization array in response")
		}

		aliceStillOwner := false
		for _, entry := range authz {
			if e, ok := entry.(map[string]interface{}); ok {
				if e["provider_id"] == aliceEmail && e["role"] == "owner" {
					aliceStillOwner = true
					break
				}
			}
		}
		framework.AssertTrue(t, aliceStillOwner, "Alice should still be owner after escalation attempts")
		t.Logf("Alice confirmed still owner after Bob's escalation attempts")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Alice deleted threat model %s", tmID)
	})

	t.Log("All role escalation prevention tests completed successfully")
}

// TestAuthorizationRevocation verifies that removing a user's authorization
// immediately prevents further access.
//
// Covers:
// - User can access after being granted a role
// - User is denied access after role is revoked
func TestAuthorizationRevocation(t *testing.T) {
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

	aliceID := framework.UniqueUserID()
	bobID := framework.UniqueUserID()

	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")
	bobTokens, err := framework.AuthenticateUser(bobID)
	framework.AssertNoError(t, err, "Bob authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")
	bobClient, err := framework.NewClient(serverURL, bobTokens)
	framework.AssertNoError(t, err, "Failed to create Bob client")

	aliceEmail := aliceID + "@tmi.local"
	bobEmail := bobID + "@tmi.local"

	// Alice creates a TM
	tmFixture := framework.NewThreatModelFixture().WithName("Revocation Test TM")
	resp, err := aliceClient.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body:   tmFixture,
	})
	framework.AssertNoError(t, err, "Alice failed to create threat model")
	framework.AssertStatusCreated(t, resp)
	tmID := framework.ExtractID(t, resp, "id")
	t.Logf("Alice created threat model: %s", tmID)

	// Grant Bob reader access
	t.Run("GrantAccess", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Revocation Test TM",
				"description": "Testing authorization revocation",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    bobEmail,
						"role":           "reader",
					},
				},
			},
		})
		framework.AssertNoError(t, err, "Failed to grant access")
		framework.AssertStatusOK(t, resp)
		t.Logf("Alice granted Bob reader access")
	})

	// Verify Bob can access
	t.Run("Bob_Can_Access_With_Role", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusOK(t, resp)
		t.Logf("Bob can access with reader role")
	})

	// Revoke Bob's access
	t.Run("RevokeAccess", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Revocation Test TM",
				"description": "Testing authorization revocation",
				"authorization": []map[string]interface{}{
					{
						"principal_type": "user",
						"provider":       "tmi",
						"provider_id":    aliceEmail,
						"role":           "owner",
					},
					// Bob is no longer in the authorization list
				},
			},
		})
		framework.AssertNoError(t, err, "Failed to revoke access")
		framework.AssertStatusOK(t, resp)
		t.Logf("Alice revoked Bob's access")
	})

	// Verify Bob can no longer access
	t.Run("Bob_Denied_After_Revocation", func(t *testing.T) {
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusForbidden(t, resp)
		t.Logf("Bob correctly denied after access revocation")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Alice deleted threat model %s", tmID)
	})

	t.Log("All authorization revocation tests completed successfully")
}
