package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tmiclient "github.com/ericfitz/tmi-clients/go-client-generated/v1_4_0"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

const (
	oauthStubPort = 8079
)

// apiClient holds the SDK client and auth context for API seeding.
type apiClient struct {
	sdk       *tmiclient.APIClient
	ctx       context.Context
	serverURL string
	token     string
	db        *testdb.TestDB
}

func newAPIClient(serverURL, token string) *apiClient {
	cfg := tmiclient.NewConfiguration()
	cfg.Servers = tmiclient.ServerConfigurations{
		tmiclient.ServerConfiguration{URL: serverURL},
	}
	cfg.DefaultHeader = map[string]string{
		"Authorization": "Bearer " + token,
	}

	sdk := tmiclient.NewAPIClient(cfg)
	ctx := context.WithValue(context.Background(), tmiclient.ContextAccessToken, token)

	return &apiClient{
		sdk:       sdk,
		ctx:       ctx,
		serverURL: serverURL,
		token:     token,
	}
}

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

func seedViaAPI(serverURL, token string, entry SeedEntry, refs RefMap, db *testdb.TestDB) (*SeedResult, error) {
	client := newAPIClient(serverURL, token)
	client.db = db

	switch entry.Kind {
	case kindThreatModel:
		return client.seedThreatModel(entry, refs)
	case kindTMPatch:
		return client.seedTMPatch(entry, refs)
	case kindTeam:
		return client.seedTeam(entry, refs)
	case kindProject:
		return client.seedProject(entry, refs)
	case kindGroup:
		return client.seedGroup(entry, refs)
	case kindGroupMember:
		return client.seedGroupMember(entry, refs)
	case kindDiagram:
		return client.seedChildResource(entry, refs, "threat_model_ref", "diagrams")
	case kindDiagramUpdate:
		return client.seedDiagramUpdate(entry, refs)
	case kindThreat:
		return client.seedChildResource(entry, refs, "threat_model_ref", "threats")
	case kindAsset:
		return client.seedChildResource(entry, refs, "threat_model_ref", "assets")
	case kindDocument:
		return client.seedChildResource(entry, refs, "threat_model_ref", "documents")
	case kindNote:
		return client.seedChildResource(entry, refs, "threat_model_ref", "notes")
	case kindRepository:
		return client.seedChildResource(entry, refs, "threat_model_ref", "repositories")
	case kindWebhook:
		return client.seedWebhook(entry, refs)
	case kindWebhookTestDeliv:
		return client.seedWebhookTestDelivery(entry, refs)
	case kindAddon:
		return client.seedAddon(entry, refs)
	case kindClientCredential:
		return client.seedTopLevel(entry, "/me/client_credentials")
	case kindSurvey:
		return client.seedSurvey(entry, refs)
	case kindSurveyResponse:
		return client.seedSurveyResponse(entry, refs)
	case kindMetadata:
		return client.seedMetadata(entry, refs)
	default:
		return nil, fmt.Errorf("unsupported API seed kind: %s", entry.Kind)
	}
}

// --- Idempotency helpers using SDK typed list responses ---

func (c *apiClient) findExistingTM(name string) string {
	result, resp, err := c.sdk.ThreatModelsAPI.ListThreatModels(c.ctx).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return ""
	}
	for _, item := range result.GetThreatModels() {
		if item.GetName() == name {
			return item.GetId()
		}
	}
	return ""
}

func (c *apiClient) findExistingTeam(name string) string {
	result, resp, err := c.sdk.TeamsAPI.ListTeams(c.ctx).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return ""
	}
	for _, item := range result.GetTeams() {
		if item.GetName() == name {
			return item.GetId()
		}
	}
	return ""
}

func (c *apiClient) findExistingProject(name string) string {
	result, resp, err := c.sdk.ProjectsAPI.ListProjects(c.ctx).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return ""
	}
	for _, item := range result.GetProjects() {
		if item.GetName() == name {
			return item.GetId()
		}
	}
	return ""
}

func (c *apiClient) findExistingWebhook(name string) string {
	result, resp, err := c.sdk.WebhooksAPI.ListWebhookSubscriptions(c.ctx).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return ""
	}
	for _, item := range result.GetSubscriptions() {
		if item.GetName() == name {
			return item.GetId()
		}
	}
	return ""
}

