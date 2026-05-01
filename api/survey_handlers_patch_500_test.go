package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPatchAdminSurvey_NoServerErrorOnConstraintViolation pins the Zero-500
// invariant for /admin/surveys/{survey_id}: a constraint violation returned
// by the store must surface as a 4xx (specifically 400 invalid_input via the
// dberrors.ErrConstraint sentinel), never as a 500.
//
// Regression for T25 / #359.
func TestPatchAdminSurvey_NoServerErrorOnConstraintViolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)

	surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)

	// Simulate the database returning a constraint violation (e.g. NOT NULL
	// or column-length violation triggered by fuzzed input).
	store.updateErr = fmt.Errorf("update failed: %w", dberrors.ErrConstraint)

	body := []byte(`[{"op":"replace","path":"/description","value":"new description"}]`)
	c, w := CreateTestGinContextWithBody(http.MethodPatch,
		fmt.Sprintf("/admin/surveys/%s", surveyID),
		"application/json-patch+json", body)
	TestUsers.Owner.SetContext(c)

	server.PatchAdminSurvey(c, surveyID)

	assert.NotEqual(t, http.StatusInternalServerError, w.Code,
		"constraint violations must not surface as 500; got body=%s", w.Body.String())
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_input")
}

// TestPatchAdminSurvey_RejectsOversizeName pins the column-length validator.
// CATS ExamplesFields fuzzers commonly produce values that exceed varchar
// limits; the handler must catch them as 400 before they reach the store.
func TestPatchAdminSurvey_RejectsOversizeName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)

	surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)

	oversize := strings.Repeat("a", 257) // SurveyTemplate.Name is varchar(256)
	body := []byte(fmt.Sprintf(`[{"op":"replace","path":"/name","value":%q}]`, oversize))
	c, w := CreateTestGinContextWithBody(http.MethodPatch,
		fmt.Sprintf("/admin/surveys/%s", surveyID),
		"application/json-patch+json", body)
	TestUsers.Owner.SetContext(c)

	server.PatchAdminSurvey(c, surveyID)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "name")
}

// TestPatchAdminSurvey_RejectsEmptyName pins the not-null validator. The
// patch fuzzer can null out a required column; the handler must catch this.
func TestPatchAdminSurvey_RejectsEmptyName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)

	surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)

	body := []byte(`[{"op":"replace","path":"/name","value":""}]`)
	c, w := CreateTestGinContextWithBody(http.MethodPatch,
		fmt.Sprintf("/admin/surveys/%s", surveyID),
		"application/json-patch+json", body)
	TestUsers.Owner.SetContext(c)

	server.PatchAdminSurvey(c, surveyID)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}

// TestPatchAdminSurvey_RejectsInvalidStatus pins the enum validator.
func TestPatchAdminSurvey_RejectsInvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)

	surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)

	body := []byte(`[{"op":"replace","path":"/status","value":"definitely-not-a-valid-status"}]`)
	c, w := CreateTestGinContextWithBody(http.MethodPatch,
		fmt.Sprintf("/admin/surveys/%s", surveyID),
		"application/json-patch+json", body)
	TestUsers.Owner.SetContext(c)

	server.PatchAdminSurvey(c, surveyID)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "status")
}

// TestPatchAdminSurvey_NotFoundReturns404 confirms the typed-error
// reclassification still produces 404 for ErrNotFound (not 500).
func TestPatchAdminSurvey_NotFoundReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	store := newMockSurveyStore()
	store.getErr = dberrors.ErrNotFound
	saveSurveyStores(t, store, nil)

	body := []byte(`[{"op":"replace","path":"/description","value":"x"}]`)
	c, w := CreateTestGinContextWithBody(http.MethodPatch,
		"/admin/surveys/00000000-0000-0000-0000-000000000000",
		"application/json-patch+json", body)
	TestUsers.Owner.SetContext(c)

	server.PatchAdminSurvey(c, uuid.New())

	assert.Equal(t, http.StatusNotFound, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEqual(t, "server_error", resp["error"])
}
