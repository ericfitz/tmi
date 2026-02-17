package tier3_edge_cases

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestCascadeDeletion verifies that deleting a threat model also deletes
// all its sub-resources (threats, diagrams, documents, assets).
//
// Covers:
// - DELETE /threat_models/{id} cascades to sub-resources
// - GET on deleted sub-resources returns 404
func TestCascadeDeletion(t *testing.T) {
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

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client")

	var tmID string
	var threatIDs []string
	var diagramID string
	var documentID string
	var assetID string

	t.Run("Setup_CreateThreatModelWithSubResources", func(t *testing.T) {
		// Create a threat model
		tmFixture := framework.NewThreatModelFixture().
			WithName("Cascade Deletion Test TM").
			WithDescription("Will be deleted to test cascade behavior")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   tmFixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID = framework.ExtractID(t, resp, "id")
		t.Logf("Created threat model: %s", tmID)

		// Create 2 threats
		for i, name := range []string{"Threat Alpha", "Threat Beta"} {
			threatFixture := map[string]interface{}{
				"name":        name,
				"description": fmt.Sprintf("Test threat %d for cascade deletion", i+1),
				"severity":    "Medium",
				"status":      "Open",
			}
			resp, err = client.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/threats", tmID),
				Body:   threatFixture,
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Failed to create threat %d", i+1))
			framework.AssertStatusCreated(t, resp)
			id := framework.ExtractID(t, resp, "id")
			threatIDs = append(threatIDs, id)
			t.Logf("Created threat %d: %s", i+1, id)
		}

		// Create 1 diagram
		diagramFixture := map[string]interface{}{
			"name": "Cascade Test Diagram",
			"type": "DFD-1.0.0",
		}
		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/diagrams", tmID),
			Body:   diagramFixture,
		})
		framework.AssertNoError(t, err, "Failed to create diagram")
		framework.AssertStatusCreated(t, resp)
		diagramID = framework.ExtractID(t, resp, "id")
		t.Logf("Created diagram: %s", diagramID)

		// Create 1 document
		docFixture := map[string]interface{}{
			"name":         "Cascade Test Document",
			"content_type": "text/plain",
			"content":      "Test document content for cascade deletion",
		}
		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/documents", tmID),
			Body:   docFixture,
		})
		framework.AssertNoError(t, err, "Failed to create document")
		framework.AssertStatusCreated(t, resp)
		documentID = framework.ExtractID(t, resp, "id")
		t.Logf("Created document: %s", documentID)

		// Create 1 asset
		assetFixture := map[string]interface{}{
			"name":        "Cascade Test Asset",
			"description": "Test asset for cascade deletion",
			"type":        "data",
		}
		resp, err = client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/assets", tmID),
			Body:   assetFixture,
		})
		framework.AssertNoError(t, err, "Failed to create asset")
		framework.AssertStatusCreated(t, resp)
		assetID = framework.ExtractID(t, resp, "id")
		t.Logf("Created asset: %s", assetID)
	})

	t.Run("Verify_SubResourcesExist", func(t *testing.T) {
		// Verify each threat exists
		for i, id := range threatIDs {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/threat_models/%s/threats/%s", tmID, id),
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Failed to get threat %d", i+1))
			framework.AssertStatusOK(t, resp)
		}
		t.Logf("Verified %d threats exist", len(threatIDs))

		// Verify diagram exists
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/diagrams/%s", tmID, diagramID),
		})
		framework.AssertNoError(t, err, "Failed to get diagram")
		framework.AssertStatusOK(t, resp)
		t.Logf("Verified diagram exists")

		// Verify document exists
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", tmID, documentID),
		})
		framework.AssertNoError(t, err, "Failed to get document")
		framework.AssertStatusOK(t, resp)
		t.Logf("Verified document exists")

		// Verify asset exists
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/assets/%s", tmID, assetID),
		})
		framework.AssertNoError(t, err, "Failed to get asset")
		framework.AssertStatusOK(t, resp)
		t.Logf("Verified asset exists")
	})

	t.Run("Delete_ThreatModel", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Deleted threat model: %s", tmID)
	})

	t.Run("Verify_ThreatModelDeleted", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Request failed")
		framework.AssertStatusNotFound(t, resp)
		t.Logf("Threat model correctly returns 404")
	})

	t.Run("Verify_SubResourcesDeleted", func(t *testing.T) {
		// Verify threats are gone (parent TM is 404, so sub-resources should also 404)
		for i, id := range threatIDs {
			resp, err := client.Do(framework.Request{
				Method: "GET",
				Path:   fmt.Sprintf("/threat_models/%s/threats/%s", tmID, id),
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Request failed for threat %d", i+1))
			if resp.StatusCode != 404 && resp.StatusCode != 403 {
				t.Errorf("Expected 404 or 403 for cascaded threat %d, got %d", i+1, resp.StatusCode)
			}
		}
		t.Logf("Verified %d threats are gone after cascade deletion", len(threatIDs))

		// Verify diagram is gone
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/diagrams/%s", tmID, diagramID),
		})
		framework.AssertNoError(t, err, "Request failed for diagram")
		if resp.StatusCode != 404 && resp.StatusCode != 403 {
			t.Errorf("Expected 404 or 403 for cascaded diagram, got %d", resp.StatusCode)
		}
		t.Logf("Verified diagram is gone after cascade deletion")

		// Verify document is gone
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/documents/%s", tmID, documentID),
		})
		framework.AssertNoError(t, err, "Request failed for document")
		if resp.StatusCode != 404 && resp.StatusCode != 403 {
			t.Errorf("Expected 404 or 403 for cascaded document, got %d", resp.StatusCode)
		}
		t.Logf("Verified document is gone after cascade deletion")

		// Verify asset is gone
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/assets/%s", tmID, assetID),
		})
		framework.AssertNoError(t, err, "Request failed for asset")
		if resp.StatusCode != 404 && resp.StatusCode != 403 {
			t.Errorf("Expected 404 or 403 for cascaded asset, got %d", resp.StatusCode)
		}
		t.Logf("Verified asset is gone after cascade deletion")
	})

	t.Log("All cascade deletion tests completed successfully")
}

