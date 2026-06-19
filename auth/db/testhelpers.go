package db

import (
	"sync/atomic"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// testAliasCounter provides process-wide unique alias values for test threat models.
// The uniq_threat_models_alias index (added in #412) requires globally-unique aliases;
// this counter ensures each call to SeedThreatModel gets a distinct value.
var testAliasCounter atomic.Int32

// TestDB holds a test database connection and cleanup function
// SEM@02214aa5ba030b20069f011c02d8be00d1e8c0ff: test fixture holding an in-memory SQLite database and its cleanup function
type TestDB struct {
	DB      *gorm.DB
	Cleanup func()
}

// NewTestDB creates a new in-memory SQLite database for testing.
// It automatically migrates all models and returns a cleanup function.
// SEM@02214aa5ba030b20069f011c02d8be00d1e8c0ff: build an auto-migrated in-memory SQLite database for tests (mutates shared state)
func NewTestDB(t *testing.T) (*TestDB, error) {
	t.Helper()

	// Create in-memory SQLite database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	// Auto-migrate all models
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		return nil, err
	}

	cleanup := func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}

	return &TestDB{
		DB:      db,
		Cleanup: cleanup,
	}, nil
}

// MustCreateTestDB creates a test DB, failing the test on error.
// SEM@02214aa5ba030b20069f011c02d8be00d1e8c0ff: build a test database, failing the test immediately on error
func MustCreateTestDB(t *testing.T) *TestDB {
	t.Helper()

	tdb, err := NewTestDB(t)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	return tdb
}

// SeedUser creates a test user and returns it.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: insert a test user record into the test database, failing the test on error (mutates shared state)
func (tdb *TestDB) SeedUser(t *testing.T, email, provider string) *models.User {
	t.Helper()

	providerUserID := email // Use email as provider user ID for simplicity
	user := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       models.DBVarchar(provider),
		ProviderUserID: models.NewNullableDBVarchar(&providerUserID),
		Email:          models.DBVarchar(email),
		Name:           models.DBVarchar("Test User"),
		EmailVerified:  models.DBBool(true),
	}

	if err := tdb.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	return user
}

// SeedThreatModel creates a test threat model with the given owner and returns it.
// SEM@79a11352f14300d8a049192847b5c411d1d8166c: insert a test threat model owned by the given user into the test database (mutates shared state)
func (tdb *TestDB) SeedThreatModel(t *testing.T, ownerUUID, name string) *models.ThreatModel {
	t.Helper()

	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(uuid.New().String()),
		OwnerInternalUUID:     models.DBVarchar(ownerUUID),
		CreatedByInternalUUID: models.DBVarchar(ownerUUID),
		Name:                  models.DBVarchar(name),
		Description:           models.NullableDBText{String: "Test threat model", Valid: true},
		Alias:                 testAliasCounter.Add(1),
	}

	if err := tdb.DB.Create(tm).Error; err != nil {
		t.Fatalf("failed to seed threat model: %v", err)
	}

	return tm
}

// SeedGroup creates a test group and returns it.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: insert a test group record into the test database (mutates shared state)
func (tdb *TestDB) SeedGroup(t *testing.T, provider, groupName string) *models.Group {
	t.Helper()

	group := &models.Group{
		InternalUUID: models.DBVarchar(uuid.New().String()),
		Provider:     models.DBVarchar(provider),
		GroupName:    models.DBVarchar(groupName),
	}

	if err := tdb.DB.Create(group).Error; err != nil {
		t.Fatalf("failed to seed group: %v", err)
	}

	return group
}

// SeedThreatModelAccess creates a test threat model access record and returns it.
// SEM@ebf201816c3638ec74fc8483a2a649af3ccddfc9: insert a threat model access control record for a user or group into the test database (mutates shared state)
func (tdb *TestDB) SeedThreatModelAccess(t *testing.T, threatModelID string, userUUID *string, groupUUID *string, subjectType, role string) *models.ThreatModelAccess {
	t.Helper()

	access := &models.ThreatModelAccess{
		ID:                models.DBVarchar(uuid.New().String()),
		ThreatModelID:     models.DBVarchar(threatModelID),
		UserInternalUUID:  models.NewNullableDBVarchar(userUUID),
		GroupInternalUUID: models.NewNullableDBVarchar(groupUUID),
		SubjectType:       models.DBVarchar(subjectType),
		Role:              models.DBVarchar(role),
	}

	if err := tdb.DB.Create(access).Error; err != nil {
		t.Fatalf("failed to seed threat model access: %v", err)
	}

	return access
}

