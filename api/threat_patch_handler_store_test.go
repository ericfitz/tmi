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

// threatHandlerHarness wires the real PatchThreat/BulkPatchThreats handlers to
// a sqlite-backed store so the whole PATCH path (handler sanitizers, store
// applier, persistence) is exercised, not a mock.
func threatHandlerHarness(t *testing.T) (*gin.Engine, *GormThreatRepository, string) {
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
	r.PATCH("/threat_models/:threat_model_id/threats/:threat_id", handler.PatchThreat)
	r.PATCH("/threat_models/:threat_model_id/threats/bulk", handler.BulkPatchThreats)
	return r, repo, string(tm.ID)
}

func createHandlerTestThreat(t *testing.T, repo *GormThreatRepository, tmID, name string) string {
	t.Helper()
	tmUUID := uuid.MustParse(tmID)
	th := &Threat{Name: name, ThreatType: []string{"spoofing"}, ThreatModelId: &tmUUID}
	require.NoError(t, repo.Create(context.Background(), th))
	return th.Id.String()
}

func doPatch(r *gin.Engine, url string, body any, headers map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", url, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The reported bug through the real handler: add /name returned 200 and
// changed nothing.
func TestPatchThreatHandler_AddNamePersists(t *testing.T) {
	r, repo, tmID := threatHandlerHarness(t)
	id := createHandlerTestThreat(t, repo, tmID, "orig")

	w := doPatch(r, "/threat_models/"+tmID+"/threats/"+id,
		[]map[string]any{{"op": "add", "path": "/name", "value": "renamed"}}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	got, err := repo.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name)
}

// The If-Match path goes through PatchWithVersion: a current version applies
// and bumps; the stale one is then refused and changes nothing.
func TestPatchThreatHandler_IfMatchVersioned(t *testing.T) {
	r, repo, tmID := threatHandlerHarness(t)
	id := createHandlerTestThreat(t, repo, tmID, "orig")
	url := "/threat_models/" + tmID + "/threats/" + id

	// Get does not populate Version, so read the optimistic-lock column.
	var v int
	require.NoError(t, repo.db.Raw("SELECT version FROM threats WHERE id = ?", id).Scan(&v).Error)

	ifMatch := func(n int) map[string]string { return map[string]string{"If-Match": `"` + itoa(n) + `"`} }
	w := doPatch(r, url, []map[string]any{{"op": "add", "path": "/name", "value": "v2"}}, ifMatch(v))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, `"`+itoa(v+1)+`"`, w.Header().Get("ETag"))

	w = doPatch(r, url, []map[string]any{{"op": "add", "path": "/name", "value": "v3"}}, ifMatch(v))
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	got, err := repo.Get(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, "v2", got.Name, "a stale-version patch must not apply")
}

// A bulk request whose second entry is rejected must leave the first threat
// untouched: every entry is validated before any is written.
func TestBulkPatchThreats_InvalidEntryAppliesNothing(t *testing.T) {
	r, repo, tmID := threatHandlerHarness(t)
	first := createHandlerTestThreat(t, repo, tmID, "first")
	second := createHandlerTestThreat(t, repo, tmID, "second")
	url := "/threat_models/" + tmID + "/threats/bulk"

	w := doPatch(r, url, map[string]any{"patches": []map[string]any{
		{"id": first, "operations": []map[string]any{{"op": "add", "path": "/name", "value": "changed"}}},
		{"id": second, "operations": []map[string]any{{"op": "add", "path": "/name", "value": ""}}},
	}}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	got, err := repo.Get(context.Background(), first)
	require.NoError(t, err)
	assert.Equal(t, "first", got.Name, "an earlier valid entry must not be committed when a later one is rejected")
}

func TestBulkPatchThreats_ValidEntriesApplyAndDuplicateIDsChain(t *testing.T) {
	r, repo, tmID := threatHandlerHarness(t)
	first := createHandlerTestThreat(t, repo, tmID, "first")
	url := "/threat_models/" + tmID + "/threats/bulk"

	w := doPatch(r, url, map[string]any{"patches": []map[string]any{
		{"id": first, "operations": []map[string]any{{"op": "add", "path": "/name", "value": "a"}}},
		{"id": first, "operations": []map[string]any{{"op": "test", "path": "/name", "value": "a"}, {"op": "add", "path": "/description", "value": "d"}}},
	}}, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got, err := repo.Get(context.Background(), first)
	require.NoError(t, err)
	assert.Equal(t, "a", got.Name)
	require.NotNil(t, got.Description)
}

func TestBulkPatchThreats_EmptyOperationsRejected(t *testing.T) {
	r, repo, tmID := threatHandlerHarness(t)
	first := createHandlerTestThreat(t, repo, tmID, "first")
	w := doPatch(r, "/threat_models/"+tmID+"/threats/bulk", map[string]any{"patches": []map[string]any{
		{"id": first, "operations": []map[string]any{}},
	}}, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestBulkPatchThreats_ForeignThreatIsNotFound(t *testing.T) {
	r, repo, tmID := threatHandlerHarness(t)
	createHandlerTestThreat(t, repo, tmID, "first")
	w := doPatch(r, "/threat_models/"+tmID+"/threats/bulk", map[string]any{"patches": []map[string]any{
		{"id": uuid.New().String(), "operations": []map[string]any{{"op": "add", "path": "/name", "value": "x"}}},
	}}, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
