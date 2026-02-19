package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	oauthStubPort = 8079
	metadataKey   = "cats-test-key"
	metadataValue = "cats-test-value"
	nilUUIDString = "00000000-0000-0000-0000-000000000000"
)

// testDataResults holds the IDs of all created API objects.
type testDataResults struct {
	ThreatModelID         string
	ThreatID              string
	DiagramID             string
	DocumentID            string
	AssetID               string
	NoteID                string
	RepositoryID          string
	WebhookID             string
	AddonID               string
	ClientCredentialID    string
	SurveyID              string
	SurveyResponseID      string
	AdminGroupID          string
	AdminUserInternalUUID string
	AdminUserProvider     string
	AdminUserProviderID   string
	UserProvider          string
	UserProviderID        string
}

// authenticateViaOAuthStub performs OAuth authentication through the oauth-client-callback-stub.
// Returns a JWT access token.
func authenticateViaOAuthStub(serverURL, user, provider string) (string, error) {
	log := slogging.Get()
	oauthStubURL := fmt.Sprintf("http://localhost:%d", oauthStubPort)

	log.Info("Authenticating as %s@%s via OAuth stub...", user, provider)

	// Start OAuth flow
	flowPayload := fmt.Sprintf(`{"userid": "%s", "idp": "%s", "tmi_server": "%s"}`, user, provider, serverURL)
	resp, err := http.Post(
		oauthStubURL+"/flows/start",
		"application/json",
		strings.NewReader(flowPayload),
	)
	if err != nil {
		return "", fmt.Errorf("failed to start OAuth flow: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var flowResult map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&flowResult); err != nil {
		return "", fmt.Errorf("failed to decode OAuth flow response: %w", err)
	}

	flowID, ok := flowResult["flow_id"].(string)
	if !ok || flowID == "" {
		return "", fmt.Errorf("no flow_id in OAuth stub response: %v", flowResult)
	}

	// Poll for completion (max 30 seconds)
	log.Info("  Waiting for OAuth flow to complete (flow_id: %s)...", flowID)
	for i := 0; i < 30; i++ {
		pollResp, err := http.Get(fmt.Sprintf("%s/flows/%s", oauthStubURL, flowID))
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		var pollResult map[string]interface{}
		if err := json.NewDecoder(pollResp.Body).Decode(&pollResult); err != nil {
			_ = pollResp.Body.Close()
			time.Sleep(time.Second)
			continue
		}
		_ = pollResp.Body.Close()

		// Check if tokens are ready (status may be "authorization_completed" or "completed")
		tokensReady, _ := pollResult["tokens_ready"].(bool)
		if tokensReady {
			// Extract token
			if tokens, ok := pollResult["tokens"].(map[string]interface{}); ok {
				if token, ok := tokens["access_token"].(string); ok && token != "" {
					log.Info("  Authentication successful")
					return token, nil
				}
			}
			return "", fmt.Errorf("completed flow has no access_token: %v", pollResult)
		}

		// Check for error/failed status
		if status, ok := pollResult["status"].(string); ok && (status == "failed" || status == "error") {
			errMsg, _ := pollResult["error"].(string)
			return "", fmt.Errorf("OAuth flow failed: %s", errMsg)
		}

		time.Sleep(time.Second)
	}

	return "", fmt.Errorf("OAuth flow timed out after 30 seconds")
}

// apiRequest makes an authenticated HTTP request and returns the response body as parsed JSON.
func apiRequest(method, url, token string, payload interface{}) (map[string]interface{}, int, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req) //nolint:gosec // G704 - URL is from CLI flags for CATS test seeding
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("failed to parse response (status %d): %s", resp.StatusCode, string(respBody))
		}
	}

	return result, resp.StatusCode, nil
}

// extractID extracts the "id" field from a JSON response.
func extractID(result map[string]interface{}) (string, error) {
	id, ok := result["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("no 'id' field in response: %v", result)
	}
	return id, nil
}

// createAPIObject creates an API object via POST and returns its ID.
func createAPIObject(name, url, token string, payload interface{}) (string, error) {
	log := slogging.Get()
	log.Info("  Creating %s...", name)

	result, status, err := apiRequest("POST", url, token, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", name, err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("failed to create %s: HTTP %d - %v", name, status, result)
	}

	id, err := extractID(result)
	if err != nil {
		return "", fmt.Errorf("failed to extract ID for %s: %w", name, err)
	}

	log.Info("    Created %s: %s", name, id)
	return id, nil
}

// createAllAPIObjects creates all test data objects via the API.
func createAllAPIObjects(serverURL, token, user, provider string) (*testDataResults, error) {
	log := slogging.Get()
	results := &testDataResults{}

	// Get current user identity
	log.Info("Step 4: Getting current user identity...")
	if err := populateUserIdentity(results, serverURL, token, user, provider); err != nil {
		return nil, err
	}
	log.Info("  User identity: %s:%s", results.UserProvider, results.UserProviderID)

	// Create all API test objects
	log.Info("Step 5: Creating API test objects...")
	if err := createCoreObjects(results, serverURL, token, user, provider); err != nil {
		return nil, err
	}

	// Create metadata entries
	createMetadataEntries(results, serverURL, token)

	// Get admin user identity
	log.Info("Step 6: Getting admin user identity...")
	populateAdminIdentity(results, serverURL, token)
	log.Info("  Admin identity: %s:%s (UUID: %s)", results.AdminUserProvider, results.AdminUserProviderID, results.AdminUserInternalUUID)

	return results, nil
}

