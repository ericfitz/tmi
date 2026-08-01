package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// tenancyFamily describes one of the six sub-resource families nested under
// /threat_models/{id}/... that enforceChildParentage (api/authz_middleware.go)
// gates. childName is a distinct value written to the "name" field at create
// time so the "PATCH left the victim's child unchanged" assertions have
// something recognizable to check against.
// SEM@ed746bcfee9dfd98ae3ba8f994298be78d36c276: describe a sub-resource family fixture used in tenancy tests
type tenancyFamily struct {
	segment    string // path segment, e.g. "threats"
	childName  string
	createBody map[string]interface{}
}

// TestSubResourceTenancy_Integration proves the #664 fix: a caller who owns
// threat model A cannot reach a child of threat model B through A's path.
//
// Task 1 (api/authz_middleware.go: enforceChildParentage) added a centralized
// gate that runs after the parent-ACL check on every
// /threat_models/{tm}/{family}/{child_id}... route: it probes
// `id = ? AND threat_model_id = ?` for the six families below and answers 404
// on mismatch, before the request ever reaches the family's handler. This is
// the only test that exercises the real GORM probe (gormChildParentageCheck)
// against a real database — unit tests fake the checker.
//
// Every cross-parent request must answer 404, never 403 (which would leak
// the child's existence to a caller with no relationship to it — an
// "existence oracle") and never 200. Cross-parent DELETE/PATCH must leave the
// victim's child untouched. The soft-delete predicate is asymmetric: a
// deleted child is invisible on its normal GET route but its audit_trail
// stays readable (#664 fix-up, so audit history of a deleted child --
// including the delete event itself -- doesn't vanish with it).
// SEM@c11774be9599af9d8ae73a93d062afc4268a9bad: verify cross-parent sub-resource access is blocked with 404
func TestSubResourceTenancy_Integration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	attackerTokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "Failed to authenticate attacker")
	attacker, err := framework.NewClient(serverURL, attackerTokens)
	framework.AssertNoError(t, err, "Failed to create attacker client")

	victimTokens, err := framework.AuthenticateUser(framework.UniqueUserID())
	framework.AssertNoError(t, err, "Failed to authenticate victim")
	victim, err := framework.NewClient(serverURL, victimTokens)
	framework.AssertNoError(t, err, "Failed to create victim client")

	// createThreatModel returns the id of a new threat model owned by the
	// given client's user.
	createThreatModel := func(client *framework.IntegrationClient, name string) string {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName(name),
		})
		framework.AssertNoError(t, err, "Failed to create threat model "+name)
		framework.AssertStatusCreated(t, resp)
		return framework.ExtractID(t, resp, "id")
	}

	// createChild creates a family member under threatModelID via the given
	// client and returns its id.
	createChild := func(client *framework.IntegrationClient, threatModelID, segment string, body map[string]interface{}) string {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/%s", threatModelID, segment),
			Body:   body,
		})
		framework.AssertNoError(t, err, "Failed to create "+segment+" child")
		framework.AssertStatusCreated(t, resp)
		return framework.ExtractID(t, resp, "id")
	}

	// assertCrossParentBlocked asserts a 404 and, explicitly, not a 403 --
	// 403 on a cross-parent request would be an existence oracle: it tells
	// the caller the child exists (just not for them) instead of hiding it
	// behind the same "not found" a nonexistent child would produce.
	assertCrossParentBlocked := func(t *testing.T, resp *framework.Response, context string) {
		t.Helper()
		if resp.StatusCode == 403 {
			t.Fatalf("%s: got 403, not 404 -- this leaks the child's existence to a caller with no relationship to it (existence oracle)", context)
		}
		framework.AssertStatusNotFound(t, resp)
	}

	tmVictim := createThreatModel(victim, "Subresource Tenancy: victim TM")
	tmAttacker := createThreatModel(attacker, "Subresource Tenancy: attacker TM")

	families := []tenancyFamily{
		{
			segment:   "threats",
			childName: "Victim Threat",
			createBody: map[string]interface{}{
				"name":        "Victim Threat",
				"description": "created by TestSubResourceTenancy_Integration",
				"threat_type": []string{"Spoofing"},
				"severity":    "low",
				"status":      "Open",
			},
		},
		{
			segment:   "assets",
			childName: "Victim Asset",
			createBody: map[string]interface{}{
				"name":        "Victim Asset",
				"type":        "data",
				"description": "created by TestSubResourceTenancy_Integration",
			},
		},
		{
			segment:   "documents",
			childName: "Victim Document",
			createBody: map[string]interface{}{
				"name": "Victim Document",
				"uri":  "https://example.com/docs/victim.pdf",
			},
		},
		{
			segment:   "notes",
			childName: "Victim Note",
			createBody: map[string]interface{}{
				"name":    "Victim Note",
				"content": "created by TestSubResourceTenancy_Integration",
			},
		},
		{
			segment:   "repositories",
			childName: "Victim Repository",
			createBody: map[string]interface{}{
				"name": "Victim Repository",
				"uri":  "https://github.com/test/victim-repo",
				"type": "git",
			},
		},
		{
			segment:   "diagrams",
			childName: "Victim Diagram",
			createBody: map[string]interface{}{
				"name": "Victim Diagram",
				"type": "DFD-1.0.0",
			},
		},
	}

	patchBody := []map[string]interface{}{
		{"op": "replace", "path": "/name", "value": "pwned"},
	}

	for _, fam := range families {
		fam := fam
		t.Run(fam.segment, func(t *testing.T) {
			victimChildID := createChild(victim, tmVictim, fam.segment, fam.createBody)

			attackerPath := fmt.Sprintf("/threat_models/%s/%s/%s", tmAttacker, fam.segment, victimChildID)
			victimPath := fmt.Sprintf("/threat_models/%s/%s/%s", tmVictim, fam.segment, victimChildID)

			t.Run("CrossParentGet", func(t *testing.T) {
				resp, err := attacker.Do(framework.Request{Method: "GET", Path: attackerPath})
				framework.AssertNoError(t, err, "GET request failed")
				assertCrossParentBlocked(t, resp, "GET "+attackerPath)
			})

			t.Run("CrossParentPatchLeavesVictimChildUnchanged", func(t *testing.T) {
				resp, err := attacker.Do(framework.Request{Method: "PATCH", Path: attackerPath, Body: patchBody})
				framework.AssertNoError(t, err, "PATCH request failed")
				assertCrossParentBlocked(t, resp, "PATCH "+attackerPath)

				getResp, err := victim.Do(framework.Request{Method: "GET", Path: victimPath})
				framework.AssertNoError(t, err, "Failed to read victim's child after blocked cross-parent PATCH")
				framework.AssertStatusOK(t, getResp)
				framework.AssertJSONField(t, getResp, "name", fam.childName)
			})

			t.Run("CrossParentDeleteLeavesVictimChildIntact", func(t *testing.T) {
				resp, err := attacker.Do(framework.Request{Method: "DELETE", Path: attackerPath})
				framework.AssertNoError(t, err, "DELETE request failed")
				assertCrossParentBlocked(t, resp, "DELETE "+attackerPath)

				getResp, err := victim.Do(framework.Request{Method: "GET", Path: victimPath})
				framework.AssertNoError(t, err, "Failed to read victim's child after blocked cross-parent DELETE")
				framework.AssertStatusOK(t, getResp)
			})

			t.Run("CrossParentMetadataList", func(t *testing.T) {
				resp, err := attacker.Do(framework.Request{Method: "GET", Path: attackerPath + "/metadata"})
				framework.AssertNoError(t, err, "GET metadata request failed")
				assertCrossParentBlocked(t, resp, "GET "+attackerPath+"/metadata")
			})

			// Ordered last: it mutates the victim's child (soft-delete then
			// restore), so it must not interfere with the read-only
			// subtests above.
			t.Run("SoftDeleteAuditExemptionAndRestore", func(t *testing.T) {
				delResp, err := victim.Do(framework.Request{Method: "DELETE", Path: victimPath})
				framework.AssertNoError(t, err, "Victim's own delete failed")
				framework.AssertStatusNoContent(t, delResp)

				// Soft-deleted children are invisible on the normal GET route.
				getResp, err := victim.Do(framework.Request{Method: "GET", Path: victimPath})
				framework.AssertNoError(t, err, "GET after victim's own soft-delete failed")
				framework.AssertStatusNotFound(t, getResp)

				// ...but audit history, including the delete event itself,
				// must stay readable (the includeDeleted exemption).
				auditResp, err := victim.Do(framework.Request{Method: "GET", Path: victimPath + "/audit_trail"})
				framework.AssertNoError(t, err, "GET audit_trail after victim's own soft-delete failed")
				framework.AssertStatusOK(t, auditResp)

				// A cross-parent restore attempt must still 404 even though
				// the child is soft-deleted and the checker widens its probe
				// for /restore.
				attackerRestoreResp, err := attacker.Do(framework.Request{Method: "POST", Path: attackerPath + "/restore"})
				framework.AssertNoError(t, err, "Cross-parent restore request failed")
				assertCrossParentBlocked(t, attackerRestoreResp, "POST "+attackerPath+"/restore")

				// The blocked restore must not have actually un-deleted the
				// child -- restore's checker probe is the one special-cased
				// to see soft-deleted rows, so this is the verb most able to
				// silently succeed behind a 404. Confirm the child is still
				// gone before the victim restores it themselves.
				postBlockedRestoreGetResp, err := victim.Do(framework.Request{Method: "GET", Path: victimPath})
				framework.AssertNoError(t, err, "GET after blocked cross-parent restore failed")
				framework.AssertStatusNotFound(t, postBlockedRestoreGetResp)

				// The victim's own restore succeeds and un-deletes the child.
				victimRestoreResp, err := victim.Do(framework.Request{Method: "POST", Path: victimPath + "/restore"})
				framework.AssertNoError(t, err, "Victim's own restore request failed")
				framework.AssertStatusOK(t, victimRestoreResp)

				finalGetResp, err := victim.Do(framework.Request{Method: "GET", Path: victimPath})
				framework.AssertNoError(t, err, "GET after victim's own restore failed")
				framework.AssertStatusOK(t, finalGetResp)
			})
		})
	}

	// ThreatsBulkWriteScoping proves the Task 4b fix: the threats bulk routes
	// (PUT/PATCH /threat_models/{id}/threats/bulk) have a literal "bulk"
	// segment where a child id would be, so enforceChildParentage's UUID
	// check never inspects them -- the child ids arrive inside the request
	// body instead. Before this fix, the threat store's BulkUpdate and Patch
	// predicated on the child id alone: a PUT bulk item naming a victim's
	// threat id re-parented that row into the attacker's threat model (the
	// handler stamps threat_model_id from the URL onto every item before the
	// store call), and a PATCH bulk item overwrote the victim's row in place.
	// Threats are the only family with a bulk-PATCH route, and single-item
	// routes are already covered by enforceChildParentage -- this test only
	// needs to cover the two bulk verbs.
	t.Run("ThreatsBulkWriteScoping", func(t *testing.T) {
		victimThreatID := createChild(victim, tmVictim, "threats", map[string]interface{}{
			"name":        "Victim Bulk Threat",
			"description": "created by TestSubResourceTenancy_Integration (bulk scoping)",
			"threat_type": []string{"Spoofing"},
			"severity":    "low",
			"status":      "Open",
		})

		victimThreatPath := fmt.Sprintf("/threat_models/%s/threats/%s", tmVictim, victimThreatID)
		attackerThreatPath := fmt.Sprintf("/threat_models/%s/threats/%s", tmAttacker, victimThreatID)
		attackerBulkPath := fmt.Sprintf("/threat_models/%s/threats/bulk", tmAttacker)

		// Threat creation does not persist the "metadata" field (it's a
		// separate child resource), so seed it via the dedicated metadata
		// endpoint before exercising the bulk writes below.
		metadataResp, err := victim.Do(framework.Request{
			Method: "POST",
			Path:   victimThreatPath + "/metadata",
			Body:   map[string]interface{}{"key": "sensitivity", "value": "victim-secret"},
		})
		framework.AssertNoError(t, err, "Failed to seed victim threat metadata")
		framework.AssertStatusCreated(t, metadataResp)

		// assertVictimThreatIntact re-reads the victim's threat as the victim
		// and checks both its name and its metadata. Metadata is asserted
		// separately from name because it is stored in a child table keyed
		// only on (entity_type, entity_id) -- BulkUpdate's per-item metadata
		// save used to run unconditionally, even when the threat row itself
		// correctly no-opped for a foreign parent, so a bulk PUT naming the
		// victim's threat id could wipe the victim's metadata to empty
		// (delete-then-insert-nothing) without ever touching the threat row.
		assertVictimThreatIntact := func(t *testing.T, context string) {
			t.Helper()
			resp, err := victim.Do(framework.Request{Method: "GET", Path: victimThreatPath})
			framework.AssertNoError(t, err, "Failed to read victim's threat "+context)
			framework.AssertStatusOK(t, resp)

			var body struct {
				Name     string `json:"name"`
				Metadata []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(resp.Body, &body); err != nil {
				t.Fatalf("Failed to parse victim threat response %s: %v\nBody: %s", context, err, resp.Body)
			}
			if body.Name != "Victim Bulk Threat" {
				t.Errorf("%s: victim threat name changed, got %q", context, body.Name)
			}
			if len(body.Metadata) != 1 || body.Metadata[0].Key != "sensitivity" || body.Metadata[0].Value != "victim-secret" {
				t.Errorf("%s: victim threat metadata not intact, got %+v", context, body.Metadata)
			}
		}

		t.Run("PutBulkCannotReparent", func(t *testing.T) {
			putResp, err := attacker.Do(framework.Request{
				Method: "PUT",
				Path:   attackerBulkPath,
				Body: []map[string]interface{}{
					{
						"id":          victimThreatID,
						"name":        "pwned via bulk PUT",
						"threat_type": []string{"Tampering"},
					},
				},
			})
			framework.AssertNoError(t, err, "PUT bulk request failed")
			// The handler echoes the (unpersisted) item back at 200 even
			// though the store silently no-ops the foreign-parent row; the
			// real assertions are the victim's and attacker's post-state
			// below, not this response.
			framework.AssertStatusOK(t, putResp)

			// The victim's threat must still exist under its own threat
			// model, unrenamed, and its metadata must be untouched -- not
			// just the threat row (see assertVictimThreatIntact above).
			assertVictimThreatIntact(t, "after attacker PUT bulk")

			// It must not be reachable through the attacker's threat model.
			// A 200 here would mean the row was actually re-parented.
			attackerGetResp, err := attacker.Do(framework.Request{Method: "GET", Path: attackerThreatPath})
			framework.AssertNoError(t, err, "GET request failed")
			assertCrossParentBlocked(t, attackerGetResp, "GET "+attackerThreatPath)
		})

		t.Run("PatchBulkCannotOverwriteCrossTenant", func(t *testing.T) {
			patchResp, err := attacker.Do(framework.Request{
				Method: "PATCH",
				Path:   attackerBulkPath,
				// The bulk-patch body is a BulkPatchRequest object (a "patches"
				// envelope), not a raw JSON Patch document, so it needs plain
				// application/json -- the framework defaults PATCH requests to
				// application/json-patch+json, which the spec does not declare
				// for this operation and the server 415s.
				Headers: map[string]string{"Content-Type": "application/json"},
				Body: map[string]interface{}{
					"patches": []map[string]interface{}{
						{
							"id": victimThreatID,
							"operations": []map[string]interface{}{
								{"op": "replace", "path": "/name", "value": "pwned"},
							},
						},
					},
				},
			})
			framework.AssertNoError(t, err, "PATCH bulk request failed")
			assertCrossParentBlocked(t, patchResp, "PATCH "+attackerBulkPath)

			// The victim's threat must be unchanged -- not overwritten in
			// place by a patch applied through the attacker's threat model.
			assertVictimThreatIntact(t, "after attacker PATCH bulk")
		})
	})

	// Non-family control: /threat_models/{id}/metadata/{key} is the
	// threat-model-level metadata route, not a family child path
	// (enforceChildParentage only recognizes the six families above at
	// parts[2]), so it must pass through the gate untouched. The point isn't
	// a specific status -- the handler answers whatever it answers for a
	// missing key -- it's that the gate must never turn this into a 503 or a
	// panic.
	t.Run("NonFamilyMetadataPassesThroughGate", func(t *testing.T) {
		resp, err := attacker.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/metadata/%s", tmAttacker, "no-such-key"),
		})
		framework.AssertNoError(t, err, "GET threat-model metadata request failed")
		if resp.StatusCode != 200 && resp.StatusCode != 404 {
			t.Errorf("Expected 200 or 404 from the metadata handler, got %d (gate must pass this route through untouched)\nBody: %s",
				resp.StatusCode, string(resp.Body))
		}
	})
}
