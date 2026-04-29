# Timmy Integration Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add integration tests for Timmy chat endpoints, covering structural HTTP behavior and LLM quality, running against LM Studio.

**Architecture:** Prerequisite config change adds `LLMBaseURL` and `EmbeddingBaseURL` to `TimmyConfig` so LangChainGo can target LM Studio. Two test files: `timmy_crud_test.go` (structural CRUD, auth, pagination) and `timmy_llm_test.go` (LLM response quality). Both use the existing integration test framework. SSE parsing helper is co-located in the test files.

**Tech Stack:** Go, LangChainGo (openai package), LM Studio (OpenAI-compatible API), existing integration test framework (framework package), miniredis (for unit tests of config changes).

---

### Task 1: Add Base URL Config Fields

**Files:**
- Modify: `internal/config/timmy.go`
- Test: `internal/config/timmy_test.go` (create if needed, or add to existing)

- [ ] **Step 1: Write the failing test for IsConfigured with base URLs**

Check if a test file for timmy config exists:
```bash
ls internal/config/timmy_test.go 2>/dev/null || echo "no test file"
```

Create `internal/config/timmy_test.go`:
```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimmyConfig_IsConfigured(t *testing.T) {
	cfg := TimmyConfig{
		LLMProvider:       "openai",
		LLMModel:          "gpt-4",
		EmbeddingProvider: "openai",
		EmbeddingModel:    "text-embedding-ada-002",
	}
	assert.True(t, cfg.IsConfigured(), "should be configured with all required fields")

	empty := TimmyConfig{}
	assert.False(t, empty.IsConfigured(), "should not be configured with empty fields")
}

func TestTimmyConfig_BaseURLFields(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.Empty(t, cfg.LLMBaseURL, "default LLMBaseURL should be empty")
	assert.Empty(t, cfg.EmbeddingBaseURL, "default EmbeddingBaseURL should be empty")

	cfg.LLMBaseURL = "http://localhost:1234/v1"
	cfg.EmbeddingBaseURL = "http://localhost:1234/v1"
	assert.Equal(t, "http://localhost:1234/v1", cfg.LLMBaseURL)
	assert.Equal(t, "http://localhost:1234/v1", cfg.EmbeddingBaseURL)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestTimmyConfig_BaseURLFields -v`
Expected: FAIL — `cfg.LLMBaseURL` field does not exist

- [ ] **Step 3: Add the base URL fields to TimmyConfig**

In `internal/config/timmy.go`, add two fields after `EmbeddingAPIKey`:

```go
LLMBaseURL        string `yaml:"llm_base_url" env:"TMI_TIMMY_LLM_BASE_URL"`
EmbeddingBaseURL  string `yaml:"embedding_base_url" env:"TMI_TIMMY_EMBEDDING_BASE_URL"`
```

No changes to `DefaultTimmyConfig()` — empty string means "use provider default".

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestTimmyConfig -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/timmy.go internal/config/timmy_test.go
git commit -m "feat(timmy): add LLMBaseURL and EmbeddingBaseURL config fields

Allows pointing the LLM and embedding providers at custom OpenAI-compatible
endpoints like LM Studio.

Refs #214"
```

---

### Task 2: Wire Base URLs into LLM Service

**Files:**
- Modify: `api/timmy_llm_service.go`

- [ ] **Step 1: Modify NewTimmyLLMService to use base URLs**

In `api/timmy_llm_service.go`, change the chat model creation (lines 44-48) to conditionally add `openai.WithBaseURL`:

```go
	// Create chat model using openai.New with functional options
	chatOpts := []openai.Option{
		openai.WithModel(cfg.LLMModel),
		openai.WithToken(cfg.LLMAPIKey),
	}
	if cfg.LLMBaseURL != "" {
		chatOpts = append(chatOpts, openai.WithBaseURL(cfg.LLMBaseURL))
	}
	chatModel, err := openai.New(chatOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM chat model: %w", err)
	}
