package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedRequest is one call a stub API server received.
type recordedRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        any
}

// newRecordingClient returns an apiClient whose stub server records every
// request and answers with respond(method, path).
func newRecordingClient(t *testing.T, respond func(method, path string) (int, any)) (*apiClient, *[]recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var calls []recordedRequest
	c := newTestAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body any
		if len(raw) > 0 {
			require.NoError(t, json.Unmarshal(raw, &body))
		}
		mu.Lock()
		calls = append(calls, recordedRequest{r.Method, r.URL.Path, r.Header.Get("Content-Type"), body})
		mu.Unlock()
		status, payload := respond(r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			require.NoError(t, json.NewEncoder(w).Encode(payload))
		}
	})
	return c, &calls
}

func methodsAndPaths(calls []recordedRequest) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.Method+" "+c.Path)
	}
	return out
}

func TestUnknownSpecFields_ReportsEveryUnknownKey(t *testing.T) {
	spec := `{
		"version": "1.0",
		"bogus_top": 1,
		"_comment": "documentation is allowed",
		"users": [{"id": "u", "notes": "x", "_comment": "ok"}],
		"teams": [{"name": "T", "members": [{"user_id": "u", "rank": 1}]}],
		"surveys": [{"name": "S", "survey_json": {"anything": {"goes": true}}}],
		"survey_responses": [{"survey": "S", "responses": {"free": "form"}}]
	}`
	got, err := unknownSpecFields([]byte(spec))
	require.NoError(t, err)
	assert.Equal(t, []string{"bogus_top", "teams[0].members[0].rank", "users[0].notes"}, got)
}

func TestLoadSeedFile_RejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spec.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":"1.0","teams":[{"name":"T","colour":"red"}]}`), 0o600))

	_, err := loadSeedFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "teams[0].colour")
}

func TestLoadSeedFile_RepoSeedSpecsAreStrictClean(t *testing.T) {
	_, err := loadSeedFile("../../test/seeds/cats-seed-data.json")
	require.NoError(t, err)
}

func TestValidateSurveyResponses(t *testing.T) {
	users := buildUserLookup([]SeedSpecUser{{ID: "alice"}})
	for _, st := range []string{"", "draft", "submitted"} {
		assert.NoError(t, validateSurveyResponses([]SeedSpecSurveyResp{{Survey: "S", User: "alice", Status: st}}, users), st)
	}
	err := validateSurveyResponses([]SeedSpecSurveyResp{{Survey: "S", Status: "ready_for_review"}}, users)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ready_for_review")

	err = validateSurveyResponses([]SeedSpecSurveyResp{{Survey: "S", User: "mallory"}}, users)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mallory")
}

func TestTransformSurveyResponses_ActsAsSpecUser(t *testing.T) {
	users := buildUserLookup([]SeedSpecUser{{ID: "alice", OAuthProvider: "tmi"}})
	seeds := transformSurveyResponses([]SeedSpecSurveyResp{{Survey: "S", User: "alice", Status: "submitted"}}, users)
	require.Len(t, seeds, 1)
	d := seeds[0].Data
	assert.Equal(t, "alice", d[seedActAsUser])
	assert.Equal(t, "tmi", d[seedActAsProvider])
	assert.Equal(t, "submitted", d["status"])
	assert.NotContains(t, d, "authorization", "the server ignores authorization on create")
}

func TestTransformTeamsAndProjects_CarryAllSpecFields(t *testing.T) {
	teams := transformTeams([]SeedSpecTeam{{
		Name: "T", Status: "active", Description: "d", EmailAddress: "t@x", URI: "https://t",
		Members:            []SeedSpecTeamMember{{UserID: "a", Role: "member"}},
		ResponsibleParties: []SeedSpecTeamMember{{UserID: "b", Role: "lead"}},
		Metadata:           []SeedSpecKV{{Key: "k", Value: "v"}},
	}})
	require.NotEmpty(t, teams)
	td := teams[0].Data
	assert.Equal(t, "d", td["description"])
	assert.Equal(t, "t@x", td["email_address"])
	assert.Equal(t, "https://t", td["uri"])
	assert.Equal(t, []map[string]any{{"user_ref": userRef("b"), "role": "engineering_lead"}}, td["responsible_parties"])
	assert.Equal(t, []map[string]any{{"key": "k", "value": "v"}}, td["metadata"])

	projects := transformProjects([]SeedSpecProject{{
		Name: "P", Team: "T", Description: "pd", URI: "https://p",
		ResponsibleParties: []SeedSpecTeamMember{{UserID: "a", Role: "engineer"}},
		Metadata:           []SeedSpecKV{{Key: "fy", Value: "2026"}},
	}})
	require.NotEmpty(t, projects)
	pd := projects[0].Data
	assert.Equal(t, "pd", pd["description"])
	assert.Equal(t, "https://p", pd["uri"])
	assert.Equal(t, []map[string]any{{"user_ref": userRef("a"), "role": "engineer"}}, pd["responsible_parties"])
	assert.Equal(t, []map[string]any{{"key": "fy", "value": "2026"}}, pd["metadata"])
}

