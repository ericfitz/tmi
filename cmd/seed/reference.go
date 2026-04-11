package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

const nilUUID = "00000000-0000-0000-0000-000000000000"

func writeReferenceFiles(output *SeedOutput, refs RefMap, serverURL, user, provider string) error {
	log := slogging.Get()

	if output.ReferenceFile != "" {
		if err := writeJSONReference(output.ReferenceFile, refs, serverURL, user, provider); err != nil {
			return err
		}
		log.Info("Wrote JSON reference: %s", output.ReferenceFile)
	}

	if output.ReferenceYAML != "" {
		if err := writeYAMLReference(output.ReferenceYAML, refs, user, provider); err != nil {
			return err
		}
		log.Info("Wrote YAML reference: %s", output.ReferenceYAML)
	}

	return nil
}

type referenceJSON struct {
	Version   string                     `json:"version"`
	CreatedAt string                     `json:"created_at"`
	Server    string                     `json:"server"`
	User      referenceUser              `json:"user"`
	Objects   map[string]referenceObject `json:"objects"`
}

type referenceUser struct {
	ProviderUserID string `json:"provider_user_id"`
	Provider       string `json:"provider"`
	Email          string `json:"email"`
}

type referenceObject struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name,omitempty"`
	ThreatModelID string `json:"threat_model_id,omitempty"`
	URL           string `json:"url,omitempty"`
	SurveyID      string `json:"survey_id,omitempty"`
}

func writeJSONReference(path string, refs RefMap, serverURL, user, provider string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	objects := make(map[string]referenceObject, len(refs))
	for refName, result := range refs {
		obj := referenceObject{
			ID:   result.ID,
			Kind: result.Kind,
		}
		if result.Extra != nil {
			obj.ThreatModelID = result.Extra["threat_model_id"]
			obj.SurveyID = result.Extra["survey_id"]
			obj.URL = result.Extra["url"]
			if name, ok := result.Extra["name"]; ok {
				obj.Name = name
			}
		}
		objects[refName] = obj
	}

	ref := referenceJSON{
		Version:   "1.0.0",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Server:    serverURL,
		User: referenceUser{
			ProviderUserID: user,
			Provider:       provider,
			Email:          user + "@tmi.local",
		},
		Objects: objects,
	}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON reference: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

func writeYAMLReference(path string, refs RefMap, user, provider string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	tmID := findRefByKind(refs, "threat_model")
	threatID := findRefByKind(refs, "threat")
	diagramID := findRefByKind(refs, "diagram")
	documentID := findRefByKind(refs, "document")
	assetID := findRefByKind(refs, "asset")
	noteID := findRefByKind(refs, "note")
	repoID := findRefByKind(refs, "repository")
	webhookID := findRefByKind(refs, "webhook")
	deliveryID := findRefByKind(refs, "webhook_test_delivery")
	addonID := findRefByKind(refs, "addon")
	credID := findRefByKind(refs, "client_credential")
	surveyID := findRefByKind(refs, "survey")
	responseID := findRefByKind(refs, "survey_response")
	metadataKey := findRefByKind(refs, "metadata")

	adminUUID := ""
	for _, r := range refs {
		if r.Kind == "user" {
			adminUUID = r.ID
			break
		}
	}
	if adminUUID == "" {
		adminUUID = nilUUID
	}
	adminGroupID := nilUUID

	yaml := fmt.Sprintf(`# CATS Reference Data - Path-based format for parameter replacement
# Generated: %s
# See: https://endava.github.io/cats/docs/getting-started/running-cats/

# All paths - global parameter substitution
all:
  id: %s
  threat_model_id: %s
  threat_id: %s
  diagram_id: %s
  document_id: %s
  asset_id: %s
  note_id: %s
  repository_id: %s
  webhook_id: %s
  delivery_id: %s
  addon_id: %s
  client_credential_id: %s
  survey_id: %s
  survey_response_id: %s
  key: %s
  # Admin resource identifiers
  group_id: %s
  # internal_uuid for /admin/users/{internal_uuid} and /admin/groups/{internal_uuid} endpoints
  internal_uuid: %s
  # User identity uses provider:provider_id format
  user_provider: %s
  user_provider_id: %s
  admin_user_provider: %s
  admin_user_provider_id: %s
  # SAML/OAuth provider endpoints - uses the IDP name directly
  provider: %s
  idp: %s
  # Admin quota endpoints - user_id is internal UUID (OpenAPI spec defines it as UUID format)
  user_id: %s
  # Group member endpoints - user_uuid is the internal UUID of the test user
  user_uuid: %s
`,
		time.Now().UTC().Format(time.RFC3339),
		tmID, tmID,
		threatID, diagramID, documentID, assetID, noteID, repoID,
		webhookID, deliveryID, addonID, credID,
		surveyID, responseID,
		metadataKey,
		adminGroupID, adminUUID,
		provider, user,
		provider, user,
		provider, provider,
		adminUUID, adminUUID,
	)

	return os.WriteFile(path, []byte(yaml), 0o600)
}

func findRefByKind(refs RefMap, kind string) string {
	for _, r := range refs {
		if r.Kind == kind {
			return r.ID
		}
	}
	return nilUUID
}
