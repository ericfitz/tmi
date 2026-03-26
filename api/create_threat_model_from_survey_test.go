package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestParseCollectionAnswer_Repositories(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "frontend", "uri": "https://github.com/org/frontend"},
		{"name": "backend", "uri": "https://github.com/org/backend"}
	]`)
	repos, fallback := parseCollectionAnswer("repositories", answer)
	assert.Len(t, repos, 2)
	assert.Empty(t, fallback)

	repo0 := repos[0].(Repository)
	assert.Equal(t, "frontend", *repo0.Name)
	assert.Equal(t, "https://github.com/org/frontend", repo0.Uri)

	repo1 := repos[1].(Repository)
	assert.Equal(t, "backend", *repo1.Name)
	assert.Equal(t, "https://github.com/org/backend", repo1.Uri)
}

func TestParseCollectionAnswer_Documents(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "Design Doc", "uri": "https://docs.example.com/design"}
	]`)
	docs, fallback := parseCollectionAnswer("documents", answer)
	assert.Len(t, docs, 1)
	assert.Empty(t, fallback)

	doc := docs[0].(Document)
	assert.Equal(t, "Design Doc", doc.Name)
	assert.Equal(t, "https://docs.example.com/design", doc.Uri)
}

func TestParseCollectionAnswer_Assets(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "Database", "type": "data-store", "description": "Main PostgreSQL DB"}
	]`)
	assets, fallback := parseCollectionAnswer("assets", answer)
	assert.Len(t, assets, 1)
	assert.Empty(t, fallback)

	asset := assets[0].(Asset)
	assert.Equal(t, "Database", asset.Name)
	assert.Equal(t, AssetType("data-store"), asset.Type)
	assert.Equal(t, "Main PostgreSQL DB", *asset.Description)
}

func TestParseCollectionAnswer_IncompleteObject(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "frontend"},
		{"name": "backend", "uri": "https://github.com/org/backend"}
	]`)
	repos, fallback := parseCollectionAnswer("repositories", answer)
	assert.Len(t, repos, 1)
	assert.Len(t, fallback, 1)

	assert.Equal(t, "repositories.name", fallback[0].Key)
	assert.Equal(t, "frontend", fallback[0].Value)
}

func TestParseCollectionAnswer_UnrecognizedCollection(t *testing.T) {
	answer := json.RawMessage(`[{"name": "test"}]`)
	items, fallback := parseCollectionAnswer("unknowns", answer)
	assert.Empty(t, items)
	assert.Len(t, fallback, 1)
}

func TestParseCollectionAnswer_InvalidJSON(t *testing.T) {
	answer := json.RawMessage(`"just a string"`)
	items, fallback := parseCollectionAnswer("repositories", answer)
	assert.Empty(t, items)
	assert.Len(t, fallback, 1)
}

func TestCreateThreatModelFromSurveyResponse_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{},
		getErr:    fmt.Errorf("not found"),
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+uuid.New().String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice-provider-id")

	server.CreateThreatModelFromSurveyResponse(c, uuid.New())

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_WrongStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	draftStatus := ResponseStatusDraft
	owner := &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "tmi",
		ProviderId:    "alice-provider-id",
		Email:         "alice@example.com",
	}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {
				Id:     &responseID,
				Status: &draftStatus,
				Owner:  owner,
			},
		},
		accessMap: map[string]AuthorizationRole{
			responseID.String() + ":user-123": AuthorizationRoleOwner,
		},
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice-provider-id")

	server.CreateThreatModelFromSurveyResponse(c, responseID)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_DuplicateTM(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	readyStatus := ResponseStatusReadyForReview
	existingTMID := uuid.New()
	owner := &User{PrincipalType: UserPrincipalTypeUser, Provider: "tmi", ProviderId: "alice"}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {Id: &responseID, Status: &readyStatus, Owner: owner, CreatedThreatModelId: &existingTMID},
		},
		accessMap: map[string]AuthorizationRole{responseID.String() + ":user-123": AuthorizationRoleOwner},
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice")

	server.CreateThreatModelFromSurveyResponse(c, responseID)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_NilOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	readyStatus := ResponseStatusReadyForReview

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {Id: &responseID, Status: &readyStatus, Owner: nil},
		},
		accessMap: map[string]AuthorizationRole{responseID.String() + ":user-123": AuthorizationRoleOwner},
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice")

	server.CreateThreatModelFromSurveyResponse(c, responseID)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateThreatModelFromSurveyResponse_AccessDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}

	responseID := uuid.New()
	readyStatus := ResponseStatusReadyForReview
	owner := &User{PrincipalType: UserPrincipalTypeUser, Provider: "tmi", ProviderId: "alice"}

	mockResponseStore := &mockSurveyResponseStore{
		responses: map[uuid.UUID]*SurveyResponse{
			responseID: {Id: &responseID, Status: &readyStatus, Owner: owner},
		},
		accessMap: map[string]AuthorizationRole{}, // No access
	}
	origStore := GlobalSurveyResponseStore
	GlobalSurveyResponseStore = mockResponseStore
	defer func() { GlobalSurveyResponseStore = origStore }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/triage/survey_responses/"+responseID.String()+"/create_threat_model", nil)
	c.Set("userInternalUUID", "user-123")
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice")

	server.CreateThreatModelFromSurveyResponse(c, responseID)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestProcessMappedAnswers_EmptyValuesFiltered(t *testing.T) {
	// Simulate answers where some have empty/null values
	answers := []SurveyAnswerRow{
		{QuestionName: "filled_question", AnswerValue: json.RawMessage(`"some value"`), MapsToTmField: nil},
		{QuestionName: "empty_string", AnswerValue: json.RawMessage(`""`), MapsToTmField: nil},
		{QuestionName: "null_answer", AnswerValue: json.RawMessage(`null`), MapsToTmField: nil},
		{QuestionName: "whitespace_only", AnswerValue: json.RawMessage(`"   "`), MapsToTmField: nil},
	}

	result := processMappedAnswers(answers)

	// The processMappedAnswers function includes all metadata; the filtering
	// happens in createThreatModelFromResponse. Verify the raw output here.
	assert.Len(t, result.metadata, 4, "processMappedAnswers should include all entries")

	// Verify empty values are present (these are what cause the 500)
	emptyCount := 0
	for _, m := range result.metadata {
		if strings.TrimSpace(m.Value) == "" {
			emptyCount++
		}
	}
	assert.Equal(t, 3, emptyCount, "should have 3 entries with empty values")
}