func surveyResponseEntry() (SeedEntry, RefMap) {
	entry := SeedEntry{Kind: kindSurveyResponse, Ref: "survey-response:0", Data: map[string]any{
		"survey_ref": "survey:s", "status": "submitted", "answers": map[string]any{"q": "a"},
		seedActAsUser: "alice", seedActAsProvider: "tmi",
	}}
	refs := RefMap{"survey:s": {Ref: "survey:s", Kind: kindSurvey, ID: "survey-1"}}
	return entry, refs
}

// The tmi-ux bug: the response was left in draft. A new response must be
// created without the fields the server ignores, then submitted.
func TestSeedSurveyResponse_CreatesThenSubmits(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) {
		switch method + " " + path {
		case "GET /intake/survey_responses":
			return 200, map[string]any{"survey_responses": []any{}}
		case "POST /intake/survey_responses":
			return 201, map[string]any{"id": "resp-1", "status": "draft"}
		case "PATCH /intake/survey_responses/resp-1":
			return 200, map[string]any{"id": "resp-1", "status": "submitted"}
		}
		return 404, nil
	})
	entry, refs := surveyResponseEntry()

	res, err := c.seedSurveyResponse(entry, refs)
	require.NoError(t, err)
	assert.Equal(t, "resp-1", res.ID)
	assert.Equal(t, []string{
		"GET /intake/survey_responses",
		"POST /intake/survey_responses",
		"PATCH /intake/survey_responses/resp-1",
	}, methodsAndPaths(*calls))

	created := (*calls)[1].Body.(map[string]any)
	assert.Equal(t, "survey-1", created["survey_id"])
	for _, k := range []string{"status", "survey_ref", seedActAsUser, seedActAsProvider} {
		assert.NotContains(t, created, k)
	}

	patch := (*calls)[2]
	assert.Equal(t, "application/json-patch+json", patch.ContentType)
	assert.Equal(t, []any{map[string]any{"op": "replace", "path": "/status", "value": "submitted"}}, patch.Body)
}

func TestSeedSurveyResponse_ExistingResponse(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		wantCalls []string
		wantErr   string
	}{
		{"draft is submitted", "draft", []string{"GET /intake/survey_responses", "PATCH /intake/survey_responses/resp-1"}, ""},
		{"submitted is left alone", "submitted", []string{"GET /intake/survey_responses"}, ""},
		{"past submitted is an error", "ready_for_review", []string{"GET /intake/survey_responses"}, "delete the response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := newRecordingClient(t, func(method, path string) (int, any) {
				if method == "GET" {
					return 200, map[string]any{"survey_responses": []any{
						map[string]any{"id": "other", "survey_id": "survey-2", "status": "draft"},
						map[string]any{"id": "resp-1", "survey_id": "survey-1", "status": tc.current},
					}}
				}
				return 200, map[string]any{"id": "resp-1"}
			})
			entry, refs := surveyResponseEntry()

			_, err := c.seedSurveyResponse(entry, refs)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantCalls, methodsAndPaths(*calls))
		})
	}
}

func teamEntryAndRefs() (SeedEntry, RefMap) {
	entry := transformTeams([]SeedSpecTeam{{
		Name:               "Seed Team Alpha",
		Members:            []SeedSpecTeamMember{{UserID: "test-user", Role: "engineer"}},
		ResponsibleParties: []SeedSpecTeamMember{{UserID: "test-reviewer", Role: "engineering_lead"}},
		Metadata:           []SeedSpecKV{{Key: "department", Value: "Engineering"}},
	}})[0]
	refs := RefMap{
		userRef("test-user"):     {Kind: kindUser, ID: "uuid-user"},
		userRef("test-reviewer"): {Kind: kindUser, ID: "uuid-reviewer"},
	}
	return entry, refs
}

// The tmi-ux bug: an existing team kept members=[] because it was skipped.
// It must now be replaced with the spec's members, then its metadata set.
func TestSeedTeam_ExistingTeamIsReconciled(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) {
		switch method + " " + path {
		case "GET /teams":
			return 200, map[string]any{"teams": []any{map[string]any{"id": "team-1", "name": "Seed Team Alpha"}}}
		case "PUT /teams/team-1":
			return 200, map[string]any{"id": "team-1"}
		case "PUT /teams/team-1/metadata/bulk":
			return 200, []any{map[string]any{"key": "department", "value": "Engineering"}}
		}
		return 404, nil
	})
	entry, refs := teamEntryAndRefs()

	res, err := c.seedTeam(entry, refs)
	require.NoError(t, err)
	assert.Equal(t, "team-1", res.ID)
	assert.Equal(t, []string{"GET /teams", "PUT /teams/team-1", "PUT /teams/team-1/metadata/bulk"}, methodsAndPaths(*calls))

	put := (*calls)[1].Body.(map[string]any)
	assert.Equal(t, []any{map[string]any{"user_id": "uuid-user", "role": "engineer"}}, put["members"])
	assert.Equal(t, []any{map[string]any{"user_id": "uuid-reviewer", "role": "engineering_lead"}}, put["responsible_parties"])
	assert.NotContains(t, put, "metadata")
	assert.Equal(t, []any{map[string]any{"key": "department", "value": "Engineering"}}, (*calls)[2].Body)
}

