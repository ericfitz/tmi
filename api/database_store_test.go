package api

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
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
			Severity:      High,
			ThreatModelId: id,
		},
	}
	diagrams := []Diagram{}

	return ThreatModel{
		Id:                   &id,
		Name:                 "Test Threat Model",
		Description:          strPtr("Test description"),
		Owner:                "test@example.com",
		CreatedBy:            "test@example.com",
		ThreatModelFramework: ThreatModelThreatModelFrameworkSTRIDE,
		IssueUrl:             strPtr("https://github.com/test/issues/1"),
		CreatedAt:            time.Now(),
		ModifiedAt:           time.Now(),
		Authorization: []Authorization{
			{Subject: "test@example.com", Role: RoleOwner},
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

	return DfdDiagram{
		Id:          &id,
		Name:        "Test Diagram",
		Description: strPtr("Test diagram description"),
		Type:        DfdDiagramTypeDFD100,
		Cells:       cells,
		Metadata:    &metadata,
		CreatedAt:   time.Now(),
		ModifiedAt:  time.Now(),
	}
}

// Helper function for string pointers
func strPtr(s string) *string {
	return &s
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

		// Mock main threat model query
		rows := sqlmock.NewRows([]string{
			"id", "name", "description", "owner_email", "created_by",
			"threat_model_framework", "issue_url", "created_at", "updated_at",
			"document_count", "source_count", "diagram_count", "threat_count",
		}).AddRow(
			testUUID, "Test Model", "Test Description", "owner@example.com", "creator@example.com",
			"STRIDE", "https://example.com/issue", time.Now(), time.Now(),
			0, 0, 0, 1,
		)
		mock.ExpectQuery("SELECT (.+) FROM threat_models").WithArgs(testID).WillReturnRows(rows)

		// Mock authorization query
		authRows := sqlmock.NewRows([]string{"user_email", "role"}).
			AddRow("owner@example.com", "owner").
			AddRow("reader@example.com", "reader")
		mock.ExpectQuery("SELECT (.+) FROM threat_model_access").WithArgs(testID).WillReturnRows(authRows)

		// Mock metadata query
		metaRows := sqlmock.NewRows([]string{"key", "value"}).
			AddRow("priority", "high").
			AddRow("status", "active")
		mock.ExpectQuery("SELECT (.+) FROM metadata").WithArgs(testID).WillReturnRows(metaRows)

		// Mock threats query
		threatRows := sqlmock.NewRows([]string{
			"id", "name", "description", "severity", "mitigation", "created_at", "updated_at",
		}).AddRow(
			uuid.New(), "SQL Injection", "Database attack", "high", "Use prepared statements",
			time.Now(), time.Now(),
		)
		mock.ExpectQuery("SELECT (.+) FROM threats").WithArgs(testID).WillReturnRows(threatRows)

		// Mock diagrams query
		diagramRows := sqlmock.NewRows([]string{
			"id", "name", "type", "content", "cells", "metadata", "created_at", "updated_at",
		})
		mock.ExpectQuery("SELECT (.+) FROM diagrams").WithArgs(testID).WillReturnRows(diagramRows)

		result, err := store.Get(testID)

		assert.NoError(t, err)
		assert.Equal(t, "Test Model", result.Name)
		assert.Equal(t, "owner@example.com", result.Owner)
		assert.Equal(t, ThreatModelThreatModelFrameworkSTRIDE, result.ThreatModelFramework)
		assert.Len(t, result.Authorization, 2)
		assert.Len(t, *result.Metadata, 2)
		assert.Len(t, *result.Threats, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("NotFound", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectQuery("SELECT (.+) FROM threat_models").
			WithArgs(testID).
			WillReturnError(sql.ErrNoRows)

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

		mock.ExpectQuery("SELECT (.+) FROM threat_models").
			WithArgs(testID).
			WillReturnError(assert.AnError)

		_, err = store.Get(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get threat model")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestThreatModelDatabaseStore_Create tests threat model creation
func TestThreatModelDatabaseStore_Create(t *testing.T) {
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
				"STRIDE", testModel.IssueUrl, testModel.CreatedAt, testModel.ModifiedAt,
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

		// Mock UPDATE query
		mock.ExpectExec("UPDATE threat_models").
			WithArgs(
				testID, testModel.Name, testModel.Description, testModel.Owner, testModel.CreatedBy,
				"STRIDE", testModel.IssueUrl, testModel.ModifiedAt,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		// Mock authorization delete and insert
		mock.ExpectExec("DELETE FROM threat_model_access").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO threat_model_access").
			WithArgs(testID, "test@example.com", "owner", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// Mock metadata delete and insert
		mock.ExpectExec("DELETE FROM metadata").
			WithArgs(testID).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO metadata").
			WithArgs(testID, "priority", "high", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO metadata").
			WithArgs(testID, "status", "active", sqlmock.AnyArg(), sqlmock.AnyArg()).
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
		mock.ExpectExec("UPDATE threat_models").
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
									sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
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

// TestThreatModelDatabaseStore_UpdateCounts tests count field updates
func TestThreatModelDatabaseStore_UpdateCounts(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		// Mock count queries
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM documents").
			WithArgs(testID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM sources").
			WithArgs(testID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM diagrams").
			WithArgs(testID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM threats").
			WithArgs(testID).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

		// Mock update query
		mock.ExpectExec("UPDATE threat_models SET (.+) WHERE id = \\$1").
			WithArgs(testID, 2, 3, 1, 5).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err = store.UpdateCounts(testID)

		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("CountError", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)
		testID := uuid.New().String()

		mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM documents").
			WithArgs(testID).WillReturnError(assert.AnError)

		err = store.UpdateCounts(testID)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to count documents")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestThreatModelDatabaseStore_CountSubEntitiesFromPayload tests payload counting
func TestThreatModelDatabaseStore_CountSubEntitiesFromPayload(t *testing.T) {
	t.Run("WithAllEntities", func(t *testing.T) {
		db, _, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)

		documents := []Document{{}}
		sources := []Source{{}, {}}
		diagrams := []Diagram{{}, {}, {}}
		threats := []Threat{{}, {}, {}, {}}

		tm := ThreatModel{
			Documents:  &documents,
			SourceCode: &sources,
			Diagrams:   &diagrams,
			Threats:    &threats,
		}

		docCount, srcCount, diagCount, threatCount := store.CountSubEntitiesFromPayload(tm)

		assert.Equal(t, 1, docCount)
		assert.Equal(t, 2, srcCount)
		assert.Equal(t, 3, diagCount)
		assert.Equal(t, 4, threatCount)
	})

	t.Run("WithNilEntities", func(t *testing.T) {
		db, _, err := sqlmock.New()
		assert.NoError(t, err)
		defer func() { _ = db.Close() }()

		store := NewThreatModelDatabaseStore(db)

		tm := ThreatModel{
			Documents:  nil,
			SourceCode: nil,
			Diagrams:   nil,
			Threats:    nil,
		}

		docCount, srcCount, diagCount, threatCount := store.CountSubEntitiesFromPayload(tm)

		assert.Equal(t, 0, docCount)
		assert.Equal(t, 0, srcCount)
		assert.Equal(t, 0, diagCount)
		assert.Equal(t, 0, threatCount)
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

		// Mock cells and metadata JSON
		cellsJSON, _ := json.Marshal([]DfdDiagram_Cells_Item{{}})
		metadataJSON, _ := json.Marshal([]Metadata{{Key: "priority", Value: "high"}})

		rows := sqlmock.NewRows([]string{
			"id", "threat_model_id", "name", "type", "content", "cells", "metadata", "created_at", "updated_at",
		}).AddRow(
			testUUID, threatModelUUID, "Test Diagram", "DFD-1.0.0", "Test description",
			cellsJSON, metadataJSON, time.Now(), time.Now(),
		)
		mock.ExpectQuery("SELECT (.+) FROM diagrams").WithArgs(testID).WillReturnRows(rows)

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
				sqlmock.AnyArg(), sqlmock.AnyArg(), testDiagram.Name, "DFD-1.0.0", testDiagram.Description,
				sqlmock.AnyArg(), sqlmock.AnyArg(), testDiagram.CreatedAt, testDiagram.ModifiedAt,
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
				testID, testDiagram.Name, "DFD-1.0.0", testDiagram.Description,
				sqlmock.AnyArg(), sqlmock.AnyArg(), testDiagram.ModifiedAt,
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
									sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
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
func TestIsConstraintViolation(t *testing.T) {
	t.Run("ConstraintViolationError", func(t *testing.T) {
		err := &MockError{message: "check constraint violated"}
		result := isConstraintViolation(err)
		assert.True(t, result)
	})

	t.Run("RegularError", func(t *testing.T) {
		err := &MockError{message: "connection refused"}
		result := isConstraintViolation(err)
		assert.False(t, result)
	})

	t.Run("NilError", func(t *testing.T) {
		result := isConstraintViolation(nil)
		assert.False(t, result)
	})
}

func TestContainsAny(t *testing.T) {
	t.Run("ContainsSubstring", func(t *testing.T) {
		result := containsAny("this is a test string", []string{"test", "example"})
		assert.True(t, result)
	})

	t.Run("DoesNotContainSubstring", func(t *testing.T) {
		result := containsAny("this is a sample string", []string{"test", "example"})
		assert.False(t, result)
	})

	t.Run("EmptySubstrings", func(t *testing.T) {
		result := containsAny("this is a test string", []string{})
		assert.False(t, result)
	})
}

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
