package repository

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGormDeletionRepository_DeleteUserAndData_NoThreatModels(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create a user with no threat models
	user := tdb.SeedUser(t, "test@example.com", "google")

	result, err := repo.DeleteUserAndData(context.Background(), user.Email)
	require.NoError(t, err)

	assert.Equal(t, "test@example.com", result.UserEmail)
	assert.Equal(t, 0, result.ThreatModelsTransferred)
	assert.Equal(t, 0, result.ThreatModelsDeleted)

	// Verify user is deleted
	var count int64
	tdb.DB.Model(&models.User{}).Where("email = ?", "test@example.com").Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteUserAndData_TransfersOwnership(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create user to delete
	userToDelete := tdb.SeedUser(t, "delete@example.com", "google")

	// Create alternate owner
	alternateOwner := tdb.SeedUser(t, "alternate@example.com", "google")

	// Create threat model owned by user to delete
	tm := tdb.SeedThreatModel(t, userToDelete.InternalUUID, "Test TM")

	// Grant alternate owner "owner" role via threat_model_access
	tdb.SeedThreatModelAccess(t, tm.ID, &alternateOwner.InternalUUID, nil, "user", "owner")

	result, err := repo.DeleteUserAndData(context.Background(), "delete@example.com")
	require.NoError(t, err)

	assert.Equal(t, 1, result.ThreatModelsTransferred)
	assert.Equal(t, 0, result.ThreatModelsDeleted)

	// Verify threat model ownership transferred
	var updatedTM models.ThreatModel
	err = tdb.DB.First(&updatedTM, "id = ?", tm.ID).Error
	require.NoError(t, err)
	assert.Equal(t, alternateOwner.InternalUUID, updatedTM.OwnerInternalUUID)

	// Verify user is deleted
	var count int64
	tdb.DB.Model(&models.User{}).Where("email = ?", "delete@example.com").Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteUserAndData_DeletesOrphaned(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create user to delete
	userToDelete := tdb.SeedUser(t, "delete@example.com", "google")

	// Create threat model owned by user (no alternate owners)
	tm := tdb.SeedThreatModel(t, userToDelete.InternalUUID, "Orphaned TM")

	result, err := repo.DeleteUserAndData(context.Background(), "delete@example.com")
	require.NoError(t, err)

	assert.Equal(t, 0, result.ThreatModelsTransferred)
	assert.Equal(t, 1, result.ThreatModelsDeleted)

	// Verify threat model is deleted
	var count int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteUserAndData_MixedScenario(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create user to delete
	userToDelete := tdb.SeedUser(t, "delete@example.com", "google")

	// Create alternate owner
	alternateOwner := tdb.SeedUser(t, "alternate@example.com", "google")

	// Create TM that will be transferred (has alternate owner)
	tmTransfer := tdb.SeedThreatModel(t, userToDelete.InternalUUID, "Transferable TM")
	tdb.SeedThreatModelAccess(t, tmTransfer.ID, &alternateOwner.InternalUUID, nil, "user", "owner")

	// Create TM that will be deleted (no alternate owner)
	tmDelete := tdb.SeedThreatModel(t, userToDelete.InternalUUID, "Orphaned TM")

	result, err := repo.DeleteUserAndData(context.Background(), "delete@example.com")
	require.NoError(t, err)

	assert.Equal(t, 1, result.ThreatModelsTransferred)
	assert.Equal(t, 1, result.ThreatModelsDeleted)

	// Verify transferred TM still exists
	var countTransfer int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tmTransfer.ID).Count(&countTransfer)
	assert.Equal(t, int64(1), countTransfer)

	// Verify deleted TM is gone
	var countDelete int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tmDelete.ID).Count(&countDelete)
	assert.Equal(t, int64(0), countDelete)
}

func TestGormDeletionRepository_DeleteUserAndData_UserNotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	result, err := repo.DeleteUserAndData(context.Background(), "nonexistent@example.com")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestGormDeletionRepository_DeleteUserAndData_CleansPermissions(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create user to delete
	userToDelete := tdb.SeedUser(t, "delete@example.com", "google")

	// Create another user who owns a TM
	owner := tdb.SeedUser(t, "owner@example.com", "google")
	tm := tdb.SeedThreatModel(t, owner.InternalUUID, "Owner's TM")

	// Grant the user to delete reader access
	tdb.SeedThreatModelAccess(t, tm.ID, &userToDelete.InternalUUID, nil, "user", "reader")

	// Delete the user
	_, err := repo.DeleteUserAndData(context.Background(), "delete@example.com")
	require.NoError(t, err)

	// Verify the user's access record is cleaned up
	var count int64
	tdb.DB.Model(&models.ThreatModelAccess{}).
		Where("user_internal_uuid = ?", userToDelete.InternalUUID).
		Count(&count)
	assert.Equal(t, int64(0), count)

	// Verify threat model still exists (owned by different user)
	var tmCount int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).Count(&tmCount)
	assert.Equal(t, int64(1), tmCount)
}