```

Change the embedding model creation (lines 53-58) similarly:

```go
	// Create a separate LLM client configured for embeddings
	embOpts := []openai.Option{
		openai.WithModel(cfg.EmbeddingModel),
		openai.WithToken(cfg.EmbeddingAPIKey),
		openai.WithEmbeddingModel(cfg.EmbeddingModel),
	}
	if cfg.EmbeddingBaseURL != "" {
		embOpts = append(embOpts, openai.WithBaseURL(cfg.EmbeddingBaseURL))
	}
	embLLM, err := openai.New(embOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding model: %w", err)
	}
```

- [ ] **Step 2: Verify build succeeds**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 3: Run existing unit tests**

Run: `make test-unit`
Expected: All tests pass (existing Timmy unit tests don't create real LLM connections)

- [ ] **Step 4: Commit**

```bash
git add api/timmy_llm_service.go
git commit -m "feat(timmy): wire base URL config into LLM service

Passes LLMBaseURL and EmbeddingBaseURL to LangChainGo's openai.WithBaseURL()
when set, enabling use of OpenAI-compatible providers like LM Studio.

Refs #214"
```

---

### Task 3: Update Development Config

**Files:**
- Modify: `config-development.yml` (gitignored — local only)

- [ ] **Step 1: Add Timmy config block**

Append to the end of `config-development.yml` (before any trailing newline):

```yaml

timmy:
  enabled: true
  llm_provider: openai
  llm_model: google/gemma-4-26b-a4b
  llm_api_key: sk-lm-tW68Afc9:a3yyq0PHPEiSKx2qyJRJ
  llm_base_url: http://localhost:1234/v1
  embedding_provider: openai
  embedding_model: text-embedding-nomic-embed-text-v1.5
  embedding_api_key: sk-lm-tW68Afc9:a3yyq0PHPEiSKx2qyJRJ
  embedding_base_url: http://localhost:1234/v1
```

- [ ] **Step 2: Verify server starts with Timmy enabled**

Run: `make start-dev`
Then check logs for: `Timmy middleware configured (enabled=true, configured=true)`
Run: `curl -s http://localhost:8080/ | jq .version` to verify server is running.

- [ ] **Step 3: No commit** (file is gitignored)

---

### Task 4: SSE Test Helper

**Files:**
- Create: `test/integration/workflows/timmy_sse_helper_test.go`

- [ ] **Step 1: Create the SSE parsing helper**

Create `test/integration/workflows/timmy_sse_helper_test.go`:

```go
package workflows

import (
	"encoding/json"
	"strings"
)

// SSEEvent represents a parsed Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
}

// parseSSEBody parses a raw SSE response body into a slice of events.
// SSE format: "event: <name>\ndata: <json>\n\n"
func parseSSEBody(body []byte) []SSEEvent {
	var events []SSEEvent
	lines := strings.Split(string(body), "\n")

	var currentEvent string
	var currentData string

	for _, line := range lines {
		if strings.HasPrefix(line, "event: ") {
			currentEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent != "" {
			events = append(events, SSEEvent{
				Event: currentEvent,
				Data:  currentData,
			})
			currentEvent = ""
			currentData = ""
		}
	}

	return events
}

// findSSEEvent finds the first event with the given name and unmarshals its data.
func findSSEEvent(events []SSEEvent, eventName string, target any) bool {
	for _, e := range events {
		if e.Event == eventName {
			if target != nil {
				_ = json.Unmarshal([]byte(e.Data), target)
			}
			return true
		}
	}
	return false
}

// findLastSSEEvent finds the last event with the given name and unmarshals its data.
func findLastSSEEvent(events []SSEEvent, eventName string, target any) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Event == eventName {
			if target != nil {
				_ = json.Unmarshal([]byte(events[i].Data), target)
			}
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./test/integration/workflows/...`
Expected: Clean build (the functions are used in later test files)

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/timmy_sse_helper_test.go
git commit -m "test(timmy): add SSE response parsing helper for integration tests

