package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Tests for T5 (#357): confidentiality / existence-disclosure coverage on
// nested and batch endpoints. The acceptance-criterion of #357 is that a
// non-member reading a resource the caller cannot access must receive 404,
// not 403, so the response does not leak the resource's existence. These
// tests target the survey-response sub-resource family (intake, triage,
// metadata, triage notes, create-threat-model) — which previously returned
// 403 on access denial or had no ACL gate at all.

// withAccess is a small helper to grant a role on a mock survey response.
func withAccess(store *mockSurveyResponseStore, id uuid.UUID, userUUID string, role AuthorizationRole) {
	store.accessMap[store.accessKey(id, userUUID)] = role
}

func TestRequireSurveyResponseAccess_404OnDeny(t *testing.T) {
	gin.SetMode(gin.TestMode)

	respStore := newMockSurveyResponseStore()
	saveSurveyStores(t, nil, respStore)

	surveyID := uuid.New()
	responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

	t.Run("non-member reads as 404 (not 403)", func(t *testing.T) {
		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/survey_responses/%s", responseID))
		TestUsers.External.SetContext(c)

		_, ok := RequireSurveyResponseAccess(c, responseID, AuthorizationRoleReader)

		assert.False(t, ok)
		assert.Equal(t, http.StatusNotFound, w.Code, "access-denied must collapse to 404 to avoid existence disclosure")
		assert.NotContains(t, w.Body.String(), "forbidden")
	})

	t.Run("non-existent ID returns 404", func(t *testing.T) {
		c, w := CreateTestGinContext("GET", "/intake/survey_responses/"+uuid.New().String())
		TestUsers.External.SetContext(c)

		_, ok := RequireSurveyResponseAccess(c, uuid.New(), AuthorizationRoleReader)

		assert.False(t, ok)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("404 responses are indistinguishable for missing vs forbidden", func(t *testing.T) {
		// Probe an existing-but-forbidden id and a non-existent id; the
		// caller must not be able to tell them apart from the response.
		c1, w1 := CreateTestGinContext("GET", "/x")
		TestUsers.External.SetContext(c1)
		_, ok1 := RequireSurveyResponseAccess(c1, responseID, AuthorizationRoleReader)

		c2, w2 := CreateTestGinContext("GET", "/x")
		TestUsers.External.SetContext(c2)
		_, ok2 := RequireSurveyResponseAccess(c2, uuid.New(), AuthorizationRoleReader)

		assert.False(t, ok1)
		assert.False(t, ok2)
		assert.Equal(t, w1.Code, w2.Code, "response code must be identical")
		assert.Equal(t, w1.Body.String(), w2.Body.String(), "response body must be identical")
	})

	t.Run("owner read succeeds", func(t *testing.T) {
		c, _ := CreateTestGinContext("GET", "/x")
		TestUsers.Owner.SetContext(c)

		response, ok := RequireSurveyResponseAccess(c, responseID, AuthorizationRoleReader)

		assert.True(t, ok)
		assert.NotNil(t, response)
	})

	t.Run("writer can write, reader cannot", func(t *testing.T) {
		writerID := uuid.New()
		writerResp := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)
		withAccess(respStore, writerResp, TestUsers.Writer.InternalUUID, AuthorizationRoleWriter)
		withAccess(respStore, writerResp, TestUsers.Reader.InternalUUID, AuthorizationRoleReader)
		_ = writerID

		// Writer can write
		c, _ := CreateTestGinContext("PUT", "/x")
		TestUsers.Writer.SetContext(c)
		_, ok := RequireSurveyResponseAccess(c, writerResp, AuthorizationRoleWriter)
		assert.True(t, ok)

		// Reader cannot write — but the failure surfaces as 404
		c2, w2 := CreateTestGinContext("PUT", "/x")
		TestUsers.Reader.SetContext(c2)
		_, ok2 := RequireSurveyResponseAccess(c2, writerResp, AuthorizationRoleWriter)
		assert.False(t, ok2)
		assert.Equal(t, http.StatusNotFound, w2.Code)
	})
}

func TestTriageNoteHandlers_RequireParentACL(t *testing.T) {
	// Triage notes have no x-tmi-authz ownership gate (the path is not under
	// /threat_models/{id}/...) and previously enforced only existence of the
	// parent. A confidentiality leak: any authenticated user could list
	// triage notes on a confidential survey response.

	gin.SetMode(gin.TestMode)

	respStore := newMockSurveyResponseStore()
	saveSurveyStores(t, nil, respStore)

	surveyID := uuid.New()
	responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusReadyForReview, TestUsers.Owner.InternalUUID)

	// Use a real handler with a noop note store; the access gate runs before
	// the store is touched, so a working note store is not required for the
	// "denied" cases. For "allowed" we don't assert the body, only the gate.
	h := NewTriageNoteSubResourceHandler(nil)

	t.Run("ListTriageNotes denies external user with 404", func(t *testing.T) {
		c, w := CreateTestGinContext("GET", fmt.Sprintf("/triage/survey_responses/%s/triage_notes", responseID))
		TestUsers.External.SetContext(c)
		c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: responseID.String()})

		h.ListTriageNotes(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("GetTriageNote denies external user with 404", func(t *testing.T) {
		c, w := CreateTestGinContext("GET", fmt.Sprintf("/triage/survey_responses/%s/triage_notes/1", responseID))
		TestUsers.External.SetContext(c)
		c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: responseID.String()})
		c.Params = append(c.Params, gin.Param{Key: "triage_note_id", Value: "1"})

		h.GetTriageNote(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("CreateTriageNote denies external user with 404", func(t *testing.T) {
		c, w := CreateTestGinContext("POST", fmt.Sprintf("/triage/survey_responses/%s/triage_notes", responseID))
		TestUsers.External.SetContext(c)
		c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: responseID.String()})

		h.CreateTriageNote(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("CreateTriageNote denies reader-only with 404 (writer required)", func(t *testing.T) {
		// Reader has read access to the parent but not write — the existing
		// gate would have allowed this.
		withAccess(respStore, responseID, TestUsers.Reader.InternalUUID, AuthorizationRoleReader)

		c, w := CreateTestGinContext("POST", fmt.Sprintf("/triage/survey_responses/%s/triage_notes", responseID))
		TestUsers.Reader.SetContext(c)
		c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: responseID.String()})

		h.CreateTriageNote(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestSurveyResponseMetadata_ACLGated(t *testing.T) {
	// Survey-response metadata sub-resources previously inherited only
	// existence enforcement from GenericMetadataHandler. Now they are gated
	// on the parent survey-response ACL.

	gin.SetMode(gin.TestMode)

	respStore := newMockSurveyResponseStore()
	saveSurveyStores(t, nil, respStore)

	surveyID := uuid.New()
	responseID := seedSurveyResponse(respStore, surveyID, ResponseStatusDraft, TestUsers.Owner.InternalUUID)

	server := &Server{}

	t.Run("intake metadata GET denies external as 404", func(t *testing.T) {
		c, w := CreateTestGinContext("GET", fmt.Sprintf("/intake/survey_responses/%s/metadata", responseID))
		TestUsers.External.SetContext(c)

		server.GetIntakeSurveyResponseMetadata(c, responseID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("intake metadata POST denies reader with 404 (writer required)", func(t *testing.T) {
		withAccess(respStore, responseID, TestUsers.Reader.InternalUUID, AuthorizationRoleReader)

		c, w := CreateTestGinContext("POST", fmt.Sprintf("/intake/survey_responses/%s/metadata", responseID))
		TestUsers.Reader.SetContext(c)

		server.CreateIntakeSurveyResponseMetadata(c, responseID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("triage metadata GET denies external as 404", func(t *testing.T) {
		c, w := CreateTestGinContext("GET", fmt.Sprintf("/triage/survey_responses/%s/metadata", responseID))
		TestUsers.External.SetContext(c)

		server.GetTriageSurveyResponseMetadata(c, responseID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestListTriageSurveyResponses_FiltersByACL(t *testing.T) {
	// Previously this endpoint had a TODO for the ACL filter and returned
	// every survey response in the system regardless of caller identity.

	gin.SetMode(gin.TestMode)

	respStore := newMockSurveyResponseStore()
	saveSurveyStores(t, nil, respStore)

	surveyID := uuid.New()

	// Three responses: owner-A, owner-B, owner-A confidential.
	idA := seedSurveyResponse(respStore, surveyID, ResponseStatusReadyForReview, TestUsers.Owner.InternalUUID)
	idB := seedSurveyResponse(respStore, surveyID, ResponseStatusReadyForReview, TestUsers.Writer.InternalUUID)
	idC := seedSurveyResponse(respStore, surveyID, ResponseStatusReadyForReview, TestUsers.Owner.InternalUUID)
	conf := true
	respStore.responses[idC].IsConfidential = &conf

	// Wire list-items so the mock returns all three from List(...)
	respStore.listItems = []SurveyResponseListItem{
		{Id: &idA},
		{Id: &idB},
		{Id: &idC},
	}
	respStore.listTotal = 3

	// External user has no access to any response — must see empty list,
	// total=0, even though the underlying store returns 3 rows.
	server := &Server{}
	c, w := CreateTestGinContext("GET", "/triage/survey_responses")
	TestUsers.External.SetContext(c)

	server.ListTriageSurveyResponses(c, ListTriageSurveyResponsesParams{})

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"total":0`, "external user must see total=0 — list count must NOT leak existence of confidential or other-user responses")
}