func TestGormDeletionRepository_DeleteGroupAndData_NoThreatModels(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create a group with no threat models
	group := tdb.SeedGroup(t, "*", "test-group")

	result, err := repo.DeleteGroupAndData(context.Background(), group.InternalUUID)
	require.NoError(t, err)

	assert.Equal(t, "test-group", result.GroupName)
	assert.Equal(t, 0, result.ThreatModelsDeleted)
	assert.Equal(t, 0, result.ThreatModelsRetained)

	// Verify group is deleted
	var count int64
	tdb.DB.Model(&models.Group{}).Where("internal_uuid = ?", group.InternalUUID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteGroupAndData_DeletesOrphaned(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create a group
	group := tdb.SeedGroup(t, "*", "test-group")

	// Create threat model owned by the group
	tm := tdb.SeedThreatModel(t, group.InternalUUID, "Group TM")

	result, err := repo.DeleteGroupAndData(context.Background(), group.InternalUUID)
	require.NoError(t, err)

	assert.Equal(t, 1, result.ThreatModelsDeleted)
	assert.Equal(t, 0, result.ThreatModelsRetained)

	// Verify threat model is deleted
	var count int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteGroupAndData_RetainsWithUserOwners(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create a group and a user
	group := tdb.SeedGroup(t, "*", "test-group")
	user := tdb.SeedUser(t, "user@example.com", "google")

	// Create threat model owned by the group
	tm := tdb.SeedThreatModel(t, group.InternalUUID, "Group TM")

	// Add user as owner via threat_model_access
	tdb.SeedThreatModelAccess(t, tm.ID, &user.InternalUUID, nil, "user", "owner")

	result, err := repo.DeleteGroupAndData(context.Background(), group.InternalUUID)
	require.NoError(t, err)

	assert.Equal(t, 0, result.ThreatModelsDeleted)
	assert.Equal(t, 1, result.ThreatModelsRetained)

	// Verify threat model still exists
	var count int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestGormDeletionRepository_DeleteGroupAndData_GroupNotFound(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Use a random UUID that doesn't exist
	result, err := repo.DeleteGroupAndData(context.Background(), "00000000-0000-0000-0000-000000000099")

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "group not found")
}

func TestGormDeletionRepository_DeleteGroupAndData_ProtectedGroup(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create the "everyone" group
	everyoneGroup := tdb.SeedGroup(t, "*", "everyone")

	result, err := repo.DeleteGroupAndData(context.Background(), everyoneGroup.InternalUUID)

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot delete protected group")
}

func TestGormDeletionRepository_DeleteGroupAndData_CleansPermissions(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create group and user
	group := tdb.SeedGroup(t, "*", "test-group")
	user := tdb.SeedUser(t, "user@example.com", "google")

	// Create threat model owned by user
	tm := tdb.SeedThreatModel(t, user.InternalUUID, "User's TM")

	// Grant the group reader access
	tdb.SeedThreatModelAccess(t, tm.ID, nil, &group.InternalUUID, "group", "reader")

	// Delete the group
	_, err := repo.DeleteGroupAndData(context.Background(), group.InternalUUID)
	require.NoError(t, err)

	// Verify the group's access record is cleaned up
	var count int64
	tdb.DB.Model(&models.ThreatModelAccess{}).
		Where("group_internal_uuid = ?", group.InternalUUID).
		Count(&count)
	assert.Equal(t, int64(0), count)

	// Verify threat model still exists (owned by user)
	var tmCount int64
	tdb.DB.Model(&models.ThreatModel{}).Where("id = ?", tm.ID).Count(&tmCount)
	assert.Equal(t, int64(1), tmCount)
}

func TestGormDeletionRepository_DeleteUserAndData_Transaction_Success(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create complex scenario with multiple TMs
	userToDelete := tdb.SeedUser(t, "delete@example.com", "google")

	// Create multiple threat models
	for i := 0; i < 3; i++ {
		tdb.SeedThreatModel(t, userToDelete.InternalUUID, "TM")
	}

	// All should succeed in one transaction
	result, err := repo.DeleteUserAndData(context.Background(), "delete@example.com")
	require.NoError(t, err)

	assert.Equal(t, 3, result.ThreatModelsDeleted)

	// Verify all TMs are deleted
	var count int64
	tdb.DB.Model(&models.ThreatModel{}).Where("owner_internal_uuid = ?", userToDelete.InternalUUID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteUserAndData_HandlesSurveyTemplates(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	// Create user who created a survey template
	user := tdb.SeedUser(t, "template-creator@example.com", "google")
	tdb.SeedSurveyTemplate(t, user.InternalUUID)

	// Deleting the user should succeed (no FK constraint blocking)
	result, err := repo.DeleteUserAndData(context.Background(), "template-creator@example.com")
	require.NoError(t, err)
	assert.Equal(t, "template-creator@example.com", result.UserEmail)

	// Template should still exist with its created_by_internal_uuid intact
	var count int64
	tdb.DB.Model(&models.SurveyTemplate{}).Where("created_by_internal_uuid = ?", user.InternalUUID).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestGormDeletionRepository_DeleteUserAndData_NullifiesSurveyResponseOwner(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	user := tdb.SeedUser(t, "owner@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, user.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, user.InternalUUID, false)

	_, err := repo.DeleteUserAndData(context.Background(), "owner@example.com")
	require.NoError(t, err)

	// Verify response still exists with NULL owner
	var response models.SurveyResponse
	err = tdb.DB.First(&response, "id = ?", sr.ID).Error
	require.NoError(t, err)
	assert.Nil(t, response.OwnerInternalUUID)
}

func TestGormDeletionRepository_DeleteUserAndData_NullifiesReviewedBy(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	owner := tdb.SeedUser(t, "owner@example.com", "google")
	reviewer := tdb.SeedUser(t, "reviewer@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, owner.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, owner.InternalUUID, false)

	// Set the reviewer
	tdb.DB.Model(&models.SurveyResponse{}).Where("id = ?", sr.ID).
		Update("reviewed_by_internal_uuid", reviewer.InternalUUID)

	// Delete the reviewer
	_, err := repo.DeleteUserAndData(context.Background(), "reviewer@example.com")
	require.NoError(t, err)

	// Verify reviewed_by is NULL
	var response models.SurveyResponse
	err = tdb.DB.First(&response, "id = ?", sr.ID).Error
	require.NoError(t, err)
	assert.Nil(t, response.ReviewedByInternalUUID)
}

func TestGormDeletionRepository_DeleteUserAndData_DeletesSurveyResponseAccess(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	user := tdb.SeedUser(t, "grantee@example.com", "google")
	owner := tdb.SeedUser(t, "owner@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, owner.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, owner.InternalUUID, false)

	// Grant user access as grantee
	tdb.SeedSurveyResponseAccess(t, sr.ID, &user.InternalUUID, nil, "user", "reader")

	// Delete the grantee
	_, err := repo.DeleteUserAndData(context.Background(), "grantee@example.com")
	require.NoError(t, err)

	// Verify access record is deleted
	var count int64
	tdb.DB.Model(&models.SurveyResponseAccess{}).
		Where("user_internal_uuid = ?", user.InternalUUID).Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestGormDeletionRepository_DeleteUserAndData_NullifiesGrantedBy(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	granter := tdb.SeedUser(t, "granter@example.com", "google")
	grantee := tdb.SeedUser(t, "grantee@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, granter.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, granter.InternalUUID, false)

	// Create access record where granter granted access to grantee
	access := &models.SurveyResponseAccess{
		ID:                    uuid.New().String(),
		SurveyResponseID:      sr.ID,
		UserInternalUUID:      &grantee.InternalUUID,
		SubjectType:           "user",
		Role:                  "reader",
		GrantedByInternalUUID: &granter.InternalUUID,
	}
	require.NoError(t, tdb.DB.Create(access).Error)

	// Delete the granter (not the grantee)
	_, err := repo.DeleteUserAndData(context.Background(), "granter@example.com")
	require.NoError(t, err)

	// Verify granted_by is NULL but access record still exists
	var updatedAccess models.SurveyResponseAccess
	err = tdb.DB.First(&updatedAccess, "id = ?", access.ID).Error
	require.NoError(t, err)
	assert.Nil(t, updatedAccess.GrantedByInternalUUID)
	assert.Equal(t, &grantee.InternalUUID, updatedAccess.UserInternalUUID)
}

func TestGormDeletionRepository_DeleteUserAndData_NullifiesTriageNoteAuthors(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	user := tdb.SeedUser(t, "author@example.com", "google")
	owner := tdb.SeedUser(t, "owner@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, owner.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, owner.InternalUUID, false)
	note := tdb.SeedTriageNote(t, sr.ID, user.InternalUUID, user.InternalUUID)

	// Delete the author
	_, err := repo.DeleteUserAndData(context.Background(), "author@example.com")
	require.NoError(t, err)

	// Verify note content is preserved but author fields are NULL
	var updatedNote models.TriageNote
	err = tdb.DB.Where("survey_response_id = ? AND id = ?", sr.ID, note.ID).First(&updatedNote).Error
	require.NoError(t, err)
	assert.Equal(t, "Test content", string(updatedNote.Content))
	assert.Nil(t, updatedNote.CreatedByInternalUUID)
	assert.Nil(t, updatedNote.ModifiedByInternalUUID)
}

func TestGormDeletionRepository_DeleteUserAndData_EnsuresSecurityReviewersOnConfidentialResponses(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	user := tdb.SeedUser(t, "owner@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, user.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, user.InternalUUID, true) // confidential

	// Verify no Security Reviewers access exists initially
	var countBefore int64
	tdb.DB.Model(&models.SurveyResponseAccess{}).
		Where("survey_response_id = ? AND subject_type = ? AND role = ?", sr.ID, "group", "owner").
		Count(&countBefore)
	assert.Equal(t, int64(0), countBefore)

	// Delete the owner
	_, err := repo.DeleteUserAndData(context.Background(), "owner@example.com")
	require.NoError(t, err)

	// Verify Security Reviewers group was added with owner role
	var countAfter int64
	tdb.DB.Model(&models.SurveyResponseAccess{}).
		Where("survey_response_id = ? AND subject_type = ? AND role = ?", sr.ID, "group", "owner").
		Count(&countAfter)
	assert.Equal(t, int64(1), countAfter)
}

func TestGormDeletionRepository_DeleteUserAndData_SkipsDuplicateSecurityReviewers(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	user := tdb.SeedUser(t, "owner@example.com", "google")
	st := tdb.SeedSurveyTemplate(t, user.InternalUUID)
	sr := tdb.SeedSurveyResponse(t, st.ID, user.InternalUUID, false) // non-confidential

	// Create Security Reviewers group with well-known UUID
	groupUUID := securityReviewersGroupUUID
	group := tdb.SeedGroup(t, "*", securityReviewersGroupName)

	// Update to use well-known UUID
	tdb.DB.Model(&models.Group{}).Where("internal_uuid = ?", group.InternalUUID).
		Update("internal_uuid", groupUUID)

	tdb.SeedSurveyResponseAccess(t, sr.ID, nil, &groupUUID, "group", "owner")

	// Delete the owner
	_, err := repo.DeleteUserAndData(context.Background(), "owner@example.com")
	require.NoError(t, err)

	// Verify no duplicate Security Reviewers access was created
	var count int64
	tdb.DB.Model(&models.SurveyResponseAccess{}).
		Where("survey_response_id = ? AND group_internal_uuid = ? AND subject_type = ?",
			sr.ID, groupUUID, "group").
		Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestNewGormDeletionRepository(t *testing.T) {
	tdb := db.MustCreateTestDB(t)
	defer tdb.Cleanup()

	repo := NewGormDeletionRepository(tdb.DB)

	assert.NotNil(t, repo)
	assert.NotNil(t, repo.db)
	assert.NotNil(t, repo.logger)
}
