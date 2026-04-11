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
	oauthStubPort      = 8079
	kindSurvey         = "survey"
	kindSurveyResponse = "survey_response"
)

func authenticateViaOAuthStub(serverURL, user, provider string) (string, error) {
	log := slogging.Get()
	oauthStubURL := fmt.Sprintf("http://localhost:%d", oauthStubPort)

	log.Info("Authenticating as %s@%s via OAuth stub...", user, provider)

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

	var flowResult map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&flowResult); err != nil {
		return "", fmt.Errorf("failed to decode OAuth flow response: %w", err)
	}

	flowID, ok := flowResult["flow_id"].(string)
	if !ok || flowID == "" {
		return "", fmt.Errorf("no flow_id in OAuth stub response: %v", flowResult)
	}

	log.Info("  Waiting for OAuth flow to complete (flow_id: %s)...", flowID)
	for range 30 {
		pollResp, err := http.Get(fmt.Sprintf("%s/flows/%s", oauthStubURL, flowID))
		if err != nil {
			time.Sleep(time.Second)
			continue
		}

		var pollResult map[string]any
		if err := json.NewDecoder(pollResp.Body).Decode(&pollResult); err != nil {
			_ = pollResp.Body.Close()
			time.Sleep(time.Second)
			continue
		}
		_ = pollResp.Body.Close()

		tokensReady, _ := pollResult["tokens_ready"].(bool)
		if tokensReady {
			if tokens, ok := pollResult["tokens"].(map[string]any); ok {
				if token, ok := tokens["access_token"].(string); ok && token != "" {
					log.Info("  Authentication successful")
					return token, nil
				}
			}
			return "", fmt.Errorf("completed flow has no access_token: %v", pollResult)
		}

		if status, ok := pollResult["status"].(string); ok && (status == "failed" || status == "error") {
			errMsg, _ := pollResult["error"].(string)
			return "", fmt.Errorf("OAuth flow failed: %s", errMsg)
		}

		time.Sleep(time.Second)
	}

	return "", fmt.Errorf("OAuth flow timed out after 30 seconds")
}

func seedViaAPI(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	switch entry.Kind {
	case "threat_model":
		return seedThreatModel(serverURL, token, entry)
	case "diagram":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "diagrams")
	case "threat":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "threats")
	case "asset":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "assets")
	case "document":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "documents")
	case "note":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "notes")
	case "repository":
		return seedChildResource(serverURL, token, entry, refs, "threat_model_ref", "repositories")
	case "webhook":
		return seedTopLevel(serverURL, token, entry, "/admin/webhooks/subscriptions")
	case "webhook_test_delivery":
		return seedWebhookTestDelivery(serverURL, token, entry, refs)
	case "addon":
		return seedAddon(serverURL, token, entry, refs)
	case "client_credential":
		return seedTopLevel(serverURL, token, entry, "/me/client_credentials")
	case kindSurvey:
		return seedTopLevel(serverURL, token, entry, "/admin/surveys")
	case kindSurveyResponse:
		return seedSurveyResponse(serverURL, token, entry, refs)
	case "metadata":
		return seedMetadata(serverURL, token, entry, refs)
	default:
		return nil, fmt.Errorf("unsupported API seed kind: %s", entry.Kind)
	}
}

