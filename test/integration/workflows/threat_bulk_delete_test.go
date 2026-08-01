package workflows

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestThreatBulkDelete_Integration covers DELETE /threat_models/{id}/threats/bulk
// (bulkDeleteThreats), which no integration test previously reached.
//
// It could not be reached: the handler read `threat_ids` from a JSON request
// body while the spec declares it as a required query parameter with no body,
// so every spec-conforming request answered 400 (#658). The two properties
// asserted here are the ones that fix cost the most to get right:
//
//   - Scoping. Authorization is granted against the {threat_model_id} in the
//     path, but the delete used to match on threat id alone, so a caller with
//     writer access to one threat model could delete threats out of another.
//   - Atomicity. The delete used to commit per id, so a batch that failed
//     partway through stayed half-applied and could never be retried (the
//     survivors no longer satisfy `deleted_at IS NULL`, so a retry 404s).
//
// The atomicity guarantee rests on the driver reporting RowsAffected correctly
// for a multi-row UPDATE, which is worth pinning against a real database rather
// than a mock -- especially on Oracle, where this also runs via
// `make test-integration-oci`.
func TestThreatBulkDelete_Integration(t *testing.T) {
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

	userID := framework.UniqueUserID()
	tokens, err := framework.AuthenticateUser(userID)
	framework.AssertNoError(t, err, "Authentication failed")

	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// createThreatModel returns the id of a new threat model owned by this user.
	createThreatModel := func(name string) string {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models",
			Body:   framework.NewThreatModelFixture().WithName(name),
		})
		framework.AssertNoError(t, err, "Failed to create threat model "+name)
		framework.AssertStatusCreated(t, resp)
		return framework.ExtractID(t, resp, "id")
	}

	// createThreat returns the id of a new threat under the given threat model.
	createThreat := func(threatModelID, name string) string {
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/threat_models/%s/threats", threatModelID),
			Body: map[string]interface{}{
				"name":        name,
				"description": "created by TestThreatBulkDelete_Integration",
				"threat_type": []string{"Spoofing"},
				"severity":    "low",
				"status":      "Open",
			},
		})
		framework.AssertNoError(t, err, "Failed to create threat "+name)
		framework.AssertStatusCreated(t, resp)
		return framework.ExtractID(t, resp, "id")
	}

	// threatExists reports whether a threat is still readable under its parent.
	threatExists := func(threatModelID, threatID string) bool {
		resp, err := client.Do(framework.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/threat_models/%s/threats/%s", threatModelID, threatID),
		})
		framework.AssertNoError(t, err, "Failed to read threat "+threatID)
		return resp.StatusCode == 200
	}

	modelA := createThreatModel("Bulk delete: model A")
	modelB := createThreatModel("Bulk delete: model B")

	t.Run("ForeignIDDeletesNothingFromEitherModel", func(t *testing.T) {
		a1 := createThreat(modelA, "A-1")
		a2 := createThreat(modelA, "A-2")
		b1 := createThreat(modelB, "B-1")

		// a1 and a2 are legitimately deletable here; b1 is not, because it
		// belongs to another threat model. The whole batch must roll back.
		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path: fmt.Sprintf("/threat_models/%s/threats/bulk?threat_ids=%s,%s,%s",
				modelA, a1, a2, b1),
		})
		framework.AssertNoError(t, err, "Bulk delete request failed")
		framework.AssertStatusNotFound(t, resp)

		if !threatExists(modelA, a1) {
			t.Error("a1 was deleted even though the batch was rejected — the delete is not atomic")
		}
		if !threatExists(modelA, a2) {
			t.Error("a2 was deleted even though the batch was rejected — the delete is not atomic")
		}
		if !threatExists(modelB, b1) {
			t.Error("b1 was deleted through another threat model's path — the delete is not scoped to its parent")
		}

		t.Log("✓ A batch naming a foreign threat deleted nothing, from either threat model")
	})

	t.Run("ValidBatchDeletesEveryID", func(t *testing.T) {
		a3 := createThreat(modelA, "A-3")
		a4 := createThreat(modelA, "A-4")

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path: fmt.Sprintf("/threat_models/%s/threats/bulk?threat_ids=%s,%s",
				modelA, a3, a4),
		})
		framework.AssertNoError(t, err, "Bulk delete request failed")
		framework.AssertStatusOK(t, resp)

		var body struct {
			DeletedCount int      `json:"deleted_count"`
			DeletedIDs   []string `json:"deleted_ids"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &body), "Failed to parse bulk delete response")

		if body.DeletedCount != 2 {
			t.Errorf("Expected deleted_count 2, got %d", body.DeletedCount)
		}
		if len(body.DeletedIDs) != 2 {
			t.Errorf("Expected 2 deleted_ids, got %d", len(body.DeletedIDs))
		}

		if threatExists(modelA, a3) {
			t.Error("a3 still readable after a successful bulk delete")
		}
		if threatExists(modelA, a4) {
			t.Error("a4 still readable after a successful bulk delete")
		}

		t.Log("✓ A valid batch deleted every id it named")
	})

	t.Run("AlreadyDeletedIDIsRejected", func(t *testing.T) {
		a5 := createThreat(modelA, "A-5")
		a6 := createThreat(modelA, "A-6")

		resp, err := client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/threats/bulk?threat_ids=%s", modelA, a5),
		})
		framework.AssertNoError(t, err, "First bulk delete failed")
		framework.AssertStatusOK(t, resp)

		// Replaying a5 alongside a live id must not delete a6: an already
		// tombstoned row no longer matches, so the batch is short and rolls back.
		resp, err = client.Do(framework.Request{
			Method: "DELETE",
			Path:   fmt.Sprintf("/threat_models/%s/threats/bulk?threat_ids=%s,%s", modelA, a5, a6),
		})
		framework.AssertNoError(t, err, "Replayed bulk delete request failed")
		framework.AssertStatusNotFound(t, resp)

		if !threatExists(modelA, a6) {
			t.Error("a6 was deleted by a batch that named an already-deleted id — the delete is not atomic")
		}

		t.Log("✓ A batch replaying an already-deleted id left the live id alone")
	})
}