// TestTimestampIntegrity verifies that created_at and modified_at timestamps
// are properly managed on create, update, and patch operations.
//
// Covers:
// - Timestamps set on creation
// - modified_at updates on PUT but created_at stays the same
// - modified_at updates on PATCH but created_at stays the same
func TestTimestampIntegrity(t *testing.T) {
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

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client")

	var tmID string
	var originalCreatedAt string
	var originalModifiedAt string

	t.Run("Create_VerifyTimestamps", func(t *testing.T) {
		tmFixture := framework.NewThreatModelFixture().
			WithName("Timestamp Test TM").
			WithDescription("Testing timestamp integrity")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   tmFixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID = framework.ExtractID(t, resp, "id")

		// Verify timestamps exist and are valid
		createdAt := framework.AssertValidTimestamp(t, resp, "created_at")
		modifiedAt := framework.AssertValidTimestamp(t, resp, "modified_at")

		// Save as strings for later comparison
		var data map[string]interface{}
		err = json.Unmarshal(resp.Body, &data)
		framework.AssertNoError(t, err, "Failed to parse response")
		originalCreatedAt = data["created_at"].(string)
		originalModifiedAt = data["modified_at"].(string)

		// Verify timestamps are recent (within 60 seconds of now)
		now := time.Now()
		if now.Sub(createdAt) > 60*time.Second {
			t.Errorf("created_at is too old: %v (now: %v)", createdAt, now)
		}
		if now.Sub(modifiedAt) > 60*time.Second {
			t.Errorf("modified_at is too old: %v (now: %v)", modifiedAt, now)
		}

		t.Logf("Created TM %s with timestamps: created_at=%s, modified_at=%s",
			tmID, originalCreatedAt, originalModifiedAt)
	})

	t.Run("Update_VerifyTimestampBehavior", func(t *testing.T) {
		// Wait to ensure timestamps differ
		time.Sleep(1100 * time.Millisecond)

		// PUT update
		resp, err := client.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "Timestamp Test TM",
				"description": "Updated via PUT to test timestamps",
			},
		})
		framework.AssertNoError(t, err, "Failed to update threat model")
		framework.AssertStatusOK(t, resp)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Body, &data)
		framework.AssertNoError(t, err, "Failed to parse response")

		newCreatedAt := data["created_at"].(string)
		newModifiedAt := data["modified_at"].(string)

		// created_at should be unchanged
		if newCreatedAt != originalCreatedAt {
			t.Errorf("created_at changed after PUT: was %s, now %s",
				originalCreatedAt, newCreatedAt)
		}

		// modified_at should be updated (different and later)
		if newModifiedAt == originalModifiedAt {
			t.Errorf("modified_at did not change after PUT: still %s", newModifiedAt)
		}

		// Parse timestamps to verify ordering
		modifiedBefore, _ := time.Parse(time.RFC3339, originalModifiedAt)
		modifiedAfter, _ := time.Parse(time.RFC3339, newModifiedAt)
		framework.AssertTimestampOrder(t, modifiedBefore, modifiedAfter,
			"original modified_at", "updated modified_at")

		originalModifiedAt = newModifiedAt
		t.Logf("After PUT: created_at=%s (unchanged), modified_at=%s (updated)",
			newCreatedAt, newModifiedAt)
	})

	t.Run("Patch_VerifyTimestampBehavior", func(t *testing.T) {
		// Wait to ensure timestamps differ
		time.Sleep(1100 * time.Millisecond)

		// PATCH update
		patchPayload := []map[string]interface{}{
			{
				"op":    "replace",
				"path":  "/description",
				"value": "Patched to test timestamps",
			},
		}
		resp, err := client.Do(framework.Request{
			Method: "PATCH",
			Path:   "/threat_models/" + tmID,
			Body:   patchPayload,
		})
		framework.AssertNoError(t, err, "Failed to patch threat model")
		framework.AssertStatusOK(t, resp)

		var data map[string]interface{}
		err = json.Unmarshal(resp.Body, &data)
		framework.AssertNoError(t, err, "Failed to parse response")

		newCreatedAt := data["created_at"].(string)
		newModifiedAt := data["modified_at"].(string)

		// created_at should still be unchanged from original
		if newCreatedAt != originalCreatedAt {
			t.Errorf("created_at changed after PATCH: was %s, now %s",
				originalCreatedAt, newCreatedAt)
		}

		// modified_at should be updated again
		if newModifiedAt == originalModifiedAt {
			t.Errorf("modified_at did not change after PATCH: still %s", newModifiedAt)
		}

		modifiedBefore, _ := time.Parse(time.RFC3339, originalModifiedAt)
		modifiedAfter, _ := time.Parse(time.RFC3339, newModifiedAt)
		framework.AssertTimestampOrder(t, modifiedBefore, modifiedAfter,
			"PUT modified_at", "PATCH modified_at")

		t.Logf("After PATCH: created_at=%s (unchanged), modified_at=%s (updated again)",
			newCreatedAt, newModifiedAt)
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + tmID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Logf("Cleanup: Deleted threat model %s", tmID)
	})

	t.Log("All timestamp integrity tests completed successfully")
}