Parses raw SSE response bodies into typed events for Timmy endpoint testing.

Refs #214"
```

---

### Task 5: Structural Integration Tests — Setup and Session CRUD

**Files:**
- Create: `test/integration/workflows/timmy_crud_test.go`

- [ ] **Step 1: Create the test file with setup and session creation tests**

Create `test/integration/workflows/timmy_crud_test.go`:

```go
package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTimmyCRUD covers the Timmy chat session and message CRUD endpoints:
// - POST /threat_models/{id}/chat/sessions (createTimmyChatSession)
// - GET /threat_models/{id}/chat/sessions (listTimmyChatSessions)
// - GET /threat_models/{id}/chat/sessions/{sid} (getTimmyChatSession)
// - DELETE /threat_models/{id}/chat/sessions/{sid} (deleteTimmyChatSession)
// - POST /threat_models/{id}/chat/sessions/{sid}/messages (createTimmyChatMessage)
// - GET /threat_models/{id}/chat/sessions/{sid}/messages (listTimmyChatMessages)
// - GET /admin/timmy/usage (getTimmyUsage)
// - GET /admin/timmy/status (getTimmyStatus)
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

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	// Authenticate primary user
	aliceID := framework.UniqueUserID()
	aliceTokens, err := framework.AuthenticateUser(aliceID)
	framework.AssertNoError(t, err, "Alice authentication failed")

	aliceClient, err := framework.NewClient(serverURL, aliceTokens)
	framework.AssertNoError(t, err, "Failed to create Alice client")

	// --- Setup: Create threat model with entities ---
	var threatModelID string

	t.Run("Setup_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Timmy CRUD Test TM").
			WithDescription("Threat model for Timmy integration tests")

		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		threatModelID = framework.ExtractID(t, resp, "id")
		t.Logf("Created threat model %s", threatModelID)
	})

	t.Run("Setup_CreateAssets", func(t *testing.T) {
		assets := []map[string]any{
			{"name": "Web Application", "type": "process", "description": "Main web application server"},
			{"name": "User Database", "type": "data", "description": "PostgreSQL database storing user records"},
		}
		for _, asset := range assets {
			resp, err := aliceClient.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/assets", threatModelID),
				Body:   asset,
			})
			framework.AssertNoError(t, err, "Failed to create asset")
			framework.AssertStatusCreated(t, resp)
		}
		t.Log("Created 2 assets")
	})

	t.Run("Setup_CreateThreat", func(t *testing.T) {
		threat := map[string]any{
			"name":        "SQL Injection",
			"severity":    "High",
			"status":      "Open",
			"description": "SQL injection vulnerability in user search endpoint",
		}
		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
			Body:   threat,
		})
		framework.AssertNoError(t, err, "Failed to create threat")
		framework.AssertStatusCreated(t, resp)
		t.Log("Created 1 threat")
	})

	// --- Session CRUD tests ---
	var sessionID string

	t.Run("CreateSession", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to create session")

		// SSE response — parse events
		events := parseSSEBody(resp.Body)
		if len(events) == 0 {
			t.Fatal("Expected SSE events in response, got none")
		}

		// Find session_created event
		var session map[string]any
		if !findSSEEvent(events, "session_created", &session) {
			t.Fatal("Expected session_created SSE event")
		}

		// Validate session fields
		id, ok := session["id"].(string)
		if !ok || id == "" {
			t.Fatal("Expected valid session id")
		}
		sessionID = id

		tmID, _ := session["threat_model_id"].(string)
		framework.AssertEqual(t, threatModelID, tmID, "threat_model_id should match")

		status, _ := session["status"].(string)
		framework.AssertEqual(t, "active", status, "status should be active")

		framework.AssertTrue(t, session["created_at"] != nil, "created_at should be present")
		framework.AssertTrue(t, session["modified_at"] != nil, "modified_at should be present")

		// Verify source_snapshot is present (entities were snapshotted)
		snapshot, _ := session["source_snapshot"].([]any)
		framework.AssertTrue(t, len(snapshot) > 0, "source_snapshot should contain snapshotted entities")

		// Verify ready event was sent
		framework.AssertTrue(t, findSSEEvent(events, "ready", nil), "Expected ready SSE event")

		t.Logf("Created session %s with %d source entities", sessionID, len(snapshot))
	})

	var sessionWithTitleID string

	t.Run("CreateSessionWithTitle", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			Body:   map[string]string{"title": "My Analysis Session"},
		})
		framework.AssertNoError(t, err, "Failed to create session with title")

		events := parseSSEBody(resp.Body)
		var session map[string]any
		if !findSSEEvent(events, "session_created", &session) {
			t.Fatal("Expected session_created SSE event")
		}

		title, _ := session["title"].(string)
		framework.AssertEqual(t, "My Analysis Session", title, "title should match")

		id, _ := session["id"].(string)
		sessionWithTitleID = id
		t.Logf("Created titled session %s", sessionWithTitleID)
	})

	t.Run("ListSessions", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to list sessions")
		framework.AssertStatusOK(t, resp)

		var result map[string]any
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse list response")

		sessions, _ := result["sessions"].([]any)
		total, _ := result["total"].(float64)

		framework.AssertTrue(t, len(sessions) >= 2, "Should have at least 2 sessions")
		framework.AssertTrue(t, total >= 2, "Total should be at least 2")
		framework.AssertTrue(t, result["limit"] != nil, "limit should be present")
		framework.AssertTrue(t, result["offset"] != nil, "offset should be present")

		t.Logf("Listed %d sessions (total: %.0f)", len(sessions), total)
	})

	t.Run("ListSessionsPagination", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method:      "GET",
			Path:        fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			QueryParams: map[string]string{"limit": "1", "offset": "0"},
		})
		framework.AssertNoError(t, err, "Failed to list sessions with pagination")
		framework.AssertStatusOK(t, resp)

		var result map[string]any
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse paginated list response")

		sessions, _ := result["sessions"].([]any)
		total, _ := result["total"].(float64)

		framework.AssertEqual(t, 1, len(sessions), "Should return exactly 1 session with limit=1")
		framework.AssertTrue(t, total >= 2, "Total should still reflect all sessions")
	})

	t.Run("GetSession", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Failed to get session")
		framework.AssertStatusOK(t, resp)

		var session map[string]any
		err = json.Unmarshal(resp.Body, &session)
		framework.AssertNoError(t, err, "Failed to parse session response")

		id, _ := session["id"].(string)
		framework.AssertEqual(t, sessionID, id, "Session ID should match")

		status, _ := session["status"].(string)
		framework.AssertEqual(t, "active", status, "Status should be active")
	})

	t.Run("GetSessionNotFound", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/00000000-0000-0000-0000-000000000000", threatModelID),
		})
		framework.AssertNoError(t, err, "Request should not error")
		framework.AssertStatusNotFound(t, resp)
	})

	// --- Message tests ---

	t.Run("CreateMessage", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
			Body:   map[string]string{"content": "Hello, what can you tell me about this threat model?"},
		})
		framework.AssertNoError(t, err, "Failed to create message")

		// Parse SSE response
		events := parseSSEBody(resp.Body)
		if len(events) == 0 {
			t.Fatal("Expected SSE events in response, got none")
		}

		// Verify message_start event
		framework.AssertTrue(t, findSSEEvent(events, "message_start", nil), "Expected message_start event")

		// Find message_end event with assistant response
		var message map[string]any
		if !findLastSSEEvent(events, "message_end", &message) {
			t.Fatal("Expected message_end SSE event with assistant message")
		}

		content, _ := message["content"].(string)
		framework.AssertTrue(t, len(content) > 0, "Assistant message content should not be empty")

		role, _ := message["role"].(string)
		framework.AssertEqual(t, "assistant", role, "Message role should be assistant")

		framework.AssertTrue(t, message["id"] != nil, "Message should have an id")
		framework.AssertTrue(t, message["sequence"] != nil, "Message should have a sequence number")
		framework.AssertTrue(t, message["created_at"] != nil, "Message should have created_at")

		t.Logf("Got assistant response (%d chars)", len(content))
	})

	t.Run("ListMessages", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Failed to list messages")
		framework.AssertStatusOK(t, resp)

		var result map[string]any
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse list messages response")

		messages, _ := result["messages"].([]any)
		total, _ := result["total"].(float64)

		// Should have at least user + assistant messages
		framework.AssertTrue(t, len(messages) >= 2, "Should have at least 2 messages (user + assistant)")
		framework.AssertTrue(t, total >= 2, "Total should be at least 2")

		// Verify sequence ordering
		for i, m := range messages {
			msg, _ := m.(map[string]any)
			seq, _ := msg["sequence"].(float64)
			framework.AssertTrue(t, seq >= 1, fmt.Sprintf("Message %d should have positive sequence", i))
		}

		t.Logf("Listed %d messages", len(messages))
	})

	t.Run("ListMessagesPagination", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method:      "GET",
			Path:        fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
			QueryParams: map[string]string{"limit": "1", "offset": "0"},
		})
		framework.AssertNoError(t, err, "Failed to list messages with pagination")
		framework.AssertStatusOK(t, resp)

		var result map[string]any
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse paginated messages response")

		messages, _ := result["messages"].([]any)
		total, _ := result["total"].(float64)

		framework.AssertEqual(t, 1, len(messages), "Should return exactly 1 message with limit=1")
		framework.AssertTrue(t, total >= 2, "Total should still reflect all messages")
	})

	// --- Cross-user isolation ---

	t.Run("CrossUserIsolation", func(t *testing.T) {
		bobID := framework.UniqueUserID()
		bobTokens, err := framework.AuthenticateUser(bobID)
		framework.AssertNoError(t, err, "Bob authentication failed")

		bobClient, err := framework.NewClient(serverURL, bobTokens)
		framework.AssertNoError(t, err, "Failed to create Bob client")

		// Bob tries to GET Alice's session
		resp, err := bobClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Request should not error")
		// Bob should get 403 (session belongs to Alice) or 404 (threat model access)
		framework.AssertTrue(t, resp.StatusCode == 403 || resp.StatusCode == 404,
			fmt.Sprintf("Expected 403 or 404 for cross-user access, got %d", resp.StatusCode))

		// Bob tries to DELETE Alice's session
		resp, err = bobClient.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionID),
		})
		framework.AssertNoError(t, err, "Request should not error")
		framework.AssertTrue(t, resp.StatusCode == 403 || resp.StatusCode == 404,
			fmt.Sprintf("Expected 403 or 404 for cross-user delete, got %d", resp.StatusCode))

		t.Log("Cross-user isolation verified")
	})

	// --- Admin endpoints ---

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

		var result map[string]any
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse usage response")

		framework.AssertTrue(t, result["total"] != nil, "usage response should have total")
		framework.AssertTrue(t, result["usage"] != nil, "usage response should have usage array")

		t.Log("Admin usage endpoint returned successfully")
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

		var result map[string]any
		err = json.Unmarshal(resp.Body, &result)
		framework.AssertNoError(t, err, "Failed to parse status response")

		framework.AssertTrue(t, result["memory_budget_bytes"] != nil, "status should have memory_budget_bytes")
		framework.AssertTrue(t, result["loaded_indexes"] != nil, "status should have loaded_indexes")

		t.Log("Admin status endpoint returned successfully")
	})

	t.Run("AdminEndpointsForbiddenForNonAdmin", func(t *testing.T) {
		// Alice is not admin
		resp, err := aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/timmy/usage",
		})
		framework.AssertNoError(t, err, "Request should not error")
		framework.AssertStatusForbidden(t, resp)

		resp, err = aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   "/admin/timmy/status",
		})
		framework.AssertNoError(t, err, "Request should not error")
		framework.AssertStatusForbidden(t, resp)

		t.Log("Non-admin correctly rejected from admin endpoints")
	})

	// --- Delete session (last, after message tests) ---

	t.Run("DeleteSession", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionWithTitleID),
		})
		framework.AssertNoError(t, err, "Failed to delete session")
		framework.AssertStatusNoContent(t, resp)

		// Verify session is gone
		resp, err = aliceClient.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s", threatModelID, sessionWithTitleID),
		})
		framework.AssertNoError(t, err, "Request should not error")
		framework.AssertStatusNotFound(t, resp)

		t.Log("Session deleted and confirmed gone")
	})

	// --- Cleanup ---

	t.Run("Cleanup_DeleteThreatModel", func(t *testing.T) {
		resp, err := aliceClient.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Log("Cleaned up threat model")
	})
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./test/integration/workflows/...`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/timmy_crud_test.go
git commit -m "test(timmy): add structural integration tests for chat endpoints

Tests session CRUD, message creation/listing, pagination, cross-user
isolation, and admin endpoint authorization against a live server.

Refs #214"
```

