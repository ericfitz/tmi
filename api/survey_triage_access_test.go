package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Tests for #1100: triage endpoints are open only to Security Reviewers
// (non-confidential responses) and Confidential Project Reviewers
// (confidential responses). Ownership and the row ACL grant no triage access.

// triageMembershipRepo is a GroupMemberRepository that answers
// IsEffectiveMember from a per-user set of built-in group UUIDs.
type triageMembershipRepo struct {
	GroupMemberRepository
	members map[uuid.UUID]map[uuid.UUID]bool // user -> group -> member
	err     error
}

func (r *triageMembershipRepo) IsEffectiveMember(_ context.Context, groupUUID, userUUID uuid.UUID, _ []uuid.UUID) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	return r.members[userUUID][groupUUID], nil
}

func triageTestUser(name string) TestUserIdentity {
	return TestUserIdentity{
		Email:        name + "@tmi.local",
		ProviderID:   name + "-provider-id",
		InternalUUID: uuid.New().String(),
		IdP:          "tmi",
		Groups:       []string{},
	}
}

func (r *triageMembershipRepo) grant(u TestUserIdentity, groups ...BuiltInGroup) {
	uid := uuid.MustParse(u.InternalUUID)
	if r.members[uid] == nil {
		r.members[uid] = map[uuid.UUID]bool{}
	}
	for _, g := range groups {
		r.members[uid][g.UUID] = true
	}
}

// installTriageMembership swaps GlobalGroupMemberRepository for the test.
func installTriageMembership(t *testing.T) *triageMembershipRepo {
	t.Helper()
	repo := &triageMembershipRepo{members: map[uuid.UUID]map[uuid.UUID]bool{}}
	orig := GlobalGroupMemberRepository
	GlobalGroupMemberRepository = repo
	t.Cleanup(func() { GlobalGroupMemberRepository = orig })
	return repo
}

type triageFixture struct {
	alice, sr, cpr, both TestUserIdentity
	respStore            *mockSurveyResponseStore
	server               *Server
	normalID, confID     uuid.UUID
}

func newTriageFixture(t *testing.T) *triageFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f := &triageFixture{
		alice: triageTestUser("alice"),
		sr:    triageTestUser("sr"),
		cpr:   triageTestUser("cpr"),
		both:  triageTestUser("both"),
	}
	repo := installTriageMembership(t)
	repo.grant(f.sr, GroupSecurityReviewers)
	repo.grant(f.cpr, GroupConfidentialProjectReviewers)
	repo.grant(f.both, GroupSecurityReviewers, GroupConfidentialProjectReviewers)

	f.respStore = newMockSurveyResponseStore()
	saveSurveyStores(t, nil, f.respStore)

	surveyID := uuid.New()
	f.normalID = seedSurveyResponse(f.respStore, surveyID, ResponseStatusSubmitted, f.alice.InternalUUID)
	f.confID = seedSurveyResponse(f.respStore, surveyID, ResponseStatusSubmitted, f.alice.InternalUUID)
	yes, no := true, false
	f.respStore.responses[f.confID].IsConfidential = &yes
	f.respStore.responses[f.normalID].IsConfidential = &no
	// The reviewer groups hold ACL rows too, as seeded in production; the
	// guard must not depend on them.
	f.respStore.listItems = []SurveyResponseListItem{
		{Id: &f.normalID, IsConfidential: &no, Status: ResponseStatusSubmitted},
		{Id: &f.confID, IsConfidential: &yes, Status: ResponseStatusSubmitted},
	}
	f.respStore.listTotal = 2

	noteStore := newMockTriageNoteStore()
	f.server = &Server{triageNoteHandler: NewTriageNoteSubResourceHandler(noteStore)}
	return f
}