// SeedSurveyTemplate creates a test survey template and returns it.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: insert a test survey template into the test database (mutates shared state)
func (tdb *TestDB) SeedSurveyTemplate(t *testing.T, createdByUUID string) *models.SurveyTemplate {
	t.Helper()

	st := &models.SurveyTemplate{
		ID:                    models.DBVarchar(uuid.New().String()),
		Name:                  models.DBVarchar("Test Survey"),
		Version:               "v1",
		Status:                "active",
		CreatedByInternalUUID: models.DBVarchar(createdByUUID),
	}

	if err := tdb.DB.Create(st).Error; err != nil {
		t.Fatalf("failed to seed survey template: %v", err)
	}

	return st
}

// SeedSurveyResponse creates a test survey response and returns it.
// SEM@e530c9655ae71e6bf78a13b97320afcbd9b1e7b5: insert a test survey response into the test database (mutates shared state)
func (tdb *TestDB) SeedSurveyResponse(t *testing.T, templateID, ownerUUID string, isConfidential bool) *models.SurveyResponse {
	t.Helper()

	sr := &models.SurveyResponse{
		ID:                models.DBVarchar(uuid.New().String()),
		TemplateID:        models.DBVarchar(templateID),
		TemplateVersion:   "v1",
		Status:            "draft",
		IsConfidential:    models.DBBool(isConfidential),
		OwnerInternalUUID: models.NewNullableDBVarchar(&ownerUUID),
	}

	if err := tdb.DB.Create(sr).Error; err != nil {
		t.Fatalf("failed to seed survey response: %v", err)
	}

	return sr
}

// SeedSurveyResponseAccess creates a test survey response access record and returns it.
// SEM@ebf201816c3638ec74fc8483a2a649af3ccddfc9: insert a survey response access control record for a user or group into the test database (mutates shared state)
func (tdb *TestDB) SeedSurveyResponseAccess(t *testing.T, responseID string, userUUID *string, groupUUID *string, subjectType, role string) *models.SurveyResponseAccess {
	t.Helper()

	access := &models.SurveyResponseAccess{
		ID:                models.DBVarchar(responseID + "-" + uuid.New().String()[:8]),
		SurveyResponseID:  models.DBVarchar(responseID),
		UserInternalUUID:  models.NewNullableDBVarchar(userUUID),
		GroupInternalUUID: models.NewNullableDBVarchar(groupUUID),
		SubjectType:       models.DBVarchar(subjectType),
		Role:              models.DBVarchar(role),
	}

	if err := tdb.DB.Create(access).Error; err != nil {
		t.Fatalf("failed to seed survey response access: %v", err)
	}

	return access
}

// SeedTriageNote creates a test triage note and returns it.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: insert a test triage note linked to a survey response into the test database (mutates shared state)
func (tdb *TestDB) SeedTriageNote(t *testing.T, responseID, createdByUUID, modifiedByUUID string) *models.TriageNote {
	t.Helper()

	note := &models.TriageNote{
		SurveyResponseID:       models.DBVarchar(responseID),
		Name:                   models.DBVarchar("Test Note"),
		Content:                models.DBText("Test content"),
		CreatedByInternalUUID:  models.NewNullableDBVarchar(&createdByUUID),
		ModifiedByInternalUUID: models.NewNullableDBVarchar(&modifiedByUUID),
	}

	if err := tdb.DB.Create(note).Error; err != nil {
		t.Fatalf("failed to seed triage note: %v", err)
	}

	return note
}

// SeedClientCredential creates a test client credential and returns it.
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: insert a test client credential record into the test database (mutates shared state)
func (tdb *TestDB) SeedClientCredential(t *testing.T, ownerUUID, clientID, name string) *models.ClientCredential {
	t.Helper()

	cc := &models.ClientCredential{
		ID:               models.DBVarchar(uuid.New().String()),
		ClientID:         models.DBVarchar(clientID),
		ClientSecretHash: models.DBText("hashed_secret"),
		Name:             models.DBVarchar(name),
		OwnerUUID:        models.DBVarchar(ownerUUID),
		IsActive:         models.DBBool(true),
	}

	if err := tdb.DB.Create(cc).Error; err != nil {
		t.Fatalf("failed to seed client credential: %v", err)
	}

	return cc
}
