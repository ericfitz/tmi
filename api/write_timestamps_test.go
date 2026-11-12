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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// #1155: every write response must carry the timestamps the database stores,
// at microsecond resolution. The DB below uses a NowFunc that adds 761ns, so
// any path that echoes a GORM-generated or untruncated value fails on every OS.

func setupWriteTimestampDB(t *testing.T) (*gorm.DB, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
		NowFunc:                                  func() time.Time { return time.Now().UTC().Add(761 * time.Nanosecond) },
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{}, &models.Group{}, &models.ThreatModel{}, &models.ThreatModelAccess{},
		&models.Diagram{}, &models.Threat{}, &models.Asset{}, &models.Document{}, &models.Note{},
		&models.Repository{}, &models.Metadata{}, &models.AliasCounter{},
		&models.TeamRecord{}, &models.ProjectRecord{}, &models.TeamNoteRecord{}, &models.ProjectNoteRecord{},
		&models.Addon{}, &models.ContentFeedback{}, &models.UsabilityFeedback{},
		&models.TimmySession{}, &models.TimmyMessage{}, &models.SystemSetting{},
	))
	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(strPtr("write-ts-user")),
		Email:          models.DBVarchar("alice@example.com"),
		Name:           models.DBVarchar("Alice"),
	}
	require.NoError(t, db.Create(user).Error)
	return db, user
}

// storedTime reads one timestamp column of one row.
func storedTime(t *testing.T, db *gorm.DB, model any, keyCol, key, col string) time.Time {
	t.Helper()
	var row map[string]any
	require.NoError(t, db.Model(model).Select(col).Where(keyCol+" = ?", key).Take(&row).Error)
	v, ok := row[col].(time.Time)
	require.True(t, ok, "column %s is %T", col, row[col])
	return v
}

// assertTime checks got equals the stored value and has no sub-microsecond part.
func assertTime(t *testing.T, name string, got, stored time.Time) {
	t.Helper()
	assert.True(t, stored.Equal(got), "%s: response %v, stored %v", name, got, stored)
	assert.Zero(t, got.Nanosecond()%1000, "%s must carry microsecond resolution", name)
}

func TestWriteTimestamps_AddonCreate(t *testing.T) {
	t.Run("handler sets a UTC microsecond CreatedAt", func(t *testing.T) {
		original := GlobalAddonStore
		originalAdmin := GlobalGroupMemberRepository
		defer restoreAddonStores(original, originalAdmin)
		mockStore := &MockAddonStore{}
		r := setupAddonHandlerTest(mockStore, true)
		var captured *Addon
		mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Addon")).
			Run(func(args mock.Arguments) { captured = args.Get(1).(*Addon) }).Return(nil)

		body, _ := json.Marshal(map[string]any{"name": "Scanner", "webhook_id": uuid.New().String()})
		req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
		require.NotNil(t, captured)
		assert.Zero(t, captured.CreatedAt.Nanosecond()%1000)
		assert.Equal(t, time.UTC, captured.CreatedAt.Location())
	})

	t.Run("store returns the stored value", func(t *testing.T) {
		db, _ := setupWriteTimestampDB(t)
		store := NewGormAddonStore(db)
		addon := &Addon{Name: "Scanner", WebhookID: uuid.New()}
		require.NoError(t, store.Create(context.Background(), addon))
		assertTime(t, "created_at", addon.CreatedAt, storedTime(t, db, &models.Addon{}, "id", addon.ID.String(), "created_at"))
	})
}

