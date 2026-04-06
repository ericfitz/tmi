package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTimmyCRUD covers the following OpenAPI operations:
// - POST /threat_models/{id}/chat/sessions (createChatSession)
// - GET /threat_models/{id}/chat/sessions (listChatSessions)
// - GET /threat_models/{id}/chat/sessions/{sid} (getChatSession)
// - DELETE /threat_models/{id}/chat/sessions/{sid} (deleteChatSession)
// - POST /threat_models/{id}/chat/sessions/{sid}/messages (createChatMessage)
// - GET /threat_models/{id}/chat/sessions/{sid}/messages (listChatMessages)
// - GET /admin/timmy/usage (getAdminTimmyUsage)
// - GET /admin/timmy/status (getAdminTimmyStatus)
//
// Total: 8 operations
func TestTimmyCRUD(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if os.Getenv("TIMMY_INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping Timmy integration test (set TIMMY_INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Ensure OAuth stub is running
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	// Authenticate as Alice (the primary test user)
	aliceID := framework.UniqueUserID()
	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")

	// Create a regular client (non-SSE requests) with schema validation
	client, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// Create an SSE client without schema validation (SSE responses are text/event-stream,
	// not application/json, so the OpenAPI validator cannot validate them)
	sseClient, err := framework.NewClient(serverURL, aliceTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create SSE integration client")

	var (
		threatModelID string
		assetID1      string
		assetID2      string
		sessionID     string   // first (untitled) session
		titledSessID  string   // second session (with title)
		messageID     string   // ID of the user message in CreateMessage
	)

	// --- Setup ------------------------------------------------------------------

	t.Run("Setup_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Timmy CRUD Test Threat Model").
			WithDescription("Container for Timmy chat tests")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)

		threatModelID = framework.ExtractID(t, resp, "id")
		client.SaveState("threat_model_id", threatModelID)

		t.Logf("Setup: created threat model %s", threatModelID)
	})

	t.Run("Setup_CreateAssets", func(t *testing.T) {
		for i, assetName := range []string{"User Database", "Payment Service"} {
			resp, err := client.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/assets", threatModelID),
				Body: map[string]any{
					"name":        assetName,
					"type":        "data",
					"description": fmt.Sprintf("Asset %d for Timmy chat tests", i+1),
				},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Failed to create asset %d", i+1))
			framework.AssertStatusCreated(t, resp)

			id := framework.ExtractID(t, resp, "id")
			if i == 0 {
				assetID1 = id
			} else {
				assetID2 = id
			}
			t.Logf("Setup: created asset %s (%s)", assetName, id)
		}
		framework.AssertTrue(t, assetID1 != "", "assetID1 must be set")
		framework.AssertTrue(t, assetID2 != "", "assetID2 must be set")
	})

	t.Run("Setup_CreateThreat", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
			Body: map[string]any{
				"name":        "SQL Injection via User Input",
				"description": "Attacker injects SQL through unvalidated user fields",
				"severity":    "high",
				"status":      "Open",
			},
		})
		framework.AssertNoError(t, err, "Failed to create threat")
		framework.AssertStatusCreated(t, resp)

		threatID := framework.ExtractID(t, resp, "id")
		t.Logf("Setup: created threat %s", threatID)
	})

	// --- Session CRUD -----------------------------------------------------------

	t.Run("CreateSession", func(t *testing.T) {
		resp, err := sseClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to create chat session")
		framework.AssertStatusOK(t, resp)

		events := parseSSEBody(resp.Body)
		framework.AssertTrue(t, len(events) > 0, "Expected SSE events in response")

		// Verify session_created event
		var sessionCreated map[string]any
		framework.AssertTrue(t, findSSEEvent(events, "session_created", &sessionCreated),
			"Expected session_created SSE event")
		framework.AssertTrue(t, sessionCreated != nil, "session_created data must not be nil")

		idVal, ok := sessionCreated["id"].(string)
		framework.AssertTrue(t, ok && idVal != "", "session_created must contain non-empty id")
		sessionID = idVal

		// Verify threat_model_id
		tmIDVal, ok := sessionCreated["threat_model_id"].(string)
		framework.AssertTrue(t, ok, "session_created must contain threat_model_id")
		framework.AssertEqual(t, threatModelID, tmIDVal, "threat_model_id in session_created")

		// Verify status
		statusVal, ok := sessionCreated["status"].(string)
		framework.AssertTrue(t, ok, "session_created must contain status")
		framework.AssertEqual(t, "active", statusVal, "session status should be active")

		// Verify source_snapshot is present
		_, hasSnapshot := sessionCreated["source_snapshot"]
		framework.AssertTrue(t, hasSnapshot, "session_created must contain source_snapshot")

		// Verify timestamps
		_, hasCreatedAt := sessionCreated["created_at"]
		framework.AssertTrue(t, hasCreatedAt, "session_created must contain created_at")

		// Verify ready event
		framework.AssertTrue(t, findSSEEvent(events, "ready", nil),
			"Expected ready SSE event")

		client.SaveState("session_id", sessionID)
		t.Logf("Created session: %s", sessionID)
	})

	t.Run("CreateSessionWithTitle", func(t *testing.T) {
		resp, err := sseClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			Body:   map[string]any{"title": "My Analysis Session"},
		})
		framework.AssertNoError(t, err, "Failed to create titled chat session")
		framework.AssertStatusOK(t, resp)

		events := parseSSEBody(resp.Body)
		framework.AssertTrue(t, len(events) > 0, "Expected SSE events in response")

		var sessionCreated map[string]any
		framework.AssertTrue(t, findSSEEvent(events, "session_created", &sessionCreated),
			"Expected session_created SSE event")

		idVal, ok := sessionCreated["id"].(string)
		framework.AssertTrue(t, ok && idVal != "", "session_created must contain non-empty id")
		titledSessID = idVal

		// Verify the title was stored
		titleVal, ok := sessionCreated["title"].(string)
		framework.AssertTrue(t, ok, "session_created must contain title")
		framework.AssertEqual(t, "My Analysis Session", titleVal, "session title")

		client.SaveState("titled_session_id", titledSessID)
		t.Logf("Created titled session: %s", titledSessID)
	})

	t.Run("ListSessions", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to list chat sessions")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Sessions []map[string]any `json:"sessions"`
			Total    int                      `json:"total"`
			Limit    int                      `json:"limit"`
			Offset   int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse sessions response")

		framework.AssertTrue(t, response.Total >= 2,
			fmt.Sprintf("Expected at least 2 sessions, total=%d", response.Total))
		framework.AssertTrue(t, len(response.Sessions) >= 2,
			fmt.Sprintf("Expected at least 2 sessions in list, got %d", len(response.Sessions)))

		// Verify both our sessions are present
		found := make(map[string]bool)
		for _, s := range response.Sessions {
			if id, ok := s["id"].(string); ok {
				found[id] = true
			}
		}
		framework.AssertTrue(t, found[sessionID],
			fmt.Sprintf("Expected to find session %s in list", sessionID))
		framework.AssertTrue(t, found[titledSessID],
			fmt.Sprintf("Expected to find titled session %s in list", titledSessID))

		t.Logf("Listed %d sessions (total: %d)", len(response.Sessions), response.Total)
	})

	t.Run("ListSessionsPagination", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			QueryParams: map[string]string{
				"limit":  "1",
				"offset": "0",
			},
		})
		framework.AssertNoError(t, err, "Failed to list chat sessions with pagination")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Sessions []map[string]any `json:"sessions"`
			Total    int                      `json:"total"`
			Limit    int                      `json:"limit"`
			Offset   int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse paginated sessions response")

		framework.AssertEqual(t, 1, len(response.Sessions), "Expected 1 session with limit=1")
		framework.AssertTrue(t, response.Total >= 2,
			fmt.Sprintf("Expected total >= 2, got %d", response.Total))
		framework.AssertEqual(t, 1, response.Limit, "Expected limit=1 in response")
		framework.AssertEqual(t, 0, response.Offset, "Expected offset=0 in response")

		t.Logf("Pagination verified: 1 session returned, total=%d", response.Total)
	})

	t.Run("GetSession", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Failed to get chat session")
		framework.AssertStatusOK(t, resp)

		framework.AssertJSONField(t, resp, "id", sessionID)
		framework.AssertJSONField(t, resp, "threat_model_id", threatModelID)
		framework.AssertJSONField(t, resp, "status", "active")
		framework.AssertJSONFieldExists(t, resp, "created_at")
		framework.AssertJSONFieldExists(t, resp, "source_snapshot")

		t.Logf("Retrieved session: %s", sessionID)
	})

	t.Run("GetSessionNotFound", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/00000000-0000-0000-0000-000000000000", threatModelID),
		})
		framework.AssertNoError(t, err, "Request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Log("Verified 404 for non-existent session")
	})

	// --- Message CRUD -----------------------------------------------------------

	t.Run("CreateMessage", func(t *testing.T) {
		resp, err := sseClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
			Body: map[string]any{
				"content": "Hello, what can you tell me about this threat model?",
			},
		})
		framework.AssertNoError(t, err, "Failed to send chat message")
		framework.AssertStatusOK(t, resp)

		events := parseSSEBody(resp.Body)
		framework.AssertTrue(t, len(events) > 0, "Expected SSE events in response")

		// Verify message_start event
		var messageStart map[string]any
		framework.AssertTrue(t, findSSEEvent(events, "message_start", &messageStart),
			"Expected message_start SSE event")
		framework.AssertTrue(t, messageStart != nil, "message_start data must not be nil")

		idVal, ok := messageStart["id"].(string)
		framework.AssertTrue(t, ok && idVal != "", "message_start must contain non-empty id")
		messageID = idVal

		// Verify message_end event and extract full assistant response
		var messageEnd map[string]any
		framework.AssertTrue(t, findLastSSEEvent(events, "message_end", &messageEnd),
			"Expected message_end SSE event")
		framework.AssertTrue(t, messageEnd != nil, "message_end data must not be nil")

		// The assistant response should be non-empty
		contentVal, ok := messageEnd["content"].(string)
		framework.AssertTrue(t, ok && contentVal != "", "message_end must contain non-empty content")

		// Verify role
		roleVal, ok := messageEnd["role"].(string)
		framework.AssertTrue(t, ok, "message_end must contain role")
		framework.AssertEqual(t, "assistant", roleVal, "message role should be assistant")

		t.Logf("Created message %s, assistant response length: %d chars", messageID, len(contentVal))
	})

	t.Run("ListMessages", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Failed to list chat messages")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Messages []map[string]any `json:"messages"`
			Total    int                      `json:"total"`
			Limit    int                      `json:"limit"`
			Offset   int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse messages response")

		// Should have at least a user message and an assistant reply
		framework.AssertTrue(t, response.Total >= 2,
			fmt.Sprintf("Expected at least 2 messages (user + assistant), total=%d", response.Total))
		framework.AssertTrue(t, len(response.Messages) >= 2,
			fmt.Sprintf("Expected at least 2 messages in list, got %d", len(response.Messages)))

		// Verify sequence ordering: messages should have ascending sequence numbers
		if len(response.Messages) >= 2 {
			seq0, ok0 := response.Messages[0]["sequence"].(float64)
			seq1, ok1 := response.Messages[1]["sequence"].(float64)
			if ok0 && ok1 {
				framework.AssertTrue(t, seq0 < seq1,
					fmt.Sprintf("Messages should be in ascending sequence order: seq[0]=%v, seq[1]=%v", seq0, seq1))
			}
		}

		// Verify we have both user and assistant roles
		roles := make(map[string]bool)
		for _, msg := range response.Messages {
			if role, ok := msg["role"].(string); ok {
				roles[role] = true
			}
		}
		framework.AssertTrue(t, roles["user"], "Expected at least one user message")
		framework.AssertTrue(t, roles["assistant"], "Expected at least one assistant message")

		t.Logf("Listed %d messages (total: %d)", len(response.Messages), response.Total)
	})

	t.Run("ListMessagesPagination", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
			QueryParams: map[string]string{
				"limit":  "1",
				"offset": "0",
			},
		})
		framework.AssertNoError(t, err, "Failed to list messages with pagination")
		framework.AssertStatusOK(t, resp)

		var response struct {
			Messages []map[string]any `json:"messages"`
			Total    int                      `json:"total"`
			Limit    int                      `json:"limit"`
			Offset   int                      `json:"offset"`
		}
		err = json.Unmarshal(resp.Body, &response)
		framework.AssertNoError(t, err, "Failed to parse paginated messages response")

		framework.AssertEqual(t, 1, len(response.Messages), "Expected 1 message with limit=1")
		framework.AssertTrue(t, response.Total >= 2,
			fmt.Sprintf("Expected total >= 2, got %d", response.Total))
		framework.AssertEqual(t, 1, response.Limit, "Expected limit=1 in response")
		framework.AssertEqual(t, 0, response.Offset, "Expected offset=0 in response")

		t.Logf("Pagination verified: 1 message returned, total=%d", response.Total)
	})

	// --- Cross-user isolation ---------------------------------------------------

	t.Run("CrossUserIsolation", func(t *testing.T) {
		// Authenticate as Bob — a different user who has no access to Alice's threat model
		bobID := framework.UniqueUserID()
		bobTokens, err := framework.AuthenticateUser(bobID)
		framework.AssertNoError(t, err, "Bob authentication failed")

		bobClient, err := framework.NewClient(serverURL, bobTokens)
		framework.AssertNoError(t, err, "Failed to create Bob's integration client")

		// Bob attempts to GET Alice's session — should get 403 or 404
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Bob GET session request failed unexpectedly")
		isForbiddenOrNotFound := resp.StatusCode == 403 || resp.StatusCode == 404
		framework.AssertTrue(t, isForbiddenOrNotFound,
			fmt.Sprintf("Bob should get 403 or 404 on Alice's session, got %d", resp.StatusCode))

		// Bob attempts to DELETE Alice's session — should get 403 or 404
		resp, err = bobClient.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Bob DELETE session request failed unexpectedly")
		isForbiddenOrNotFound = resp.StatusCode == 403 || resp.StatusCode == 404
		framework.AssertTrue(t, isForbiddenOrNotFound,
			fmt.Sprintf("Bob should get 403 or 404 when deleting Alice's session, got %d", resp.StatusCode))

		t.Logf("Cross-user isolation verified: Bob cannot access Alice's session")
	})

	// --- Admin endpoints --------------------------------------------------------

	t.Run("AdminUsage", func(t *testing.T) {
		adminTokens, err := framework.AuthenticateAdmin()
		framework.AssertNoError(t, err, "Admin authentication failed")

		adminClient, err := framework.NewClient(serverURL, adminTokens)
		framework.AssertNoError(t, err, "Failed to create admin client")

		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/timmy/usage",
		})
		framework.AssertNoError(t, err, "Failed to get Timmy usage")
		framework.AssertStatusOK(t, resp)

		var usageResp struct {
			Total any   `json:"total"`
			Usage []any `json:"usage"`
		}
		err = json.Unmarshal(resp.Body, &usageResp)
		framework.AssertNoError(t, err, "Failed to parse usage response")

		framework.AssertTrue(t, usageResp.Total != nil, "Expected total field in usage response")
		framework.AssertTrue(t, usageResp.Usage != nil, "Expected usage field in usage response")

		t.Logf("Admin usage response verified (total=%v, %d entries)", usageResp.Total, len(usageResp.Usage))
	})

	t.Run("AdminStatus", func(t *testing.T) {
		adminTokens, err := framework.AuthenticateAdmin()
		framework.AssertNoError(t, err, "Admin authentication failed")

		adminClient, err := framework.NewClient(serverURL, adminTokens)
		framework.AssertNoError(t, err, "Failed to create admin client")

		resp, err := adminClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/timmy/status",
		})
		framework.AssertNoError(t, err, "Failed to get Timmy status")
		framework.AssertStatusOK(t, resp)

		// Verify required status fields
		framework.AssertJSONFieldExists(t, resp, "memory_budget_bytes")
		framework.AssertJSONFieldExists(t, resp, "loaded_indexes")

		t.Log("Admin status response verified")
	})

	t.Run("AdminEndpointsForbiddenForNonAdmin", func(t *testing.T) {
		// Alice is not an admin — she should get 403 on admin endpoints
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/timmy/usage",
		})
		framework.AssertNoError(t, err, "Alice GET usage request failed unexpectedly")
		framework.AssertStatusForbidden(t, resp)

		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/timmy/status",
		})
		framework.AssertNoError(t, err, "Alice GET status request failed unexpectedly")
		framework.AssertStatusForbidden(t, resp)

		t.Log("Non-admin user correctly forbidden from admin Timmy endpoints")
	})

	// --- Delete session ---------------------------------------------------------

	t.Run("DeleteSession", func(t *testing.T) {
		// Delete the titled session
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, titledSessID),
		})
		framework.AssertNoError(t, err, "Failed to delete chat session")
		framework.AssertStatusNoContent(t, resp)

		// Verify session is gone
		resp, err = client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, titledSessID),
		})
		framework.AssertNoError(t, err, "GET after delete request failed unexpectedly")
		framework.AssertStatusNotFound(t, resp)

		t.Logf("Deleted session %s and verified 404", titledSessID)
	})

	// --- Cleanup ----------------------------------------------------------------

	t.Run("Cleanup_DeleteThreatModel", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   "/threat_models/" + threatModelID,
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)

		t.Logf("Cleanup: deleted threat model %s", threatModelID)
	})

	// Suppress unused variable warnings — variables used only in setup subtests
	// are referenced here to satisfy the compiler when subtests are skipped.
	_ = assetID1
	_ = assetID2
	_ = messageID

	t.Log("All Timmy CRUD tests completed successfully")
}
