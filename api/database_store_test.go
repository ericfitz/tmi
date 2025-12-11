package api

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
)

// Test data helpers

func createTestThreatModelDB() ThreatModel {
	id := uuid.New()
	metadata := []Metadata{
		{Key: "priority", Value: "high"},
		{Key: "status", Value: "active"},
	}
	threats := []Threat{
		{
			Id:            &id,
			Name:          "SQL Injection",
			Description:   strPtr("Database attack"),
			Severity:      strPtr("High"),
			ThreatModelId: &id,
			Priority:      strPtr("High"),
			Status:        strPtr("Open"),
			ThreatType:    []string{"Injection"},
			Mitigated:     boolPtr(false),
		},
	}
	diagrams := []Diagram{}

	testUser := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "test",
		ProviderId:    "test@example.com",
		DisplayName:   "Test User",
		Email:         openapi_types.Email("test@example.com"),
	}

	return ThreatModel{
		Id:                   &id,
		Name:                 "Test Threat Model",
		Description:          strPtr("Test description"),
		Owner:                testUser,
		CreatedBy:            &testUser,
		ThreatModelFramework: "STRIDE",
		IssueUri:             strPtr("https://github.com/test/issues/1"),
		CreatedAt:            func() *time.Time { t := time.Now(); return &t }(),
		ModifiedAt:           func() *time.Time { t := time.Now(); return &t }(),
		Authorization: []Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    "test@example.com",
				Role:          RoleOwner,
			},
		},
		Metadata: &metadata,
		Threats:  &threats,
		Diagrams: &diagrams,
	}
}

func createTestDiagramDB() DfdDiagram {
	id := uuid.New()
	// Create simple cells - since DfdDiagram_Cells_Item uses union, we'll create empty cells
	cells := []DfdDiagram_Cells_Item{{}}
	metadata := []Metadata{
		{Key: "priority", Value: "high"},
	}

	now := time.Now()
	return DfdDiagram{
		Id:         &id,
		Name:       "Test Diagram",
		Type:       DfdDiagramTypeDFD100,
		Cells:      cells,
		Metadata:   &metadata,
		CreatedAt:  &now,
		ModifiedAt: &now,
	}
}

// TestNewThreatModelDatabaseStore tests store creation
func TestNewThreatModelDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewThreatModelDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

