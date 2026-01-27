package workflows

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestSettingsCRUD covers the following OpenAPI operations:
// - GET /config (getClientConfig) - public endpoint
// - GET /admin/settings (listSystemSettings) - admin only
// - GET /admin/settings/{key} (getSystemSetting) - admin only
// - PUT /admin/settings/{key} (updateSystemSetting) - admin only
// - DELETE /admin/settings/{key} (deleteSystemSetting) - admin only
// - POST /admin/settings/migrate (migrateSystemSettings) - admin only
//
// Total: 6 operations
func TestSettingsCRUD(t *testing.T) {
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

	t.Run("GetPublicConfig_Unauthenticated", func(t *testing.T) {
		// Create unauthenticated client
		client, err := framework.NewUnauthenticatedClient(serverURL)
		framework.AssertNoError(t, err, "Failed to create unauthenticated client")

		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/config",
		})
		framework.AssertNoError(t, err, "Failed to get public config")
		framework.AssertStatusOK(t, resp)

		// Parse response
		var config map[string]interface{}
		err = json.Unmarshal(resp.Body, &config)
		framework.AssertNoError(t, err, "Failed to parse config response")

		// Verify expected sections exist
		if _, ok := config["features"]; !ok {
			t.Error("Expected 'features' section in config")
		}
		if _, ok := config["limits"]; !ok {
			t.Error("Expected 'limits' section in config")
		}

		// Verify cache headers
		if cacheControl := resp.Headers.Get("Cache-Control"); cacheControl != "public, max-age=300" {
			t.Errorf("Expected Cache-Control 'public, max-age=300', got '%s'", cacheControl)
		}

		t.Log("✓ Public config endpoint accessible without authentication")
	})

	// Authenticate as admin user for admin endpoints
	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Check if user is admin (first user auto-promotion or configured admin)
	resp, err := client.Do(framework.Request{
		Method: "GET",
		Path:   "/me",
	})
	framework.AssertNoError(t, err, "Failed to get user profile")

	var me map[string]interface{}
	err = json.Unmarshal(resp.Body, &me)
	framework.AssertNoError(t, err, "Failed to parse /me response")

	isAdmin, _ := me["is_admin"].(bool)
	if !isAdmin {
		t.Log("User is not admin - skipping admin-only settings tests")
		t.Log("Note: Configure user as admin or enable auto_promote_first_user in test config")
		return
	}

	t.Run("ListSystemSettings", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/settings",
		})
		framework.AssertNoError(t, err, "Failed to list system settings")
		framework.AssertStatusOK(t, resp)

		// Parse response as array
		var settings []map[string]interface{}
		err = json.Unmarshal(resp.Body, &settings)
		framework.AssertNoError(t, err, "Failed to parse settings list")

		// Should have default settings seeded
		if len(settings) == 0 {
			t.Log("Warning: No default settings found - they may not have been seeded")
		} else {
			t.Logf("✓ Listed %d system settings", len(settings))
		}

		// Verify setting structure
		for _, setting := range settings {
			if _, ok := setting["key"]; !ok {
				t.Error("Setting missing 'key' field")
			}
			if _, ok := setting["value"]; !ok {
				t.Error("Setting missing 'value' field")
			}
			if _, ok := setting["type"]; !ok {
				t.Error("Setting missing 'type' field")
			}
		}
	})

	testKey := "test.integration.setting"

	t.Run("CreateSystemSetting", func(t *testing.T) {
		body := map[string]interface{}{
			"value":       "100",
			"type":        "int",
			"description": "Integration test setting",
		}

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/admin/settings/" + testKey,
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to create system setting")
		framework.AssertStatusOK(t, resp)

		// Verify response
		var setting map[string]interface{}
		err = json.Unmarshal(resp.Body, &setting)
		framework.AssertNoError(t, err, "Failed to parse setting response")

		if setting["key"] != testKey {
			t.Errorf("Expected key '%s', got '%v'", testKey, setting["key"])
		}
		if setting["value"] != "100" {
			t.Errorf("Expected value '100', got '%v'", setting["value"])
		}
		if setting["type"] != "int" {
			t.Errorf("Expected type 'int', got '%v'", setting["type"])
		}

		t.Logf("✓ Created system setting: %s", testKey)
	})

	t.Run("GetSystemSetting", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/settings/" + testKey,
		})
		framework.AssertNoError(t, err, "Failed to get system setting")
		framework.AssertStatusOK(t, resp)

		var setting map[string]interface{}
		err = json.Unmarshal(resp.Body, &setting)
		framework.AssertNoError(t, err, "Failed to parse setting response")

		if setting["key"] != testKey {
			t.Errorf("Expected key '%s', got '%v'", testKey, setting["key"])
		}

		t.Logf("✓ Retrieved system setting: %s", testKey)
	})

	t.Run("UpdateSystemSetting", func(t *testing.T) {
		body := map[string]interface{}{
			"value":       "200",
			"type":        "int",
			"description": "Updated integration test setting",
		}

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/admin/settings/" + testKey,
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to update system setting")
		framework.AssertStatusOK(t, resp)

		var setting map[string]interface{}
		err = json.Unmarshal(resp.Body, &setting)
		framework.AssertNoError(t, err, "Failed to parse setting response")

		if setting["value"] != "200" {
			t.Errorf("Expected updated value '200', got '%v'", setting["value"])
		}

		t.Log("✓ Updated system setting")
	})

	t.Run("GetSystemSetting_NotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/settings/nonexistent.setting.key",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ Correctly returns 404 for nonexistent setting")
	})

	t.Run("UpdateSystemSetting_InvalidType", func(t *testing.T) {
		body := map[string]interface{}{
			"value": "not-an-int",
			"type":  "int",
		}

		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/admin/settings/" + testKey,
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusBadRequest(t, resp)

		t.Log("✓ Correctly rejects invalid type value")
	})

	t.Run("DeleteSystemSetting", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/settings/" + testKey,
		})
		framework.AssertNoError(t, err, "Failed to delete system setting")
		framework.AssertStatusNoContent(t, resp)

		// Verify deletion
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/settings/" + testKey,
		})
		framework.AssertNoError(t, err, "Failed to get deleted setting")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ Deleted system setting")
	})

	t.Run("DeleteSystemSetting_NotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/settings/nonexistent.setting",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusNotFound(t, resp)

		t.Log("✓ Correctly returns 404 when deleting nonexistent setting")
	})

	t.Run("MigrateSystemSettings_Default", func(t *testing.T) {
		// Migrate settings without overwrite (default behavior)
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/settings/migrate",
		})
		framework.AssertNoError(t, err, "Failed to migrate settings")
		framework.AssertStatusOK(t, resp)

		// Parse response
		var result map[string]interface{}
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse migrate response")

		// Verify response structure
		if _, ok := result["migrated"]; !ok {
			t.Error("Expected 'migrated' field in response")
		}
		if _, ok := result["skipped"]; !ok {
			t.Error("Expected 'skipped' field in response")
		}
		if _, ok := result["settings"]; !ok {
			t.Error("Expected 'settings' field in response")
		}

		migratedCount := int(result["migrated"].(float64))
		skippedCount := int(result["skipped"].(float64))
		t.Logf("✓ Migrated settings: %d migrated, %d skipped", migratedCount, skippedCount)
	})

	t.Run("MigrateSystemSettings_WithOverwrite", func(t *testing.T) {
		// Migrate settings with overwrite=true
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/settings/migrate?overwrite=true",
		})
		framework.AssertNoError(t, err, "Failed to migrate settings with overwrite")
		framework.AssertStatusOK(t, resp)

		// Parse response
		var result map[string]interface{}
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse migrate response")

		// With overwrite=true, skipped should be 0
		skippedCount := int(result["skipped"].(float64))
		if skippedCount != 0 {
			t.Errorf("Expected 0 skipped with overwrite=true, got %d", skippedCount)
		}

		t.Log("✓ Migrated settings with overwrite=true")
	})
}

