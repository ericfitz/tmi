package repository

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGormClientCredentialRepository_Create_Success(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()
	params := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
		Description:      "Test description",
	}

	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	assert.NotEqual(t, uuid.Nil, cred.ID)
	assert.Equal(t, ownerUUID, cred.OwnerUUID)
	assert.Equal(t, "tmi_cc_test123", cred.ClientID)
	assert.Equal(t, "hashed_secret", cred.ClientSecretHash)
	assert.Equal(t, "Test Credential", cred.Name)
	assert.Equal(t, "Test description", cred.Description)
	assert.True(t, cred.IsActive)
	assert.Nil(t, cred.LastUsedAt)
	assert.False(t, cred.CreatedAt.IsZero())
	assert.False(t, cred.ModifiedAt.IsZero())
}

func TestGormClientCredentialRepository_Create_WithExpiration(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	expiresAt := time.Now().Add(24 * time.Hour)
	params := ClientCredentialCreateParams{
		OwnerUUID:        uuid.New(),
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
		ExpiresAt:        &expiresAt,
	}

	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	require.NotNil(t, cred.ExpiresAt)
	assert.WithinDuration(t, expiresAt, *cred.ExpiresAt, time.Second)
}

func TestGormClientCredentialRepository_Create_IsActiveDefault(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	params := ClientCredentialCreateParams{
		OwnerUUID:        uuid.New(),
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}

	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	assert.True(t, cred.IsActive, "IsActive should default to true")
}

func TestGormClientCredentialRepository_GetByClientID_Found(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	// Create a credential
	params := ClientCredentialCreateParams{
		OwnerUUID:        uuid.New(),
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	created, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	// Retrieve by client ID
	found, err := repo.GetByClientID(context.Background(), "tmi_cc_test123")
	require.NoError(t, err)

	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, "tmi_cc_test123", found.ClientID)
}

func TestGormClientCredentialRepository_GetByClientID_NotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	found, err := repo.GetByClientID(context.Background(), "nonexistent")

	assert.Nil(t, found)
	assert.ErrorIs(t, err, ErrClientCredentialNotFound)
}

func TestGormClientCredentialRepository_GetByClientID_InactiveFiltered(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	// Create a credential
	ownerUUID := uuid.New()
	params := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	created, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	// Deactivate it
	err = repo.Deactivate(context.Background(), created.ID, ownerUUID)
	require.NoError(t, err)

	// Try to retrieve - should not find because inactive
	found, err := repo.GetByClientID(context.Background(), "tmi_cc_test123")

	assert.Nil(t, found)
	assert.ErrorIs(t, err, ErrClientCredentialNotFound)
}

func TestGormClientCredentialRepository_ListByOwner_Empty(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	creds, err := repo.ListByOwner(context.Background(), uuid.New())
	require.NoError(t, err)

	assert.Empty(t, creds)
}

func TestGormClientCredentialRepository_ListByOwner_Multiple(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()

	// Create multiple credentials
	for i := 0; i < 3; i++ {
		params := ClientCredentialCreateParams{
			OwnerUUID:        ownerUUID,
			ClientID:         uuid.New().String(),
			ClientSecretHash: "hashed_secret",
			Name:             "Test Credential",
		}
		_, err := repo.Create(context.Background(), params)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Ensure different CreatedAt timestamps
	}

	// Create one for a different owner
	otherParams := ClientCredentialCreateParams{
		OwnerUUID:        uuid.New(),
		ClientID:         uuid.New().String(),
		ClientSecretHash: "hashed_secret",
		Name:             "Other Owner's Credential",
	}
	_, err := repo.Create(context.Background(), otherParams)
	require.NoError(t, err)

	// List by owner
	creds, err := repo.ListByOwner(context.Background(), ownerUUID)
	require.NoError(t, err)

	assert.Len(t, creds, 3)
	// All should belong to the owner
	for _, cred := range creds {
		assert.Equal(t, ownerUUID, cred.OwnerUUID)
	}
}

