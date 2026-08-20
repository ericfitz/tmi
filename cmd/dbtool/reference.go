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

	tmID := findRefByName(refs, tmRef(realThreatModelName), "threat_model")
	threatID := findRefByKind(refs, "threat")
	diagramID := findRefByKind(refs, "diagram")
	documentID := findRefByKind(refs, "document")
	assetID := findRefByKind(refs, "asset")
	noteID := findRefByKind(refs, "note")
	// Index 0 explicitly, for the same reason as survey-response:0 below: team
	// notes are seeded positionally and findRefByKind ranges over a map, so once
	// the #608 decoy note exists this could return either one — and the decoy is
	// the one DELETE consumes.
	teamNoteID := findRefByName(refs, teamNoteRef(realTeamName, 0), kindTeamNote)
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
	// By name, not by kind: the #608 decoy webhook makes a map-ranging lookup
	// nondeterministic, and returning the decoy here would leave the real
	// subscription unfuzzed while the decoy absorbed both roles.
	webhookID := findRefByName(refs, webhookRef(realWebhookName), "webhook")
	deliveryID := findRefByKind(refs, "webhook_test_delivery")
	addonID := findRefByKind(refs, "addon")
	credID := findRefByKind(refs, "client_credential")
	surveyID := findRefByKind(refs, "survey")
	// Index 0 explicitly: survey responses are seeded positionally and
	// findRefByKind ranges over a map, so with the #608 decoy present it
	// could return either one — and the decoy is the one DELETE consumes.
	responseID := findRefByName(refs, "survey-response:0", "survey_response")
	metadataKey := findRefByKind(refs, "metadata")
	teamID := findRefByName(refs, teamRef(realTeamName), "team")
	projectID := findRefByName(refs, projectRef(realProjectName), "project")

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
	adminGroupID := findRefByName(refs, groupRef(realGroupName), kindGroup)

	// Anchor-path decoys (#608). A campaign deletes 15 of its own seeded
	// fixtures, and because CATS walks paths in lexical order the anchor
	// (/threat_models/{threat_model_id}) is fuzzed BEFORE everything nested
	// under it — the seeded group died at test 3608 while the first
	// /admin/groups/{group_id}/members test was 4151, so every member
	// operation ran against a group that no longer existed. Pointing each
	// anchor path at a throwaway keeps DELETE coverage on the anchor while the
	// fixture the nested paths depend on survives.
	//
	// Only the exact anchor path is overridden; nested paths are different
	// path strings and keep the global value.
	decoys := []struct{ path, param, id string }{
		{"/teams/{team_id}", "team_id", findRefByName(refs, teamRef(throwawayTeamName), "")},
		{"/projects/{project_id}", "project_id", findRefByName(refs, projectRef(throwawayProjectName), "")},
		{"/threat_models/{threat_model_id}", "threat_model_id",
			findRefByName(refs, tmRef(throwawayThreatModelName), "")},
		{"/admin/groups/{group_id}", "group_id",
			findRefByName(refs, groupRef(throwawayGroupName), "")},
		// cats-target is itself a throwaway (#591), but it is the id every
		// NESTED /admin/users route uses, so the anchor needs its own decoy —
		// run 20260728T062255Z deleted a user at test 13815 and fuzzed
		// .../client_credentials immediately after.
		{"/admin/users/{user_id}", "user_id", findRefByName(refs, userRef("cats-decoy"), "")},
		// Survey responses are seeded positionally; index 1 is the decoy.
		{"/intake/survey_responses/{survey_response_id}", "survey_response_id",
			findRefByName(refs, "survey-response:1", "")},
		// Run 20260730T153736Z lost team_note_id and webhook_id to ordinary
		// (non-collapsed) DELETEs: both paths are leaves with no decoy, so the
		// fuzzer consumed the only seeded record. Team notes are positional like
		// survey responses, so index 1 is the decoy; it must hang off the REAL
		// team, because only the /teams/{team_id} anchor is overridden and the
		// nested path keeps the real team_id.
		{"/teams/{team_id}/notes/{team_note_id}", "team_note_id",
			findRefByName(refs, teamNoteRef(realTeamName, 1), "")},
		{"/admin/webhooks/subscriptions/{webhook_id}", "webhook_id",
			findRefByName(refs, webhookRef(throwawayWebhookName), "")},
	}
	decoySections := ""
	for _, d := range decoys {
		// A decoy that was not seeded must not emit a section: an empty or nil
		// value would make every request to the anchor 404 on a bad id, which
		// is worse than sharing the real fixture.
		if d.id == "" || d.id == nilUUID {
			continue
		}
		decoySections += fmt.Sprintf("%s:\n  %s: %s\n", d.path, d.param, d.id)
	}

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
  # They are kept for the day CATS substitutes refData into nested array
  # items; today the arrays that contain them are removed outright (below),
  # so nothing substitutes.
  team_id: %s
  project_id: %s
  related_team_id: %s
  related_project_id: %s
  # Body arrays removed from every generated payload, because CATS 13.8.0
  # cannot produce a SERVER-VALID item for any of them and so poisons every
  # POST/PUT to /projects and /teams (#596 and #604):
  #
  #   related_projects[] / related_teams[]  (#596, root-caused 2026-08-20)
  #     CATS's cyclic-reference guard (JsonUtils.isCyclicReference) tokenizes
  #     the property path on '_' as well as '#', and an array contributes its
  #     name twice (name + name.items), so related_projects#
  #     related_projects.items#related_project_id counts "related" x3 and is
  #     silently dropped at the fuzz engine's default --selfReferenceDepth 2
  #     ("cats generate" uses 4, which is why generate emits the field).
  #     GENERATION is fixed machine-locally by passing --selfReferenceDepth 4
  #     in .local/cats/config.yaml extra_args (also fixes the third victim,
  #     color_palette[].color, which needs no refData -- its regex pattern
  #     generates a valid hex color). But the generated id is the spec's
  #     canonical example UUID (aaaaaaaa-.../bbbbbbbb-...), and refData does
  #     NOT substitute into nested array items (re-verified 2026-08-20: with
  #     the arrays restored, related_project_id carried the example and
  #     user_id a CATS-random UUID, neither the refData value), so the server
  #     correctly answers "related project not found" and the requests 400.
  #     The arrays therefore stay removed until the fixtures carry the
  #     canonical example UUIDs (or CATS substitutes nested items) -- see the
  #     coverage-restoration follow-up filed from #596.
  #
  #   responsible_parties[] / members[]  (#604)
  #     No longer a SCHEMA problem: the spec now splits request and response
  #     shapes (TeamMemberInput / ResponsiblePartyInput have no "user" at all),
  #     so CATS generates structurally valid items and the readOnly rejection
  #     is gone. They stay removed for a different reason -- CATS fills the
  #     nested user_id with a random UUID that refData does not substitute, so
  #     the server correctly answers "user not found for responsible party" and
  #     POST /projects + POST /teams go back to 400 on every test. Removing
  #     them is now a coverage trade, not a workaround for a blocker: drop
  #     these two the moment nested items can carry real ids (same follow-up
  #     as above).
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
%s%s`,
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
		decoySections,
		auditSections,
	)

	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		return err
	}

	// Fixture manifest for the CATS runner's fixture-integrity gates (#608).
	// Written alongside the refData it describes so the two can never disagree
	// about which ids a campaign depends on.
	return writeFixtureManifest(
		filepath.Join(filepath.Dir(path), "cats-fixtures.json"),
		fixtureIDs{
			threatModelID: tmID, threatID: threatID, diagramID: diagramID,
			documentID: documentID, assetID: assetID, noteID: noteID,
			feedbackID: feedbackID, repositoryID: repoID,
			teamID: teamID, projectID: projectID,
			teamNoteID: teamNoteID, projectNoteID: projectNoteID,
			groupID: adminGroupID, userID: targetUUID,
			surveyID: surveyID, surveyResponseID: responseID,
			triageNoteID: triageNoteID, webhookID: webhookID, deliveryID: deliveryID,
		},
	)
}

// SEM@0: resolved seeded ids the fixture manifest is built from (pure)
type fixtureIDs struct {
	threatModelID, threatID, diagramID, documentID, assetID, noteID string
	feedbackID, repositoryID                                        string
	teamID, projectID, teamNoteID, projectNoteID                    string
	groupID, userID                                                 string
	surveyID, surveyResponseID, triageNoteID                        string
	webhookID, deliveryID                                           string
}

// SEM@0: one seeded fixture the campaign depends on, plus how to check it is still alive (pure)
type referenceFixture struct {
	// Key is the refData key CATS substitutes this id into.
	Key string `json:"key"`
	ID  string `json:"id"`
	// VerifyURL is a server-relative GET that returns 200 iff the fixture
	// still exists. Parent ids are already resolved.
	VerifyURL string `json:"verify_url"`
	// AnchorPath is the CATS path template whose DELETE would consume this
	// fixture. Named in the gate's failure message so the fix (add a decoy
	// override for that anchor) is obvious from the error alone.
	AnchorPath string `json:"anchor_path"`
	// Decoyed records whether AnchorPath already has a decoy override, so a
	// fixture that dies anyway is reported as "decoy not working" rather than
	// "decoy missing".
	Decoyed bool `json:"decoyed"`
}

// SEM@0: fixture manifest file structure listing verifiable and unverifiable seeded fixtures (pure)
type fixtureManifest struct {
	Version   string             `json:"version"`
	CreatedAt string             `json:"created_at"`
	Fixtures  []referenceFixture `json:"fixtures"`
	// Unverifiable names fixtures the gates cannot check, with the reason.
	// Recorded explicitly rather than omitted: a gate that silently skips
	// part of the fixture set reads as "all fixtures survived" when it is
	// really "the ones I could see survived".
	Unverifiable map[string]string `json:"unverifiable"`
}

// SEM@0: serialize the seeded-fixture integrity manifest consumed by the CATS runner gates (mutates shared state)
func writeFixtureManifest(path string, ids fixtureIDs) error {
	tm := ids.threatModelID
	// Anchors that writeYAMLReference emits a decoy override for. Keep in step
	// with the `decoys` slice above.
	decoyed := map[string]bool{
		"/teams/{team_id}":                              true,
		"/projects/{project_id}":                        true,
		"/threat_models/{threat_model_id}":              true,
		"/admin/groups/{group_id}":                      true,
		"/admin/users/{user_id}":                        true,
		"/intake/survey_responses/{survey_response_id}": true,
		"/teams/{team_id}/notes/{team_note_id}":         true,
		"/admin/webhooks/subscriptions/{webhook_id}":    true,
	}

	candidates := []referenceFixture{
		{"threat_model_id", tm, "/threat_models/" + tm, "/threat_models/{threat_model_id}", false},
		{"threat_id", ids.threatID, "/threat_models/" + tm + "/threats/" + ids.threatID,
			"/threat_models/{threat_model_id}/threats/{threat_id}", false},
		{"diagram_id", ids.diagramID, "/threat_models/" + tm + "/diagrams/" + ids.diagramID,
			"/threat_models/{threat_model_id}/diagrams/{diagram_id}", false},
		{"document_id", ids.documentID, "/threat_models/" + tm + "/documents/" + ids.documentID,
			"/threat_models/{threat_model_id}/documents/{document_id}", false},
		{"asset_id", ids.assetID, "/threat_models/" + tm + "/assets/" + ids.assetID,
			"/threat_models/{threat_model_id}/assets/{asset_id}", false},
		{"note_id", ids.noteID, "/threat_models/" + tm + "/notes/" + ids.noteID,
			"/threat_models/{threat_model_id}/notes/{note_id}", false},
		{"feedback_id", ids.feedbackID, "/threat_models/" + tm + "/feedback/" + ids.feedbackID,
			"/threat_models/{threat_model_id}/feedback/{feedback_id}", false},
		{"repository_id", ids.repositoryID, "/threat_models/" + tm + "/repositories/" + ids.repositoryID,
			"/threat_models/{threat_model_id}/repositories/{repository_id}", false},
		{"team_id", ids.teamID, "/teams/" + ids.teamID, "/teams/{team_id}", false},
		{"project_id", ids.projectID, "/projects/" + ids.projectID, "/projects/{project_id}", false},
		{"team_note_id", ids.teamNoteID, "/teams/" + ids.teamID + "/notes/" + ids.teamNoteID,
			"/teams/{team_id}/notes/{team_note_id}", false},
		{"project_note_id", ids.projectNoteID, "/projects/" + ids.projectID + "/notes/" + ids.projectNoteID,
			"/projects/{project_id}/notes/{project_note_id}", false},
		{"group_id", ids.groupID, "/admin/groups/" + ids.groupID, "/admin/groups/{group_id}", false},
		{"user_id", ids.userID, "/admin/users/" + ids.userID, "/admin/users/{user_id}", false},
		{"survey_id", ids.surveyID, "/admin/surveys/" + ids.surveyID, "/admin/surveys/{survey_id}", false},
		{"survey_response_id", ids.surveyResponseID, "/intake/survey_responses/" + ids.surveyResponseID,
			"/intake/survey_responses/{survey_response_id}", false},
		{"triage_note_id", ids.triageNoteID,
			"/intake/survey_responses/" + ids.surveyResponseID + "/triage_notes/" + ids.triageNoteID,
			"/intake/survey_responses/{survey_response_id}/triage_notes/{triage_note_id}", false},
		{"webhook_id", ids.webhookID, "/admin/webhooks/subscriptions/" + ids.webhookID,
			"/admin/webhooks/subscriptions/{webhook_id}", false},
		{"delivery_id", ids.deliveryID, "/admin/webhooks/deliveries/" + ids.deliveryID,
			"/admin/webhooks/deliveries/{delivery_id}", false},
	}

	// #nosec G101 -- these are refData key names and prose reasons, not credentials
	unverifiable := map[string]string{
		"credential_id": "spec has no GET for /me/client_credentials/{credential_id} (delete-only)",
		"addon_id":      "no spec path uses {addon_id}",
		"key":           "metadata key is not an addressable resource",
		"entry_id":      "audit entries are harvested after seeding, not seeded, and are not deletable fixtures",
	}

	fixtures := make([]referenceFixture, 0, len(candidates))
	for _, f := range candidates {
		// A fixture that was never seeded has no integrity to check, and
		// including it would make the gate fail every run on a false death.
		if f.ID == "" || f.ID == nilUUID {
			unverifiable[f.Key] = "not present in this seed run"
			continue
		}
		f.Decoyed = decoyed[f.AnchorPath]
		fixtures = append(fixtures, f)
	}

	data, err := json.MarshalIndent(fixtureManifest{
		Version:      "1.0.0",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		Fixtures:     fixtures,
		Unverifiable: unverifiable,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal fixture manifest: %w", err)
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Seed refs for the destructive-fuzz decoys. Anchor paths (/teams/{team_id},
// /threat_models/{threat_model_id}, ...) are pointed at these while everything
// nested underneath keeps the real fixture, so a successful DELETE consumes
// the decoy instead of the resource 63 nested paths depend on (#608).
const (
	throwawayTeamName        = "CATS Throwaway Team"
	throwawayProjectName     = "CATS Throwaway Project"
	throwawayGroupName       = "CATS Throwaway Group"
	throwawayThreatModelName = "CATS Throwaway Threat Model"
	throwawayWebhookName     = "CATS Throwaway Webhook"
	realTeamName             = "CATS Test Team"
	realWebhookName          = "CATS Test Webhook"
	realProjectName          = "CATS Test Project"
	realGroupName            = "CATS Test Group"
	realThreatModelName      = "CATS Test Threat Model"
)

// findRefByName returns a seeded id by its exact seed ref, falling back to the
// first ref of `kind` when that ref is absent.
//
// The fallback is what keeps a seed file that defines only one team (or one
// threat model) working. It is also why the named lookup has to come first:
// findRefByKind ranges over a map, so with two instances seeded it would
// return an arbitrary one — fine while every kind had exactly one instance,
// actively wrong now that the decoys exist.
// SEM@a3b4c5d6e7f8091a2b3c4d5e6f70819293041526: fetch a seeded id by exact ref, falling back to the first of a kind (pure)
func findRefByName(refs RefMap, ref, kind string) string {
	if r, ok := refs[ref]; ok && r.ID != "" {
		return r.ID
	}
	return findRefByKind(refs, kind)
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