---

### Task 6: LLM Quality Integration Tests

**Files:**
- Create: `test/integration/workflows/timmy_llm_test.go`

- [ ] **Step 1: Create the LLM quality test file**

Create `test/integration/workflows/timmy_llm_test.go`:

```go
package workflows

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTimmyLLM covers LLM response quality — grounding, multi-turn conversation,
// and embedding retrieval. These tests require a real LLM provider (e.g., LM Studio)
// and are gated behind TIMMY_LLM_TESTS=true.
func TestTimmyLLM(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if os.Getenv("TIMMY_LLM_TESTS") != "true" {
		t.Skip("Skipping Timmy LLM test (set TIMMY_LLM_TESTS=true to run)")
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

	// --- Setup: Create threat model with rich content ---
	var threatModelID string

	t.Run("Setup_CreateThreatModel", func(t *testing.T) {
		fixture := framework.NewThreatModelFixture().
			WithName("Timmy LLM Quality Test TM").
			WithDescription("E-commerce platform threat model for LLM quality testing")

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   fixture,
		})
		framework.AssertNoError(t, err, "Failed to create threat model")
		framework.AssertStatusCreated(t, resp)
		threatModelID = framework.ExtractID(t, resp, "id")
		t.Logf("Created threat model %s", threatModelID)
	})

	t.Run("Setup_CreateAssets", func(t *testing.T) {
		assets := []map[string]any{
			{
				"name":        "Customer Database",
				"type":        "data",
				"description": "PostgreSQL database storing customer PII including names, emails, and payment tokens",
			},
			{
				"name":        "API Gateway",
				"type":        "process",
				"description": "Kong API gateway handling authentication, rate limiting, and request routing",
			},
		}
		for _, asset := range assets {
			resp, err := client.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/assets", threatModelID),
				Body:   asset,
			})
			framework.AssertNoError(t, err, "Failed to create asset")
			framework.AssertStatusCreated(t, resp)
		}
		t.Log("Created 2 assets")
	})

	t.Run("Setup_CreateThreats", func(t *testing.T) {
		threats := []map[string]any{
			{
				"name":        "SQL Injection on Customer Database",
				"severity":    "High",
				"status":      "Open",
				"description": "Attacker exploits unsanitized input in search queries to extract customer PII via SQL injection",
			},
			{
				"name":        "Broken Authentication on API Gateway",
				"severity":    "Critical",
				"status":      "Open",
				"description": "Weak JWT validation allows token forgery, bypassing API gateway authentication checks",
			},
		}
		for _, threat := range threats {
			resp, err := client.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
				Body:   threat,
			})
			framework.AssertNoError(t, err, "Failed to create threat")
			framework.AssertStatusCreated(t, resp)
		}
		t.Log("Created 2 threats")
	})

	t.Run("Setup_CreateNote", func(t *testing.T) {
		note := map[string]any{
			"name": "Architecture Overview",
			"content": "The e-commerce platform uses a microservices architecture. " +
				"The API Gateway (Kong) sits in front of all services and handles JWT validation. " +
				"Customer data is stored in a PostgreSQL database with row-level security enabled. " +
				"The payment processing service communicates with Stripe via mTLS. " +
				"All inter-service communication uses gRPC with service mesh (Istio). " +
				"The platform processes approximately 50,000 transactions per day.",
			"timmy_enabled": true,
		}
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/notes", threatModelID),
			Body:   note,
		})
		framework.AssertNoError(t, err, "Failed to create note")
		framework.AssertStatusCreated(t, resp)
		t.Log("Created architecture note")
	})

	// --- Create a session for LLM tests ---
	var sessionID string

	t.Run("Setup_CreateSession", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			Body:   map[string]string{"title": "LLM Quality Test Session"},
		})
		framework.AssertNoError(t, err, "Failed to create session")

		events := parseSSEBody(resp.Body)
		var session map[string]any
		if !findSSEEvent(events, "session_created", &session) {
			t.Fatal("Expected session_created SSE event")
		}

		id, ok := session["id"].(string)
		if !ok || id == "" {
			t.Fatal("Expected valid session id")
		}
		sessionID = id
		t.Logf("Created LLM test session %s", sessionID)
	})

	// Helper to send a message and get the assistant response text
	sendMessage := func(t *testing.T, content string) string {
		t.Helper()
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
			Body:   map[string]string{"content": content},
		})
		framework.AssertNoError(t, err, "Failed to send message")

		events := parseSSEBody(resp.Body)
		var message map[string]any
		if !findLastSSEEvent(events, "message_end", &message) {
			t.Fatal("Expected message_end SSE event")
		}

		response, _ := message["content"].(string)
		framework.AssertTrue(t, len(response) > 20,
			fmt.Sprintf("Response should be substantial, got %d chars", len(response)))
		return response
	}

	// --- LLM Quality Tests ---

	t.Run("ResponseReferencesContext", func(t *testing.T) {
		response := sendMessage(t, "What are the main assets in this threat model?")
		lower := strings.ToLower(response)

		mentionsAsset := strings.Contains(lower, "customer database") ||
			strings.Contains(lower, "api gateway") ||
			strings.Contains(lower, "database") ||
			strings.Contains(lower, "gateway")

		framework.AssertTrue(t, mentionsAsset,
			"Response should reference at least one asset from the threat model")
		t.Logf("Response references context (%d chars)", len(response))
	})

	t.Run("ThreatAnalysis", func(t *testing.T) {
		response := sendMessage(t, "What threats have been identified and what are their severities?")
		lower := strings.ToLower(response)

		mentionsThreat := strings.Contains(lower, "sql injection") ||
			strings.Contains(lower, "authentication") ||
			strings.Contains(lower, "injection") ||
			strings.Contains(lower, "jwt")

		framework.AssertTrue(t, mentionsThreat,
			"Response should reference at least one threat from the threat model")
		t.Logf("Response discusses threats (%d chars)", len(response))
	})

	t.Run("MultiTurnConversation", func(t *testing.T) {
		// First message
		response1 := sendMessage(t, "Which threat has the highest severity?")
		framework.AssertTrue(t, len(response1) > 20, "First response should be substantial")

		// Follow-up referencing previous context
		response2 := sendMessage(t, "What mitigations would you suggest for that threat?")
		framework.AssertTrue(t, len(response2) > 20, "Follow-up response should be substantial")

		t.Logf("Multi-turn: response1=%d chars, response2=%d chars", len(response1), len(response2))
	})

	t.Run("LongUserMessage", func(t *testing.T) {
		longMessage := "Please provide a detailed security analysis of this threat model. " +
			"I would like you to cover the following aspects in your analysis: " +
			"First, review each asset and describe what sensitive data or functionality it handles. " +
			"Second, for each identified threat, explain the attack vector, potential impact, and likelihood. " +
			"Third, suggest specific mitigations for each threat, including both preventive and detective controls. " +
			"Fourth, identify any gaps in the threat model where additional threats should be considered. " +
			"Finally, provide an overall risk assessment with prioritized recommendations. " +
			strings.Repeat("Please be thorough in your analysis and cite specific entities from the threat model. ", 20)

		framework.AssertTrue(t, len(longMessage) > 2000, "Test message should be >2000 chars")

		response := sendMessage(t, longMessage)
		framework.AssertTrue(t, len(response) > 20, "Response to long message should be substantial")
		t.Logf("Long message response: %d chars", len(response))
	})

	t.Run("EmbeddingRetrieval", func(t *testing.T) {
		// Ask about specific content from the note
		response := sendMessage(t, "How does the platform handle payment processing and what security measures are in place for it?")
		lower := strings.ToLower(response)

		// The note mentions Stripe, mTLS, and payment processing
		mentionsNoteContent := strings.Contains(lower, "stripe") ||
			strings.Contains(lower, "mtls") ||
			strings.Contains(lower, "payment") ||
			strings.Contains(lower, "tls")

		framework.AssertTrue(t, mentionsNoteContent,
			"Response should reference payment/security details from the architecture note")
		t.Logf("Embedding retrieval response references note content (%d chars)", len(response))
	})

	// --- Cleanup ---

	t.Run("Cleanup_DeleteThreatModel", func(t *testing.T) {
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s", threatModelID),
		})
		framework.AssertNoError(t, err, "Failed to delete threat model")
		framework.AssertStatusNoContent(t, resp)
		t.Log("Cleaned up threat model")
	})
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./test/integration/workflows/...`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/timmy_llm_test.go
git commit -m "test(timmy): add LLM quality integration tests