func TestWriteTimestamps_Notes(t *testing.T) {
	ctx := context.Background()

	t.Run("team note create update patch", func(t *testing.T) {
		db, user := setupWriteTimestampDB(t)
		team := &models.TeamRecord{ID: models.DBVarchar(uuid.New().String()), Name: "T", CreatedByInternalUUID: user.InternalUUID}
		require.NoError(t, db.Create(team).Error)
		store := NewGormTeamNoteStore(db)

		created, err := store.Create(ctx, &TeamNote{Name: "n", Content: "c"}, string(team.ID))
		require.NoError(t, err)
		id := created.Id.String()
		assertStoredTimes(t, db, &models.TeamNoteRecord{}, id, created.CreatedAt, created.ModifiedAt)

		backdate(t, db, &models.TeamNoteRecord{}, id, tsOld)
		updated, err := store.Update(ctx, id, &TeamNote{Name: "n2", Content: "c2"}, string(team.ID))
		require.NoError(t, err)
		assertStoredTimes(t, db, &models.TeamNoteRecord{}, id, updated.CreatedAt, updated.ModifiedAt)
		assert.True(t, updated.ModifiedAt.After(tsOld))
		assert.Equal(t, "n2", storedName(t, db, &models.TeamNoteRecord{}, id))

		backdate(t, db, &models.TeamNoteRecord{}, id, tsOld)
		patched, err := store.Patch(ctx, id, []PatchOperation{{Op: "replace", Path: "/name", Value: "n3"}}, nil)
		require.NoError(t, err)
		assertStoredTimes(t, db, &models.TeamNoteRecord{}, id, patched.CreatedAt, patched.ModifiedAt)
		assert.True(t, patched.ModifiedAt.After(tsOld))
	})

	t.Run("project note create update patch", func(t *testing.T) {
		db, user := setupWriteTimestampDB(t)
		project := &models.ProjectRecord{ID: models.DBVarchar(uuid.New().String()), Name: "P", TeamID: models.DBVarchar(uuid.New().String()), CreatedByInternalUUID: user.InternalUUID}
		require.NoError(t, db.Create(project).Error)
		store := NewGormProjectNoteStore(db)

		created, err := store.Create(ctx, &ProjectNote{Name: "n", Content: "c"}, string(project.ID))
		require.NoError(t, err)
		id := created.Id.String()
		assertStoredTimes(t, db, &models.ProjectNoteRecord{}, id, created.CreatedAt, created.ModifiedAt)

		backdate(t, db, &models.ProjectNoteRecord{}, id, tsOld)
		updated, err := store.Update(ctx, id, &ProjectNote{Name: "n2", Content: "c2"}, string(project.ID))
		require.NoError(t, err)
		assertStoredTimes(t, db, &models.ProjectNoteRecord{}, id, updated.CreatedAt, updated.ModifiedAt)
		assert.True(t, updated.ModifiedAt.After(tsOld))

		backdate(t, db, &models.ProjectNoteRecord{}, id, tsOld)
		patched, err := store.Patch(ctx, id, []PatchOperation{{Op: "replace", Path: "/name", Value: "n3"}}, nil)
		require.NoError(t, err)
		assertStoredTimes(t, db, &models.ProjectNoteRecord{}, id, patched.CreatedAt, patched.ModifiedAt)
		assert.True(t, patched.ModifiedAt.After(tsOld))
	})
}

func storedName(t *testing.T, db *gorm.DB, model any, id string) string {
	t.Helper()
	var row map[string]any
	require.NoError(t, db.Model(model).Select("name").Where("id = ?", id).Take(&row).Error)
	s, _ := row["name"].(string)
	return s
}

func TestWriteTimestamps_Timmy(t *testing.T) {
	ctx := context.Background()
	db, user := setupWriteTimestampDB(t)
	sessions := NewGormTimmySessionStore(db)
	messages := NewGormTimmyMessageStore(db)

	session := &models.TimmySession{ThreatModelID: models.DBVarchar(uuid.New().String()), UserID: user.InternalUUID, Title: "t"}
	require.NoError(t, sessions.Create(ctx, session))
	assertStoredTimes(t, db, &models.TimmySession{}, string(session.ID), &session.CreatedAt, &session.ModifiedAt)

	msg := &models.TimmyMessage{SessionID: session.ID, Role: "user", Content: "hi", Sequence: 1}
	require.NoError(t, messages.Create(ctx, msg))
	assertTime(t, "message created_at", msg.CreatedAt, storedTime(t, db, &models.TimmyMessage{}, "id", string(msg.ID), "created_at"))
}

func TestWriteTimestamps_Feedback(t *testing.T) {
	ctx := context.Background()
	db, user := setupWriteTimestampDB(t)

	content := &models.ContentFeedback{
		ThreatModelID: models.DBVarchar(uuid.New().String()), TargetType: "note", TargetID: models.DBVarchar(uuid.New().String()),
		Sentiment: "up", ClientID: "c", CreatedBy: user.InternalUUID,
	}
	require.NoError(t, NewGormContentFeedbackRepository(db).Create(ctx, content))
	assertTime(t, "content feedback created_at", content.CreatedAt, storedTime(t, db, &models.ContentFeedback{}, "id", string(content.ID), "created_at"))

	usability := &models.UsabilityFeedback{Sentiment: "up", Surface: "s", ClientID: "c", CreatedBy: user.InternalUUID}
	require.NoError(t, NewGormUsabilityFeedbackRepository(db).Create(ctx, usability))
	assertTime(t, "usability feedback created_at", usability.CreatedAt, storedTime(t, db, &models.UsabilityFeedback{}, "id", string(usability.ID), "created_at"))
}

func TestWriteTimestamps_SettingSet(t *testing.T) {
	db, _ := setupWriteTimestampDB(t)
	service := NewSettingsService(db, nil)
	setting := &models.SystemSetting{SettingKey: "ts.test", Value: "v", SettingType: "string"}
	require.NoError(t, service.Set(context.Background(), setting))
	assertTime(t, "modified_at", setting.ModifiedAt, storedTime(t, db, &models.SystemSetting{}, "setting_key", "ts.test", "modified_at"))

	// Second run: an upsert over the existing row.
	setting.Value = "v2"
	require.NoError(t, service.Set(context.Background(), setting))
	assertTime(t, "modified_at (update)", setting.ModifiedAt, storedTime(t, db, &models.SystemSetting{}, "setting_key", "ts.test", "modified_at"))
}

