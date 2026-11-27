package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// renderRefData writes refData for refs into a temp dir and parses it back.
func renderRefData(t *testing.T, refs RefMap) map[string]map[string]any {
	t.Helper()
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
	return doc
}

// Add-ons were seeded without a ref, so the seeder never registered them and
// refData carried the nil UUID; /addons/{id} then took the global `id` (the
// threat-model id) and 90 tests 404d in run 20261122T014607Z (#1235).
func TestTransformSeedSpec_AddonsCarryRefs(t *testing.T) {
	seeds := transformAdminWebhooksAndAddons(&SeedSpecAdmin{
		Addons: []SeedSpecAddon{{Name: realAddonName}, {Name: throwawayAddonName}},
	})
	got := map[string]bool{}
	for _, s := range seeds {
		if s.Kind == kindAddon {
			got[s.Ref] = true
		}
	}
	for _, name := range []string{realAddonName, throwawayAddonName} {
		if !got[addonRef(name)] {
			t.Errorf("addon %q seeded without ref %q (got %v)", name, addonRef(name), got)
		}
	}
}

func TestTransformSeedSpec_UsabilityFeedback(t *testing.T) {
	seeds := transformAdminWebhooksAndAddons(&SeedSpecAdmin{
		UsabilityFeedback: []SeedSpecUsabilityFeedback{
			{Sentiment: "up", Surface: "cats.seed", ClientID: "cats-seed"},
		},
	})
	var found *SeedEntry
	for i := range seeds {
		if seeds[i].Kind == kindUsabilityFeedback {
			found = &seeds[i]
		}
	}
	if found == nil {
		t.Fatal("no usability_feedback seed entry")
	}
	if found.Ref != usabilityFeedbackRef(0) {
		t.Errorf("ref = %q, want %q", found.Ref, usabilityFeedbackRef(0))
	}
	for k, want := range map[string]string{"sentiment": "up", "surface": "cats.seed", "client_id": "cats-seed"} {
		if got := found.Data[k]; got != want {
			t.Errorf("data[%s] = %v, want %s", k, got, want)
		}
	}
	if classifyStrategy(kindUsabilityFeedback) != strategyAPI {
		t.Errorf("usability_feedback must be seeded via the API")
	}
}

// The Harness note 2 bindings: each one names the resource its path actually
// addresses, instead of inheriting a global value that points elsewhere.
func TestWriteYAMLReferenceSeedGapBindings(t *testing.T) {
	refs := RefMap{
		addonRef(realAddonName):             {Kind: kindAddon, ID: "ad-real"},
		addonRef(throwawayAddonName):        {Kind: kindAddon, ID: "ad-decoy"},
		usabilityFeedbackRef(0):             {Kind: kindUsabilityFeedback, ID: "uf-1"},
		userRef("cats-target"):              {Kind: kindUser, ID: "usr-target"},
		userRef(transferTargetUser):         {Kind: kindUser, ID: "usr-transfer"},
		groupRef(realGroupName):             {Kind: kindGroup, ID: "grp-real"},
		groupRef(memberGroupName):           {Kind: kindGroup, ID: "grp-member"},
		tmRef(realThreatModelName):          {Kind: kindThreatModel, ID: "tm-real"},
		tmRef(throwawayThreatModelName):     {Kind: kindThreatModel, ID: "tm-decoy"},
		webhookRef(realWebhookName):         {Kind: kindWebhook, ID: "wh-real"},
		webhookRef(throwawayWebhookName):    {Kind: kindWebhook, ID: "wh-decoy"},
		groupRef(throwawayGroupName):        {Kind: kindGroup, ID: "grp-decoy"},
		userRef("cats-decoy"):               {Kind: kindUser, ID: "usr-decoy"},
		webhookRef(testEndpointWebhookName): {Kind: kindWebhook, ID: "wh-test"},
	}
	// Map iteration order is random; repeat so a kind lookup cannot pass by luck.
	for i := 0; i < 40; i++ {
		doc := renderRefData(t, refs)
		checks := []struct{ section, key, want string }{
			{"all", "addon_id", "ad-real"},
			// DELETE /addons/{id} sorts before /invoke and consumes whatever it
			// is pointed at, so the anchor gets the decoy.
			{"/addons/{id}", "id", "ad-decoy"},
			{"/addons/{id}/invoke", "id", "ad-real"},
			{"/usability_feedback/{id}", "id", "uf-1"},
			// The source is cats-target (the nested /admin/users path), so the
			// target must be a different account, and not the decoy user that
			// DELETE /admin/users/{user_id} consumes first.
			{"/admin/users/{user_id}/transfer", "target_user_id", "usr-transfer"},
			// Not cats-target: it is already a member of the real group, so
			// every add would be a 409.
			{"all", "user_internal_uuid", "usr-transfer"},
			{"all", "member_group_internal_uuid", "grp-member"},
			{"/me/identities/link/start", "idp", "tmi"},
			{"/me/identities/link/start", "client_callback", "http://localhost:8079/"},
		}
		for _, c := range checks {
			if got := doc[c.section][c.key]; got != c.want {
				t.Fatalf("%s.%s = %v, want %s", c.section, c.key, got, c.want)
			}
		}
		// /me/transfer moves everything the fuzzing identity owns; it must stay
		// self-targeted (a documented 400), so no binding may reach it.
		if _, ok := doc["/me/transfer"]; ok {
			t.Fatal("/me/transfer must not get a target_user_id binding")
		}
		if _, ok := doc["all"]["target_user_id"]; ok {
			t.Fatal("target_user_id must not be global: it would reach /me/transfer")
		}
	}
}

// A binding whose fixture was not seeded must be omitted, not emitted as the
// nil UUID: CATS would then send a dead id instead of generating one.
func TestWriteYAMLReferenceSeedGapBindingsOmittedWhenUnseeded(t *testing.T) {
	doc := renderRefData(t, RefMap{})
	for _, section := range []string{
		"/addons/{id}", "/addons/{id}/invoke", "/usability_feedback/{id}",
		"/admin/users/{user_id}/transfer",
	} {
		if _, ok := doc[section]; ok {
			t.Errorf("section %s emitted with nothing seeded", section)
		}
	}
	for _, key := range []string{"member_group_internal_uuid", "user_internal_uuid"} {
		if _, ok := doc["all"][key]; ok {
			t.Errorf("%s emitted with nothing seeded", key)
		}
	}
}

func TestFixtureManifestTracksAddon(t *testing.T) {
	ids := fullIDs()
	ids.addonID = "ad-1"
	m := readManifest(t, ids)
	for _, f := range m.Fixtures {
		if f.Key == "addon_id" {
			if f.VerifyURL != "/addons/ad-1" || !f.Decoyed || f.AnchorPath != "/addons/{id}" {
				t.Fatalf("addon fixture = %+v", f)
			}
			return
		}
	}
	t.Fatal("addon_id fixture missing from manifest")
}

// The checked-in seed must define every fixture the new bindings resolve by name.
func TestCatsSeedDefinesSeedGapFixtures(t *testing.T) {
	seedFile, err := loadSeedFile("../../test/seeds/cats-seed-data.json")
	if err != nil {
		t.Fatalf("loadSeedFile: %v", err)
	}
	have := map[string]bool{}
	for _, e := range seedFile.Seeds {
		have[e.Ref] = true
	}
	for _, ref := range []string{
		addonRef(realAddonName), addonRef(throwawayAddonName),
		usabilityFeedbackRef(0), userRef(transferTargetUser), groupRef(memberGroupName),
	} {
		if !have[ref] {
			t.Errorf("seed lacks ref %q", ref)
		}
	}
}