Tests response grounding, threat analysis, multi-turn conversation,
long message handling, and embedding retrieval against a real LLM.
Gated behind TIMMY_LLM_TESTS=true.

Refs #214"
```

---

### Task 7: Lint and Full Test Suite Validation

**Files:** None (verification only)

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: 0 issues (or only pre-existing issues in api/api.go)

- [ ] **Step 2: Run unit tests**

Run: `make test-unit`
Expected: All tests pass

- [ ] **Step 3: Build server**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Run structural integration tests**

Prerequisites:
1. LM Studio running at localhost:1234
2. `config-development.yml` has Timmy block (Task 3)
3. `make start-dev` (server running with Timmy enabled)
4. `make start-oauth-stub`

Run: `INTEGRATION_TESTS=true TIMMY_INTEGRATION_TESTS=true make test-integration`

If the integration test make target doesn't pass the env vars through, run directly:
```bash
cd test/integration && INTEGRATION_TESTS=true TIMMY_INTEGRATION_TESTS=true go test -v -run TestTimmyCRUD -timeout 300s ./workflows/
```

Expected: All structural tests pass

- [ ] **Step 5: Run LLM quality tests**

Run:
```bash
cd test/integration && INTEGRATION_TESTS=true TIMMY_LLM_TESTS=true go test -v -run TestTimmyLLM -timeout 600s ./workflows/
```

Expected: All LLM quality tests pass (may take 2-5 minutes depending on LM Studio inference speed)

- [ ] **Step 6: Fix any failures and commit fixes**

If any tests fail, fix root causes and commit:
```bash
git add -A
git commit -m "fix(timmy): resolve integration test issues

Refs #214"
```

---

### Task 8: Push and Update Issue

- [ ] **Step 1: Push to remote**

```bash
git pull --rebase
git push
git status  # Must show "up to date with origin"
```

- [ ] **Step 2: Update issue #214 with integration test status**

Add a comment to issue #214 noting that integration tests are complete, covering the 8 Timmy endpoints with structural and LLM quality tests.
