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
	"gorm.io/gorm"
)

func setupContentFeedbackHandler(t *testing.T) (*ContentFeedbackHandler, *gin.Engine, *gorm.DB, *models.User, *models.ThreatModel) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, user, tm := setupContentFeedbackTestDB(t)

	repo := NewGormContentFeedbackRepository(db)
	handler := NewContentFeedbackHandler(repo, db)

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
	return handler, r, db, user, tm
}

func TestContentFeedbackHandler_PostHappyPath(t *testing.T) {
	handler, r, db, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	// Pre-create a target threat in this TM.
	threat := &models.Threat{
		ID:            uuid.New().String(),
		ThreatModelID: tm.ID,
		Name:          "Test Threat",
		ThreatType:    models.StringArray{"X"},
	}
	require.NoError(t, db.Create(threat).Error)

	body := map[string]any{
		"sentiment":   "down",
		"target_type": "threat",
		"target_id":   threat.ID,
		"client_id":   "tmi-ux",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	var resp ContentFeedback
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Id)
	assert.Equal(t, "threat", string(resp.TargetType))
}

func TestContentFeedbackHandler_PostRejectsMissingTarget(t *testing.T) {
	handler, r, _, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	body := map[string]any{
		"sentiment":   "down",
		"target_type": "threat",
		"target_id":   uuid.New().String(),
		"client_id":   "tmi-ux",
	}
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContentFeedbackHandler_PostRejectsTargetFieldOnNonClassification(t *testing.T) {
	handler, r, _, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	body := `{"sentiment":"up","target_type":"note","target_id":"` + uuid.New().String() + `","target_field":"x","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContentFeedbackHandler_PostRequiresTargetFieldForClassification(t *testing.T) {
	handler, r, _, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	body := `{"sentiment":"up","target_type":"threat_classification","target_id":"` + uuid.New().String() + `","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContentFeedbackHandler_PostRejectsFalsePositiveOnSentimentUp(t *testing.T) {
	handler, r, _, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	body := `{"sentiment":"up","target_type":"threat","target_id":"` + uuid.New().String() + `","false_positive_reason":"duplicate","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContentFeedbackHandler_PostRejectsBadSubreasonForReason(t *testing.T) {
	handler, r, db, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)

	threat := &models.Threat{
		ID:            uuid.New().String(),
		ThreatModelID: tm.ID,
		Name:          "T",
		ThreatType:    models.StringArray{"X"},
	}
	require.NoError(t, db.Create(threat).Error)

	body := `{"sentiment":"down","target_type":"threat","target_id":"` + threat.ID + `","false_positive_reason":"detection_misfired","false_positive_subreason":"sanctioned_by_design","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContentFeedbackHandler_GetAndList(t *testing.T) {
	handler, r, db, _, tm := setupContentFeedbackHandler(t)
	r.POST("/threat_models/:threat_model_id/feedback", handler.Create)
	r.GET("/threat_models/:threat_model_id/feedback/:feedback_id", handler.Get)
	r.GET("/threat_models/:threat_model_id/feedback", handler.List)

	threat := &models.Threat{
		ID:            uuid.New().String(),
		ThreatModelID: tm.ID,
		Name:          "T",
		ThreatType:    models.StringArray{"X"},
	}
	require.NoError(t, db.Create(threat).Error)

	body := `{"sentiment":"up","target_type":"threat","target_id":"` + threat.ID + `","client_id":"tmi-ux"}`
	req := httptest.NewRequest(http.MethodPost, "/threat_models/"+tm.ID+"/feedback", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created ContentFeedback
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	// Get by id.
	req2 := httptest.NewRequest(http.MethodGet, "/threat_models/"+tm.ID+"/feedback/"+created.Id.String(), nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	// List.
	req3 := httptest.NewRequest(http.MethodGet, "/threat_models/"+tm.ID+"/feedback", nil)
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)

	var list struct {
		Items []ContentFeedback `json:"items"`
		Total int64             `json:"total"`
	}
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &list))
	assert.Equal(t, int64(1), list.Total)
}
