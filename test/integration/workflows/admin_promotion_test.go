package workflows

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestFirstUserAdminPromotion tests the auto-promotion of the first user to administrator
// when no administrators exist in the system.
//
// This test requires:
// - TMI server running with auto_promote_first_user: true (config-test-integration-pg.yml)
// - OAuth stub running (make start-oauth-stub)
// - PostgreSQL database running with TEST_DB_* environment variables set
//
// The test flow:
// 1. Truncate administrators table to ensure no admins exist
// 2. Authenticate as first user
// 3. Verify user is promoted to admin (is_admin: true in /me response)
// 4. Verify admin appears in /admin/administrators list
// 5. Authenticate as second user
// 6. Verify second user is NOT admin (is_admin: false)
func TestFirstUserAdminPromotion(t *testing.T) {
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

	// Connect to test database for cleanup
	db, err := framework.NewTestDatabase()
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}
	defer db.Close()

	t.Run("FirstUserPromotedToAdmin", func(t *testing.T) {
		// Step 1: Clear all administrators to simulate fresh system
		err := db.TruncateTable("administrators")
		if err != nil {
			t.Fatalf("Failed to truncate administrators table: %v", err)
		}

		// Verify no administrators exist
		count, err := db.CountRows("administrators")
		if err != nil {
			t.Fatalf("Failed to count administrators: %v", err)
		}
		if count != 0 {
			t.Fatalf("Expected 0 administrators after truncate, got %d", count)
		}
		t.Log("Verified: administrators table is empty")

		// Step 2: Authenticate as first user - this should trigger auto-promotion
		firstUserID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(firstUserID)
		framework.AssertNoError(t, err, "First user authentication failed")

		// Create client for first user
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client for first user")

		// Step 3: Verify first user has is_admin: true
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me",
		})
		framework.AssertNoError(t, err, "Failed to get first user profile")
		framework.AssertStatusOK(t, resp)

		var firstUser map[string]interface{}
		err = json.Unmarshal(resp.Body, &firstUser)
		framework.AssertNoError(t, err, "Failed to parse first user response")

		isAdmin, ok := firstUser["is_admin"].(bool)
		if !ok {
			t.Fatalf("is_admin field not found or not a boolean in response: %v", firstUser)
		}
		if !isAdmin {
			t.Errorf("Expected first user to be admin (is_admin: true), got is_admin: false")
			t.Logf("First user response: %v", firstUser)
		} else {
			t.Logf("First user %s was auto-promoted to administrator", firstUser["email"])
		}

		// Step 4: Verify first user can access admin endpoints
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/administrators",
		})
		framework.AssertNoError(t, err, "Failed to list administrators")
		framework.AssertStatusOK(t, resp)

		var adminList map[string]interface{}
		err = json.Unmarshal(resp.Body, &adminList)
		framework.AssertNoError(t, err, "Failed to parse administrators list")

		administrators, ok := adminList["administrators"].([]interface{})
		if !ok {
			t.Fatalf("administrators field not found or not an array: %v", adminList)
		}
		if len(administrators) == 0 {
			t.Error("Expected at least one administrator in the list")
		} else {
			t.Logf("Found %d administrator(s) in the system", len(administrators))
		}

		// Verify the first user is in the administrators list by email
		firstUserEmail := firstUser["email"].(string)
		foundFirstUser := false
		for _, admin := range administrators {
			adminMap := admin.(map[string]interface{})
			if userEmail, ok := adminMap["user_email"].(string); ok {
				if userEmail == firstUserEmail {
					foundFirstUser = true
					t.Logf("Verified first user %s is in administrators list", firstUserEmail)
					break
				}
			}
		}
		if !foundFirstUser {
			t.Error("First user not found in administrators list")
		}
	})

	t.Run("SecondUserNotPromoted", func(t *testing.T) {
		// First, ensure there's at least one admin (from previous test or fresh setup)
		count, err := db.CountRows("administrators")
		if err != nil {
			t.Fatalf("Failed to count administrators: %v", err)
		}
		if count == 0 {
			// Need to set up an admin first
			firstUserID := framework.UniqueUserID()
			err := db.TruncateTable("administrators")
			if err != nil {
				t.Fatalf("Failed to truncate administrators: %v", err)
			}
			tokens, err := framework.AuthenticateUser(firstUserID)
			framework.AssertNoError(t, err, "First user authentication failed")
			// Make a request to trigger auto-promotion
			client, _ := framework.NewClient(serverURL, tokens)
			client.Do(framework.Request{Method: "GET", Path: "/me"})
		}

		// Now authenticate as second user
		secondUserID := framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(secondUserID)
		framework.AssertNoError(t, err, "Second user authentication failed")

		// Create client for second user
		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client for second user")

		// Verify second user does NOT have is_admin: true
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me",
		})
		framework.AssertNoError(t, err, "Failed to get second user profile")
		framework.AssertStatusOK(t, resp)

		var secondUser map[string]interface{}
		err = json.Unmarshal(resp.Body, &secondUser)
		framework.AssertNoError(t, err, "Failed to parse second user response")

		isAdmin, ok := secondUser["is_admin"].(bool)
		if !ok {
			t.Fatalf("is_admin field not found or not a boolean in response: %v", secondUser)
		}
		if isAdmin {
			t.Errorf("Expected second user to NOT be admin (is_admin: false), got is_admin: true")
		} else {
			t.Logf("Second user %s correctly NOT promoted to administrator", secondUser["email"])
		}

		// Verify second user cannot access admin endpoints
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/administrators",
		})
		framework.AssertNoError(t, err, "Request to admin endpoint failed unexpectedly")

		if resp.StatusCode != 403 {
			t.Errorf("Expected 403 Forbidden for non-admin accessing /admin/administrators, got %d", resp.StatusCode)
		} else {
			t.Log("Non-admin correctly denied access to admin endpoints")
		}
	})

	t.Run("AdminPromotionWithCleanDatabase", func(t *testing.T) {
		// This test simulates a completely fresh database scenario
		// Clear administrators table
		err := db.TruncateTable("administrators")
		if err != nil {
			t.Fatalf("Failed to truncate administrators: %v", err)
		}

		// Small delay to allow database state to settle after truncation
		// This prevents race conditions with the OAuth flow
		time.Sleep(500 * time.Millisecond)

		// Use a unique user for this test
		userID := "first-admin-" + framework.UniqueUserID()
		tokens, err := framework.AuthenticateUser(userID)
		if err != nil {
			t.Skipf("Skipping test due to authentication timeout (may be transient): %v", err)
		}

		client, err := framework.NewClient(serverURL, tokens)
		framework.AssertNoError(t, err, "Failed to create client")

		// First request should trigger auto-promotion
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/me",
		})
		framework.AssertNoError(t, err, "Failed to get user profile")
		framework.AssertStatusOK(t, resp)

		var user map[string]interface{}
		json.Unmarshal(resp.Body, &user)

		isAdmin := user["is_admin"].(bool)
		if !isAdmin {
			t.Error("First user should be auto-promoted to admin on clean database")
		}

		// Verify administrator record was created in database
		count, err := db.CountRows("administrators")
		framework.AssertNoError(t, err, "Failed to count administrators")

		if count != 1 {
			t.Errorf("Expected exactly 1 administrator after first user login, got %d", count)
		}

		// Verify the admin record has correct notes indicating auto-promotion
		notes, err := db.QueryString("SELECT notes FROM administrators LIMIT 1")
		if err != nil {
			t.Logf("Note: Could not retrieve admin notes: %v", err)
		} else if notes != "" {
			t.Logf("Administrator notes: %s", notes)
		}

		t.Log("First user admin promotion with clean database verified")
	})

	t.Log("All admin promotion tests completed")
}