func (f *triageFixture) listIDs(t *testing.T, u TestUserIdentity) []uuid.UUID {
	t.Helper()
	c, w := CreateTestGinContext("GET", "/triage/survey_responses")
	u.SetContext(c)
	f.server.ListTriageSurveyResponses(c, ListTriageSurveyResponsesParams{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp ListSurveyResponsesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, len(resp.SurveyResponses), resp.Total)
	ids := []uuid.UUID{}
	for _, it := range resp.SurveyResponses {
		ids = append(ids, *it.Id)
	}
	return ids
}

func TestTriageList_ScopedByReviewerGroup(t *testing.T) {
	f := newTriageFixture(t)

	assert.Empty(t, f.listIDs(t, f.alice), "submitter without reviewer group sees nothing")
	assert.Equal(t, []uuid.UUID{f.normalID}, f.listIDs(t, f.sr))
	assert.Equal(t, []uuid.UUID{f.confID}, f.listIDs(t, f.cpr))
	assert.ElementsMatch(t, []uuid.UUID{f.normalID, f.confID}, f.listIDs(t, f.both))
}

func TestTriageList_MembershipLookupFailureIs500(t *testing.T) {
	f := newTriageFixture(t)
	GlobalGroupMemberRepository.(*triageMembershipRepo).err = fmt.Errorf("db down")
	c, w := CreateTestGinContext("GET", "/triage/survey_responses")
	f.sr.SetContext(c)
	f.server.ListTriageSurveyResponses(c, ListTriageSurveyResponsesParams{})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestTriagePerID_DeniedWithout404Leak(t *testing.T) {
	f := newTriageFixture(t)
	patch := []byte(`[{"op":"replace","path":"/status","value":"ready_for_review"}]`)

	type call func(u TestUserIdentity, id uuid.UUID) int
	calls := map[string]call{
		"get": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContext("GET", "/x")
			u.SetContext(c)
			f.server.GetTriageSurveyResponse(c, id)
			return w.Code
		},
		"patch": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContextWithBody("PATCH", "/x", "application/json-patch+json", patch)
			u.SetContext(c)
			f.server.PatchTriageSurveyResponse(c, id)
			return w.Code
		},
		"create_threat_model": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContext("POST", "/x")
			u.SetContext(c)
			f.server.CreateThreatModelFromSurveyResponse(c, id)
			return w.Code
		},
		"metadata": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContext("GET", "/x")
			u.SetContext(c)
			f.server.GetTriageSurveyResponseMetadata(c, id)
			return w.Code
		},
		"metadata_by_key": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContext("GET", "/x")
			u.SetContext(c)
			f.server.GetTriageSurveyResponseMetadataByKey(c, id, "k")
			return w.Code
		},
		"triage_notes_list": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContext("GET", "/x")
			u.SetContext(c)
			f.server.ListTriageSurveyResponseTriageNotes(c, id, ListTriageSurveyResponseTriageNotesParams{})
			return w.Code
		},
		"triage_notes_create": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContextWithBody("POST", "/x", "application/json", []byte(`{"name":"n","content":"c"}`))
			u.SetContext(c)
			f.server.CreateTriageSurveyResponseTriageNote(c, id)
			return w.Code
		},
		"triage_notes_get": func(u TestUserIdentity, id uuid.UUID) int {
			c, w := CreateTestGinContext("GET", "/x")
			u.SetContext(c)
			f.server.GetTriageSurveyResponseTriageNote(c, id, 1)
			return w.Code
		},
	}

	for name, do := range calls {
		t.Run(name, func(t *testing.T) {
			// Submitter owns both rows and must be denied on both.
			assert.Equal(t, http.StatusNotFound, do(f.alice, f.normalID), "alice/N")
			assert.Equal(t, http.StatusNotFound, do(f.alice, f.confID), "alice/C")
			// Cross-group denial.
			assert.Equal(t, http.StatusNotFound, do(f.sr, f.confID), "sr/C")
			assert.Equal(t, http.StatusNotFound, do(f.cpr, f.normalID), "cpr/N")
			// Nonexistent id is indistinguishable.
			assert.Equal(t, http.StatusNotFound, do(f.sr, uuid.New()), "sr/missing")
		})
	}
}

