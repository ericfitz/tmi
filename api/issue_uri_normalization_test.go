package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1233: an empty or whitespace-only issue_uri must be normalized to "absent"
// on every write path (never stored or echoed as ""), and a padded value is
// trimmed so the echoed value satisfies format: uri.

func issueURIThreatHarness(t *testing.T) (*gin.Engine, *GormThreatRepository, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, tm := setupThreatAliasTestDB(t)
	repo := NewGormThreatRepository(db, nil, nil)
	handler := NewThreatSubResourceHandler(repo, nil, nil, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userRole", RoleWriter)
		c.Next()
	})
	base := "/threat_models/:threat_model_id/threats"
	r.POST(base, handler.CreateThreat)
	r.PUT(base+"/:threat_id", handler.UpdateThreat)
	r.PATCH(base+"/:threat_id", handler.PatchThreat)
	r.POST(base+"/bulk", handler.BulkCreateThreats)
	r.PUT(base+"/bulk", handler.BulkUpdateThreats)
	r.PATCH(base+"/bulk", handler.BulkPatchThreats)
	return r, repo, string(tm.ID)
}

func issueURIDo(r *gin.Engine, method, url string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, url, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func storedIssueURI(t *testing.T, repo *GormThreatRepository, id string) *string {
	t.Helper()
	got, err := repo.Get(context.Background(), id)
	require.NoError(t, err)
	return got.IssueUri
}

func seedThreatWithURI(t *testing.T, repo *GormThreatRepository, tmID string) string {
	t.Helper()
	tmUUID := uuid.MustParse(tmID)
	uri := "https://example.com/issue/1"
	th := &Threat{Name: "seed", ThreatType: []string{"spoofing"}, ThreatModelId: &tmUUID, IssueUri: &uri}
	require.NoError(t, repo.Create(context.Background(), th))
	require.NotNil(t, storedIssueURI(t, repo, th.Id.String()))
	return th.Id.String()
}

// issueURIFromBody extracts issue_uri from a single-object or first-array-element body.
func issueURIFromBody(t *testing.T, body []byte) (value any, present bool) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		var arr []map[string]any
		require.NoError(t, json.Unmarshal(body, &arr), string(body))
		require.NotEmpty(t, arr)
		obj = arr[0]
	}
	value, present = obj["issue_uri"]
	return
}

var blankURIs = []string{"", "   ", "\t "}

func TestThreatIssueURI_BlankNormalizedOnEveryWritePath(t *testing.T) {
	for _, blank := range blankURIs {
		t.Run("blank="+blank, func(t *testing.T) {
			r, repo, tmID := issueURIThreatHarness(t)
			base := "/threat_models/" + tmID + "/threats"
			threat := func() map[string]any {
				return map[string]any{"name": "t", "threat_type": []string{"spoofing"}, "issue_uri": blank}
			}

			// POST
			w := issueURIDo(r, "POST", base, threat())
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			v, present := issueURIFromBody(t, w.Body.Bytes())
			assert.True(t, !present || v == nil, "POST echoed issue_uri=%v", v)
			var created Threat
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
			assert.Nil(t, storedIssueURI(t, repo, created.Id.String()))

			// PUT
			id := seedThreatWithURI(t, repo, tmID)
			w = issueURIDo(r, "PUT", base+"/"+id, threat())
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = issueURIFromBody(t, w.Body.Bytes())
			assert.True(t, !present || v == nil, "PUT echoed issue_uri=%v", v)
			assert.Nil(t, storedIssueURI(t, repo, id), "PUT must clear the stored value")

			// PATCH replace
			id = seedThreatWithURI(t, repo, tmID)
			w = issueURIDo(r, "PATCH", base+"/"+id, []map[string]any{{"op": "replace", "path": "/issue_uri", "value": blank}})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = issueURIFromBody(t, w.Body.Bytes())
			assert.True(t, !present || v == nil, "PATCH echoed issue_uri=%v", v)
			assert.Nil(t, storedIssueURI(t, repo, id), "PATCH must clear the stored value")

			// Bulk POST
			w = issueURIDo(r, "POST", base+"/bulk", []map[string]any{threat()})
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			v, present = issueURIFromBody(t, w.Body.Bytes())
			assert.True(t, !present || v == nil, "bulk POST echoed issue_uri=%v", v)

			// Bulk PUT
			id = seedThreatWithURI(t, repo, tmID)
			bt := threat()
			bt["id"] = id
			w = issueURIDo(r, "PUT", base+"/bulk", []map[string]any{bt})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			v, present = issueURIFromBody(t, w.Body.Bytes())
			assert.True(t, !present || v == nil, "bulk PUT echoed issue_uri=%v", v)
			assert.Nil(t, storedIssueURI(t, repo, id), "bulk PUT must clear the stored value")

			// Bulk PATCH
			id = seedThreatWithURI(t, repo, tmID)
			w = issueURIDo(r, "PATCH", base+"/bulk", map[string]any{"patches": []map[string]any{
				{"id": id, "operations": []map[string]any{{"op": "replace", "path": "/issue_uri", "value": blank}}},
			}})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Nil(t, storedIssueURI(t, repo, id), "bulk PATCH must clear the stored value")
		})
	}
}

func TestThreatIssueURI_PaddedValueTrimmed(t *testing.T) {
	r, repo, tmID := issueURIThreatHarness(t)
	base := "/threat_models/" + tmID + "/threats"
	w := issueURIDo(r, "POST", base, map[string]any{
		"name": "t", "threat_type": []string{"spoofing"}, "issue_uri": "  https://example.com/issue/2  ",
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	v, _ := issueURIFromBody(t, w.Body.Bytes())
	assert.Equal(t, "https://example.com/issue/2", v)

	id := seedThreatWithURI(t, repo, tmID)
	w = issueURIDo(r, "PATCH", base+"/"+id, []map[string]any{{"op": "replace", "path": "/issue_uri", "value": " https://example.com/issue/3 "}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := storedIssueURI(t, repo, id)
	require.NotNil(t, got)
	assert.Equal(t, "https://example.com/issue/3", *got)
}

func TestThreatIssueURI_LegacyEmptyRowReadsAsAbsent(t *testing.T) {
	_, repo, tmID := issueURIThreatHarness(t)
	id := seedThreatWithURI(t, repo, tmID)
	require.NoError(t, repo.db.Exec("UPDATE threats SET issue_uri = '' WHERE id = ?", id).Error)
	assert.Nil(t, storedIssueURI(t, repo, id))
}