// TestSettingsNonAdminDenied verifies non-admin users cannot access admin settings endpoints
func TestSettingsNonAdminDenied(t *testing.T) {
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

	// Connect to test database to ensure there are existing admins
	// This prevents auto-promotion of our test user
	db, err := framework.NewTestDatabase()
	if err != nil {
		t.Skip("Database not available for non-admin test")
	}
	defer db.Close()

	// Check if there are existing admins
	count, err := db.CountRows("administrators")
	if err != nil {
		t.Skipf("Failed to count administrators: %v", err)
	}
	if count == 0 {
		t.Skip("No existing admins - test user would be auto-promoted")
	}

	// Authenticate as a new user (should not be admin since admins already exist)
	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Verify user is NOT admin
	resp, err := client.Do(framework.Request{
		Method: "GET",
		Path:   "/me",
	})
	framework.AssertNoError(t, err, "Failed to get user profile")

	var me map[string]interface{}
	err = json.Unmarshal(resp.Body, &me)
	framework.AssertNoError(t, err, "Failed to parse /me response")

	isAdmin, _ := me["is_admin"].(bool)
	if isAdmin {
		t.Skip("User is admin - cannot test non-admin denial")
	}

	t.Run("ListSystemSettings_Forbidden", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/settings",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusForbidden(t, resp)

		t.Log("✓ Non-admin correctly denied access to list settings")
	})

	t.Run("GetSystemSetting_Forbidden", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/settings/any.key",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusForbidden(t, resp)

		t.Log("✓ Non-admin correctly denied access to get setting")
	})

	t.Run("UpdateSystemSetting_Forbidden", func(t *testing.T) {
		body := map[string]interface{}{
			"value": "test",
			"type":  "string",
		}
		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/admin/settings/any.key",
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusForbidden(t, resp)

		t.Log("✓ Non-admin correctly denied access to update setting")
	})

	t.Run("DeleteSystemSetting_Forbidden", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/admin/settings/any.key",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusForbidden(t, resp)

		t.Log("✓ Non-admin correctly denied access to delete setting")
	})

	t.Run("MigrateSystemSettings_Forbidden", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/admin/settings/migrate",
		})
		framework.AssertNoError(t, err, "Failed to make request")
		framework.AssertStatusForbidden(t, resp)

		t.Log("✓ Non-admin correctly denied access to migrate settings")
	})
}
