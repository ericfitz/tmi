package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// referenceJSON is the JSON structure for cats-test-data.json.
type referenceJSON struct {
	Version   string           `json:"version"`
	CreatedAt string           `json:"created_at"`
	Server    string           `json:"server"`
	User      referenceUser    `json:"user"`
	Objects   referenceObjects `json:"objects"`
}

type referenceUser struct {
	ProviderUserID string `json:"provider_user_id"`
	Provider       string `json:"provider"`
	Email          string `json:"email"`
}

type referenceObjects struct {
	ThreatModel      referenceObject `json:"threat_model"`
	Threat           referenceObject `json:"threat"`
	Diagram          referenceObject `json:"diagram"`
	Document         referenceObject `json:"document"`
	Asset            referenceObject `json:"asset"`
	Note             referenceObject `json:"note"`
	Repository       referenceObject `json:"repository"`
	Addon            referenceObject `json:"addon"`
	Webhook          referenceObject `json:"webhook"`
	ClientCredential referenceObject `json:"client_credential"`
	Survey           referenceObject `json:"survey"`
	SurveyResponse   referenceObject `json:"survey_response"`
	MetadataKey      string          `json:"metadata_key"`
}

type referenceObject struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Title         string `json:"title,omitempty"`
	ThreatModelID string `json:"threat_model_id,omitempty"`
	URL           string `json:"url,omitempty"`
	SurveyID      string `json:"survey_id,omitempty"`
}

// writeReferenceFiles writes both JSON and YAML reference files for CATS.
func writeReferenceFiles(outputPath, serverURL, user, provider string, results *testDataResults) error {
	log := slogging.Get()

	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}

	// Write JSON reference file
	log.Info("Step 7: Writing reference files...")
	if err := writeJSONReference(outputPath, serverURL, user, provider, results); err != nil {
		return err
	}
	log.Info("  JSON reference: %s", outputPath)

	// Write YAML reference file
	yamlPath := strings.TrimSuffix(outputPath, filepath.Ext(outputPath)) + ".yml"
	if err := writeYAMLReference(yamlPath, results); err != nil {
		return err
	}
	log.Info("  YAML reference: %s", yamlPath)

	return nil
}

func writeJSONReference(path, serverURL, user, provider string, r *testDataResults) error {
	ref := referenceJSON{
		Version:   "1.0.0",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Server:    serverURL,
		User: referenceUser{
			ProviderUserID: user,
			Provider:       provider,
			Email:          user + "@tmi.local",
		},
		Objects: referenceObjects{
			ThreatModel: referenceObject{
				ID:   r.ThreatModelID,
				Name: "CATS Test Threat Model",
			},
			Threat: referenceObject{
				ID:            r.ThreatID,
				ThreatModelID: r.ThreatModelID,
				Name:          "CATS Test Threat",
			},
			Diagram: referenceObject{
				ID:            r.DiagramID,
				ThreatModelID: r.ThreatModelID,
				Title:         "CATS Test Diagram",
			},
			Document: referenceObject{
				ID:            r.DocumentID,
				ThreatModelID: r.ThreatModelID,
				Name:          "CATS Test Document",
			},
			Asset: referenceObject{
				ID:            r.AssetID,
				ThreatModelID: r.ThreatModelID,
				Name:          "CATS Test Asset",
			},
			Note: referenceObject{
				ID:            r.NoteID,
				ThreatModelID: r.ThreatModelID,
			},
			Repository: referenceObject{
				ID:            r.RepositoryID,
				ThreatModelID: r.ThreatModelID,
				URL:           "https://github.com/example/cats-test-repo",
			},
			Addon: referenceObject{
				ID:            r.AddonID,
				ThreatModelID: r.ThreatModelID,
				Name:          "CATS Test Addon",
			},
			Webhook: referenceObject{
				ID:  r.WebhookID,
				URL: "https://webhook.site/cats-test-webhook",
			},
			ClientCredential: referenceObject{
				ID:   r.ClientCredentialID,
				Name: "CATS Test Credential",
			},
			Survey: referenceObject{
				ID:   r.SurveyID,
				Name: "CATS Test Survey",
			},
			SurveyResponse: referenceObject{
				ID:       r.SurveyResponseID,
				SurveyID: r.SurveyID,
			},
			MetadataKey: metadataKey,
		},
	}

	data, err := json.MarshalIndent(ref, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON reference: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0o600)
}

func writeYAMLReference(path string, r *testDataResults) error {
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
		r.ThreatModelID,
		r.ThreatModelID,
		r.ThreatID,
		r.DiagramID,
		r.DocumentID,
		r.AssetID,
		r.NoteID,
		r.RepositoryID,
		r.WebhookID,
		r.AddonID,
		r.ClientCredentialID,
		r.SurveyID,
		r.SurveyResponseID,
		metadataKey,
		r.AdminGroupID,
		r.AdminUserInternalUUID,
		r.UserProvider,
		r.UserProviderID,
		r.AdminUserProvider,
		r.AdminUserProviderID,
		r.UserProvider,
		r.UserProvider,
		r.AdminUserInternalUUID,
		r.AdminUserInternalUUID,
	)

	return os.WriteFile(path, []byte(yaml), 0o600)
}
