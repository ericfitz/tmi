package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
		// team_note_id and webhook_id are leaves rather than anchors, but run
		// 20260730T153736Z showed an undecoyed leaf is consumed just as readily:
		// an ordinary DELETE took the only seeded record and the run went INVALID.
		// They now get decoys of their own.
		"team_note_id": true, "webhook_id": true,
		// Threat-model children with nested paths (metadata, restore, audit_trail)
		// and a DELETE on their own path (#1115): CATS walks lexical order, so an
		// undecoyed DELETE runs before every nested path and leaves them all on a
		// deleted fixture.
		"diagram_id": true, "threat_id": true, "document_id": true,
		"asset_id": true, "note_id": true, "repository_id": true,
		// /admin/surveys/{survey_id} has nested paths and a DELETE (#1115).
		"survey_id": true,
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

// childFixtures lists the threat-model children that get a real fixture plus a
// throwaway decoy (#1115): the kind, the seed ref builder for each name, the
// exact CATS anchor path, and the refData parameter name.
var childFixtures = []struct {
	kind, param, path   string
	realName, decoyName string
	ref                 func(name string) string
}{
	{"diagram", "diagram_id", "/threat_models/{threat_model_id}/diagrams/{diagram_id}",
		realDiagramName, throwawayDiagramName,
		func(n string) string { return childRef("diagram", realThreatModelName, n) }},
	{"threat", "threat_id", "/threat_models/{threat_model_id}/threats/{threat_id}",
		realThreatName, throwawayThreatName,
		func(n string) string { return childRef("threat", realThreatModelName, n) }},
	{"document", "document_id", "/threat_models/{threat_model_id}/documents/{document_id}",
		realDocumentName, throwawayDocumentName,
		func(n string) string { return childRef("document", realThreatModelName, n) }},
	{"asset", "asset_id", "/threat_models/{threat_model_id}/assets/{asset_id}",
		realAssetName, throwawayAssetName,
		func(n string) string { return childRef("asset", realThreatModelName, n) }},
	{"note", "note_id", "/threat_models/{threat_model_id}/notes/{note_id}",
		realNoteName, throwawayNoteName,
		func(n string) string { return childRef("note", realThreatModelName, n) }},
	{"repository", "repository_id", "/threat_models/{threat_model_id}/repositories/{repository_id}",
		realRepositoryName, throwawayRepositoryName,
		func(n string) string { return childRef("repo", realThreatModelName, n) }},
}

// With two instances of a kind seeded, the global refData id must still be the
// REAL fixture (findRefByKind ranges over a map and would pick either), and the
// exact anchor path must be overridden with the decoy (#1115).
func TestWriteYAMLReferenceDecoysThreatModelChildren(t *testing.T) {
	refs := RefMap{
		tmRef(realThreatModelName):      {Kind: "threat_model", ID: "tm-real"},
		tmRef(throwawayThreatModelName): {Kind: "threat_model", ID: "tm-decoy"},
	}
	for _, c := range childFixtures {
		refs[c.ref(c.realName)] = &SeedResult{Kind: c.kind, ID: c.kind + "-real"}
		refs[c.ref(c.decoyName)] = &SeedResult{Kind: c.kind, ID: c.kind + "-decoy"}
	}

	// Map iteration order is random per range; repeat so a kind-based lookup
	// cannot pass by luck.
	for i := 0; i < 40; i++ {
		path := filepath.Join(t.TempDir(), "cats-test-data.yml")
		if err := writeYAMLReference(path, refs, "charlie", "tmi"); err != nil {
			t.Fatalf("writeYAMLReference: %v", err)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
		if err != nil {
			t.Fatalf("read refData: %v", err)
		}
		var doc map[string]map[string]any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse refData: %v", err)
		}
		for _, c := range childFixtures {
			if got := doc["all"][c.param]; got != c.kind+"-real" {
				t.Fatalf("all.%s = %v, want %s-real (global id must be the real fixture)", c.param, got, c.kind)
			}
			sec, ok := doc[c.path]
			if !ok {
				t.Fatalf("no decoy section for %s", c.path)
			}
			if got := sec[c.param]; got != c.kind+"-decoy" {
				t.Fatalf("%s: %s = %v, want %s-decoy", c.path, c.param, got, c.kind)
			}
		}
	}
}

// The checked-in CATS seed must define a real and a throwaway instance of every
// decoyed child kind under the real threat model, with the names the reference
// writer resolves them by (#1115).
func TestCatsSeedDefinesChildDecoys(t *testing.T) {
	seedFile, err := loadSeedFile("../../test/seeds/cats-seed-data.json")
	if err != nil {
		t.Fatalf("loadSeedFile: %v", err)
	}
	have := map[string]bool{}
	for _, e := range seedFile.Seeds {
		have[e.Ref] = true
	}
	for _, c := range childFixtures {
		for _, name := range []string{c.realName, c.decoyName} {
			if ref := c.ref(name); !have[ref] {
				t.Errorf("seed lacks %s %q (ref %q)", c.kind, name, ref)
			}
		}
	}
}

// survey_id has the same failure class as the threat-model children: DELETE on
// /admin/surveys/{survey_id} sorts before its nested paths. The decoy must be a
// survey of its own, never the one that carries the survey_response decoy.
func TestWriteYAMLReferenceDecoysSurvey(t *testing.T) {
	refs := RefMap{
		surveyRef(realSurveyName):      {Kind: "survey", ID: "sv-real"},
		surveyRef("CATS Decoy Survey"): {Kind: "survey", ID: "sv-responses-decoy"},
		surveyRef(throwawaySurveyName): {Kind: "survey", ID: "sv-decoy"},
	}
	for i := 0; i < 40; i++ {
		path := filepath.Join(t.TempDir(), "cats-test-data.yml")
		if err := writeYAMLReference(path, refs, "charlie", "tmi"); err != nil {
			t.Fatalf("writeYAMLReference: %v", err)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
		if err != nil {
			t.Fatalf("read refData: %v", err)
		}
		var doc map[string]map[string]any
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse refData: %v", err)
		}
		if got := doc["all"]["survey_id"]; got != "sv-real" {
			t.Fatalf("all.survey_id = %v, want sv-real", got)
		}
		if got := doc["/admin/surveys/{survey_id}"]["survey_id"]; got != "sv-decoy" {
			t.Fatalf("survey anchor = %v, want sv-decoy", got)
		}
	}
}

// Every ref a seed entry points at (target_ref and the *_ref fields) must
// resolve to an entry defined EARLIER in the file, because the seeder resolves
// them in order against the refs created so far. Renaming a fixture used to
// break a metadata target only at live-seed time (#1115). Pure and offline.
func TestCatsSeedRefsAllResolve(t *testing.T) {
	seedFile, err := loadSeedFile("../../test/seeds/cats-seed-data.json")
	if err != nil {
		t.Fatalf("loadSeedFile: %v", err)
	}
	defined := map[string]bool{}
	for i, e := range seedFile.Seeds {
		for field, v := range e.Data {
			if field != "target_ref" && !strings.HasSuffix(field, "_ref") {
				continue
			}
			ref, ok := v.(string)
			if !ok || ref == "" {
				continue
			}
			if !defined[ref] {
				t.Errorf("seed entry %d (kind=%s, ref=%q): %s %q is not defined by an earlier entry",
					i, e.Kind, e.Ref, field, ref)
			}
		}
		if e.Ref != "" {
			defined[e.Ref] = true
		}
	}
}
