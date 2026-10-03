package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Status-transition validation failures (unknown status, missing
// revision_notes) are request validation errors and must return 400, not 409
// (#987). 409 stays reserved for real state/version conflicts.
func TestPatchSurveyResponse_StatusValidationReturns400(t *testing.T) {
	patch := []byte(`[{"op":"replace","path":"/status","value":"needs_revision"}]`)

	t.Run("intake: needs_revision without revision_notes", func(t *testing.T) {
		store := newMockSurveyResponseStore()
		saveSurveyStores(t, nil, store)
		id := seedSurveyResponse(store, uuid.New(), ResponseStatusDraft, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/intake/survey_responses/%s", id), "application/json-patch+json", patch)
		TestUsers.Owner.SetContext(c)
		(&Server{}).PatchIntakeSurveyResponse(c, id, PatchIntakeSurveyResponseParams{})

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "revision_notes required")
	})

	t.Run("triage: store reports missing revision_notes", func(t *testing.T) {
		store := newMockSurveyResponseStore()
		store.updateStatusErr = ValidateSurveyResponseStatusTransition(ResponseStatusNeedsRevision, nil)
		saveSurveyStores(t, nil, store)
		id := seedSurveyResponse(store, uuid.New(), ResponseStatusSubmitted, TestUsers.Owner.InternalUUID)

		c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/triage/survey_responses/%s", id), "application/json-patch+json", patch)
		TestUsers.Owner.SetContext(c)
		(&Server{}).PatchTriageSurveyResponse(c, id)

		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "revision_notes required")
	})
}
