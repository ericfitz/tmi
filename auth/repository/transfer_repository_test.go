package repository

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGormDeletionRepository_TransferOwnership_NoOwnedItems(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")

	result, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	assert.Empty(t, result.ThreatModelIDs)
	assert.Empty(t, result.SurveyResponseIDs)
}

func TestGormDeletionRepository_TransferOwnership_ThreatModels(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")

	// Create two threat models owned by source
	tm1 := tdb.SeedThreatModel(t, string(source.InternalUUID), "TM 1")
	tm2 := tdb.SeedThreatModel(t, string(source.InternalUUID), "TM 2")

	// Give source explicit owner access on tm1 (to test downgrade)
	sourceUUID := string(source.InternalUUID)
	tdb.SeedThreatModelAccess(t, string(tm1.ID), &sourceUUID, nil, "user", "owner")

	result, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	assert.Len(t, result.ThreatModelIDs, 2)
	assert.Contains(t, result.ThreatModelIDs, string(tm1.ID))
	assert.Contains(t, result.ThreatModelIDs, string(tm2.ID))
	assert.Empty(t, result.SurveyResponseIDs)

	// Verify ownership transferred
	var updatedTM1 models.ThreatModel
	require.NoError(t, tdb.DB.First(&updatedTM1, "id = ?", tm1.ID).Error)
	assert.Equal(t, target.InternalUUID, updatedTM1.OwnerInternalUUID)

	var updatedTM2 models.ThreatModel
	require.NoError(t, tdb.DB.First(&updatedTM2, "id = ?", tm2.ID).Error)
	assert.Equal(t, target.InternalUUID, updatedTM2.OwnerInternalUUID)

	// Verify target has owner role in access table
	var targetAccess models.ThreatModelAccess
	err = tdb.DB.Where("threat_model_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		tm1.ID, target.InternalUUID, "user").First(&targetAccess).Error
	require.NoError(t, err)
	assert.Equal(t, "owner", string(targetAccess.Role))

	// Verify source was downgraded to writer
	var sourceAccess models.ThreatModelAccess
	err = tdb.DB.Where("threat_model_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		tm1.ID, source.InternalUUID, "user").First(&sourceAccess).Error
	require.NoError(t, err)
	assert.Equal(t, "writer", string(sourceAccess.Role))
}

func TestGormDeletionRepository_TransferOwnership_SurveyResponses(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")

	// Create survey template and response
	template := tdb.SeedSurveyTemplate(t, string(source.InternalUUID))
	sr := tdb.SeedSurveyResponse(t, string(template.ID), string(source.InternalUUID), false)

	result, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	assert.Empty(t, result.ThreatModelIDs)
	assert.Len(t, result.SurveyResponseIDs, 1)
	assert.Contains(t, result.SurveyResponseIDs, string(sr.ID))

	// Verify ownership transferred
	var updatedSR models.SurveyResponse
	require.NoError(t, tdb.DB.First(&updatedSR, "id = ?", sr.ID).Error)
	assert.Equal(t, string(target.InternalUUID), updatedSR.OwnerInternalUUID.String)

	// Verify target has owner role in access table
	var targetAccess models.SurveyResponseAccess
	err = tdb.DB.Where("survey_response_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		sr.ID, target.InternalUUID, "user").First(&targetAccess).Error
	require.NoError(t, err)
	assert.Equal(t, "owner", string(targetAccess.Role))

	// Verify source was downgraded to writer
	var sourceAccess models.SurveyResponseAccess
	err = tdb.DB.Where("survey_response_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		sr.ID, source.InternalUUID, "user").First(&sourceAccess).Error
	require.NoError(t, err)
	assert.Equal(t, "writer", string(sourceAccess.Role))
}

func TestGormDeletionRepository_TransferOwnership_Mixed(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")

	// Create threat models and survey responses
	tm := tdb.SeedThreatModel(t, string(source.InternalUUID), "TM Mixed")
	template := tdb.SeedSurveyTemplate(t, string(source.InternalUUID))
	sr := tdb.SeedSurveyResponse(t, string(template.ID), string(source.InternalUUID), false)

	result, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	assert.Len(t, result.ThreatModelIDs, 1)
	assert.Contains(t, result.ThreatModelIDs, string(tm.ID))
	assert.Len(t, result.SurveyResponseIDs, 1)
	assert.Contains(t, result.SurveyResponseIDs, string(sr.ID))
}

func TestGormDeletionRepository_TransferOwnership_TargetAlreadyHasAccess(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")

	tm := tdb.SeedThreatModel(t, string(source.InternalUUID), "TM With Existing Access")

	// Target already has reader access
	targetUUID2 := string(target.InternalUUID)
	tdb.SeedThreatModelAccess(t, string(tm.ID), &targetUUID2, nil, "user", "reader")

	result, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	assert.Len(t, result.ThreatModelIDs, 1)

	// Verify target was upgraded from reader to owner
	var targetAccess models.ThreatModelAccess
	err = tdb.DB.Where("threat_model_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		tm.ID, target.InternalUUID, "user").First(&targetAccess).Error
	require.NoError(t, err)
	assert.Equal(t, "owner", string(targetAccess.Role))
}

func TestGormDeletionRepository_TransferOwnership_SourceAccessRecordCreated(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")

	// Create TM but do NOT create an access record for source
	// (ownership is only in threat_models.owner_internal_uuid)
	tm := tdb.SeedThreatModel(t, string(source.InternalUUID), "TM No Access Record")

	result, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	assert.Len(t, result.ThreatModelIDs, 1)

	// Verify a writer access record was created for source
	var sourceAccess models.ThreatModelAccess
	err = tdb.DB.Where("threat_model_id = ? AND user_internal_uuid = ? AND subject_type = ?",
		tm.ID, source.InternalUUID, "user").First(&sourceAccess).Error
	require.NoError(t, err)
	assert.Equal(t, "writer", string(sourceAccess.Role))
}

func TestGormDeletionRepository_TransferOwnership_SelfTransfer(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	user := tdb.SeedUser(t, "user@example.com", "tmi")

	// Self-transfer should succeed at the repository level (validation is at the service layer)
	// but since the user would own the same items, this is effectively a no-op
	// The service layer validates source != target before calling the repository
	tdb.SeedThreatModel(t, string(user.InternalUUID), "TM Self")

	result, err := repo.TransferOwnership(context.Background(), string(user.InternalUUID), string(user.InternalUUID))
	require.NoError(t, err)
	// Items are "transferred" to the same user
	assert.Len(t, result.ThreatModelIDs, 1)
}

func TestGormDeletionRepository_TransferOwnership_TargetNotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	source := tdb.SeedUser(t, "source@example.com", "tmi")

	_, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), "00000000-0000-0000-0000-000000000099")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestGormDeletionRepository_TransferOwnership_SourceNotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	target := tdb.SeedUser(t, "target@example.com", "tmi")

	_, err := repo.TransferOwnership(context.Background(), "00000000-0000-0000-0000-000000000099", string(target.InternalUUID))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// A row that already holds a framework outside the enum (e.g. written by CATS
// before the enum was enforced) must not make ownership transfer fail: the
// transfer touches only ownership columns and must not re-validate the rest.
func TestGormDeletionRepository_TransferOwnership_LegacyInvalidFramework(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)
	source := tdb.SeedUser(t, "source@example.com", "tmi")
	target := tdb.SeedUser(t, "target@example.com", "tmi")
	tm := tdb.SeedThreatModel(t, string(source.InternalUUID), "Legacy TM")
	// UpdateColumn skips hooks, simulating a pre-enum stored value.
	require.NoError(t, tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).
		UpdateColumn("threat_model_framework", "ICFDENVPMLPYOVPQQIBZSXRAZSULJF").Error)

	_, err := repo.TransferOwnership(context.Background(), string(source.InternalUUID), string(target.InternalUUID))
	require.NoError(t, err)

	var got models.ThreatModel
	require.NoError(t, tdb.DB.First(&got, "id = ?", tm.ID).Error)
	assert.Equal(t, target.InternalUUID, got.OwnerInternalUUID)
	assert.Equal(t, "ICFDENVPMLPYOVPQQIBZSXRAZSULJF", string(got.ThreatModelFramework), "unrelated field is untouched")
}

// Same trap on the user-deletion path, which transfers to an alternate owner.
func TestGormDeletionRepository_DeleteUserAndData_LegacyInvalidFramework(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)
	del := tdb.SeedUser(t, "delete@example.com", "google")
	alt := tdb.SeedUser(t, "alternate@example.com", "google")
	tm := tdb.SeedThreatModel(t, string(del.InternalUUID), "Legacy TM")
	altUUID := string(alt.InternalUUID)
	tdb.SeedThreatModelAccess(t, string(tm.ID), &altUUID, nil, "user", "owner")
	require.NoError(t, tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).
		UpdateColumn("threat_model_framework", "LEGACY-BAD").Error)

	result, err := repo.DeleteUserAndData(context.Background(), "delete@example.com")
	require.NoError(t, err)
	assert.Equal(t, 1, result.ThreatModelsTransferred)

	var got models.ThreatModel
	require.NoError(t, tdb.DB.First(&got, "id = ?", tm.ID).Error)
	assert.Equal(t, alt.InternalUUID, got.OwnerInternalUUID)
}