func TestSeedTeam_NewTeamIsCreated(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) {
		switch method + " " + path {
		case "GET /teams":
			return 200, map[string]any{"teams": []any{}}
		case "POST /teams":
			return 201, map[string]any{"id": "team-new"}
		case "PUT /teams/team-new":
			return 200, map[string]any{"id": "team-new"}
		case "PUT /teams/team-new/metadata/bulk":
			return 200, []any{}
		}
		return 404, nil
	})
	entry, refs := teamEntryAndRefs()

	res, err := c.seedTeam(entry, refs)
	require.NoError(t, err)
	assert.Equal(t, "team-new", res.ID)
	// The PUT after the create drops the creator the server adds as a member.
	assert.Equal(t, []string{"GET /teams", "POST /teams", "PUT /teams/team-new", "PUT /teams/team-new/metadata/bulk"}, methodsAndPaths(*calls))
}

func TestSeedTeam_UnresolvableMemberIsAnError(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) {
		return 200, map[string]any{"teams": []any{}}
	})
	entry, _ := teamEntryAndRefs()

	_, err := c.seedTeam(entry, RefMap{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "members")
	assert.Equal(t, []string{"GET /teams"}, methodsAndPaths(*calls), "nothing may be written for a team with a dropped member")
}

func TestSeedProject_ExistingProjectIsReconciledWithTeamID(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) {
		switch method + " " + path {
		case "GET /projects":
			return 200, map[string]any{"projects": []any{map[string]any{"id": "proj-1", "name": "P"}}}
		case "PUT /projects/proj-1":
			return 200, map[string]any{"id": "proj-1"}
		}
		return 404, nil
	})
	entry := transformProjects([]SeedSpecProject{{Name: "P", Team: "T"}})[0]
	refs := RefMap{teamRef("T"): {Kind: kindTeam, ID: "team-1"}}

	_, err := c.seedProject(entry, refs)
	require.NoError(t, err)
	assert.Equal(t, []string{"GET /projects", "PUT /projects/proj-1"}, methodsAndPaths(*calls))
	put := (*calls)[1].Body.(map[string]any)
	assert.Equal(t, "team-1", put["team_id"])
	assert.NotContains(t, put, "team_ref")
}

func TestSeedMetadata_TeamAndProjectTargets(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) {
		return 201, map[string]any{"key": "k", "value": "v"}
	})
	refs := RefMap{
		"team:t":    {Kind: kindTeam, ID: "team-1"},
		"project:p": {Kind: kindProject, ID: "proj-1"},
	}
	for ref, kind := range map[string]string{"team:t": kindTeam, "project:p": kindProject} {
		_, err := c.seedMetadata(SeedEntry{Kind: kindMetadata, Data: map[string]any{
			"target_ref": ref, "target_kind": kind, "key": "k", "value": "v",
		}}, refs)
		require.NoError(t, err)
	}
	got := methodsAndPaths(*calls)
	assert.ElementsMatch(t, []string{"POST /teams/team-1/metadata", "POST /projects/proj-1/metadata"}, got)
}

func TestSeedMetadata_UnsupportedTargetIsRejected(t *testing.T) {
	c, calls := newRecordingClient(t, func(method, path string) (int, any) { return 200, nil })
	refs := RefMap{"team-note:t:0": {Kind: kindTeamNote, ID: "1", Extra: map[string]string{"team_id": "team-1"}}}

	_, err := c.seedMetadata(SeedEntry{Kind: kindMetadata, Data: map[string]any{
		"target_ref": "team-note:t:0", "target_kind": kindTeamNote, "key": "k", "value": "v",
	}}, refs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a supported metadata target")
	assert.Empty(t, *calls)
}

func TestTokenCache_AuthenticatesEachUserOnce(t *testing.T) {
	var logins []string
	tc := newTokenCache("http://tmi", func(_, user, provider string) (string, error) {
		logins = append(logins, provider+"/"+user)
		return "tok-" + user, nil
	})
	for _, u := range []string{"charlie", "alice", "charlie", "alice"} {
		tok, err := tc.get(u, "tmi")
		require.NoError(t, err)
		assert.Equal(t, "tok-"+u, tok)
	}
	assert.Equal(t, []string{"tmi/charlie", "tmi/alice"}, logins)
}

func TestActAsUser(t *testing.T) {
	u, p := actAsUser(SeedEntry{Data: map[string]any{seedActAsUser: "alice"}})
	assert.Equal(t, "alice", u)
	assert.Equal(t, defaultProvider, p)

	u, _ = actAsUser(SeedEntry{Data: map[string]any{"name": "x"}})
	assert.Empty(t, u)
}
