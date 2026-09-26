package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// SEM@72e97a5fe3efd8612113f9b4ff7dd27df233dd77: seed one metadata row for an entity (mutates DB)
func seedMetadata(t *testing.T, db *gorm.DB, entityType, entityID string) {
	t.Helper()
	require.NoError(t, db.Create(&models.Metadata{
		ID:         models.DBVarchar(uuid.New().String()),
		EntityType: models.DBVarchar(entityType),
		EntityID:   models.DBVarchar(entityID),
		Key:        "k",
		Value:      "v",
	}).Error)
}

// SEM@72e97a5fe3efd8612113f9b4ff7dd27df233dd77: count metadata rows for an entity (reads DB)
func countMetadata(t *testing.T, db *gorm.DB, entityType, entityID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.Model(&models.Metadata{}).
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).Count(&n).Error)
	return n
}

// SEM@72e97a5fe3efd8612113f9b4ff7dd27df233dd77: validate each sub-resource hard delete removes only its own metadata (#947)
func TestHardDelete_RemovesOwnMetadata(t *testing.T) {
	ctx := context.Background()
	// Each case creates an entity with the given ID and returns the store's hard delete.
	type harness func(t *testing.T) (db *gorm.DB, create func(uuid.UUID), hardDelete func(string) error)
	cases := map[string]harness{
		"threat": func(t *testing.T) (*gorm.DB, func(uuid.UUID), func(string) error) {
			db, tm := setupThreatAliasTestDB(t)
			repo := NewGormThreatRepository(db, nil, nil)
			tmUUID := uuid.MustParse(string(tm.ID))
			return db, func(id uuid.UUID) {
				require.NoError(t, repo.Create(ctx, &Threat{Id: &id, Name: "t", ThreatType: []string{"spoofing"}, ThreatModelId: &tmUUID}))
			}, func(id string) error { return repo.hardDeleteThreat(ctx, id) }
		},
		"asset": func(t *testing.T) (*gorm.DB, func(uuid.UUID), func(string) error) {
			db, _, tm := setupAssetTestDB(t)
			repo := NewGormAssetRepository(db, nil, nil)
			return db, func(id uuid.UUID) {
				require.NoError(t, repo.Create(ctx, &Asset{Id: &id, Name: "a", Type: AssetTypeSoftware}, string(tm.ID)))
			}, func(id string) error { return repo.hardDeleteAsset(ctx, id) }
		},
		"document": func(t *testing.T) (*gorm.DB, func(uuid.UUID), func(string) error) {
			db, _, tm := setupDocumentAliasTestDB(t)
			repo := NewGormDocumentRepository(db, nil, nil)
			return db, func(id uuid.UUID) {
				require.NoError(t, repo.Create(ctx, &Document{Id: &id, Name: "d", Uri: "https://example.com/d"}, string(tm.ID)))
			}, func(id string) error { return repo.hardDeleteDocument(ctx, id) }
		},
		"note": func(t *testing.T) (*gorm.DB, func(uuid.UUID), func(string) error) {
			db, _, tm := setupNoteTestDB(t)
			repo := NewGormNoteRepository(db, nil, nil)
			return db, func(id uuid.UUID) {
				require.NoError(t, repo.Create(ctx, &Note{Id: &id, Name: "n", Content: "c"}, string(tm.ID)))
			}, func(id string) error { return repo.hardDeleteNote(ctx, id) }
		},
		"repository": func(t *testing.T) (*gorm.DB, func(uuid.UUID), func(string) error) {
			db, _, tm := setupRepositoryTestDB(t)
			repo := NewGormRepositoryRepository(db, nil, nil)
			return db, func(id uuid.UUID) {
				require.NoError(t, repo.Create(ctx, &Repository{Id: &id, Uri: "https://github.com/example/r"}, string(tm.ID)))
			}, func(id string) error { return repo.hardDeleteRepository(ctx, id) }
		},
	}
	for entityType, setup := range cases {
		t.Run(entityType, func(t *testing.T) {
			db, create, hardDelete := setup(t)
			gone, kept := uuid.New(), uuid.New()
			create(gone)
			create(kept)
			seedMetadata(t, db, entityType, gone.String())
			seedMetadata(t, db, entityType, kept.String())

			require.NoError(t, hardDelete(gone.String()))

			assert.Zero(t, countMetadata(t, db, entityType, gone.String()))
			assert.Equal(t, int64(1), countMetadata(t, db, entityType, kept.String()))
		})
	}
}

// SEM@72e97a5fe3efd8612113f9b4ff7dd27df233dd77: validate a hard delete of a missing entity leaves unrelated metadata alone (#947)
func TestHardDelete_NotFoundKeepsMetadata(t *testing.T) {
	db, _ := setupThreatAliasTestDB(t)
	repo := NewGormThreatRepository(db, nil, nil)
	orphan := uuid.New().String()
	seedMetadata(t, db, "threat", orphan)

	require.Error(t, repo.hardDeleteThreat(context.Background(), orphan))
	assert.Equal(t, int64(1), countMetadata(t, db, "threat", orphan))
}

// SEM@72e97a5fe3efd8612113f9b4ff7dd27df233dd77: validate bulk create rejects client-supplied IDs with 400 for every sub-resource (#947)
func TestBulkCreate_RejectsClientID(t *testing.T) {
	id := uuid.New().String()
	threatR, _ := setupThreatSubResourceHandler()
	assetR, _ := setupAssetSubResourceHandler()
	documentR, _ := setupDocumentSubResourceHandler()
	repositoryR, _ := setupRepositorySubRerepositoryHandler()

	cases := []struct {
		name string
		r    http.Handler
		path string
		item map[string]any
	}{
		{"threats", threatR, "threats", map[string]any{"name": "t"}},
		{"assets", assetR, "assets", map[string]any{"name": "a", "type": "software"}},
		{"documents", documentR, "documents", map[string]any{"name": "d", "uri": "https://example.com/d"}},
		// The repository test router mounts the handler at .../repositorys/bulk.
		{"repositories", repositoryR, "repositorys", map[string]any{"uri": "https://github.com/example/r"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.item["id"] = id
			body, _ := json.Marshal([]map[string]any{tc.item})
			req := httptest.NewRequest("POST", "/threat_models/"+testUUID1+"/"+tc.path+"/bulk", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			tc.r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "Field 'id' is not allowed")
		})
	}
}
