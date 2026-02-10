package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestOwnershipTransfer covers the following OpenAPI operations:
// - POST /me/transfer (transferCurrentUserOwnership)
// - POST /admin/users/{internal_uuid}/transfer (transferAdminUserOwnership)
//
// Total: 2 operations
func TestOwnershipTransfer(t *testing.T) {
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

	db, err := framework.NewTestDatabase()
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	t.Run("TransferCurrentUserOwnership_Success", func(t *testing.T) {
		// Create two unique users
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

		// Alice creates a threat model
		tmFixture := framework.NewThreatModelFixture().WithName("Transfer Test TM")
		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   tmFixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID := framework.ExtractID(t, resp, "id")
		t.Logf("Alice created threat model: %s", tmID)

		// Look up Bob's internal UUID from the database
		bobEmail := bobID + "@tmi.local"
		bobUUID, err := db.QueryString("SELECT internal_uuid FROM users WHERE email = '" + bobEmail + "'")
		if err != nil || bobUUID == "" {
			t.Fatalf("Failed to look up Bob's internal UUID: %v", err)
		}
		t.Logf("Bob's internal UUID: %s", bobUUID)

		// Alice transfers ownership to Bob
		resp, err = aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   "/me/transfer",
			Body:   map[string]string{"target_user_id": bobUUID},
		})
		framework.AssertNoError(t, err, "Failed to transfer ownership")
		framework.AssertStatusOK(t, resp)

		// Parse response
		var transferResult map[string]interface{}
		err = json.Unmarshal(resp.Body, &transferResult)
		framework.AssertNoError(t, err, "Failed to parse transfer response")

		// Verify response structure
		tmTransferred, ok := transferResult["threat_models_transferred"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected threat_models_transferred in response")
		}
		count, _ := tmTransferred["count"].(float64)
		if count < 1 {
			t.Errorf("Expected at least 1 threat model transferred, got %v", count)
		}
		ids, ok := tmTransferred["threat_model_ids"].([]interface{})
		if !ok || len(ids) == 0 {
			t.Error("Expected threat_model_ids array in response")
		}
		t.Logf("Transferred %v threat models, IDs: %v", count, ids)

		// Verify Bob can access the threat model
		resp, err = bobClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Bob failed to access transferred threat model")
		framework.AssertStatusOK(t, resp)

		// Verify the owner is now Bob
		var tmData map[string]interface{}
		err = json.Unmarshal(resp.Body, &tmData)
		framework.AssertNoError(t, err, "Failed to parse threat model response")
		if owner, ok := tmData["owner"].(map[string]interface{}); ok {
			if ownerEmail, ok := owner["email"].(string); ok {
				if ownerEmail != bobEmail {
					t.Errorf("Expected owner email %s, got %s", bobEmail, ownerEmail)
				}
			}
		}

		// Verify Alice can still write (she should have writer access)
		resp, err = aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Alice failed to access transferred threat model")
		framework.AssertStatusOK(t, resp)

		t.Log("✓ Successfully transferred ownership from Alice to Bob")
	})

	t.Run("TransferCurrentUserOwnership_SelfTransfer", func(t *testing.T) {
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		// Look up own UUID
		userEmail := userID + "@tmi.local"
		userUUID, err := db.QueryString("SELECT internal_uuid FROM users WHERE email = '" + userEmail + "'")
		if err != nil || userUUID == "" {
			t.Fatalf("Failed to look up user UUID: %v", err)
		}

		// Attempt self-transfer
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/transfer",
			Body:   map[string]string{"target_user_id": userUUID},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode != 400 {
			t.Errorf("Expected 400 for self-transfer, got %d", resp.StatusCode)
		}
		t.Log("✓ Self-transfer properly rejected with 400")
	})

	t.Run("TransferCurrentUserOwnership_TargetNotFound", func(t *testing.T) {
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		// Transfer to nonexistent user
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/transfer",
			Body:   map[string]string{"target_user_id": "00000000-0000-0000-0000-999999999999"},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode != 404 {
			t.Errorf("Expected 404 for nonexistent target, got %d", resp.StatusCode)
		}
		t.Log("✓ Nonexistent target properly rejected with 404")
	})

	t.Run("TransferCurrentUserOwnership_Unauthorized", func(t *testing.T) {
		client, err := framework.NewClient(serverURL, nil)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/me/transfer",
			Body:   map[string]string{"target_user_id": "00000000-0000-0000-0000-000000000001"},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode != 401 {
			t.Errorf("Expected 401 Unauthorized without token, got %d", resp.StatusCode)
		}
		t.Log("✓ Unauthorized access properly rejected")
	})

	t.Run("TransferAdminUserOwnership_NonAdmin", func(t *testing.T) {
		// Regular user should get 403 on admin endpoint
		userID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		framework.AssertNoError(t, err, "Authentication failed")

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/users/00000000-0000-0000-0000-000000000001/transfer",
			Body:   map[string]string{"target_user_id": "00000000-0000-0000-0000-000000000002"},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode != 403 {
			t.Errorf("Expected 403 Forbidden for non-admin, got %d", resp.StatusCode)
		}
		t.Log("✓ Non-admin access properly rejected with 403")
	})

	t.Log("✓ All ownership transfer tests completed successfully")
}