// TestProhibitedFieldRejection verifies that server-generated fields
// (id, created_at, modified_at) cannot be overwritten by clients.
//
// Covers:
// - Server ignores client-provided id on creation
// - Server ignores client-provided created_at on creation
// - Server ignores client-provided id on update
// - Server preserves created_at on update even if client tries to change it
func TestProhibitedFieldRejection(t *testing.T) {
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

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create client")

	var tmIDs []string // Track TMs for cleanup

	t.Run("Create_WithProhibitedId", func(t *testing.T) {
		fakeID := "00000000-1111-2222-3333-444444444444"
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body: map[string]interface{}{
				"name":        "Prohibited ID Test",
				"description": "Testing that id cannot be set by client",
				"id":          fakeID,
			},
		})
		framework.AssertNoError(t, err, "Request failed")

		// Should succeed (server ignores the id) or reject it
		if resp.StatusCode == 201 {
			actualID := framework.ExtractID(t, resp, "id")
			tmIDs = append(tmIDs, actualID)
			if actualID == fakeID {
				t.Errorf("Server accepted client-provided id! This is a security issue. Got: %s", actualID)
			} else {
				t.Logf("Server correctly ignored client-provided id. Fake: %s, Actual: %s", fakeID, actualID)
			}
		} else if resp.StatusCode == 400 {
			t.Logf("Server rejected request with prohibited id field (status 400)")
		} else {
			t.Logf("Server returned status %d for prohibited id (acceptable behavior)", resp.StatusCode)
		}
	})

	t.Run("Create_WithTimestampOverride", func(t *testing.T) {
		fakeTimestamp := "2020-01-01T00:00:00Z"
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body: map[string]interface{}{
				"name":        "Prohibited Timestamp Test",
				"description": "Testing that created_at cannot be set by client",
				"created_at":  fakeTimestamp,
			},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode == 201 {
			tmID := framework.ExtractID(t, resp, "id")
			tmIDs = append(tmIDs, tmID)

			var data map[string]interface{}
			err = json.Unmarshal(resp.Body, &data)
			framework.AssertNoError(t, err, "Failed to parse response")

			actualCreatedAt, ok := data["created_at"].(string)
			if ok && actualCreatedAt == fakeTimestamp {
				t.Errorf("Server accepted client-provided created_at! Got: %s", actualCreatedAt)
			} else {
				t.Logf("Server correctly ignored client-provided created_at. Fake: %s, Actual: %s",
					fakeTimestamp, actualCreatedAt)
			}
		} else {
			t.Logf("Server returned status %d for prohibited timestamp (acceptable behavior)", resp.StatusCode)
		}
	})

	t.Run("Update_CannotChangeId", func(t *testing.T) {
		// Create a TM first
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName("ID Immutability Test"),
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		realID := framework.ExtractID(t, resp, "id")
		tmIDs = append(tmIDs, realID)

		// Try to change the id via PUT
		fakeID := "00000000-5555-6666-7777-888888888888"
		resp, err = client.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + realID,
			Body: map[string]interface{}{
				"name":        "ID Immutability Test",
				"description": "Attempting to change the ID",
				"id":          fakeID,
			},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode == 200 {
			// Verify the ID didn't change
			var data map[string]interface{}
			err = json.Unmarshal(resp.Body, &data)
			framework.AssertNoError(t, err, "Failed to parse response")

			actualID, ok := data["id"].(string)
			if ok && actualID == fakeID {
				t.Errorf("Server allowed ID to be changed via PUT! Real: %s, New: %s", realID, actualID)
			} else {
				t.Logf("ID correctly unchanged after PUT. Real: %s", realID)
			}
		} else {
			t.Logf("Server returned status %d for PUT with different id (acceptable)", resp.StatusCode)
		}
	})

	t.Run("Update_CannotChangeCreatedAt", func(t *testing.T) {
		// Create a TM first
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName("CreatedAt Immutability Test"),
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		tmID := framework.ExtractID(t, resp, "id")
		tmIDs = append(tmIDs, tmID)

		var createData map[string]interface{}
		err = json.Unmarshal(resp.Body, &createData)
		framework.AssertNoError(t, err, "Failed to parse create response")
		originalCreatedAt := createData["created_at"].(string)

		// Try to change created_at via PUT
		fakeTimestamp := "2020-01-01T00:00:00Z"
		resp, err = client.Do(framework.Request{
			Method: "PUT",
			Path:   "/threat_models/" + tmID,
			Body: map[string]interface{}{
				"name":        "CreatedAt Immutability Test",
				"description": "Attempting to change created_at",
				"created_at":  fakeTimestamp,
			},
		})
		framework.AssertNoError(t, err, "Request failed")

		if resp.StatusCode == 200 {
			// GET the TM to verify created_at wasn't changed
			resp, err = client.Do(framework.Request{
				Method: "GET",
				Path:   "/threat_models/" + tmID,
			})
			framework.AssertNoError(t, err, "Failed to get threat model")
			framework.AssertStatusOK(t, resp)

			var getData map[string]interface{}
			err = json.Unmarshal(resp.Body, &getData)
			framework.AssertNoError(t, err, "Failed to parse GET response")

			currentCreatedAt, ok := getData["created_at"].(string)
			if ok && currentCreatedAt == fakeTimestamp {
				t.Errorf("Server allowed created_at to be overwritten! Original: %s, Now: %s",
					originalCreatedAt, currentCreatedAt)
			} else {
				t.Logf("created_at correctly preserved. Original: %s, Current: %s",
					originalCreatedAt, currentCreatedAt)
			}
		} else {
			t.Logf("Server returned status %d for PUT with created_at override (acceptable)", resp.StatusCode)
		}
	})

	// Cleanup all created TMs
	t.Run("Cleanup", func(t *testing.T) {
		for _, id := range tmIDs {
			resp, err := client.Do(framework.Request{
				Method: "DELETE",
				Path:   "/threat_models/" + id,
			})
			if err != nil {
				t.Logf("Warning: cleanup failed for TM %s: %v", id, err)
				continue
			}
			if resp.StatusCode != 204 && resp.StatusCode != 404 {
				t.Logf("Warning: unexpected status %d when cleaning up TM %s", resp.StatusCode, id)
			}
		}
		t.Logf("Cleanup: Deleted %d threat models", len(tmIDs))
	})

	t.Log("All prohibited field rejection tests completed successfully")
}