func TestGormClientCredentialRepository_ListByOwner_IncludesInactive(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()

	// Create an active credential
	params1 := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "active-cred",
		ClientSecretHash: "hashed_secret",
		Name:             "Active Credential",
	}
	_, err := repo.Create(context.Background(), params1)
	require.NoError(t, err)

	// Create and deactivate a credential
	params2 := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "inactive-cred",
		ClientSecretHash: "hashed_secret",
		Name:             "Inactive Credential",
	}
	inactive, err := repo.Create(context.Background(), params2)
	require.NoError(t, err)

	err = repo.Deactivate(context.Background(), inactive.ID, ownerUUID)
	require.NoError(t, err)

	// List should include both
	creds, err := repo.ListByOwner(context.Background(), ownerUUID)
	require.NoError(t, err)

	assert.Len(t, creds, 2)

	// Verify we have one active and one inactive
	activeCount := 0
	inactiveCount := 0
	for _, cred := range creds {
		if cred.IsActive {
			activeCount++
		} else {
			inactiveCount++
		}
	}
	assert.Equal(t, 1, activeCount)
	assert.Equal(t, 1, inactiveCount)
}

func TestGormClientCredentialRepository_ListByOwner_OrderByCreatedAt(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()

	// Create credentials with distinct timestamps
	var createdIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		params := ClientCredentialCreateParams{
			OwnerUUID:        ownerUUID,
			ClientID:         uuid.New().String(),
			ClientSecretHash: "hashed_secret",
			Name:             "Test Credential",
		}
		cred, err := repo.Create(context.Background(), params)
		require.NoError(t, err)
		createdIDs = append(createdIDs, cred.ID)
		time.Sleep(10 * time.Millisecond)
	}

	// List should be ordered by created_at DESC (newest first)
	creds, err := repo.ListByOwner(context.Background(), ownerUUID)
	require.NoError(t, err)

	// The last created should be first
	assert.Equal(t, createdIDs[2], creds[0].ID)
	assert.Equal(t, createdIDs[1], creds[1].ID)
	assert.Equal(t, createdIDs[0], creds[2].ID)
}

func TestGormClientCredentialRepository_UpdateLastUsed_Success(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	// Create a credential
	params := ClientCredentialCreateParams{
		OwnerUUID:        uuid.New(),
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	assert.Nil(t, cred.LastUsedAt)

	// Update last used
	before := time.Now().Add(-time.Second)
	err = repo.UpdateLastUsed(context.Background(), cred.ID)
	require.NoError(t, err)
	after := time.Now().Add(time.Second)

	// Verify
	found, err := repo.GetByClientID(context.Background(), "tmi_cc_test123")
	require.NoError(t, err)

	require.NotNil(t, found.LastUsedAt)
	assert.True(t, found.LastUsedAt.After(before))
	assert.True(t, found.LastUsedAt.Before(after))
}

func TestGormClientCredentialRepository_UpdateLastUsed_NotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	err := repo.UpdateLastUsed(context.Background(), uuid.New())

	assert.ErrorIs(t, err, ErrClientCredentialNotFound)
}

func TestGormClientCredentialRepository_Deactivate_Success(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()
	params := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	assert.True(t, cred.IsActive)

	// Deactivate
	err = repo.Deactivate(context.Background(), cred.ID, ownerUUID)
	require.NoError(t, err)

	// Verify - should not be found by GetByClientID (which filters inactive)
	found, err := repo.GetByClientID(context.Background(), "tmi_cc_test123")
	assert.Nil(t, found)
	assert.ErrorIs(t, err, ErrClientCredentialNotFound)

	// But should appear in ListByOwner
	creds, err := repo.ListByOwner(context.Background(), ownerUUID)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.False(t, creds[0].IsActive)
}

