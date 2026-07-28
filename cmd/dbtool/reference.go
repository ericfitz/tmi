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

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: write seeded resource IDs to JSON and/or YAML reference files for CATS parameter substitution (mutates shared state)
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

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: JSON reference file structure mapping seeded object names to their IDs and metadata (pure)
type referenceJSON struct {
	Version   string                     `json:"version"`
	CreatedAt string                     `json:"created_at"`
	Server    string                     `json:"server"`
	User      referenceUser              `json:"user"`
	Objects   map[string]referenceObject `json:"objects"`
}

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: user identity embedded in a JSON reference file (pure)
type referenceUser struct {
	ProviderUserID string `json:"provider_user_id"`
	Provider       string `json:"provider"`
	Email          string `json:"email"`
}

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: a single seeded resource record with ID, kind, and optional relationship IDs (pure)
type referenceObject struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Name          string `json:"name,omitempty"`
	ThreatModelID string `json:"threat_model_id,omitempty"`
	URL           string `json:"url,omitempty"`
	SurveyID      string `json:"survey_id,omitempty"`
}

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: serialize seeded resource refs to a versioned JSON file at the given path (mutates shared state)
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

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: serialize seeded resource IDs to a CATS-compatible YAML parameter substitution file (mutates shared state)
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
	teamNoteID := findRefByKind(refs, kindTeamNote)
	projectNoteID := findRefByKind(refs, kindProjectNote)
	feedbackID := findRefByKind(refs, kindFeedback)
	// TriageNote.id is a per-survey-response sequential INTEGER, not a UUID, so
	// the nilUUID fallback the other lookups use would be a type error in that
	// path parameter (a 400 rather than the 404 a missing id should produce).
	triageNoteID := findRefByKind(refs, kindTriageNote)
	if triageNoteID == nilUUID {
		triageNoteID = "0"
	}
	repoID := findRefByKind(refs, "repository")
	webhookID := findRefByKind(refs, "webhook")
	deliveryID := findRefByKind(refs, "webhook_test_delivery")
	addonID := findRefByKind(refs, "addon")
	credID := findRefByKind(refs, "client_credential")
	surveyID := findRefByKind(refs, "survey")
	responseID := findRefByKind(refs, "survey_response")
	metadataKey := findRefByKind(refs, "metadata")
	teamID := findRefByKind(refs, "team")
	projectID := findRefByKind(refs, "project")

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

	// targetUUID identifies the account used for destructive/mutating admin
	// user endpoints (PATCH/DELETE /admin/users/{user_id}, quota PUT/DELETE,
	// ownership transfer, client-credential deletion, ...). It must NEVER be the
	// fuzzing identity's own account (#591): a successful mutation against the
	// live identity (e.g. a 200 from an injection fuzzer) invalidates its bearer
	// token, silently running the rest of the campaign unauthenticated. Prefer a
	// dedicated throwaway user seeded under ref "user:cats-target"; fall back to
	// the first seeded user (historically the fuzzing identity itself) only for
	// seed files that don't define a throwaway target, to preserve old behavior.
	targetUUID := adminUUID
	if r, ok := refs[userRef("cats-target")]; ok {
		targetUUID = r.ID
	}

	// The group routes used to share the user routes' {internal_uuid} parameter
	// name, so a single global refData value could not satisfy both and the
	// group family permanently 404d. The spec now names them {group_id} and
	// {user_id} (#603), so this is just an ordinary global value again — the
	// per-path override sections this file used to emit are gone.
	adminGroupID := findRefByKind(refs, kindGroup)

	// Per-path sections for the harvested audit entry ids. Emitting a section
	// with an empty value would be worse than omitting it: CATS would then
	// substitute "" into the URL and every request would 404 on a malformed
	// path rather than falling back to the global value.
	auditSections := ""
	for _, s := range []struct{ kind, path string }{
		{"audit_entry_system", "/admin/audit/system/{entry_id}"},
		{"audit_entry_admin_tm", "/admin/audit/threat_models/{entry_id}"},
		{"audit_entry_tm_trail", "/threat_models/{threat_model_id}/audit_trail/{entry_id}"},
		{"audit_entry_tm_trail", "/threat_models/{threat_model_id}/audit_trail/{entry_id}/rollback"},
	} {
		if id := findRefByKind(refs, s.kind); id != "" && id != nilUUID {
			auditSections += fmt.Sprintf("%s:\n  entry_id: %s\n", s.path, id)
		}
	}

	// Syntactically-valid actor email for the audit-endpoint refData (#494). The
	// value only needs to pass the OpenAPI email-format check so HappyPath returns
	// 200 (an empty result set is still 200); it need not match a real audit row.
	actorEmail := fmt.Sprintf("%s@tmi.local", user)

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
  # Notes on teams and projects are separate resources from threat-model notes
  # and have their own path parameters (#597).
  team_note_id: %s
  project_note_id: %s
  # Content feedback on a threat model, and triage notes on a survey response.
  feedback_id: %s
  triage_note_id: %s
  repository_id: %s
  webhook_id: %s
  delivery_id: %s
  addon_id: %s
  client_credential_id: %s
  # credential_id is the spec's actual path parameter name for
  # /me/client_credentials/{credential_id} and
  # /admin/users/{user_id}/client_credentials/{credential_id} (#590);
  # client_credential_id above is kept for backward compatibility.
  credential_id: %s
  survey_id: %s
  survey_response_id: %s
  key: %s
  # team_id/project_id for /teams/{team_id} and /projects/{project_id}.
  # related_team_id/related_project_id are body fields nested inside
  # related_teams[]/related_projects[] (#590, #582's canonical UUID spec
  # examples) - CATS substitutes by field name in bodies too, so pointing
  # these at real seeded team/project ids makes those requests validate
  # without needing the seeded entities to literally carry the canonical ids.
  # They are kept for the day the CATS defect below is fixed upstream; today
  # the arrays that contain them are removed outright, so nothing substitutes.
  team_id: %s
  project_id: %s
  related_team_id: %s
  related_project_id: %s
  # Body arrays removed from every generated payload, because CATS 13.8.0
  # cannot produce a valid item for any of them and so poisons every
  # POST/PUT to /projects and /teams (#596 and #604):
  #
  #   related_projects[] / related_teams[]  (#596)
  #     CATS drops a nested property whose name shares an underscore-delimited
  #     token with an ANCESTOR property's name -- a self-reference guard that
  #     matches on name similarity rather than actual schema recursion. So
  #     related_projects[].related_project_id is dropped ("related" collides
  #     with the parent array), while responsible_parties[].user_id is not.
  #     Verified by renaming the parent array to linked_projects, at which
  #     point related_project_id appears. Since related_project_id is
  #     required, every generated item fails validation.
  #
  #   responsible_parties[] / members[]  (#604)
  #     CATS ignores "readOnly: true" on request-body properties, so it emits
  #     the server-populated "user" object inside each item and TMI's OpenAPI
  #     validation middleware rejects the request. refData cannot remove a
  #     nested field (only top-level ones), so the whole array goes.
  #
  # Cost: these four arrays are not fuzzed. Before this, POST /projects and
  # POST /teams returned 400 on EVERY test, so no part of either resource was
  # fuzzed at all. Drop the entries below as each defect is resolved.
  related_projects: cats_remove_field
  related_teams: cats_remove_field
  responsible_parties: cats_remove_field
  members: cats_remove_field
  # Admin resource identifiers. group_id serves /admin/groups/{group_id}* and
  # /me/groups/{group_id}/members; both used to be spelled {internal_uuid},
  # which collided with the user routes and made every group happy path
  # unreachable (#603). The rename removed the collision, so the per-path
  # override sections this file used to emit are gone.
  group_id: %s
  # member_uuid identifies the USER being added to or removed from a group,
  # so it points at the throwaway target for the same reason user_id does.
  member_uuid: %s
  # User identity uses provider:provider_id format
  user_provider: %s
  user_provider_id: %s
  admin_user_provider: %s
  admin_user_provider_id: %s
  # SAML/OAuth provider endpoints - uses the IDP name directly
  provider: %s
  idp: %s
  # user_id is the user's internal UUID. It now serves BOTH the admin quota
  # endpoints and /admin/users/{user_id}* (formerly {internal_uuid}, #603).
  # Deliberately NOT the fuzzing identity's own account (#591): a successful
  # mutation here (PATCH/DELETE) would invalidate the identity's own bearer
  # token mid-campaign. Points at a dedicated throwaway seeded user instead.
  user_id: %s
  # Group member endpoints - user_uuid is the internal UUID of the test user.
  # Not currently a real path parameter name in the spec (see member_uuid),
  # kept for compatibility; also switched to the throwaway target user.
  user_uuid: %s
