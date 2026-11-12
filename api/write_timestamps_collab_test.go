package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1155 follow-up: the stores persist the caller's ModifiedAt, so every caller
// must stamp a fresh one. These tests cover callers that used to rely on the
// store/GORM to advance modified_at.

func setupStampedStores(t *testing.T) (*GormThreatModelStore, *GormDiagramStore, *models.User, string) {
	t.Helper()
	db, user := setupWriteTimestampDB(t)
	tmStore := NewGormThreatModelStore(db)
	tm, err := tmStore.Create(context.Background(), newWriteTimestampThreatModel(user), tmIDSetter)
	require.NoError(t, err)
	return tmStore, NewGormDiagramStore(db), user, tm.Id.String()
}

func createStampedDiagram(t *testing.T, ds *GormDiagramStore, tmID string) DfdDiagram {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	d := DfdDiagram{Name: "d", Type: DfdDiagramTypeDFD100, Cells: []DfdDiagram_Cells_Item{}, CreatedAt: &now, ModifiedAt: &now}
	created, err := ds.CreateWithThreatModel(d, tmID, func(d DfdDiagram, id string) DfdDiagram {
		u, _ := uuid.Parse(id)
		d.Id = &u
		return d
	})
	require.NoError(t, err)
	return created
}

func TestCellOperationProcessor_AdvancesModifiedAt(t *testing.T) {
	_, ds, _, tmID := setupStampedStores(t)
	created := createStampedDiagram(t, ds, tmID)
	id := created.Id.String()
	db := ds.db
	backdate(t, db, &models.Diagram{}, id, tsOld)

	cellID := uuid.New()
	node := makeNodeCellItem(cellID)
	op := CellPatchOperation{Type: cellOperationTypePatch, Cells: []CellOperation{{ID: cellID.String(), Operation: "add", Data: &node}}}
	result, err := NewCellOperationProcessor(ds).ProcessCellOperations(id, op)
	require.NoError(t, err)
	require.True(t, result.Valid && result.StateChanged, "operation must apply: %+v", result)

	modified := storedTime(t, db, &models.Diagram{}, "id", id, "modified_at")
	assert.True(t, modified.After(tsOld), "modified_at must advance, got %v", modified)
	assert.Zero(t, modified.Nanosecond()%1000)
}

func TestAuditRollback_StampsFreshModifiedAt(t *testing.T) {
	tmStore, ds, _, tmID := setupStampedStores(t)
	origTM, origDiagram := ThreatModelStore, DiagramStore
	defer func() { ThreatModelStore, DiagramStore = origTM, origDiagram }()
	ThreatModelStore, DiagramStore = tmStore, ds
	h := &AuditHandler{}
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	db := ds.db

	t.Run("threat model", func(t *testing.T) {
		tm, err := tmStore.Get(tmID)
		require.NoError(t, err)
		tm.ModifiedAt = &tsOld // snapshot's historical value
		snapshot, err := json.Marshal(tm)
		require.NoError(t, err)
		backdate(t, db, &models.ThreatModel{}, tmID, tsOld)

		entry := &AuditEntryResponse{ObjectID: tmID, ObjectType: models.ObjectTypeThreatModel, ChangeType: models.ChangeTypeUpdated}
		restored, err := h.performRollback(c, entry, snapshot)
		require.NoError(t, err)
		stored := storedTime(t, db, &models.ThreatModel{}, "id", tmID, "modified_at")
		assert.True(t, stored.After(tsOld))
		assertRestoredModifiedAt(t, restored, stored)
	})

	t.Run("diagram", func(t *testing.T) {
		d := createStampedDiagram(t, ds, tmID)
		id := d.Id.String()
		d.ModifiedAt = &tsOld
		snapshot, err := json.Marshal(d)
		require.NoError(t, err)
		backdate(t, db, &models.Diagram{}, id, tsOld)

		entry := &AuditEntryResponse{ObjectID: id, ThreatModelID: tmID, ObjectType: models.ObjectTypeDiagram, ChangeType: models.ChangeTypeUpdated}
		restored, err := h.performRollback(c, entry, snapshot)
		require.NoError(t, err)
		stored := storedTime(t, db, &models.Diagram{}, "id", id, "modified_at")
		assert.True(t, stored.After(tsOld))
		assertRestoredModifiedAt(t, restored, stored)
	})
}

func TestStoreUpdate_NilModifiedAtFallsBackToNow(t *testing.T) {
	ctx := context.Background()
	tmStore, ds, user, tmID := setupStampedStores(t)
	db := ds.db

	backdate(t, db, &models.ThreatModel{}, tmID, tsOld)
	update := newWriteTimestampThreatModel(user)
	require.Nil(t, update.ModifiedAt)
	require.NoError(t, tmStore.Update(ctx, tmID, update))
	got := storedTime(t, db, &models.ThreatModel{}, "id", tmID, "modified_at")
	assert.True(t, got.After(tsOld))
	assert.Zero(t, got.Nanosecond()%1000)

	d := createStampedDiagram(t, ds, tmID)
	id := d.Id.String()
	backdate(t, db, &models.Diagram{}, id, tsOld)
	d.ModifiedAt = nil
	require.NoError(t, ds.Update(ctx, id, d))
	got = storedTime(t, db, &models.Diagram{}, "id", id, "modified_at")
	assert.True(t, got.After(tsOld))
	assert.Zero(t, got.Nanosecond()%1000)
}

func TestThreatModelPut_ResponseModifiedAtEqualsStored(t *testing.T) {
	tmStore, ds, user, tmID := setupStampedStores(t)
	origTM, origDiagram := ThreatModelStore, DiagramStore
	defer func() { ThreatModelStore, DiagramStore = origTM, origDiagram }()
	ThreatModelStore, DiagramStore = tmStore, ds

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", string(user.Email))
		c.Set("userID", user.ProviderUserID.String)
		c.Set("userProvider", string(user.Provider))
		c.Set("userInternalUUID", string(user.InternalUUID))
		c.Set("userRole", RoleOwner)
		c.Next()
	})
	h := NewThreatModelHandler(NewWebSocketHubForTests())
	r.PUT("/threat_models/:threat_model_id", h.UpdateThreatModel)

	backdate(t, ds.db, &models.ThreatModel{}, tmID, tsOld)
	body, _ := json.Marshal(map[string]any{"name": "renamed"})
	req := httptest.NewRequest("PUT", "/threat_models/"+tmID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got ThreatModel
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.ModifiedAt)
	assertTime(t, "modified_at", *got.ModifiedAt, storedTime(t, ds.db, &models.ThreatModel{}, "id", tmID, "modified_at"))
}

// assertRestoredModifiedAt checks the rollback response entity carries the
// stored modified_at as it would serialize it, not the snapshot's old value.
func assertRestoredModifiedAt(t *testing.T, restored map[string]any, stored time.Time) {
	t.Helper()
	raw, err := json.Marshal(restored["modified_at"])
	require.NoError(t, err)
	var got time.Time
	require.NoError(t, json.Unmarshal(raw, &got))
	assertTime(t, "restored modified_at", got, stored)
}