func (c *apiClient) findExistingSurvey(name string) string {
	log := slogging.Get()
	// Try admin endpoint, then intake endpoint as fallback
	id := c.findExistingByNameHTTP("/admin/surveys", "surveys", name)
	if id == "" {
		// The admin endpoint may fail if token lacks admin privileges in this session.
		// Try the intake endpoint which is available to all authenticated users.
		id = c.findExistingByNameHTTP("/intake/surveys", "surveys", name)
	}
	if id == "" {
		log.Debug("  findExistingSurvey: no match for %q", name)
	} else {
		log.Debug("  findExistingSurvey: found %q -> %s", name, id)
	}
	return id
}

// findExistingByNameHTTP is a raw HTTP fallback for finding existing resources by name.
func (c *apiClient) findExistingByNameHTTP(path, itemsKey, name string) string {
	result, status, err := c.apiRequest("GET", path+"?limit=100", nil)
	if err != nil || status >= 300 {
		return ""
	}
	if items, ok := result[itemsKey].([]any); ok {
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if n, _ := m["name"].(string); n == name {
					if id, _ := m["id"].(string); id != "" {
						return id
					}
				}
			}
		}
	}
	return ""
}

func (c *apiClient) findExistingGroup(groupName string) string {
	result, resp, err := c.sdk.AdministrationAPI.ListAdminGroups(c.ctx).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return ""
	}
	for _, item := range result.GetGroups() {
		if item.GetGroupName() == groupName {
			return item.GetInternalUuid()
		}
	}
	return ""
}

// --- Seed handlers ---