// TestThreatModelDatabaseStore_Get tests threat model retrieval
func TestThreatModelDatabaseStore_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()
		testUUID := uuid.MustParse(testID)

		// Mock transaction begin
		mock.ExpectBegin()

		// Mock main threat model query - now includes owner_internal_uuid and created_by_internal_uuid
		ownerUUID := uuid.New()
		createdByUUID := uuid.New()
		rows := sqlmock.NewRows([]string{
			"id", "name", "description", "owner_internal_uuid", "created_by_internal_uuid",
			"threat_model_framework", "issue_uri", "status", "status_updated",
			"created_at", "modified_at",
		}).AddRow(
			testUUID, "Test Model", "Test Description", ownerUUID.String(), createdByUUID.String(),
			"STRIDE", "https://example.com/issue", "In progress", time.Now(),
			time.Now(), time.Now(),
		)
		mock.ExpectQuery("SELECT (.+) FROM threat_models").WithArgs(testID).WillReturnRows(rows)

		// Mock enrichUserPrincipal for owner
		ownerRows := sqlmock.NewRows([]string{"provider", "provider_user_id", "name", "email"}).
			AddRow("test", "owner@example.com", "Owner User", "owner@example.com")
		mock.ExpectQuery("SELECT (.+) FROM users WHERE internal_uuid").WithArgs(ownerUUID.String()).WillReturnRows(ownerRows)

		// Mock enrichUserPrincipal for created_by
		createdByRows := sqlmock.NewRows([]string{"provider", "provider_user_id", "name", "email"}).
			AddRow("test", "creator@example.com", "Creator User", "creator@example.com")
		mock.ExpectQuery("SELECT (.+) FROM users WHERE internal_uuid").WithArgs(createdByUUID.String()).WillReturnRows(createdByRows)

		// Mock authorization query (now uses same transaction)
		authUserUUID1 := uuid.New()
		authUserUUID2 := uuid.New()
		authRows := sqlmock.NewRows([]string{"user_internal_uuid", "group_internal_uuid", "subject_type", "role"}).
			AddRow(authUserUUID1.String(), nil, "user", "owner").
			AddRow(authUserUUID2.String(), nil, "user", "reader")
		mock.ExpectQuery("SELECT (.+) FROM threat_model_access").WithArgs(testID).WillReturnRows(authRows)

		// Mock enrichment for authorization users
		authUser1Rows := sqlmock.NewRows([]string{"provider", "provider_user_id", "name", "email"}).
			AddRow("test", "owner@example.com", "Owner User", "owner@example.com")
		mock.ExpectQuery("SELECT (.+) FROM users WHERE internal_uuid").WithArgs(authUserUUID1.String()).WillReturnRows(authUser1Rows)

		authUser2Rows := sqlmock.NewRows([]string{"provider", "provider_user_id", "name", "email"}).
			AddRow("test", "reader@example.com", "Reader User", "reader@example.com")
		mock.ExpectQuery("SELECT (.+) FROM users WHERE internal_uuid").WithArgs(authUserUUID2.String()).WillReturnRows(authUser2Rows)

		// Mock metadata query (called before tx.Commit but uses s.db.Query, not tx.Query)
		metaRows := sqlmock.NewRows([]string{"key", "value"}).
			AddRow("priority", "high").
			AddRow("status", "active")
		mock.ExpectQuery("SELECT (.+) FROM metadata").WithArgs(testID).WillReturnRows(metaRows)

		// Mock threats query (called before tx.Commit but uses s.db.Query, not tx.Query)
		threatRows := sqlmock.NewRows([]string{
			"id", "name", "description", "severity", "mitigation", "diagram_id", "cell_id", "asset_id",
			"priority", "mitigated", "status", "threat_type", "score", "issue_uri",
			"created_at", "modified_at",
		}).AddRow(
			uuid.New(), "SQL Injection", "Database attack", "high", "Use prepared statements", nil, nil, nil,
			"High", false, "Open", pq.StringArray{"Injection"}, nil, nil,
			time.Now(), time.Now(),
		)
		mock.ExpectQuery("SELECT (.+) FROM threats").WithArgs(testID).WillReturnRows(threatRows)

		// Mock diagrams query (called before tx.Commit but uses s.db.Query, not tx.Query)
		diagramRows := sqlmock.NewRows([]string{
			"id", "name", "type", "cells", "metadata", "created_at", "modified_at",
		})
		mock.ExpectQuery("SELECT (.+) FROM diagrams").WithArgs(testID).WillReturnRows(diagramRows)

		// Mock transaction commit (happens at end of Get function via defer)
		mock.ExpectCommit()

		result, err := store.Get(testID)

		assert.NoError(t, err)
		assert.Equal(t, "Test Model", result.Name)
		assert.Equal(t, "owner@example.com", result.Owner.ProviderId)
		assert.Equal(t, "STRIDE", result.ThreatModelFramework)
		assert.Len(t, result.Authorization, 2)
		if result.Metadata != nil {
			assert.Len(t, *result.Metadata, 2)
		}
		if result.Threats != nil {
			assert.Len(t, *result.Threats, 1)
		}
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		// Mock transaction begin
		mock.ExpectBegin()

		mock.ExpectQuery("SELECT (.+) FROM threat_models").
			WithArgs(testID).
			WillReturnError(sql.ErrNoRows)

		// Mock transaction rollback (called in defer)
		mock.ExpectRollback()

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		// Mock transaction begin
		mock.ExpectBegin()

		mock.ExpectQuery("SELECT (.+) FROM threat_models").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		// Mock transaction rollback (called in defer)
		mock.ExpectRollback()

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get threat model")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestThreatModelDatabaseStore_Create tests threat model creation
func TestThreatModelDatabaseStore_Create(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testModel := createTestThreatModelDB()

		// Mock transaction
		mock.ExpectBegin()

		// Mock INSERT query
		mock.ExpectExec("INSERT INTO threat_models").
			WithArgs(
				sqlmock.AnyArg(), testModel.Name, testModel.Description, testModel.Owner, testModel.CreatedBy,
				"STRIDE", testModel.IssueUri, testModel.CreatedAt, testModel.ModifiedAt,
				0, 0, 0, 0,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock authorization insert
		mock.ExpectExec("INSERT INTO threat_model_access").
			WithArgs(sqlmock.AnyArg(), "test@example.com", "owner", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock metadata insert (note the parameter order from the actual SQL)
		mock.ExpectExec("INSERT INTO metadata").
			WithArgs(sqlmock.AnyArg(), "priority", "high", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO metadata").
			WithArgs(sqlmock.AnyArg(), "status", "active", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		result, err := store.Create(testModel, func(tm ThreatModel, id string) ThreatModel {
			parsedID := uuid.MustParse(id)
			tm.Id = &parsedID
			return tm
		})

		assert.NoError(t, err)
		assert.NotNil(t, result.Id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("TransactionBeginError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testModel := createTestThreatModelDB()

		mock.ExpectBegin().WillReturnError(assert.AnError)

		_, err = store.Create(testModel, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to begin transaction")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("InsertError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testModel := createTestThreatModelDB()

		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO threat_models").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg()).
			WillReturnError(assert.AnError)
		mock.ExpectRollback()

		_, err = store.Create(testModel, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to insert threat model")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestThreatModelDatabaseStore_Update tests threat model updates
func TestThreatModelDatabaseStore_Update(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testModel := createTestThreatModelDB()
		testID := uuid.New().String()

		mock.ExpectBegin()

		// Mock SELECT query to get current status
		mock.ExpectQuery("SELECT status FROM threat_models WHERE id").
			WithArgs(testID).
			WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(pq.Array([]string{})))

		// Mock user lookup query for owner (resolveUserIdentifierToUUID)
		ownerUUID := uuid.New().String()
		mock.ExpectQuery("SELECT internal_uuid FROM users WHERE provider_user_id").
			WithArgs(testModel.Owner.ProviderId).
			WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}).AddRow(ownerUUID))

		// Mock user lookup query for created_by (resolveUserIdentifierToUUID)
		createdByUUID := uuid.New().String()
		mock.ExpectQuery("SELECT internal_uuid FROM users WHERE provider_user_id").
			WithArgs(testModel.CreatedBy.ProviderId).
			WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}).AddRow(createdByUUID))

		// Mock UPDATE query
		mock.ExpectExec("UPDATE threat_models").
			WithArgs(
				testID, testModel.Name, testModel.Description, ownerUUID, createdByUUID,
				"STRIDE", testModel.IssueUri, sqlmock.AnyArg(), sqlmock.AnyArg(), testModel.ModifiedAt,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// Mock authorization delete and insert
		mock.ExpectExec("DELETE FROM threat_model_access").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		// Mock user lookup for authorization subject (will also resolve)
		authUserUUID := uuid.New().String()
		mock.ExpectQuery("SELECT internal_uuid FROM users WHERE provider_user_id").
			WithArgs("test@example.com").
			WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}).AddRow(authUserUUID))

		mock.ExpectExec("INSERT INTO threat_model_access").
			WithArgs(testID, authUserUUID, nil, "user", "owner", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock metadata delete and insert
		mock.ExpectExec("DELETE FROM metadata").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO metadata").
			WithArgs(sqlmock.AnyArg(), testID, "priority", "high", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO metadata").
			WithArgs(sqlmock.AnyArg(), testID, "status", "active", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		mock.ExpectCommit()

		err = store.Update(testID, testModel)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testModel := createTestThreatModelDB()
		testID := uuid.New().String()

		mock.ExpectBegin()
		// Mock SELECT query to get current status
		mock.ExpectQuery("SELECT status FROM threat_models WHERE id").
			WithArgs(testID).
			WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(pq.Array([]string{})))

		// Mock user lookup query for owner
		ownerUUID := uuid.New().String()
		mock.ExpectQuery("SELECT internal_uuid FROM users WHERE provider_user_id").
			WithArgs(testModel.Owner.ProviderId).
			WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}).AddRow(ownerUUID))

		// Mock user lookup query for created_by
		createdByUUID := uuid.New().String()
		mock.ExpectQuery("SELECT internal_uuid FROM users WHERE provider_user_id").
			WithArgs(testModel.CreatedBy.ProviderId).
			WillReturnRows(sqlmock.NewRows([]string{"internal_uuid"}).AddRow(createdByUUID))

		mock.ExpectExec("UPDATE threat_models").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
									sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected
		mock.ExpectRollback()

		err = store.Update(testID, testModel)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestThreatModelDatabaseStore_Delete tests threat model deletion
func TestThreatModelDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM threat_models").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM threat_models").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM threat_models").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete threat model")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestThreatModelDatabaseStore_Count tests threat model counting
func TestThreatModelDatabaseStore_Count(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)

		rows := sqlmock.NewRows([]string{"count"}).AddRow(5)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM threat_models").WillReturnRows(rows)

		count := store.Count()

		assert.Equal(t, 5, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM threat_models").WillReturnError(assert.AnError)

		count := store.Count()

		assert.Equal(t, 0, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestDiagramDatabaseStore tests diagram database operations
func TestNewDiagramDatabaseStore(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	store := NewDiagramDatabaseStore(db)

	assert.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestDiagramDatabaseStore_Get(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testID := uuid.New().String()
		testUUID := uuid.MustParse(testID)
		threatModelUUID := uuid.New()

		// Mock cells JSON
		cellsJSON, _ := json.Marshal([]DfdDiagram_Cells_Item{{}})

		// Mock diagram query
		rows := sqlmock.NewRows([]string{
			"id", "threat_model_id", "name", "type", "cells", "svg_image", "image_update_vector", "update_vector", "created_at", "modified_at",
		}).AddRow(
			testUUID, threatModelUUID, "Test Diagram", "DFD-1.0.0",
			cellsJSON, nil, nil, int64(1), time.Now(), time.Now(),
		)
		mock.ExpectQuery("SELECT (.+) FROM diagrams").WithArgs(testID).WillReturnRows(rows)

		// Mock metadata query
		metadataRows := sqlmock.NewRows([]string{"key", "value"}).
			AddRow("priority", "high")
		mock.ExpectQuery("SELECT key, value FROM metadata").
			WithArgs("diagram", testID).
			WillReturnRows(metadataRows)

		result, err := store.Get(testID)

		assert.NoError(t, err)
		assert.Equal(t, "Test Diagram", result.Name)
		assert.Equal(t, DfdDiagramTypeDFD100, result.Type)
		assert.Len(t, result.Cells, 1)
		assert.Len(t, *result.Metadata, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM diagrams").
			WithArgs(testID).
			WillReturnError(sql.ErrNoRows)

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDiagramDatabaseStore_CreateWithThreatModel(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testDiagram := createTestDiagramDB()
		threatModelID := uuid.New().String()

		mock.ExpectExec("INSERT INTO diagrams").
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), testDiagram.Name, "DFD-1.0.0",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), testDiagram.CreatedAt, testDiagram.ModifiedAt,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		result, err := store.CreateWithThreatModel(testDiagram, threatModelID, func(d DfdDiagram, id string) DfdDiagram {
			parsedID := uuid.MustParse(id)
			d.Id = &parsedID
			return d
		})

		assert.NoError(t, err)
		assert.NotNil(t, result.Id)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("InvalidThreatModelID", func(t *testing.T) {
		db, _, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testDiagram := createTestDiagramDB()
		invalidID := "invalid-uuid"

		_, err = store.CreateWithThreatModel(testDiagram, invalidID, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid threat model ID format")
	})
}

func TestDiagramDatabaseStore_Update(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testDiagram := createTestDiagramDB()
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE diagrams").
			WithArgs(
				testID, testDiagram.Name, "DFD-1.0.0",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), testDiagram.ModifiedAt,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Update(testID, testDiagram)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testDiagram := createTestDiagramDB()
		testID := uuid.New().String()

		mock.ExpectExec("UPDATE diagrams").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
									sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected

		err = store.Update(testID, testDiagram)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDiagramDatabaseStore_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM diagrams").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.Delete(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectExec("DELETE FROM diagrams").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err = store.Delete(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDiagramDatabaseStore_Count(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)

		rows := sqlmock.NewRows([]string{"count"}).AddRow(3)
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM diagrams").WillReturnRows(rows)

		count := store.Count()

		assert.Equal(t, 3, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("DatabaseError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewDiagramDatabaseStore(db)

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM diagrams").WillReturnError(assert.AnError)

		count := store.Count()

		assert.Equal(t, 0, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestUtilityFunctions tests utility functions

// MockError implements error interface for testing
type MockError struct {
	message string
}

func (e *MockError) Error() string {
	return e.message
}

// Test driver.Valuer interface for complex types (used in database operations)
func TestDatabaseSerialization(t *testing.T) {
	t.Run("JSONSerialization", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "priority", Value: "high"},
			{Key: "status", Value: "active"},
		}

		data, err := json.Marshal(metadata)
		assert.NoError(t, err)
		assert.Contains(t, string(data), "priority")

		var unmarshaled []Metadata
		err = json.Unmarshal(data, &unmarshaled)
		assert.NoError(t, err)
		assert.Len(t, unmarshaled, 2)
		assert.Equal(t, "priority", unmarshaled[0].Key)
	})
}