func TestTriagePerID_ReviewersAllowed(t *testing.T) {
	f := newTriageFixture(t)

	get := func(u TestUserIdentity, id uuid.UUID) int {
		c, w := CreateTestGinContext("GET", "/x")
		u.SetContext(c)
		f.server.GetTriageSurveyResponse(c, id)
		return w.Code
	}
	assert.Equal(t, http.StatusOK, get(f.sr, f.normalID))
	assert.Equal(t, http.StatusOK, get(f.cpr, f.confID))
	assert.Equal(t, http.StatusOK, get(f.both, f.normalID))
	assert.Equal(t, http.StatusOK, get(f.both, f.confID))

	// sr can patch N even though the ACL grants sr no row.
	c, w := CreateTestGinContextWithBody("PATCH", "/x", "application/json-patch+json",
		[]byte(`[{"op":"replace","path":"/status","value":"ready_for_review"}]`))
	f.sr.SetContext(c)
	f.server.PatchTriageSurveyResponse(c, f.normalID)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Triage notes: a reviewer who has no ACL row reaches the note store.
	c2, w2 := CreateTestGinContext("GET", "/x")
	f.cpr.SetContext(c2)
	f.server.ListTriageSurveyResponseTriageNotes(c2, f.confID, ListTriageSurveyResponseTriageNotesParams{})
	assert.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
}

func TestTriageAccess_IntakeUnchangedForSubmitter(t *testing.T) {
	f := newTriageFixture(t)

	c, w := CreateTestGinContext("GET", "/x")
	f.alice.SetContext(c)
	f.server.GetIntakeSurveyResponse(c, f.normalID)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	draftID := seedSurveyResponse(f.respStore, uuid.New(), ResponseStatusDraft, f.alice.InternalUUID)
	c2, w2 := CreateTestGinContextWithBody("PATCH", "/x", "application/json-patch+json",
		[]byte(`[{"op":"replace","path":"/ui_state","value":{"a":1}}]`))
	f.alice.SetContext(c2)
	f.server.PatchIntakeSurveyResponse(c2, draftID, PatchIntakeSurveyResponseParams{})
	assert.Equal(t, http.StatusOK, w2.Code, "submitter keeps intake patch access: %s", w2.Body.String())

	c3, w3 := CreateTestGinContext("GET", "/x")
	f.alice.SetContext(c3)
	f.server.ListIntakeSurveyResponseTriageNotes(c3, f.normalID, ListIntakeSurveyResponseTriageNotesParams{})
	assert.Equal(t, http.StatusOK, w3.Code, w3.Body.String())
}

func TestTriagePerID_NilConfidentialityFailsClosed(t *testing.T) {
	f := newTriageFixture(t)
	f.respStore.responses[f.normalID].IsConfidential = nil

	get := func(u TestUserIdentity) int {
		c, w := CreateTestGinContext("GET", "/x")
		u.SetContext(c)
		f.server.GetTriageSurveyResponse(c, f.normalID)
		return w.Code
	}
	// Unknown confidentiality is treated as confidential: only the
	// Confidential Project Reviewers group may triage it.
	assert.Equal(t, http.StatusNotFound, get(f.sr), "sr/nil")
	assert.Equal(t, http.StatusOK, get(f.cpr), "cpr/nil")
}

func TestTriageReviewerGroup(t *testing.T) {
	assert.Equal(t, GroupConfidentialProjectReviewers, triageReviewerGroup(true))
	assert.Equal(t, GroupSecurityReviewers, triageReviewerGroup(false))
}

// asTriageUser installs a membership repository and authenticates c as a fresh
// user belonging to the given built-in groups (none = a non-reviewer).
func asTriageUser(t *testing.T, c *gin.Context, groups ...BuiltInGroup) TestUserIdentity {
	t.Helper()
	u := triageTestUser("triage")
	installTriageMembership(t).grant(u, groups...)
	u.SetContext(c)
	return u
}

