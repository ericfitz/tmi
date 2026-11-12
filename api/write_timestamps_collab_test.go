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
	"gorm.io/gorm"
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

// rollbackCase drives one sub-resource type through the audit rollback.
type rollbackCase struct {
	objectType string
	model      any
	install    func(db *gorm.DB)
	create     func(t *testing.T, ctx context.Context, tmID string) string
	snapshot   func(id string) any
	remove     func(t *testing.T, ctx context.Context, id string)
}

func rollbackCases(db *gorm.DB, tmID string) []rollbackCase {
	ctx := context.Background()
	uid := uuid.MustParse
	assetRepo := NewGormAssetRepository(db, nil, nil)
	docRepo := NewGormDocumentRepository(db, nil, nil)
	noteRepo := NewGormNoteRepository(db, nil, nil)
	repoRepo := NewGormRepositoryRepository(db, nil, nil)
	threatRepo := NewGormThreatRepository(db, nil, nil)
	_ = ctx
	return []rollbackCase{
		{objectType: models.ObjectTypeAsset, model: &models.Asset{},
			install: func(*gorm.DB) { GlobalAssetRepository = assetRepo },
			create: func(t *testing.T, ctx context.Context, tm string) string {
				id := uuid.New()
				require.NoError(t, assetRepo.Create(ctx, &Asset{Id: &id, Name: "a", Type: AssetTypeData}, tm))
				return id.String()
			},
			snapshot: func(id string) any {
				i := uid(id)
				return Asset{Id: &i, Name: "a2", Type: AssetTypeData, CreatedAt: &tsOld, ModifiedAt: &tsOld}
			},
			remove: func(t *testing.T, ctx context.Context, id string) { require.NoError(t, assetRepo.Delete(ctx, id)) }},
		{objectType: models.ObjectTypeDocument, model: &models.Document{},
			install: func(*gorm.DB) { GlobalDocumentRepository = docRepo },
			create: func(t *testing.T, ctx context.Context, tm string) string {
				id := uuid.New()
				require.NoError(t, docRepo.Create(ctx, &Document{Id: &id, Name: "d", Uri: "https://example.com/d"}, tm))
				return id.String()
			},
			snapshot: func(id string) any {
				i := uid(id)
				return Document{Id: &i, Name: "d2", Uri: "https://example.com/d", CreatedAt: &tsOld, ModifiedAt: &tsOld}
			},
			remove: func(t *testing.T, ctx context.Context, id string) { require.NoError(t, docRepo.Delete(ctx, id)) }},
		{objectType: models.ObjectTypeNote, model: &models.Note{},
			install: func(*gorm.DB) { GlobalNoteRepository = noteRepo },
			create: func(t *testing.T, ctx context.Context, tm string) string {
				id := uuid.New()
				require.NoError(t, noteRepo.Create(ctx, &Note{Id: &id, Name: "n", Content: "c"}, tm))
				return id.String()
			},
			snapshot: func(id string) any {
				i := uid(id)
				return Note{Id: &i, Name: "n2", Content: "c", CreatedAt: &tsOld, ModifiedAt: &tsOld}
			},
			remove: func(t *testing.T, ctx context.Context, id string) { require.NoError(t, noteRepo.Delete(ctx, id)) }},
		{objectType: models.ObjectTypeRepository, model: &models.Repository{},
			install: func(*gorm.DB) { GlobalRepositoryRepository = repoRepo },
			create: func(t *testing.T, ctx context.Context, tm string) string {
				id := uuid.New()
				require.NoError(t, repoRepo.Create(ctx, &Repository{Id: &id, Uri: "https://github.com/example/r"}, tm))
				return id.String()
			},
			snapshot: func(id string) any {
				i := uid(id)
				return Repository{Id: &i, Uri: "https://github.com/example/r2", CreatedAt: &tsOld, ModifiedAt: &tsOld}
			},
			remove: func(t *testing.T, ctx context.Context, id string) { require.NoError(t, repoRepo.Delete(ctx, id)) }},
		{objectType: models.ObjectTypeThreat, model: &models.Threat{},
			install: func(*gorm.DB) { GlobalThreatRepository = threatRepo },
			create: func(t *testing.T, ctx context.Context, tm string) string {
				tmUUID := uid(tm)
				th := &Threat{Name: "t", ThreatModelId: &tmUUID}
				require.NoError(t, threatRepo.Create(ctx, th))
				return th.Id.String()
			},
			snapshot: func(id string) any {
				i := uid(id)
				tmUUID := uid(tmID)
				return Threat{Id: &i, Name: "t2", ThreatModelId: &tmUUID, CreatedAt: &tsOld, ModifiedAt: &tsOld}
			},
			remove: func(t *testing.T, ctx context.Context, id string) { require.NoError(t, threatRepo.Delete(ctx, id)) }},
	}
}