// populateUserIdentity fetches the current user identity from the /me endpoint.
func populateUserIdentity(results *testDataResults, serverURL, token, user, provider string) error {
	meResult, _, err := apiRequest("GET", serverURL+"/me", token, nil)
	if err != nil {
		return fmt.Errorf("failed to get user identity: %w", err)
	}
	if p, ok := meResult["provider"].(string); ok {
		results.UserProvider = p
	} else {
		results.UserProvider = provider
	}
	if pid, ok := meResult["provider_id"].(string); ok {
		results.UserProviderID = pid
	} else {
		results.UserProviderID = user
	}
	return nil
}

// createCoreObjects creates all test API objects (threat models, surveys, etc.).
func createCoreObjects(results *testDataResults, serverURL, token, user, provider string) error {
	var err error

	results.ThreatModelID, err = createAPIObject("threat model", serverURL+"/threat_models", token, map[string]interface{}{
		"name":                   "CATS Test Threat Model",
		"description":            "Created by cats-seed for comprehensive API fuzzing. DO NOT DELETE.",
		"threat_model_framework": "STRIDE",
		"metadata": []map[string]interface{}{
			{"key": "version", "value": "1.0"},
			{"key": "purpose", "value": "cats-fuzzing-test-data"},
		},
	})
	if err != nil {
		return err
	}

	results.ThreatID, err = createAPIObject("threat",
		fmt.Sprintf("%s/threat_models/%s/threats", serverURL, results.ThreatModelID), token,
		map[string]interface{}{
			"name":        "CATS Test Threat",
			"description": "Test threat for CATS fuzzing",
			"threat_type": []string{"Tampering", "Information Disclosure"},
			"severity":    "high",
			"priority":    "high",
			"status":      "identified",
		})
	if err != nil {
		return err
	}

	results.DiagramID, err = createAPIObject("diagram",
		fmt.Sprintf("%s/threat_models/%s/diagrams", serverURL, results.ThreatModelID), token,
		map[string]interface{}{
			"name": "CATS Test Diagram",
			"type": "DFD-1.0.0",
		})
	if err != nil {
		return err
	}

	results.DocumentID, err = createAPIObject("document",
		fmt.Sprintf("%s/threat_models/%s/documents", serverURL, results.ThreatModelID), token,
		map[string]interface{}{
			"name":        "CATS Test Document",
			"uri":         "https://docs.example.com/cats-test-document.pdf",
			"description": "Test document for CATS fuzzing",
		})
	if err != nil {
		return err
	}

	results.AssetID, err = createAPIObject("asset",
		fmt.Sprintf("%s/threat_models/%s/assets", serverURL, results.ThreatModelID), token,
		map[string]interface{}{
			"name":        "CATS Test Asset",
			"description": "Test asset for CATS fuzzing",
			"type":        "software",
		})
	if err != nil {
		return err
	}

	results.NoteID, err = createAPIObject("note",
		fmt.Sprintf("%s/threat_models/%s/notes", serverURL, results.ThreatModelID), token,
		map[string]interface{}{
			"name":    "CATS Test Note",
			"content": "CATS test note for comprehensive API fuzzing",
		})
	if err != nil {
		return err
	}

	results.RepositoryID, err = createAPIObject("repository",
		fmt.Sprintf("%s/threat_models/%s/repositories", serverURL, results.ThreatModelID), token,
		map[string]interface{}{
			"uri": "https://github.com/example/cats-test-repo",
		})
	if err != nil {
		return err
	}

	// Webhook must succeed - no fallback to placeholder
	results.WebhookID, err = createAPIObject("webhook", serverURL+"/webhooks/subscriptions", token,
		map[string]interface{}{
			"name":   "CATS Test Webhook",
			"url":    "https://webhook.site/cats-test-webhook",
			"events": []string{"threat_model.created", "threat.created"},
		})
	if err != nil {
		return err
	}

	results.AddonID, err = createAPIObject("addon", serverURL+"/addons", token,
		map[string]interface{}{
			"name":            "CATS Test Addon",
			"webhook_id":      results.WebhookID,
			"threat_model_id": results.ThreatModelID,
		})
	if err != nil {
		return err
	}

	results.ClientCredentialID, err = createAPIObject("client credential", serverURL+"/me/client_credentials", token,
		map[string]interface{}{
			"name":        "CATS Test Credential",
			"description": "Test credential for CATS fuzzing",
		})
	if err != nil {
		return err
	}

	surveyVersion := fmt.Sprintf("v1-cats-%s", time.Now().Format("20060102-150405"))
	results.SurveyID, err = createAPIObject("survey", serverURL+"/admin/surveys", token,
		map[string]interface{}{
			"name":        "CATS Test Survey",
			"description": "Created by cats-seed for comprehensive API fuzzing. DO NOT DELETE.",
			"version":     surveyVersion,
			"status":      "active",
			"survey_json": map[string]interface{}{
				"pages": []map[string]interface{}{
					{
						"name": "page1",
						"elements": []map[string]interface{}{
							{
								"type":  "text",
								"name":  "project_name",
								"title": "Project Name",
							},
						},
					},
				},
			},
			"settings": map[string]interface{}{
				"allow_threat_model_linking": true,
			},
		})
	if err != nil {
		return err
	}

	results.SurveyResponseID, err = createAPIObject("survey response", serverURL+"/intake/survey_responses", token,
		map[string]interface{}{
			"survey_id": results.SurveyID,
			"answers": map[string]interface{}{
				"project_name": "CATS Test Project",
			},
			"authorization": []map[string]interface{}{
				{
					"principal_type": "user",
					"provider":       provider,
					"provider_id":    user,
					"role":           "owner",
				},
			},
		})
	if err != nil {
		return err
	}

	return nil
}

