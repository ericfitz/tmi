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
