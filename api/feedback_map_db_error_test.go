package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDBError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantNil    bool
		wantStatus int
	}{
		{"nil", nil, true, 0},
		{"not found", dberrors.ErrNotFound, false, http.StatusNotFound},
		{"transient", dberrors.ErrTransient, false, http.StatusServiceUnavailable},
		{"constraint", dberrors.ErrConstraint, false, http.StatusBadRequest},
		{"duplicate", fmt.Errorf("insert: %w", dberrors.ErrDuplicate), false, http.StatusBadRequest},
		{"foreign key", fmt.Errorf("insert: %w", dberrors.ErrForeignKey), false, http.StatusBadRequest},
		{"unclassified", errors.New("boom"), false, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDBError(tt.err)
			if tt.wantNil {
				assert.NoError(t, got)
				return
			}
			var reqErr *RequestError
			require.ErrorAs(t, got, &reqErr)
			assert.Equal(t, tt.wantStatus, reqErr.Status)
			if tt.wantStatus == http.StatusBadRequest {
				assert.Equal(t, "invalid_input", string(reqErr.Code))
				assert.Equal(t, "request violates a data constraint", reqErr.Message)
			}
		})
	}
}

// failingUsabilityRepo returns a fixed error from Create.
type failingUsabilityRepo struct {
	UsabilityFeedbackRepository
	err error
}

func (r failingUsabilityRepo) Create(context.Context, *models.UsabilityFeedback) error { return r.err }

// failingContentRepo returns a fixed error from CreateWithTargetCheck.
type failingContentRepo struct {
	ContentFeedbackRepository
	err error
}

func (r failingContentRepo) CreateWithTargetCheck(context.Context, *models.ContentFeedback, ContentFeedbackTargetRef) error {
	return r.err
}

func TestUsabilityFeedbackHandler_CreateConstraintViolationReturns400(t *testing.T) {
	handler, r, _ := setupUsabilityFeedbackHandler(t)
	handler.repo = failingUsabilityRepo{err: fmt.Errorf("insert: %w", dberrors.ErrDuplicate)}
	r.POST("/usability_feedback", handler.Create)

	buf, _ := json.Marshal(map[string]any{"sentiment": "up", "surface": "tm_list", "client_id": "tmi-ux"})
	req := httptest.NewRequest(http.MethodPost, "/usability_feedback", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "invalid_input")
	assert.NotContains(t, rec.Body.String(), "insert:")
}

func TestContentFeedbackHandler_CreateConstraintViolationReturns400(t *testing.T) {
	handler, r, _, _, tm := setupContentFeedbackHandler(t)
	handler.repo = failingContentRepo{err: fmt.Errorf("insert: %w", dberrors.ErrDuplicate)}
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	buf, _ := json.Marshal(map[string]any{
		"sentiment": "down", "target_type": "threat", "target_id": "11111111-1111-4111-8111-111111111111", "client_id": "tmi-ux",
	})
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+string(tm.ID)+"/feedback", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "invalid_input")
	assert.NotContains(t, rec.Body.String(), "insert:")
}

func TestCreateAdminSurvey_ConstraintViolationReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockSurveyStore()
	store.createErr = fmt.Errorf("insert: %w", dberrors.ErrConstraint)
	saveSurveyStores(t, store, nil)

	body, _ := json.Marshal(SurveyBase{Name: "S", Version: "v1", SurveyJson: validSurveyJSON()})
	c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", body)
	TestUsers.Owner.SetContext(c)

	(&Server{}).CreateAdminSurvey(c)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")
	assert.NotContains(t, w.Body.String(), "insert:")
}

func TestUpdateAdminSurvey_ConstraintViolationReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)
	surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)
	store.updateErr = fmt.Errorf("update: %w", dberrors.ErrConstraint)

	body, _ := json.Marshal(SurveyBase{Name: "S", Version: "v2", Status: strPtr(SurveyStatusActive), SurveyJson: validSurveyJSON()})
	c, w := CreateTestGinContextWithBody("PUT", "/admin/surveys/"+surveyID.String(), "application/json", body)
	TestUsers.Owner.SetContext(c)

	(&Server{}).UpdateAdminSurvey(c, surveyID)

	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_input")
	assert.NotContains(t, w.Body.String(), "update:")
}
