package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ericfitz/tmi/auth"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// =============================================================================
// TestTransferCurrentUserOwnership
// =============================================================================

func TestTransferCurrentUserOwnership_Unauthenticated(t *testing.T) {
	handler := NewOwnershipTransferHandler(nil)

	// Create context without setting userInternalUUID (unauthenticated)
	targetUUID := uuid.New()
	body, err := json.Marshal(TransferOwnershipRequest{
		TargetUserId: targetUUID,
	})
	require.NoError(t, err)

	c, w := CreateTestGinContextWithBody(http.MethodPost, "/me/transfer", "application/json", body)
	// Do NOT set userInternalUUID - simulates unauthenticated request

	handler.TransferCurrentUserOwnership(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Authentication required")
}

func TestTransferCurrentUserOwnership_InvalidRequestBody(t *testing.T) {
	handler := NewOwnershipTransferHandler(nil)

	// Create context with authentication but invalid JSON body
	c, w := CreateTestGinContextWithBody(http.MethodPost, "/me/transfer", "application/json", []byte(`{invalid json`))
	sourceUUID := uuid.New().String()
	c.Set("userInternalUUID", sourceUUID)

	handler.TransferCurrentUserOwnership(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestTransferCurrentUserOwnership_SelfTransfer(t *testing.T) {
	handler := NewOwnershipTransferHandler(nil)

	// Use the same UUID for source and target
	selfUUID := uuid.New()
	body, err := json.Marshal(TransferOwnershipRequest{
		TargetUserId: selfUUID,
	})
	require.NoError(t, err)

	c, w := CreateTestGinContextWithBody(http.MethodPost, "/me/transfer", "application/json", body)
	c.Set("userInternalUUID", selfUUID.String())

	handler.TransferCurrentUserOwnership(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Cannot transfer ownership to yourself")
}

// =============================================================================
// TestTransferAdminUserOwnership
// =============================================================================

func TestTransferAdminUserOwnership_InvalidRequestBody(t *testing.T) {
	handler := NewOwnershipTransferHandler(nil)

	// Valid source UUID, invalid body
	sourceUUID := uuid.New()
	c, w := CreateTestGinContextWithBody(http.MethodPost, "/admin/users/"+sourceUUID.String()+"/transfer", "application/json", []byte(`not json`))
	c.Set("userInternalUUID", uuid.New().String())
	c.Set("userEmail", "admin@example.com")

	handler.TransferAdminUserOwnership(c, sourceUUID)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestTransferAdminUserOwnership_SelfTransfer(t *testing.T) {
	handler := NewOwnershipTransferHandler(nil)

	// Source and target are the same UUID
	sameUUID := uuid.New()
	body, err := json.Marshal(TransferOwnershipRequest{
		TargetUserId: sameUUID,
	})
	require.NoError(t, err)

	c, w := CreateTestGinContextWithBody(http.MethodPost, "/admin/users/"+sameUUID.String()+"/transfer", "application/json", body)
	c.Set("userInternalUUID", uuid.New().String())
	c.Set("userEmail", "admin@example.com")

	handler.TransferAdminUserOwnership(c, sameUUID)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Cannot transfer ownership to the same user")
}

// =============================================================================
// TestBuildTransferResponse
// =============================================================================

func TestBuildTransferResponse_EmptyResult(t *testing.T) {
	result := &auth.TransferResult{
		ThreatModelIDs:    []string{},
		SurveyResponseIDs: []string{},
	}

	resp := buildTransferResponse(result)

	assert.Equal(t, 0, resp.ThreatModelsTransferred.Count)
	assert.Empty(t, resp.ThreatModelsTransferred.ThreatModelIds)
	assert.Equal(t, 0, resp.SurveyResponsesTransferred.Count)
	assert.Empty(t, resp.SurveyResponsesTransferred.SurveyResponseIds)
}

func TestBuildTransferResponse_WithThreatModelIDs(t *testing.T) {
	tmID1 := uuid.New().String()
	tmID2 := uuid.New().String()

	result := &auth.TransferResult{
		ThreatModelIDs:    []string{tmID1, tmID2},
		SurveyResponseIDs: []string{},
	}

	resp := buildTransferResponse(result)

	assert.Equal(t, 2, resp.ThreatModelsTransferred.Count)
	assert.Len(t, resp.ThreatModelsTransferred.ThreatModelIds, 2)
	assert.Equal(t, tmID1, resp.ThreatModelsTransferred.ThreatModelIds[0].String())
	assert.Equal(t, tmID2, resp.ThreatModelsTransferred.ThreatModelIds[1].String())
	assert.Equal(t, 0, resp.SurveyResponsesTransferred.Count)
	assert.Empty(t, resp.SurveyResponsesTransferred.SurveyResponseIds)
}

func TestBuildTransferResponse_WithBothIDs(t *testing.T) {
	tmID := uuid.New().String()
	srID1 := uuid.New().String()
	srID2 := uuid.New().String()
	srID3 := uuid.New().String()

	result := &auth.TransferResult{
		ThreatModelIDs:    []string{tmID},
		SurveyResponseIDs: []string{srID1, srID2, srID3},
	}

	resp := buildTransferResponse(result)

	assert.Equal(t, 1, resp.ThreatModelsTransferred.Count)
	assert.Len(t, resp.ThreatModelsTransferred.ThreatModelIds, 1)
	assert.Equal(t, tmID, resp.ThreatModelsTransferred.ThreatModelIds[0].String())

	assert.Equal(t, 3, resp.SurveyResponsesTransferred.Count)
	assert.Len(t, resp.SurveyResponsesTransferred.SurveyResponseIds, 3)
	assert.Equal(t, srID1, resp.SurveyResponsesTransferred.SurveyResponseIds[0].String())
	assert.Equal(t, srID2, resp.SurveyResponsesTransferred.SurveyResponseIds[1].String())
	assert.Equal(t, srID3, resp.SurveyResponsesTransferred.SurveyResponseIds[2].String())
}

// =============================================================================
// TestNewOwnershipTransferHandler
// =============================================================================

func TestNewOwnershipTransferHandler(t *testing.T) {
	// Test that constructor creates handler with nil auth service
	handler := NewOwnershipTransferHandler(nil)
	assert.NotNil(t, handler)
	assert.Nil(t, handler.authService)
}