func newWriteTimestampThreatModel(user *models.User) ThreatModel {
	providerID := user.ProviderUserID.String
	empty := []Authorization{}
	return ThreatModel{
		Name:          "TM",
		Owner:         User{PrincipalType: UserPrincipalTypeUser, Provider: string(user.Provider), ProviderId: providerID},
		CreatedBy:     &User{PrincipalType: UserPrincipalTypeUser, Provider: string(user.Provider), ProviderId: providerID},
		Authorization: &empty,
	}
}

func tmIDSetter(item ThreatModel, id string) ThreatModel {
	uid, _ := uuid.Parse(id)
	item.Id = &uid
	return item
}

func TestWriteTimestamps_ThreatModel(t *testing.T) {
	ctx := context.Background()
	db, user := setupWriteTimestampDB(t)
	store := NewGormThreatModelStore(db)

	created, err := store.Create(ctx, newWriteTimestampThreatModel(user), tmIDSetter)
	require.NoError(t, err)
	id := created.Id.String()
	require.NotNil(t, created.StatusUpdated)
	assertTime(t, "status_updated", *created.StatusUpdated, storedTime(t, db, &models.ThreatModel{}, "id", id, "status_updated"))

	// Update with the handler's explicit microsecond ModifiedAt: the row must
	// hold exactly that value, not a second instant from autoUpdateTime.
	backdate(t, db, &models.ThreatModel{}, id, tsOld)
	update := newWriteTimestampThreatModel(user)
	update.Id = created.Id
	modifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	update.ModifiedAt = &modifiedAt
	require.NoError(t, store.Update(ctx, id, update))
	assertTime(t, "modified_at (update)", modifiedAt, storedTime(t, db, &models.ThreatModel{}, "id", id, "modified_at"))

	// Update with a status change bumps status_updated at microsecond resolution.
	status := "In Review"
	update.Status = &status
	require.NoError(t, store.Update(ctx, id, update))
	statusUpdated := storedTime(t, db, &models.ThreatModel{}, "id", id, "status_updated")
	assert.Zero(t, statusUpdated.Nanosecond()%1000, "status_updated must carry microsecond resolution")
	assert.True(t, statusUpdated.After(*created.StatusUpdated), "status_updated must advance when the status changes")
}

func TestWriteTimestamps_Diagram(t *testing.T) {
	ctx := context.Background()
	db, user := setupWriteTimestampDB(t)
	tmStore := NewGormThreatModelStore(db)
	diagramStore := NewGormDiagramStore(db)
	tm, err := tmStore.Create(ctx, newWriteTimestampThreatModel(user), tmIDSetter)
	require.NoError(t, err)
	tmID := tm.Id.String()
	idSetter := func(d DfdDiagram, id string) DfdDiagram {
		u, _ := uuid.Parse(id)
		d.Id = &u
		return d
	}

	t.Run("handler create returns stored timestamps", func(t *testing.T) {
		origTM, origDiagram := ThreatModelStore, DiagramStore
		defer func() { ThreatModelStore, DiagramStore = origTM, origDiagram }()
		ThreatModelStore, DiagramStore = tmStore, diagramStore

		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("userEmail", string(user.Email))
			c.Set("userID", user.ProviderUserID.String)
			c.Set("userProvider", string(user.Provider))
			c.Set("userInternalUUID", string(user.InternalUUID))
			c.Next()
		})
		h := NewThreatModelDiagramHandler(NewWebSocketHubForTests())
		r.POST("/threat_models/:id/diagrams", func(c *gin.Context) { h.CreateDiagram(c, c.Param("id")) })

		body, _ := json.Marshal(map[string]any{"name": "d", "type": "DFD-1.0.0"})
		req := httptest.NewRequest("POST", "/threat_models/"+tmID+"/diagrams", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

		var got DfdDiagram
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assertStoredTimes(t, db, &models.Diagram{}, got.Id.String(), got.CreatedAt, got.ModifiedAt)
	})

	t.Run("update stores the caller's ModifiedAt", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		d := DfdDiagram{Name: "d2", Type: DfdDiagramTypeDFD100, Cells: []DfdDiagram_Cells_Item{}, CreatedAt: &now, ModifiedAt: &now}
		created, err := diagramStore.CreateWithThreatModel(d, tmID, idSetter)
		require.NoError(t, err)
		id := created.Id.String()
		assertStoredTimes(t, db, &models.Diagram{}, id, created.CreatedAt, created.ModifiedAt)

		for _, withVersion := range []bool{false, true} {
			backdate(t, db, &models.Diagram{}, id, tsOld)
			updated := created
			updated.Name = "d3"
			updated = *UpdateTimestamps(&updated, false)
			if withVersion {
				_, err = diagramStore.UpdateWithVersion(ctx, id, updated, 1)
			} else {
				err = diagramStore.Update(ctx, id, updated)
			}
			require.NoError(t, err)
			assertTime(t, "modified_at", *updated.ModifiedAt, storedTime(t, db, &models.Diagram{}, "id", id, "modified_at"))
			created = updated
		}
	})
}