func TestTriageMetadata_ReviewerAllowed(t *testing.T) {
	f := newTriageFixture(t)
	md := &MockMetadataStore{}
	md.On("List", mock.Anything, "survey_response", f.normalID.String()).Return([]Metadata{{Key: "k", Value: "v"}}, nil)
	md.On("Get", mock.Anything, "survey_response", f.normalID.String(), "k").Return(&Metadata{Key: "k", Value: "v"}, nil)
	f.server.surveyResponseMetadata = NewGenericMetadataHandler(md, "survey_response", "survey_response_id", nil)

	c, w := CreateTestGinContext("GET", "/x")
	f.sr.SetContext(c)
	c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: f.normalID.String()})
	f.server.GetTriageSurveyResponseMetadata(c, f.normalID)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	c2, w2 := CreateTestGinContext("GET", "/x")
	f.sr.SetContext(c2)
	c2.Params = append(c2.Params,
		gin.Param{Key: "survey_response_id", Value: f.normalID.String()},
		gin.Param{Key: "key", Value: "k"})
	f.server.GetTriageSurveyResponseMetadataByKey(c2, f.normalID, "k")
	assert.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
}

// Intake callers may only set status to draft or submitted; approval states
// belong to triage reviewers (#1100).
func TestIntakePatch_SubmitterCannotSetReviewStatus(t *testing.T) {
	f := newTriageFixture(t)

	patchStatus := func(id uuid.UUID, status string) (int, string) {
		body := []byte(fmt.Sprintf(`[{"op":"replace","path":"/status","value":%q}]`, status))
		c, w := CreateTestGinContextWithBody("PATCH", "/x", "application/json-patch+json", body)
		f.alice.SetContext(c)
		f.server.PatchIntakeSurveyResponse(c, id, PatchIntakeSurveyResponseParams{})
		return w.Code, w.Body.String()
	}

	for _, from := range []string{ResponseStatusDraft, ResponseStatusNeedsRevision} {
		for _, target := range []string{ResponseStatusReadyForReview, ResponseStatusNeedsRevision, ResponseStatusReviewCreated} {
			if from == target {
				continue
			}
			t.Run(from+"_to_"+target, func(t *testing.T) {
				id := seedSurveyResponse(f.respStore, uuid.New(), from, f.alice.InternalUUID)
				code, body := patchStatus(id, target)
				assert.Equal(t, http.StatusBadRequest, code, body)
				assert.Equal(t, from, *f.respStore.responses[id].Status, "status must be unchanged")
			})
		}
	}

	t.Run("draft_to_submitted_allowed", func(t *testing.T) {
		id := seedSurveyResponse(f.respStore, uuid.New(), ResponseStatusDraft, f.alice.InternalUUID)
		code, body := patchStatus(id, ResponseStatusSubmitted)
		assert.Equal(t, http.StatusOK, code, body)
	})
	t.Run("needs_revision_to_submitted_allowed", func(t *testing.T) {
		id := seedSurveyResponse(f.respStore, uuid.New(), ResponseStatusNeedsRevision, f.alice.InternalUUID)
		code, body := patchStatus(id, ResponseStatusSubmitted)
		assert.Equal(t, http.StatusOK, code, body)
	})
}

func TestValidateIntakeStatusTarget(t *testing.T) {
	assert.NoError(t, validateIntakeStatusTarget(ResponseStatusDraft))
	assert.NoError(t, validateIntakeStatusTarget(ResponseStatusSubmitted))
	assert.Error(t, validateIntakeStatusTarget(ResponseStatusReadyForReview))
	assert.Error(t, validateIntakeStatusTarget(ResponseStatusReviewCreated))
	assert.Error(t, validateIntakeStatusTarget("bogus"))
}