// createMetadataEntries creates metadata entries for all test objects.
func createMetadataEntries(results *testDataResults, serverURL, token string) {
	log := slogging.Get()
	log.Info("  Creating metadata entries...")
	metadataPayload := map[string]interface{}{
		"key":   metadataKey,
		"value": metadataValue,
	}

	metadataEndpoints := []struct {
		name string
		url  string
	}{
		{"threat model metadata", fmt.Sprintf("%s/threat_models/%s/metadata/%s", serverURL, results.ThreatModelID, metadataKey)},
		{"threat metadata", fmt.Sprintf("%s/threat_models/%s/threats/%s/metadata/%s", serverURL, results.ThreatModelID, results.ThreatID, metadataKey)},
		{"diagram metadata", fmt.Sprintf("%s/threat_models/%s/diagrams/%s/metadata/%s", serverURL, results.ThreatModelID, results.DiagramID, metadataKey)},
		{"document metadata", fmt.Sprintf("%s/threat_models/%s/documents/%s/metadata/%s", serverURL, results.ThreatModelID, results.DocumentID, metadataKey)},
		{"asset metadata", fmt.Sprintf("%s/threat_models/%s/assets/%s/metadata/%s", serverURL, results.ThreatModelID, results.AssetID, metadataKey)},
		{"note metadata", fmt.Sprintf("%s/threat_models/%s/notes/%s/metadata/%s", serverURL, results.ThreatModelID, results.NoteID, metadataKey)},
		{"repository metadata", fmt.Sprintf("%s/threat_models/%s/repositories/%s/metadata/%s", serverURL, results.ThreatModelID, results.RepositoryID, metadataKey)},
	}

	for _, ep := range metadataEndpoints {
		_, status, mErr := apiRequest("PUT", ep.url, token, metadataPayload)
		if mErr != nil || status >= 300 {
			log.Debug("    Warning: failed to create %s (status %d): %v", ep.name, status, mErr)
		}
	}

	// Survey metadata via POST (different endpoint pattern)
	for _, ep := range []struct {
		name string
		url  string
	}{
		{"survey metadata", fmt.Sprintf("%s/admin/surveys/%s/metadata", serverURL, results.SurveyID)},
		{"survey response metadata", fmt.Sprintf("%s/intake/survey_responses/%s/metadata", serverURL, results.SurveyResponseID)},
	} {
		_, status, mErr := apiRequest("POST", ep.url, token, metadataPayload)
		if mErr != nil || status >= 300 {
			log.Debug("    Warning: failed to create %s (status %d): %v", ep.name, status, mErr)
		}
	}

	log.Info("    Created all metadata entries")
}

// populateAdminIdentity fetches admin user identity from the /admin/users endpoint.
func populateAdminIdentity(results *testDataResults, serverURL, token string) {
	log := slogging.Get()
	results.AdminGroupID = nilUUIDString

	adminResult, _, err := apiRequest("GET", serverURL+"/admin/users", token, nil)
	if err != nil {
		log.Debug("  Warning: failed to get admin users: %v", err)
		results.AdminUserProvider = results.UserProvider
		results.AdminUserProviderID = results.UserProviderID
		results.AdminUserInternalUUID = nilUUIDString
		return
	}

	if users, ok := adminResult["users"].([]interface{}); ok && len(users) > 0 {
		if firstUser, ok := users[0].(map[string]interface{}); ok {
			if p, ok := firstUser["provider"].(string); ok {
				results.AdminUserProvider = p
			}
			if pid, ok := firstUser["provider_id"].(string); ok {
				results.AdminUserProviderID = pid
			}
			if uid, ok := firstUser["internal_uuid"].(string); ok {
				results.AdminUserInternalUUID = uid
			}
		}
	}
	if results.AdminUserProvider == "" {
		results.AdminUserProvider = results.UserProvider
	}
	if results.AdminUserProviderID == "" {
		results.AdminUserProviderID = results.UserProviderID
	}
	if results.AdminUserInternalUUID == "" {
		results.AdminUserInternalUUID = nilUUIDString
	}
}
