package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/auth/repository"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// OwnershipTransferHandler handles ownership transfer operations
type OwnershipTransferHandler struct {
	authService *auth.Service
}

// NewOwnershipTransferHandler creates a new ownership transfer handler
func NewOwnershipTransferHandler(authService *auth.Service) *OwnershipTransferHandler {
	return &OwnershipTransferHandler{
		authService: authService,
	}
}

// TransferCurrentUserOwnership handles POST /me/transfer
func (h *OwnershipTransferHandler) TransferCurrentUserOwnership(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user's internal UUID from context
	sourceUUID := c.GetString("userInternalUUID")
	if sourceUUID == "" {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		})
		return
	}

	// Parse request body
	var req TransferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, InvalidInputError(fmt.Sprintf("Invalid request body: %v", err)))
		return
	}

	targetUUID := req.TargetUserId.String()

	// Validate source != target
	if sourceUUID == targetUUID {
		HandleRequestError(c, InvalidInputError("Cannot transfer ownership to yourself"))
		return
	}

	// Perform transfer
	result, err := h.authService.TransferOwnership(c.Request.Context(), sourceUUID, targetUUID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			HandleRequestError(c, NotFoundError("Target user not found"))
			return
		}
		logger.Error("Failed to transfer ownership: %v", err)
		HandleRequestError(c, ServerError("Failed to transfer ownership"))
		return
	}

	userEmail := c.GetString("userEmail")
	logger.Info("[AUDIT] Ownership transferred: source=%s (email=%s), target=%s, threat_models=%d, survey_responses=%d",
		sourceUUID, userEmail, targetUUID, len(result.ThreatModelIDs), len(result.SurveyResponseIDs))

	c.JSON(http.StatusOK, buildTransferResponse(result))
}

// TransferAdminUserOwnership handles POST /admin/users/{internal_uuid}/transfer
func (h *OwnershipTransferHandler) TransferAdminUserOwnership(c *gin.Context, internalUuid openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get admin actor info for audit log
	actorUserID := c.GetString("userInternalUUID")
	actorEmail := c.GetString("userEmail")

	// Parse source user UUID from path parameter
	sourceUUID, err := uuid.Parse(internalUuid.String())
	if err != nil {
		HandleRequestError(c, InvalidInputError("internal_uuid must be a valid UUID"))
		return
	}

	// Parse request body
	var req TransferOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, InvalidInputError(fmt.Sprintf("Invalid request body: %v", err)))
		return
	}

	targetUUID := req.TargetUserId.String()

	// Validate source != target
	if sourceUUID.String() == targetUUID {
		HandleRequestError(c, InvalidInputError("Cannot transfer ownership to the same user"))
		return
	}

	// Perform transfer
	result, err := h.authService.TransferOwnership(c.Request.Context(), sourceUUID.String(), targetUUID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			HandleRequestError(c, NotFoundError("Source or target user not found"))
			return
		}
		logger.Error("Failed to transfer ownership: %v", err)
		HandleRequestError(c, ServerError("Failed to transfer ownership"))
		return
	}

	logger.Info("[AUDIT] Admin ownership transfer: source=%s, target=%s, transferred_by=%s (email=%s), threat_models=%d, survey_responses=%d",
		sourceUUID, targetUUID, actorUserID, actorEmail, len(result.ThreatModelIDs), len(result.SurveyResponseIDs))

	c.JSON(http.StatusOK, buildTransferResponse(result))
}

// buildTransferResponse converts the auth.TransferResult to the OpenAPI TransferOwnershipResult
func buildTransferResponse(result *auth.TransferResult) TransferOwnershipResult {
	tmIDs := make([]openapi_types.UUID, len(result.ThreatModelIDs))
	for i, id := range result.ThreatModelIDs {
		tmIDs[i] = uuid.MustParse(id)
	}

	srIDs := make([]openapi_types.UUID, len(result.SurveyResponseIDs))
	for i, id := range result.SurveyResponseIDs {
		srIDs[i] = uuid.MustParse(id)
	}

	resp := TransferOwnershipResult{}
	resp.ThreatModelsTransferred.Count = len(result.ThreatModelIDs)
	resp.ThreatModelsTransferred.ThreatModelIds = tmIDs
	resp.SurveyResponsesTransferred.Count = len(result.SurveyResponseIDs)
	resp.SurveyResponsesTransferred.SurveyResponseIds = srIDs
	return resp
}
