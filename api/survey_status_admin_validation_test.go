package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue #1104: POST/PUT /admin/surveys must reject an unknown status with a
// 400 naming the field before any store write.
func TestCreateAdminSurvey_RejectsInvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)

	for _, status := range []string{"bogus", strings.Repeat("x", 21), strings.Repeat("x", 128), "ACTIVE", ""} {
		body, _ := json.Marshal(SurveyBase{Name: "S", Version: "v1", Status: &status, SurveyJson: validSurveyJSON()})
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", body)
		TestUsers.Owner.SetContext(c)

		(&Server{}).CreateAdminSurvey(c)

		require.Equal(t, http.StatusBadRequest, w.Code, "status=%q body=%s", status, w.Body.String())
		assert.Contains(t, w.Body.String(), "status")
		assert.Contains(t, w.Body.String(), "invalid_input")
	}
}

func TestCreateAdminSurvey_AcceptsNilAndValidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)

	for _, status := range []*string{nil, strPtr(SurveyStatusActive), strPtr(SurveyStatusInactive), strPtr(SurveyStatusArchived)} {
		body, _ := json.Marshal(SurveyBase{Name: "S", Version: "v1", Status: status, SurveyJson: validSurveyJSON()})
		c, w := CreateTestGinContextWithBody("POST", "/admin/surveys", "application/json", body)
		TestUsers.Owner.SetContext(c)

		(&Server{}).CreateAdminSurvey(c)

		assert.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())
	}
}

func TestUpdateAdminSurvey_RejectsInvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newMockSurveyStore()
	saveSurveyStores(t, store, nil)
	surveyID := seedSurvey(store, "Original", "v1", SurveyStatusActive)

	for _, status := range []string{"bogus", strings.Repeat("x", 21), strings.Repeat("x", 128), ""} {
		body, _ := json.Marshal(SurveyBase{Name: "S", Version: "v2", Status: &status, SurveyJson: validSurveyJSON()})
		c, w := CreateTestGinContextWithBody("PUT", "/admin/surveys/"+surveyID.String(), "application/json", body)
		TestUsers.Owner.SetContext(c)

		(&Server{}).UpdateAdminSurvey(c, surveyID)

		require.Equal(t, http.StatusBadRequest, w.Code, "status=%q body=%s", status, w.Body.String())
		assert.Contains(t, w.Body.String(), "status")
		assert.Contains(t, w.Body.String(), "invalid_input")
	}
}

func TestValidateSurveyStatus(t *testing.T) {
	assert.Nil(t, validateSurveyStatus(nil))
	for _, ok := range []string{SurveyStatusActive, SurveyStatusInactive, SurveyStatusArchived} {
		assert.Nil(t, validateSurveyStatus(&ok), ok)
	}
	for _, bad := range []string{"", "bogus", "Active", " active"} {
		assert.NotNil(t, validateSurveyStatus(&bad), bad)
	}
}

// TestSurveyBaseStatusSpecEnum confirms the OpenAPI request validator enforces the
// status enum on POST /admin/surveys while still accepting null and omission.
func TestSurveyBaseStatusSpecEnum(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)
	router := gin.New()
	router.Use(validator)
	router.POST("/admin/surveys", func(c *gin.Context) { c.Status(http.StatusCreated) })

	cases := []struct {
		name   string
		status string
		want   int
	}{
		{"omitted", ``, http.StatusCreated},
		{"null", `,"status":null`, http.StatusCreated},
		{"active", `,"status":"active"`, http.StatusCreated},
		{"archived", `,"status":"archived"`, http.StatusCreated},
		{"unknown", `,"status":"bogus"`, http.StatusBadRequest},
		{"too long", `,"status":"` + strings.Repeat("x", 100) + `"`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"name":"S","version":"v1","survey_json":{"pages":[]}` + tc.status + `}`
			req := httptest.NewRequest(http.MethodPost, "/admin/surveys", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code, "body=%s", w.Body.String())
		})
	}
}
