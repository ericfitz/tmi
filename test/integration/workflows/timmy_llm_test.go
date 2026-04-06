package workflows

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTimmyLLM verifies LLM response quality for the Timmy AI assistant.
// It checks grounding (responses reference context), multi-turn conversation,
// and embedding retrieval against a real LLM.
//
// Gated behind INTEGRATION_TESTS=true AND TIMMY_LLM_TESTS=true.
func TestTimmyLLM(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	if os.Getenv("TIMMY_LLM_TESTS") != "true" {
		t.Skip("Skipping Timmy LLM quality test (set TIMMY_LLM_TESTS=true to run)")
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

	// Create an SSE client without schema validation (SSE responses are text/event-stream)
	sseClient, err := framework.NewClient(serverURL, aliceTokens, framework.WithValidation(false))
	framework.AssertNoError(t, err, "Failed to create SSE integration client")

	var (
		threatModelID string
		sessionID     string
	)

	// sendMessage sends a chat message to the current session and returns the
	// assistant's response text. It fails the test if no valid response arrives.
	sendMessage := func(t *testing.T, content string) string {
		t.Helper()
		resp, err := sseClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions/%s/messages", threatModelID, sessionID),
			Body: map[string]any{
				"content": content,
			},
		})
		framework.AssertNoError(t, err, "Failed to send chat message")
		framework.AssertStatusOK(t, resp)

		events := parseSSEBody(resp.Body)
		framework.AssertTrue(t, len(events) > 0, "Expected SSE events in response")

		var messageEnd map[string]any
		framework.AssertTrue(t, findLastSSEEvent(events, "message_end", &messageEnd),
			"Expected message_end SSE event")
		framework.AssertTrue(t, messageEnd != nil, "message_end data must not be nil")

		responseText, ok := messageEnd["content"].(string)
		framework.AssertTrue(t, ok && len(responseText) > 20,
			fmt.Sprintf("message_end content must be > 20 chars, got %q", responseText))

		t.Logf("Assistant response length: %d chars", len(responseText))
		return responseText
	}

	// --- Setup ------------------------------------------------------------------

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
		client.SaveState("threat_model_id", threatModelID)

		t.Logf("Setup: created threat model %s", threatModelID)
	})

	t.Run("Setup_CreateAssets", func(t *testing.T) {
		assets := []struct {
			name        string
			assetType   string
			description string
		}{
			{
				name:        "Customer Database",
				assetType:   "data",
				description: "PostgreSQL database storing customer PII including names, emails, and payment tokens",
			},
			{
				name:        "API Gateway",
				assetType:   "process",
				description: "Kong API gateway handling authentication, rate limiting, and request routing",
			},
		}

		for _, a := range assets {
			resp, err := client.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/assets", threatModelID),
				Body: map[string]any{
					"name":        a.name,
					"type":        a.assetType,
					"description": a.description,
				},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Failed to create asset %q", a.name))
			framework.AssertStatusCreated(t, resp)

			id := framework.ExtractID(t, resp, "id")
			t.Logf("Setup: created asset %q (%s)", a.name, id)
		}
	})

	t.Run("Setup_CreateThreats", func(t *testing.T) {
		threats := []struct {
			name        string
			severity    string
			status      string
			description string
		}{
			{
				name:        "SQL Injection on Customer Database",
				severity:    "High",
				status:      "Open",
				description: "Attacker exploits unsanitized input in search queries to extract customer PII via SQL injection",
			},
			{
				name:        "Broken Authentication on API Gateway",
				severity:    "Critical",
				status:      "Open",
				description: "Weak JWT validation allows token forgery, bypassing API gateway authentication checks",
			},
		}

		for _, th := range threats {
			resp, err := client.Do(framework.Request{
				Method: "POST",
				Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
				Body: map[string]any{
					"name":        th.name,
					"description": th.description,
					"severity":    th.severity,
					"status":      th.status,
				},
			})
			framework.AssertNoError(t, err, fmt.Sprintf("Failed to create threat %q", th.name))
			framework.AssertStatusCreated(t, resp)

			id := framework.ExtractID(t, resp, "id")
			t.Logf("Setup: created threat %q (%s)", th.name, id)
		}
	})

	t.Run("Setup_CreateNote", func(t *testing.T) {
		noteContent := "The e-commerce platform uses a microservices architecture. " +
			"The API Gateway (Kong) sits in front of all services and handles JWT validation. " +
			"Customer data is stored in a PostgreSQL database with row-level security enabled. " +
			"The payment processing service communicates with Stripe via mTLS. " +
			"All inter-service communication uses gRPC with service mesh (Istio). " +
			"The platform processes approximately 50,000 transactions per day."

		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/notes", threatModelID),
			Body: map[string]any{
				"name":          "Architecture Overview",
				"content":       noteContent,
				"timmy_enabled": true,
			},
		})
		framework.AssertNoError(t, err, "Failed to create note")
		framework.AssertStatusCreated(t, resp)

		noteID := framework.ExtractID(t, resp, "id")
		t.Logf("Setup: created note %s", noteID)
	})

	t.Run("Setup_CreateSession", func(t *testing.T) {
		resp, err := sseClient.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/chat/sessions", threatModelID),
			Body:   map[string]any{"title": "LLM Quality Test Session"},
		})
		framework.AssertNoError(t, err, "Failed to create chat session")
		framework.AssertStatusOK(t, resp)

		events := parseSSEBody(resp.Body)
		framework.AssertTrue(t, len(events) > 0, "Expected SSE events in response")

		var sessionCreated map[string]any
		framework.AssertTrue(t, findSSEEvent(events, "session_created", &sessionCreated),
			"Expected session_created SSE event")
		framework.AssertTrue(t, sessionCreated != nil, "session_created data must not be nil")

		idVal, ok := sessionCreated["id"].(string)
		framework.AssertTrue(t, ok && idVal != "", "session_created must contain non-empty id")
		sessionID = idVal

		client.SaveState("session_id", sessionID)
		t.Logf("Setup: created session %s", sessionID)
	})

	// --- LLM Quality Tests ------------------------------------------------------

	t.Run("ResponseReferencesContext", func(t *testing.T) {
		response := sendMessage(t, "What are the main assets in this threat model?")

		lower := strings.ToLower(response)
		mentionsAsset := strings.Contains(lower, "customer database") ||
			strings.Contains(lower, "api gateway") ||
			strings.Contains(lower, "database") ||
			strings.Contains(lower, "gateway")

		framework.AssertTrue(t, mentionsAsset,
			fmt.Sprintf("Response should mention assets from context (customer database, api gateway, etc.), got: %q", response))
	})

	t.Run("ThreatAnalysis", func(t *testing.T) {
		response := sendMessage(t, "What threats have been identified and what are their severities?")

		lower := strings.ToLower(response)
		mentionsThreat := strings.Contains(lower, "sql injection") ||
			strings.Contains(lower, "authentication") ||
			strings.Contains(lower, "injection") ||
			strings.Contains(lower, "jwt")

		framework.AssertTrue(t, mentionsThreat,
			fmt.Sprintf("Response should mention identified threats (SQL injection, authentication, etc.), got: %q", response))
	})

	t.Run("MultiTurnConversation", func(t *testing.T) {
		response1 := sendMessage(t, "Which threat has the highest severity?")
		framework.AssertTrue(t, len(response1) > 20,
			fmt.Sprintf("First multi-turn response must be > 20 chars, got %d", len(response1)))

		response2 := sendMessage(t, "What mitigations would you suggest for that threat?")
		framework.AssertTrue(t, len(response2) > 20,
			fmt.Sprintf("Second multi-turn response must be > 20 chars, got %d", len(response2)))

		t.Logf("Multi-turn: first=%d chars, second=%d chars", len(response1), len(response2))
	})

	t.Run("LongUserMessage", func(t *testing.T) {
		base := "Please provide a detailed security analysis of this threat model, " +
			"including all assets, threats, and any architectural concerns. " +
			"Consider the STRIDE threat model framework in your analysis. "
		padding := strings.Repeat("Consider all aspects of confidentiality, integrity, and availability. ", 20)
		longMessage := base + padding
		// Ensure we're well over 2000 chars
		for len(longMessage) < 2000 {
			longMessage += "Please be thorough in your analysis. "
		}

		response := sendMessage(t, longMessage)
		framework.AssertTrue(t, len(response) > 20,
			fmt.Sprintf("Response to long message must be > 20 chars, got %d", len(response)))

		t.Logf("Long message (%d chars) response: %d chars", len(longMessage), len(response))
	})

	t.Run("EmbeddingRetrieval", func(t *testing.T) {
		response := sendMessage(t,
			"How does the platform handle payment processing and what security measures are in place for it?")

		lower := strings.ToLower(response)
		mentionsPayment := strings.Contains(lower, "stripe") ||
			strings.Contains(lower, "mtls") ||
			strings.Contains(lower, "payment") ||
			strings.Contains(lower, "tls")

		framework.AssertTrue(t, mentionsPayment,
			fmt.Sprintf("Response should mention payment security context (stripe, mtls, payment, tls), got: %q", response))
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

	t.Log("All Timmy LLM quality tests completed successfully")
}
