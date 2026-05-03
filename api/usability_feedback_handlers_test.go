package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupUsabilityFeedbackHandler(t *testing.T) (*UsabilityFeedbackHandler, *gin.Engine, *models.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := setupUsabilityFeedbackTestDB(t)
	repo := NewGormUsabilityFeedbackRepository(db)
	handler := NewUsabilityFeedbackHandler(repo)

	aliceProviderIDVal := aliceTestProviderID
	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       "test",
		ProviderUserID: &aliceProviderIDVal,
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	require.NoError(t, db.Create(user).Error)

	providerIDStr := ""
	if user.ProviderUserID != nil {
		providerIDStr = *user.ProviderUserID
	}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", user.Email)
		c.Set("userID", providerIDStr)
		c.Set("userInternalUUID", user.InternalUUID)
		c.Next()
	})
	return handler, r, user
}

func TestUsabilityFeedbackHandler_PostHappyPath(t *testing.T) {
	handler, r, user := setupUsabilityFeedbackHandler(t)
	r.POST("/usability_feedback", handler.Create)

	body := map[string]any{
		"sentiment": "up",
		"surface":   "tm_list",
		"client_id": "tmi-ux",
		"verbatim":  "love it",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/usability_feedback", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())

	var resp UsabilityFeedback
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Id)
	assert.Equal(t, "up", string(resp.Sentiment))
	assert.Equal(t, "tm_list", resp.Surface)
	assert.Equal(t, "tmi-ux", resp.ClientId)
	assert.Equal(t, user.InternalUUID, resp.CreatedBy.String())
}

func TestUsabilityFeedbackHandler_PostRejectsInvalidSurface(t *testing.T) {
	handler, r, _ := setupUsabilityFeedbackHandler(t)
	r.POST("/usability_feedback", handler.Create)

	body := `{"sentiment":"up","surface":"BAD SURFACE","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/usability_feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUsabilityFeedbackHandler_GetHappyPath(t *testing.T) {
	handler, r, user := setupUsabilityFeedbackHandler(t)
	r.POST("/usability_feedback", handler.Create)
	r.GET("/usability_feedback/:id", handler.Get)

	// Submit a feedback row first.
	body := `{"sentiment":"down","surface":"intake_form.step_2","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/usability_feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created UsabilityFeedback
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	// Now fetch it.
	req2 := httptest.NewRequest(http.MethodGet, "/usability_feedback/"+created.Id.String(), nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var got UsabilityFeedback
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &got))
	assert.Equal(t, created.Id, got.Id)
	assert.Equal(t, "down", string(got.Sentiment))
	assert.Equal(t, user.InternalUUID, got.CreatedBy.String())
}

func TestUsabilityFeedbackHandler_GetNotFound(t *testing.T) {
	handler, r, _ := setupUsabilityFeedbackHandler(t)
	r.GET("/usability_feedback/:id", handler.Get)

	req := httptest.NewRequest(http.MethodGet, "/usability_feedback/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestUsabilityFeedbackHandler_ListWithFilter(t *testing.T) {
	handler, r, _ := setupUsabilityFeedbackHandler(t)
	r.POST("/usability_feedback", handler.Create)
	r.GET("/usability_feedback", handler.List)

	for _, sentiment := range []string{"up", "up", "down"} {
		body := `{"sentiment":"` + sentiment + `","surface":"tm_list","client_id":"tmi-ux"}`
		req := httptest.NewRequest(http.MethodPost, "/usability_feedback", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/usability_feedback?sentiment=up", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Items []UsabilityFeedback `json:"items"`
		Total int64               `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
	assert.Len(t, resp.Items, 2)
}