func (c *apiClient) seedThreatModel(entry SeedEntry, _ RefMap) (*SeedResult, error) {
	log := slogging.Get()

	name, _ := entry.Data["name"].(string)
	if name != "" {
		if existingID := c.findExistingTM(name); existingID != "" {
			log.Info("  %s already exists: %s (skipping)", entry.Kind, existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	id, err := c.createAPIObject(entry.Kind, "/threat_models", entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedTMPatch(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	tmRefName, _ := entry.Data["tm_ref"].(string)
	tmID, err := resolveRef(refs, tmRefName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tm_ref: %w", err)
	}

	patches, ok := entry.Data["patches"].([]map[string]any)
	if !ok || len(patches) == 0 {
		return &SeedResult{Kind: entry.Kind, ID: tmID}, nil
	}

	// Apply each patch as a separate request to avoid ordering conflicts
	// (e.g., security_reviewer grants owner role, which can conflict with owner transfer)
	for i, p := range patches {
		patch := copyMap(p)
		if projRef, ok := patch["project_ref"].(string); ok && projRef != "" {
			projID, err := resolveRef(refs, projRef)
			if err != nil {
				log.Debug("  Warning: could not resolve project_ref %q: %v", projRef, err)
				continue
			}
			patch["value"] = projID
			delete(patch, "project_ref")
		}

		patchPath, _ := patch["path"].(string)
		log.Info("  Patching threat model %s: %s (%d/%d)...", tmID, patchPath, i+1, len(patches))

		patchDoc := []map[string]any{patch}
		data, err := json.Marshal(patchDoc)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal patch: %w", err)
		}

		url := fmt.Sprintf("%s/threat_models/%s", c.serverURL, tmID)
		req, err := http.NewRequest("PATCH", url, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to create PATCH request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json-patch+json")

		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req) //nolint:gosec // URL from CLI flags
		if err != nil {
			return nil, fmt.Errorf("PATCH request failed: %w", err)
		}
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			// Owner transfer may fail due to server identity matching bug (see #253).
			// Fall back to direct DB update for ownership.
			if patchPath == "/owner" {
				log.Info("  Owner transfer via API failed (HTTP %d), falling back to DB...", resp.StatusCode)
				if dbErr := c.transferOwnerViaDB(tmID, patch); dbErr != nil {
					return nil, fmt.Errorf("owner transfer failed via both API and DB: API=%s, DB=%w", string(body), dbErr)
				}
				continue
			}
			return nil, fmt.Errorf("PATCH threat model %s failed: HTTP %d - %s", patchPath, resp.StatusCode, string(body))
		}
		_ = resp.Body.Close()
	}

	log.Info("  Patched threat model: %s", tmID)
	return &SeedResult{Kind: entry.Kind, ID: tmID}, nil
}

// transferOwnerViaDB sets threat model ownership directly in the database.
// This is a fallback for when the API ownership transfer fails due to
// server identity matching bugs (see #253).
func (c *apiClient) transferOwnerViaDB(tmID string, patch map[string]any) error {
	log := slogging.Get()

	if c.db == nil {
		return fmt.Errorf("no database connection available for owner transfer fallback")
	}

	// Extract new owner's provider_id from the patch value
	ownerPrincipal, ok := patch["value"].(map[string]any)
	if !ok {
		return fmt.Errorf("owner patch value is not a valid principal object")
	}
	providerID, _ := ownerPrincipal["provider_id"].(string)
	provider, _ := ownerPrincipal["provider"].(string)
	if providerID == "" {
		return fmt.Errorf("owner principal missing provider_id")
	}
	if provider == "" {
		provider = defaultProvider
	}

	// Look up the new owner's internal UUID
	var ownerUUID string
	err := c.db.DB().Raw(
		"SELECT internal_uuid FROM users WHERE provider_user_id = ? AND provider = ? LIMIT 1",
		providerID, provider,
	).Scan(&ownerUUID).Error
	if err != nil || ownerUUID == "" {
		return fmt.Errorf("could not find user %s@%s in database: %w", providerID, provider, err)
	}

	// Update the threat model's owner
	result := c.db.DB().Exec(
		"UPDATE threat_models SET owner_internal_uuid = ?, modified_at = NOW() WHERE id = ?",
		ownerUUID, tmID,
	)
	if result.Error != nil {
		return fmt.Errorf("failed to update owner: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("threat model %s not found", tmID)
	}

	log.Info("  Transferred ownership via DB to %s (%s)", providerID, ownerUUID)
	return nil
}

func (c *apiClient) seedTeam(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	name, _ := entry.Data["name"].(string)
	if name != "" {
		if existingID := c.findExistingTeam(name); existingID != "" {
			log.Info("  team already exists: %s (skipping)", existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	// Resolve user_ref in members to UUIDs
	payload := copyMap(entry.Data)
	if members, ok := payload["members"].([]map[string]any); ok {
		resolved := make([]map[string]any, 0, len(members))
		for _, m := range members {
			member := copyMap(m)
			if userRefName, ok := member["user_ref"].(string); ok {
				userUUID, err := resolveRef(refs, userRefName)
				if err != nil {
					log.Debug("  Warning: could not resolve user_ref %q: %v", userRefName, err)
					continue
				}
				member["user_id"] = userUUID
				delete(member, "user_ref")
			}
			resolved = append(resolved, member)
		}
		payload["members"] = resolved
	}

	id, err := c.createAPIObject(entry.Kind, "/teams", payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedProject(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	name, _ := entry.Data["name"].(string)
	if name != "" {
		if existingID := c.findExistingProject(name); existingID != "" {
			log.Info("  project already exists: %s (skipping)", existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	payload := copyMap(entry.Data)
	if teamRefName, _ := payload["team_ref"].(string); teamRefName != "" {
		teamID, err := resolveRef(refs, teamRefName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve team_ref: %w", err)
		}
		payload["team_id"] = teamID
		delete(payload, "team_ref")
	}

	id, err := c.createAPIObject(entry.Kind, "/projects", payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedGroup(entry SeedEntry, _ RefMap) (*SeedResult, error) {
	log := slogging.Get()

	groupName, _ := entry.Data["group_name"].(string)
	if groupName != "" {
		if existingID := c.findExistingGroup(groupName); existingID != "" {
			log.Info("  group already exists: %s (skipping)", existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	log.Info("  Creating group...")
	result, status, err := c.apiRequest("POST", "/admin/groups", entry.Data)
	if err != nil || status < 200 || status >= 300 {
		return nil, fmt.Errorf("failed to create group: HTTP %d - %w", status, err)
	}

	// Admin groups return internal_uuid instead of id
	id, _ := result["internal_uuid"].(string)
	if id == "" {
		id, _ = result["id"].(string)
	}
	if id == "" {
		return nil, fmt.Errorf("no 'internal_uuid' or 'id' in group response: %v", result)
	}

	log.Info("    Created group: %s", id)
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedGroupMember(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	// Resolve group UUID (either from ref or direct UUID)
	var groupUUID string
	if groupRefName, ok := entry.Data["group_ref"].(string); ok && groupRefName != "" {
		var err error
		groupUUID, err = resolveRef(refs, groupRefName)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve group_ref: %w", err)
		}
	} else if directUUID, ok := entry.Data["group_uuid"].(string); ok {
		groupUUID = directUUID
	}
	if groupUUID == "" {
		return nil, fmt.Errorf("group_member seed requires group_ref or group_uuid")
	}

	// Resolve user UUID
	userRefName, _ := entry.Data["user_ref"].(string)
	userUUID, err := resolveRef(refs, userRefName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve user_ref: %w", err)
	}

	payload := map[string]any{
		"user_internal_uuid": userUUID,
	}

	log.Info("  Adding user %s to group %s...", userUUID, groupUUID)
	url := fmt.Sprintf("/admin/groups/%s/members", groupUUID)

	_, status, apiErr := c.apiRequest("POST", url, payload)
	if apiErr != nil {
		// Non-fatal: 409 means already a member
		if status == http.StatusConflict {
			log.Info("  User already in group (skipping)")
			return &SeedResult{Kind: entry.Kind, ID: userUUID}, nil
		}
		log.Debug("  Warning: failed to add group member (status %d): %v", status, apiErr)
		return &SeedResult{Kind: entry.Kind, ID: userUUID}, nil
	}

	log.Info("  Added user to group")
	return &SeedResult{Kind: entry.Kind, ID: userUUID}, nil
}

func (c *apiClient) seedChildResource(entry SeedEntry, refs RefMap, refField, resourcePath string) (*SeedResult, error) {
	tmID, err := resolveRefField(entry.Data, refField, refs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", refField, err)
	}
	if tmID == "" {
		return nil, fmt.Errorf("%s is required for %s seed", refField, entry.Kind)
	}

	payload := copyMap(entry.Data)
	delete(payload, refField)

	url := fmt.Sprintf("/threat_models/%s/%s", tmID, resourcePath)
	id, err := c.createAPIObject(entry.Kind, url, payload)
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

func (c *apiClient) seedDiagramUpdate(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	tmRefName, _ := entry.Data["tm_ref"].(string)
	tmID, err := resolveRef(refs, tmRefName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tm_ref: %w", err)
	}

	diagramRefName, _ := entry.Data["diagram_ref"].(string)
	diagramID, err := resolveRef(refs, diagramRefName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve diagram_ref: %w", err)
	}

	// Build PUT payload
	payload := map[string]any{
		"name":  entry.Data["name"],
		"type":  entry.Data["type"],
		"cells": entry.Data["cells"],
	}
	if desc, ok := entry.Data["description"]; ok {
		payload["description"] = desc
	}

	log.Info("  Updating diagram %s with cells...", diagramID)
	url := fmt.Sprintf("/threat_models/%s/diagrams/%s", tmID, diagramID)

	_, status, apiErr := c.apiRequest("PUT", url, payload)
	if apiErr != nil || status >= 300 {
		return nil, fmt.Errorf("failed to update diagram cells: HTTP %d - %w", status, apiErr)
	}

	log.Info("  Updated diagram with cells: %s", diagramID)
	return &SeedResult{Kind: entry.Kind, ID: diagramID, Extra: map[string]string{
		"threat_model_id": tmID,
	}}, nil
}

func (c *apiClient) seedWebhook(entry SeedEntry, _ RefMap) (*SeedResult, error) {
	log := slogging.Get()

	name, _ := entry.Data["name"].(string)
	if name != "" {
		if existingID := c.findExistingWebhook(name); existingID != "" {
			log.Info("  webhook already exists: %s (skipping)", existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	id, err := c.createAPIObject(entry.Kind, "/admin/webhooks/subscriptions", entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedWebhookTestDelivery(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	log := slogging.Get()

	webhookRefName, _ := entry.Data["webhook_ref"].(string)
	webhookID, err := resolveRef(refs, webhookRefName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve webhook_ref: %w", err)
	}

	log.Info("  Triggering webhook test delivery...")
	url := fmt.Sprintf("/admin/webhooks/subscriptions/%s/test", webhookID)
	result, status, apiErr := c.apiRequest("POST", url, map[string]any{
		"event_type": "webhook.test",
	})
	if apiErr != nil || status >= 300 {
		return nil, fmt.Errorf("webhook test delivery failed: HTTP %d - %v", status, result)
	}

	deliveryID, _ := result["delivery_id"].(string)
	if deliveryID == "" {
		return nil, fmt.Errorf("no delivery_id in response: %v", result)
	}

	log.Info("  Created webhook test delivery: %s", deliveryID)
	return &SeedResult{Ref: entry.Ref, Kind: kindWebhookTestDeliv, ID: deliveryID}, nil
}

func (c *apiClient) seedAddon(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	payload := copyMap(entry.Data)

	if webhookRefName, _ := payload["webhook_ref"].(string); webhookRefName != "" {
		webhookID, err := resolveRef(refs, webhookRefName)
		if err != nil {
			return nil, err
		}
		payload["webhook_id"] = webhookID
		delete(payload, "webhook_ref")
	}

	if tmRefName, _ := payload["threat_model_ref"].(string); tmRefName != "" {
		tmID, err := resolveRef(refs, tmRefName)
		if err != nil {
			return nil, err
		}
		payload["threat_model_id"] = tmID
		delete(payload, "threat_model_ref")
	}

	id, err := c.createAPIObject(kindAddon, "/addons", payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: kindAddon, ID: id}, nil
}

func (c *apiClient) seedSurvey(entry SeedEntry, _ RefMap) (*SeedResult, error) {
	log := slogging.Get()

	name, _ := entry.Data["name"].(string)
	if name != "" {
		if existingID := c.findExistingSurvey(name); existingID != "" {
			log.Info("  survey already exists: %s (skipping)", existingID)
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
	}

	log.Info("  Creating survey...")
	result, status, err := c.apiRequest("POST", "/admin/surveys", entry.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to create survey: %w", err)
	}

	// Handle 409 as idempotent (name+version already exists)
	if status == http.StatusConflict {
		log.Info("  Survey already exists (conflict), looking up by name...")
		if existingID := c.findExistingSurvey(name); existingID != "" {
			return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: existingID}, nil
		}
		return nil, fmt.Errorf("survey conflict but could not find existing: %v", result)
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("failed to create survey: HTTP %d - %v", status, result)
	}

	id, _ := result["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("no 'id' in survey response: %v", result)
	}

	log.Info("    Created survey: %s", id)
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedSurveyResponse(entry SeedEntry, refs RefMap) (*SeedResult, error) {
	payload := copyMap(entry.Data)

	if surveyRefName, _ := payload["survey_ref"].(string); surveyRefName != "" {
		surveyID, err := resolveRef(refs, surveyRefName)
		if err != nil {
			return nil, err
		}
		payload["survey_id"] = surveyID
		delete(payload, "survey_ref")
	}

	id, err := c.createAPIObject("survey response", "/intake/survey_responses", payload)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: kindSurveyResponse, ID: id, Extra: map[string]string{
		"survey_id": fmt.Sprint(payload["survey_id"]),
	}}, nil
}

func (c *apiClient) seedTopLevel(entry SeedEntry, path string) (*SeedResult, error) {
	id, err := c.createAPIObject(entry.Kind, path, entry.Data)
	if err != nil {
		return nil, err
	}
	return &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: id}, nil
}

func (c *apiClient) seedMetadata(entry SeedEntry, refs RefMap) (*SeedResult, error) {
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
	case kindThreatModel:
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

	_, status, err := c.apiRequest(method, resourcePath, payload)
	if err != nil || status >= 300 {
		log.Debug("  Warning: failed to create metadata %s on %s (status %d): %v", key, targetRef, status, err)
	} else {
		log.Info("  Created metadata %s on %s", key, targetRef)
	}

	return &SeedResult{Ref: entry.Ref, Kind: kindMetadata, ID: key}, nil
}

// --- HTTP helpers ---

func (c *apiClient) apiRequest(method, path string, payload any) (map[string]any, int, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal payload: %w", err)
		}
		body = bytes.NewReader(data)
	}

	url := c.serverURL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req) //nolint:gosec // URL from CLI flags
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

func (c *apiClient) createAPIObject(name, path string, payload any) (string, error) {
	log := slogging.Get()
	log.Info("  Creating %s...", name)

	result, status, err := c.apiRequest("POST", path, payload)
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
	case kindThreat:
		return "threats"
	case kindDiagram:
		return "diagrams"
	case kindAsset:
		return "assets"
	case kindDocument:
		return "documents"
	case kindNote:
		return "notes"
	case kindRepository:
		return "repositories"
	default:
		return kind + "s"
	}
}