func TestGormClientCredentialRepository_Deactivate_WrongOwner(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()
	params := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	// Try to deactivate with wrong owner
	err = repo.Deactivate(context.Background(), cred.ID, uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or unauthorized")
}

func TestGormClientCredentialRepository_Deactivate_NotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	err := repo.Deactivate(context.Background(), uuid.New(), uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or unauthorized")
}

func TestGormClientCredentialRepository_Delete_Success(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()
	params := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	// Delete
	err = repo.Delete(context.Background(), cred.ID, ownerUUID)
	require.NoError(t, err)

	// Verify - should not be found anywhere
	creds, err := repo.ListByOwner(context.Background(), ownerUUID)
	require.NoError(t, err)
	assert.Empty(t, creds)
}

func TestGormClientCredentialRepository_Delete_WrongOwner(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	ownerUUID := uuid.New()
	params := ClientCredentialCreateParams{
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
	}
	cred, err := repo.Create(context.Background(), params)
	require.NoError(t, err)

	// Try to delete with wrong owner
	err = repo.Delete(context.Background(), cred.ID, uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or unauthorized")

	// Original should still exist
	found, err := repo.GetByClientID(context.Background(), "tmi_cc_test123")
	require.NoError(t, err)
	assert.NotNil(t, found)
}

func TestGormClientCredentialRepository_Delete_NotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormClientCredentialRepository(tdb.DB)

	err := repo.Delete(context.Background(), uuid.New(), uuid.New())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found or unauthorized")
}

func TestConvertModelToClientCredential(t *testing.T) {
	now := time.Now()
	description := "Test description"
	id := uuid.New().String()
	ownerUUID := uuid.New().String()

	model := &models.ClientCredential{
		ID:               id,
		OwnerUUID:        ownerUUID,
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
		Description:      &description,
		IsActive:         models.DBBool(true),
		LastUsedAt:       &now,
		CreatedAt:        now,
		ModifiedAt:       now,
		ExpiresAt:        &now,
	}

	cred := convertModelToClientCredential(model)

	parsedID, _ := uuid.Parse(id)
	parsedOwnerUUID, _ := uuid.Parse(ownerUUID)

	assert.Equal(t, parsedID, cred.ID)
	assert.Equal(t, parsedOwnerUUID, cred.OwnerUUID)
	assert.Equal(t, "tmi_cc_test123", cred.ClientID)
	assert.Equal(t, "hashed_secret", cred.ClientSecretHash)
	assert.Equal(t, "Test Credential", cred.Name)
	assert.Equal(t, "Test description", cred.Description)
	assert.True(t, cred.IsActive)
	assert.NotNil(t, cred.LastUsedAt)
	assert.Equal(t, now, cred.CreatedAt)
	assert.Equal(t, now, cred.ModifiedAt)
	assert.NotNil(t, cred.ExpiresAt)
}

func TestConvertModelToClientCredential_NilDescription(t *testing.T) {
	model := &models.ClientCredential{
		ID:               uuid.New().String(),
		OwnerUUID:        uuid.New().String(),
		ClientID:         "tmi_cc_test123",
		ClientSecretHash: "hashed_secret",
		Name:             "Test Credential",
		Description:      nil,
		IsActive:         models.DBBool(true),
	}

	cred := convertModelToClientCredential(model)

	assert.Empty(t, cred.Description)
}

func TestConvertModelToClientCredential_DBBool(t *testing.T) {
	tests := []struct {
		name           string
		isActive       models.DBBool
		expectedResult bool
	}{
		{"active", models.DBBool(true), true},
		{"inactive", models.DBBool(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &models.ClientCredential{
				ID:               uuid.New().String(),
				OwnerUUID:        uuid.New().String(),
				ClientID:         "tmi_cc_test123",
				ClientSecretHash: "hashed_secret",
				Name:             "Test Credential",
				IsActive:         tt.isActive,
			}

			cred := convertModelToClientCredential(model)

			assert.Equal(t, tt.expectedResult, cred.IsActive)
		})
	}
}