func seedThreatModel(serverURL, token string, entry SeedEntry) (*SeedResult, error) {
	id, err := createAPIObject(entry.Kind, serverURL+"/threat_models", token, entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func seedChildResource(serverURL, token string, entry SeedEntry, refs RefMap, refField, resourcePath string) (*SeedResult, error) {
	tmID, err := resolveRefField(entry.Data, refField, refs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", refField, err)
	}
	if tmID == "" {
		return nil, fmt.Errorf("%s is required for %s seed", refField, entry.Kind)
	}

	payload := copyMap(entry.Data)
	delete(payload, refField)

	url := fmt.Sprintf("%s/threat_models/%s/%s", serverURL, tmID, resourcePath)
	id, err := createAPIObject(entry.Kind, url, token, payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{
		Ref:  entry.Ref,
		Kind: entry.Kind,
		ID:   id,
		Extra: map[string]string{
			"threat_model_id": tmID,
		},
	}, nil
}

func seedTopLevel(serverURL, token string, entry SeedEntry, path string) (*SeedResult, error) {
	id, err := createAPIObject(entry.Kind, serverURL+path, token, entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func seedAddon(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	payload := copyMap(entry.Data)

	if webhookRef, err := resolveRefField(payload, "webhook_ref", refs); err != nil {
		return nil, err
	} else if webhookRef != "" {
		payload["webhook_id"] = webhookRef
		delete(payload, "webhook_ref")
	}

	if tmRef, err := resolveRefField(payload, "threat_model_ref", refs); err != nil {
		return nil, err
	} else if tmRef != "" {
		payload["threat_model_id"] = tmRef
		delete(payload, "threat_model_ref")
	}

	id, err := createAPIObject("addon", serverURL+"/addons", token, payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: "addon", ID: id}, nil
}

func seedSurveyResponse(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	payload := copyMap(entry.Data)

	if surveyRef, err := resolveRefField(payload, "survey_ref", refs); err != nil {
		return nil, err
	} else if surveyRef != "" {
		payload["survey_id"] = surveyRef
		delete(payload, "survey_ref")
	}

	id, err := createAPIObject("survey response", serverURL+"/intake/survey_responses", token, payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: kindSurveyResponse, ID: id, Extra: map[string]string{
		"survey_id": fmt.Sprint(payload["survey_id"]),
	}}, nil
}

func seedWebhookTestDelivery(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()
	payload := copyMap(entry.Data)

	webhookID, err := resolveRefField(payload, "webhook_ref", refs)
	if err != nil {
		return nil, err
	}
	if webhookID == "" {
		return nil, fmt.Errorf("webhook_ref is required for webhook_test_delivery")
	}

	log.Info("  Triggering webhook test delivery...")
	url := fmt.Sprintf("%s/admin/webhooks/subscriptions/%s/test", serverURL, webhookID)
	result, status, err := apiRequest("POST", url, token, map[string]any{
		"event_type": "webhook.test",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook test delivery: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("webhook test delivery failed: HTTP %d - %v", status, result)
	}

	deliveryID, ok := result["delivery_id"].(string)
	if !ok || deliveryID == "" {
		return nil, fmt.Errorf("no delivery_id in response: %v", result)
	}

	log.Info("  Created webhook test delivery: %s", deliveryID)
	return &SeedResult{Ref: entry.Ref, Kind: "webhook_test_delivery", ID: deliveryID}, nil
}

func seedMetadata(serverURL, token string, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	targetRef, _ := entry.Data["target_ref"].(string)
	targetKind, _ := entry.Data["target_kind"].(string)
	key, _ := entry.Data["key"].(string)
	value, _ := entry.Data["value"].(string)

	if targetRef == "" || key == "" {
		return nil, fmt.Errorf("metadata seed requires target_ref and key")
	}

	target, ok := refs[targetRef]
	if !ok {
		return nil, fmt.Errorf("unresolved target_ref: %q", targetRef)
	}

	tmID := target.ID
	var resourcePath string

	switch targetKind {
	case "threat_model":
		resourcePath = fmt.Sprintf("/threat_models/%s/metadata/%s", tmID, key)
	case kindSurvey:
		resourcePath = fmt.Sprintf("/admin/surveys/%s/metadata", tmID)
	case kindSurveyResponse:
		resourcePath = fmt.Sprintf("/intake/survey_responses/%s/metadata", tmID)
	default:
		if target.Extra != nil {
			tmID = target.Extra["threat_model_id"]
		}
		resourceID := target.ID
		childPath := pluralizeKind(target.Kind)
		resourcePath = fmt.Sprintf("/threat_models/%s/%s/%s/metadata/%s", tmID, childPath, resourceID, key)
	}

	payload := map[string]any{"key": key, "value": value}
	method := "PUT"
	if targetKind == kindSurvey || targetKind == kindSurveyResponse {
		method = "POST"
	}

	_, status, err := apiRequest(method, serverURL+resourcePath, token, payload)
	if err != nil || status >= 300 {
		log.Debug("  Warning: failed to create metadata %s on %s (status %d): %v", key, targetRef, status, err)
	} else {
		log.Info("  Created metadata %s on %s", key, targetRef)
	}

	return &SeedResult{Ref: entry.Ref, Kind: "metadata", ID: key}, nil
}

func apiRequest(method, url, token string, payload any) (map[string]any, int, error) {
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
	resp, err := client.Do(req) //nolint:gosec // URL from CLI flags
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	var result map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, resp.StatusCode, fmt.Errorf("failed to parse response (status %d): %s", resp.StatusCode, string(respBody))
		}
	}

	return result, resp.StatusCode, nil
}

func createAPIObject(name, url, token string, payload any) (string, error) {
	log := slogging.Get()
	log.Info("  Creating %s...", name)

	result, status, err := apiRequest("POST", url, token, payload)
	if err != nil {
		return "", fmt.Errorf("failed to create %s: %w", name, err)
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("failed to create %s: HTTP %d - %v", name, status, result)
	}

	id, ok := result["id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("no 'id' field in response for %s: %v", name, result)
	}

	log.Info("    Created %s: %s", name, id)
	return id, nil
}

func copyMap(m map[string]any) map[string]any {
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func pluralizeKind(kind string) string {
	switch kind {
	case "threat":
		return "threats"
	case "diagram":
		return "diagrams"
	case "asset":
		return "assets"
	case "document":
		return "documents"
	case "note":
		return "notes"
	case "repository":
		return "repositories"
	default:
		return kind + "s"
	}
}