# Admin audit list endpoints (#494): supply valid values for the query params CATS
# otherwise randomizes, so HappyPath reaches the server cleanly (200) and the
# field-mutation fuzzers exercise their target field instead of every request
# dying on a malformed param. 'cursor' (an opaque keyset token) and 'around'
# (an entry UUID) are intentionally omitted - they have no static valid value;
# the residual injection-fuzzer 400s are classified as false positives by
# the CATS plugin's rule set (test/cats/false-positives.yaml, AUDIT_QUERY_VALIDATION_400).
/admin/audit/threat_models:
  actor_email: %s
  actor_provider: %s
  created_after: "2020-01-01T00:00:00Z"
  created_before: "2035-01-01T00:00:00Z"
  threat_model_id: %s
/admin/audit/system:
  actor_email: %s
  actor_provider: %s
  created_after: "2020-01-01T00:00:00Z"
  created_before: "2035-01-01T00:00:00Z"
# entry_id names three different audit families (system, admin threat-model,
# and a threat model's own trail) — the one-name-many-resources problem #603
# fixed for groups vs users, still present here. These ids are harvested after
# seeding rather than seeded, because audit entries are a side effect of the
# seeding itself. A section is omitted entirely when its listing was empty, in
# which case that family stays uncovered exactly as before.
%s`,
		time.Now().UTC().Format(time.RFC3339),
		tmID, tmID,
		threatID, diagramID, documentID, assetID, noteID,
		teamNoteID, projectNoteID, feedbackID, triageNoteID, repoID,
		webhookID, deliveryID, addonID, credID,
		credID,
		surveyID, responseID,
		metadataKey,
		teamID, projectID, teamID, projectID,
		adminGroupID, targetUUID,
		provider, user,
		provider, user,
		provider, provider,
		targetUUID, targetUUID,
		actorEmail, provider, tmID,
		actorEmail, provider,
		auditSections,
	)

	return os.WriteFile(path, []byte(yaml), 0o600)
}

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: return the ID of the first ref matching a resource kind, or the nil UUID if not found (pure)
func findRefByKind(refs RefMap, kind string) string {
	for _, r := range refs {
		if r.Kind == kind {
			return r.ID
		}
	}
	return nilUUID
}
