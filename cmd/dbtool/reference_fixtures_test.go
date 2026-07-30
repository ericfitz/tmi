package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// readManifest writes a manifest for `ids` into a temp dir and parses it back.
func readManifest(t *testing.T, ids fixtureIDs) fixtureManifest {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cats-fixtures.json")
	if err := writeFixtureManifest(path, ids); err != nil {
		t.Fatalf("writeFixtureManifest: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m fixtureManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return m
}

func fullIDs() fixtureIDs {
	return fixtureIDs{
		threatModelID: "tm-1", threatID: "th-1", diagramID: "dg-1",
		documentID: "doc-1", assetID: "as-1", noteID: "nt-1",
		feedbackID: "fb-1", repositoryID: "repo-1",
		teamID: "team-1", projectID: "proj-1",
		teamNoteID: "tn-1", projectNoteID: "pn-1",
		groupID: "grp-1", userID: "usr-1",
		surveyID: "sv-1", surveyResponseID: "sr-1", triageNoteID: "1",
		webhookID: "wh-1", deliveryID: "dl-1",
	}
}

// A fully-seeded run must produce a verify URL for every fixture, with parent
// ids resolved inline — the runner GETs these verbatim, so an unsubstituted
// "{threat_model_id}" would make the gate report a false fixture death.
func TestFixtureManifestResolvesParentIDs(t *testing.T) {
	m := readManifest(t, fullIDs())

	want := map[string]string{
		"threat_model_id":    "/threat_models/tm-1",
		"threat_id":          "/threat_models/tm-1/threats/th-1",
		"diagram_id":         "/threat_models/tm-1/diagrams/dg-1",
		"asset_id":           "/threat_models/tm-1/assets/as-1",
		"feedback_id":        "/threat_models/tm-1/feedback/fb-1",
		"repository_id":      "/threat_models/tm-1/repositories/repo-1",
		"team_id":            "/teams/team-1",
		"project_id":         "/projects/proj-1",
		"team_note_id":       "/teams/team-1/notes/tn-1",
		"project_note_id":    "/projects/proj-1/notes/pn-1",
		"group_id":           "/admin/groups/grp-1",
		"user_id":            "/admin/users/usr-1",
		"survey_response_id": "/intake/survey_responses/sr-1",
		"triage_note_id":     "/intake/survey_responses/sr-1/triage_notes/1",
		"webhook_id":         "/admin/webhooks/subscriptions/wh-1",
		"delivery_id":        "/admin/webhooks/deliveries/dl-1",
	}

	got := make(map[string]string, len(m.Fixtures))
	for _, f := range m.Fixtures {
		got[f.Key] = f.VerifyURL
	}
	for key, url := range want {
		if got[key] != url {
			t.Errorf("fixture %q verify_url = %q, want %q", key, got[key], url)
		}
	}
}

// Every anchor that writeYAMLReference emits a decoy override for must be
// marked decoyed, so a fixture that dies anyway is reported as "the decoy is
// not working" rather than sending the reader off to add one that exists.
func TestFixtureManifestMarksDecoyedAnchors(t *testing.T) {
	m := readManifest(t, fullIDs())

	decoyed := map[string]bool{
		"threat_model_id": true, "team_id": true, "project_id": true,
		"group_id": true, "user_id": true, "survey_response_id": true,
		// Nested fixtures have no anchor decoy.
		"threat_id": false, "note_id": false, "team_note_id": false,
		"survey_id": false, "webhook_id": false,
	}
	for _, f := range m.Fixtures {
		want, tracked := decoyed[f.Key]
		if tracked && f.Decoyed != want {
			t.Errorf("fixture %q decoyed = %v, want %v (anchor %s)",
				f.Key, f.Decoyed, want, f.AnchorPath)
		}
	}
}

// An id that was never seeded must be reported as unverifiable rather than
// emitted as a fixture: the gate would GET a nil-UUID URL, get a 404, and fail
// every run on a fixture that never existed.
func TestFixtureManifestExcludesUnseededIDs(t *testing.T) {
	ids := fullIDs()
	ids.webhookID = nilUUID
	ids.deliveryID = ""

	m := readManifest(t, ids)
	for _, f := range m.Fixtures {
		if f.Key == "webhook_id" || f.Key == "delivery_id" {
			t.Errorf("unseeded fixture %q must not be verifiable (id %q)", f.Key, f.ID)
		}
	}
	for _, key := range []string{"webhook_id", "delivery_id"} {
		if _, ok := m.Unverifiable[key]; !ok {
			t.Errorf("unseeded fixture %q must appear in unverifiable", key)
		}
	}
	// Fixtures with no individual GET in the spec are permanently unverifiable
	// and must be named, not silently dropped.
	for _, key := range []string{"credential_id", "addon_id"} {
		if _, ok := m.Unverifiable[key]; !ok {
			t.Errorf("%q must be recorded as unverifiable", key)
		}
	}
}