func TestAuditRollback_SubResources_ReturnStoredTimestamps(t *testing.T) {
	tmStore, ds, _, tmID := setupStampedStores(t)
	db := ds.db
	_ = tmStore
	origA, origD, origN, origR, origT := GlobalAssetRepository, GlobalDocumentRepository, GlobalNoteRepository, GlobalRepositoryRepository, GlobalThreatRepository
	defer func() {
		GlobalAssetRepository, GlobalDocumentRepository, GlobalNoteRepository, GlobalRepositoryRepository, GlobalThreatRepository = origA, origD, origN, origR, origT
	}()
	h := &AuditHandler{}
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", nil)
	ctx := context.Background()

	for _, tc := range rollbackCases(db, tmID) {
		tc.install(db)
		check := func(t *testing.T, id string, restored map[string]any) {
			t.Helper()
			created, modified := storedTimes(t, db, tc.model, id)
			assert.True(t, modified.After(tsOld), "modified_at must advance")
			assertRestoredModifiedAt(t, restored, modified)
			raw, err := json.Marshal(restored["created_at"])
			require.NoError(t, err)
			var got time.Time
			require.NoError(t, json.Unmarshal(raw, &got))
			assertTime(t, "restored created_at", got, created)
		}

		t.Run(tc.objectType+" update", func(t *testing.T) {
			id := tc.create(t, ctx, tmID)
			backdate(t, db, tc.model, id, tsOld)
			snap, _ := json.Marshal(tc.snapshot(id))
			entry := &AuditEntryResponse{ObjectID: id, ThreatModelID: tmID, ObjectType: tc.objectType, ChangeType: models.ChangeTypeUpdated}
			restored, err := h.performRollback(c, entry, snap)
			require.NoError(t, err)
			check(t, id, restored)
		})

		t.Run(tc.objectType+" restore then update", func(t *testing.T) {
			id := tc.create(t, ctx, tmID)
			tc.remove(t, ctx, id)
			backdate(t, db, tc.model, id, tsOld)
			snap, _ := json.Marshal(tc.snapshot(id))
			entry := &AuditEntryResponse{ObjectID: id, ThreatModelID: tmID, ObjectType: tc.objectType, ChangeType: models.ChangeTypeDeleted}
			restored, err := h.performRollback(c, entry, snap)
			require.NoError(t, err)
			check(t, id, restored)
		})

		t.Run(tc.objectType+" recreate", func(t *testing.T) {
			id := tc.create(t, ctx, tmID)
			require.NoError(t, db.Where("id = ?", id).Delete(tc.model).Error)
			snap, _ := json.Marshal(tc.snapshot(id))
			entry := &AuditEntryResponse{ObjectID: id, ThreatModelID: tmID, ObjectType: tc.objectType, ChangeType: models.ChangeTypeDeleted}
			restored, err := h.performRollback(c, entry, snap)
			require.NoError(t, err)
			check(t, id, restored)
		})
	}
}

