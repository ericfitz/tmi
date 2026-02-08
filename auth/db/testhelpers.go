package db

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDB holds a test database connection and cleanup function
type TestDB struct {
	DB      *gorm.DB
	Cleanup func()
}

// NewTestDB creates a new in-memory SQLite database for testing.
// It automatically migrates all models and returns a cleanup function.
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
func MustCreateTestDB(t *testing.T) *TestDB {
	t.Helper()

	tdb, err := NewTestDB(t)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	return tdb
}

// SeedUser creates a test user and returns it.
func (tdb *TestDB) SeedUser(t *testing.T, email, provider string) *models.User {
	t.Helper()

	providerUserID := email // Use email as provider user ID for simplicity
	user := &models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       provider,
		ProviderUserID: &providerUserID,
		Email:          email,
		Name:           "Test User",
		EmailVerified:  models.OracleBool(true),
	}

	if err := tdb.DB.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	return user
}

// SeedThreatModel creates a test threat model with the given owner and returns it.
func (tdb *TestDB) SeedThreatModel(t *testing.T, ownerUUID, name string) *models.ThreatModel {
	t.Helper()

	description := "Test threat model"
	tm := &models.ThreatModel{
		ID:                    uuid.New().String(),
		OwnerInternalUUID:     ownerUUID,
		CreatedByInternalUUID: ownerUUID,
		Name:                  name,
		Description:           &description,
	}

	if err := tdb.DB.Create(tm).Error; err != nil {
		t.Fatalf("failed to seed threat model: %v", err)
	}

	return tm
}

// SeedGroup creates a test group and returns it.
func (tdb *TestDB) SeedGroup(t *testing.T, provider, groupName string) *models.Group {
	t.Helper()

	group := &models.Group{
		InternalUUID: uuid.New().String(),
		Provider:     provider,
		GroupName:    groupName,
	}

	if err := tdb.DB.Create(group).Error; err != nil {
		t.Fatalf("failed to seed group: %v", err)
	}

	return group
}

// SeedThreatModelAccess creates a test threat model access record and returns it.
func (tdb *TestDB) SeedThreatModelAccess(t *testing.T, threatModelID string, userUUID *string, groupUUID *string, subjectType, role string) *models.ThreatModelAccess {
	t.Helper()

	access := &models.ThreatModelAccess{
		ID:                uuid.New().String(),
		ThreatModelID:     threatModelID,
		UserInternalUUID:  userUUID,
		GroupInternalUUID: groupUUID,
		SubjectType:       subjectType,
		Role:              role,
	}

	if err := tdb.DB.Create(access).Error; err != nil {
		t.Fatalf("failed to seed threat model access: %v", err)
	}

	return access
}

// SeedSurveyTemplate creates a test survey template and returns it.
func (tdb *TestDB) SeedSurveyTemplate(t *testing.T, createdByUUID string) *models.SurveyTemplate {
	t.Helper()

	st := &models.SurveyTemplate{
		ID:                    uuid.New().String(),
		Name:                  "Test Survey",
		Version:               "v1",
		Status:                "active",
		CreatedByInternalUUID: createdByUUID,
	}

	if err := tdb.DB.Create(st).Error; err != nil {
		t.Fatalf("failed to seed survey template: %v", err)
	}

	return st
}

// SeedSurveyResponse creates a test survey response and returns it.
func (tdb *TestDB) SeedSurveyResponse(t *testing.T, templateID, ownerUUID string, isConfidential bool) *models.SurveyResponse {
	t.Helper()

	sr := &models.SurveyResponse{
		ID:                uuid.New().String(),
		TemplateID:        templateID,
		TemplateVersion:   "v1",
		Status:            "draft",
		IsConfidential:    models.DBBool(isConfidential),
		OwnerInternalUUID: &ownerUUID,
	}

	if err := tdb.DB.Create(sr).Error; err != nil {
		t.Fatalf("failed to seed survey response: %v", err)
	}

	return sr
}

// SeedSurveyResponseAccess creates a test survey response access record and returns it.
func (tdb *TestDB) SeedSurveyResponseAccess(t *testing.T, responseID string, userUUID *string, groupUUID *string, subjectType, role string) *models.SurveyResponseAccess {
	t.Helper()

	access := &models.SurveyResponseAccess{
		ID:                responseID + "-" + uuid.New().String()[:8],
		SurveyResponseID:  responseID,
		UserInternalUUID:  userUUID,
		GroupInternalUUID: groupUUID,
		SubjectType:       subjectType,
		Role:              role,
	}

	if err := tdb.DB.Create(access).Error; err != nil {
		t.Fatalf("failed to seed survey response access: %v", err)
	}

	return access
}

// SeedTriageNote creates a test triage note and returns it.
func (tdb *TestDB) SeedTriageNote(t *testing.T, responseID, createdByUUID, modifiedByUUID string) *models.TriageNote {
	t.Helper()

	note := &models.TriageNote{
		SurveyResponseID:       responseID,
		Name:                   "Test Note",
		Content:                models.DBText("Test content"),
		CreatedByInternalUUID:  &createdByUUID,
		ModifiedByInternalUUID: &modifiedByUUID,
	}

	if err := tdb.DB.Create(note).Error; err != nil {
		t.Fatalf("failed to seed triage note: %v", err)
	}

	return note
}

// SeedClientCredential creates a test client credential and returns it.
func (tdb *TestDB) SeedClientCredential(t *testing.T, ownerUUID, clientID, name string) *models.ClientCredential {
	t.Helper()

	cc := &models.ClientCredential{
		ID:               uuid.New().String(),
		ClientID:         clientID,
		ClientSecretHash: "hashed_secret",
		Name:             name,
		OwnerUUID:        ownerUUID,
		IsActive:         models.OracleBool(true),
	}

	if err := tdb.DB.Create(cc).Error; err != nil {
		t.Fatalf("failed to seed client credential: %v", err)
	}

	return cc
}