func TestRollbackResult_RejectsMissingModifiedAt(t *testing.T) {
	_, err := rollbackResult(nil, nil, nil)
	require.Error(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	got, err := rollbackResult(nil, &now, nil)
	require.NoError(t, err)
	assert.True(t, got.modifiedAt.Equal(now))
	_, err = rollbackResult(&now, &now, assert.AnError)
	require.ErrorIs(t, err, assert.AnError)
}

func TestStoreCreate_NormalizesCallerTimestamps(t *testing.T) {
	ctx := context.Background()
	tmStore, ds, user, tmID := setupStampedStores(t)
	db := ds.db
	_ = tmID
	// A local-zone value with sub-microsecond digits, as an untruncated caller sends.
	odd := time.Now().In(time.FixedZone("X", 3600)).Add(123 * time.Nanosecond)

	tm := newWriteTimestampThreatModel(user)
	tm.CreatedAt, tm.ModifiedAt = &odd, &odd
	created, err := tmStore.Create(ctx, tm, tmIDSetter)
	require.NoError(t, err)
	assertStoredTimes(t, db, &models.ThreatModel{}, created.Id.String(), created.CreatedAt, created.ModifiedAt)
	assert.Equal(t, time.UTC, created.CreatedAt.Location())

	d := DfdDiagram{Name: "d", Type: DfdDiagramTypeDFD100, Cells: []DfdDiagram_Cells_Item{}, CreatedAt: &odd, ModifiedAt: &odd}
	createdD, err := ds.CreateWithThreatModel(d, tmID, func(d DfdDiagram, id string) DfdDiagram {
		u, _ := uuid.Parse(id)
		d.Id = &u
		return d
	})
	require.NoError(t, err)
	assertStoredTimes(t, db, &models.Diagram{}, createdD.Id.String(), createdD.CreatedAt, createdD.ModifiedAt)
	assert.Equal(t, time.UTC, createdD.CreatedAt.Location())
}

func ginWithUser(user *models.User) *gin.Engine {
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
	return r
}

func TestThreatModelPatch_ResponseModifiedAtEqualsStored(t *testing.T) {
	tmStore, ds, user, tmID := setupStampedStores(t)
	origTM, origDiagram := ThreatModelStore, DiagramStore
	defer func() { ThreatModelStore, DiagramStore = origTM, origDiagram }()
	ThreatModelStore, DiagramStore = tmStore, ds
	r := ginWithUser(user)
	h := NewThreatModelHandler(NewWebSocketHubForTests())
	r.PATCH("/threat_models/:threat_model_id", h.PatchThreatModel)

	backdate(t, ds.db, &models.ThreatModel{}, tmID, tsOld)
	body := []byte(`[{"op":"replace","path":"/name","value":"patched"}]`)
	req := httptest.NewRequest("PATCH", "/threat_models/"+tmID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json-patch+json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got ThreatModel
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.NotNil(t, got.ModifiedAt)
	stored := storedTime(t, ds.db, &models.ThreatModel{}, "id", tmID, "modified_at")
	assert.True(t, stored.After(tsOld))
	assertTime(t, "modified_at", *got.ModifiedAt, stored)
}

func TestAdminUserPatch_ResponseModifiedAtEqualsStored(t *testing.T) {
	db, user := setupWriteTimestampDB(t)
	orig := GlobalUserStore
	defer func() { GlobalUserStore = orig }()
	GlobalUserStore = NewGormUserStore(db, nil)
	require.NoError(t, db.Model(&models.User{}).Where("internal_uuid = ?", string(user.InternalUUID)).
		UpdateColumns(map[string]any{"modified_at": tsOld}).Error)

	r := ginWithUser(user)
	s := &Server{}
	r.PATCH("/admin/users/:id", func(c *gin.Context) {
		id, _ := uuid.Parse(c.Param("id"))
		s.UpdateAdminUser(c, id)
	})
	body, _ := json.Marshal(map[string]any{"name": "Alice Renamed"})
	req := httptest.NewRequest("PATCH", "/admin/users/"+string(user.InternalUUID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got AdminUser
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	stored := storedTime(t, db, &models.User{}, "internal_uuid", string(user.InternalUUID), "modified_at")
	assert.True(t, stored.After(tsOld))
	assertTime(t, "modified_at", got.ModifiedAt, stored)
}

func TestAdminGroup_ResponsesMatchStored(t *testing.T) {
	db, user := setupWriteTimestampDB(t)
	orig := GlobalGroupRepository
	defer func() { GlobalGroupRepository = orig }()
	GlobalGroupRepository = NewGormGroupRepository(db)
	r := ginWithUser(user)
	s := &Server{}
	r.POST("/admin/groups", s.CreateAdminGroup)
	r.PATCH("/admin/groups/:id", func(c *gin.Context) {
		id, _ := uuid.Parse(c.Param("id"))
		s.UpdateAdminGroup(c, id)
	})

	body, _ := json.Marshal(map[string]any{"name": "G", "group_name": "g-ts"})
	req := httptest.NewRequest("POST", "/admin/groups", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created Group
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	id := created.InternalUUID.String()
	assertTime(t, "first_used", created.FirstUsed, storedTime(t, db, &models.Group{}, "internal_uuid", id, "first_used"))
	assertTime(t, "last_used", created.LastUsed, storedTime(t, db, &models.Group{}, "internal_uuid", id, "last_used"))

	require.NoError(t, db.Model(&models.Group{}).Where("internal_uuid = ?", id).
		UpdateColumns(map[string]any{"last_used": tsOld}).Error)
	body, _ = json.Marshal(map[string]any{"name": "G2"})
	req = httptest.NewRequest("PATCH", "/admin/groups/"+id, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var patched Group
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &patched))
	stored := storedTime(t, db, &models.Group{}, "internal_uuid", id, "last_used")
	assert.True(t, stored.After(tsOld))
	assertTime(t, "last_used (patch)", patched.LastUsed, stored)
}

// A note deleted between the Update's read and its write must yield NotFound,
// not a silent success. A callback removes the row on the update's own
// connection just before GORM runs the UPDATE.
func TestNoteUpdate_RowDeletedMidUpdateReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	db, user := setupWriteTimestampDB(t)
	team := &models.TeamRecord{ID: models.DBVarchar(uuid.New().String()), Name: "T", CreatedByInternalUUID: user.InternalUUID}
	require.NoError(t, db.Create(team).Error)
	project := &models.ProjectRecord{ID: models.DBVarchar(uuid.New().String()), Name: "P", TeamID: team.ID, CreatedByInternalUUID: user.InternalUUID}
	require.NoError(t, db.Create(project).Error)

	teamStore := NewGormTeamNoteStore(db)
	projectStore := NewGormProjectNoteStore(db)
	tn, err := teamStore.Create(ctx, &TeamNote{Name: "n", Content: "c"}, string(team.ID))
	require.NoError(t, err)
	pn, err := projectStore.Create(ctx, &ProjectNote{Name: "n", Content: "c"}, string(project.ID))
	require.NoError(t, err)

	const cb = "test:delete-before-update"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(cb, func(tx *gorm.DB) {
		table := tx.Statement.Table
		if table != (models.TeamNoteRecord{}).TableName() && table != (models.ProjectNoteRecord{}).TableName() {
			return
		}
		_, err := tx.Statement.ConnPool.ExecContext(ctx, "DELETE FROM "+table)
		require.NoError(t, err)
	}))
	defer func() { _ = db.Callback().Update().Remove(cb) }()

	_, err = teamStore.Update(ctx, tn.Id.String(), &TeamNote{Name: "n2", Content: "c2"}, string(team.ID))
	assert.ErrorIs(t, err, ErrTeamNoteNotFound)
	_, err = projectStore.Update(ctx, pn.Id.String(), &ProjectNote{Name: "n2", Content: "c2"}, string(project.ID))
	assert.ErrorIs(t, err, ErrProjectNoteNotFound)
}
