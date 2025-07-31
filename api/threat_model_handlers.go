package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ThreatModelHandler provides handlers for threat model operations
type ThreatModelHandler struct {
	// Could add dependencies like logger, metrics, etc.
}

// NewThreatModelHandler creates a new threat model handler
func NewThreatModelHandler() *ThreatModelHandler {
	return &ThreatModelHandler{}
}

// GetThreatModels returns a list of threat models
func (h *ThreatModelHandler) GetThreatModels(c *gin.Context) {
	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Get username from JWT claim
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For listing endpoints, we allow unauthenticated users but return empty results
		userName = ""
	}

	// Filter by user access using new authorization utilities
	filter := func(tm ThreatModel) bool {
		// If no user is authenticated, only show public threat models (if any)
		if userName == "" {
			return false
		}

		// Create authorization data for the threat model
		authData := AuthorizationData{
			Type:          AuthTypeTMI10,
			Owner:         tm.Owner,
			Authorization: tm.Authorization,
		}

		// Check if user has at least reader access
		return AccessCheck(userName, RoleReader, authData)
	}

	// Get threat models from store with filtering
	models := ThreatModelStore.List(offset, limit, filter)

	// Convert to list items for API response
	items := make([]ListItem, 0, len(models))
	for _, tm := range models {
		items = append(items, ListItem{
			Id:   tm.Id,
			Name: tm.Name,
		})
	}

	c.JSON(http.StatusOK, items)
}

// GetThreatModelByID retrieves a specific threat model
func (h *ThreatModelHandler) GetThreatModelByID(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")

	// Validate ID format
	if _, err := ParseUUID(id); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get threat model from store
	tm, err := ThreatModelStore.Get(id)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Get authenticated user
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Check authorization using new utilities
	hasAccess, err := CheckResourceAccess(userName, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasAccess {
		HandleRequestError(c, ForbiddenError("Insufficient permissions to access this threat model"))
		return
	}

	c.JSON(http.StatusOK, tm)
}

// CreateThreatModel creates a new threat model
func (h *ThreatModelHandler) CreateThreatModel(c *gin.Context) {
	type CreateThreatModelRequest struct {
		Name          string          `json:"name" binding:"required"`
		Description   *string         `json:"description,omitempty"`
		Authorization []Authorization `json:"authorization,omitempty"`
	}

	request, err := ParseRequestBody[CreateThreatModelRequest](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get username from JWT claim
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Create new threat model
	now := time.Now().UTC()
	threatIDs := []Threat{}

	// Validate authorization entries with format checking
	if err := ValidateAuthorizationEntriesWithFormat(request.Authorization); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate authorization list for duplicates
	if err := ValidateDuplicateSubjects(request.Authorization); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate that owner is not duplicated in authorization list
	if err := ValidateOwnerNotInAuthList(userName, request.Authorization); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Create authorizations array with owner as first entry
	authorizations := []Authorization{
		{
			Subject: userName,
			Role:    RoleOwner,
		},
	}

	// Add any additional authorization subjects from the request
	authorizations = append(authorizations, request.Authorization...)

	tm := ThreatModel{
		Name:          request.Name,
		Description:   request.Description,
		CreatedAt:     now,
		ModifiedAt:    now,
		Owner:         userName,
		CreatedBy:     userName,
		Authorization: authorizations,
		Metadata:      &[]Metadata{},
		Threats:       &threatIDs,
	}

	// Add to store
	idSetter := func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = &uuid
		return tm
	}

	createdTM, err := ThreatModelStore.Create(tm, idSetter)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to create threat model"))
		return
	}

	// Set the Location header
	c.Header("Location", "/threat_models/"+createdTM.Id.String())
	c.JSON(http.StatusCreated, createdTM)
}

// UpdateThreatModel fully updates a threat model
func (h *ThreatModelHandler) UpdateThreatModel(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")
	fmt.Printf("[DEBUG HANDLER] UpdateThreatModel called for ID: %s\n", id)

	// Parse request body using utility
	request, err := ParseRequestBody[ThreatModel](c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	fmt.Printf("[DEBUG HANDLER] Successfully parsed request: %+v\n", request)

	// Get username from JWT claim
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	_ = userName // We don't use userName directly in this function

	// Get existing threat model
	tm, err := ThreatModelStore.Get(id)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Validate ID format and ensure it matches URL
	uuid, err := ParseUUID(id)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format"))
		return
	}
	request.Id = &uuid

	// Preserve creation time but update modification time
	request.CreatedAt = tm.CreatedAt
	request.ModifiedAt = time.Now().UTC()

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccess(userName, tm, RoleWriter)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasWriteAccess {
		HandleRequestError(c, ForbiddenError("Insufficient permissions to update this threat model"))
		return
	}

	// Check if user has owner access for sensitive fields
	ownerChanging := request.Owner != "" && request.Owner != tm.Owner
	authChanging := (len(request.Authorization) > 0) && (!authorizationEqual(request.Authorization, tm.Authorization))

	if ownerChanging || authChanging {
		hasOwnerAccess, err := CheckResourceAccess(userName, tm, RoleOwner)
		if err != nil {
			HandleRequestError(c, err)
			return
		}

		if !hasOwnerAccess {
			HandleRequestError(c, ForbiddenError("Only the owner can change ownership or authorization"))
			return
		}
	}

	// Validate authorization changes if present
	if authChanging {
		// Validate authorization entries with format checking
		if err := ValidateAuthorizationEntriesWithFormat(request.Authorization); err != nil {
			HandleRequestError(c, err)
			return
		}

		// Check for duplicate authorization subjects
		if err := ValidateDuplicateSubjects(request.Authorization); err != nil {
			HandleRequestError(c, err)
			return
		}

		// If owner is changing, apply ownership transfer rule
		if ownerChanging {
			request.Authorization = ApplyOwnershipTransferRule(request.Authorization, tm.Owner, request.Owner)
		}
	}

	// Update in store
	if err := ThreatModelStore.Update(id, request); err != nil {
		HandleRequestError(c, ServerError("Failed to update threat model"))
		return
	}

	c.JSON(http.StatusOK, request)
}

// PatchThreatModel partially updates a threat model
func (h *ThreatModelHandler) PatchThreatModel(c *gin.Context) {
	id := c.Param("id")
	fmt.Printf("[DEBUG HANDLER] PatchThreatModel called for ID: %s\n", id)

	// Phase 1: Parse request and validate user
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	fmt.Printf("[DEBUG HANDLER] Successfully parsed PATCH request with %d operations\n", len(operations))

	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Phase 2: Get existing threat model
	existingTM, err := h.getExistingThreatModel(c, id)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Phase 3: Apply patch operations
	modifiedTM, err := ApplyPatchOperations(existingTM, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Phase 4: Preserve critical fields and validate authorization
	modifiedTM = h.preserveThreatModelCriticalFields(modifiedTM, existingTM)

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccess(userName, existingTM, RoleWriter)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasWriteAccess {
		HandleRequestError(c, ForbiddenError("Insufficient permissions to update this threat model"))
		return
	}

	// Check authorization for sensitive changes
	ownerChanging := modifiedTM.Owner != existingTM.Owner
	authChanging := !authorizationEqual(existingTM.Authorization, modifiedTM.Authorization)

	if ownerChanging || authChanging {
		hasOwnerAccess, err := CheckResourceAccess(userName, existingTM, RoleOwner)
		if err != nil {
			HandleRequestError(c, err)
			return
		}

		if !hasOwnerAccess {
			HandleRequestError(c, ForbiddenError("Only the owner can change ownership or authorization"))
			return
		}
	}

	// Phase 5: Apply business rules
	if err := h.applyThreatModelBusinessRules(&modifiedTM, existingTM, ownerChanging, authChanging); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Phase 6: Validate the patched threat model
	if err := ValidatePatchedEntity(existingTM, modifiedTM, userName, validatePatchedThreatModel); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Final update of timestamps
	modifiedTM.ModifiedAt = time.Now().UTC()

	// Update in store
	if err := ThreatModelStore.Update(id, modifiedTM); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to update threat model",
		})
		return
	}

	c.JSON(http.StatusOK, modifiedTM)
}

// DeleteThreatModel deletes a threat model
func (h *ThreatModelHandler) DeleteThreatModel(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")

	// Validate ID format
	if _, err := ParseUUID(id); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get the user making the request
	userName, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get threat model from store
	tm, err := ThreatModelStore.Get(id)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has owner access (required for deletion)
	hasOwnerAccess, err := CheckResourceAccess(userName, tm, RoleOwner)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasOwnerAccess {
		HandleRequestError(c, ForbiddenError("Only the owner can delete a threat model"))
		return
	}

	// Delete from store
	if err := ThreatModelStore.Delete(id); err != nil {
		HandleRequestError(c, ServerError("Failed to delete threat model"))
		return
	}

	c.Status(http.StatusNoContent)
}

// Helper function to parse integer parameters with fallback
func parseIntParam(val string, fallback int) int {
	if val == "" {
		return fallback
	}

	i, err := parseInt(val, fallback)
	if err != nil {
		return fallback
	}

	return i
}

// Note: Using the PatchOperation type defined in types.go

// convertOperationsToJSONPatch converts our internal representation to RFC6902 format
func convertOperationsToJSONPatch(operations []PatchOperation) ([]byte, error) {
	return json.Marshal(operations)
}

// authorizationEqual checks if two authorization arrays are equal
func authorizationEqual(a, b []Authorization) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for easier comparison
	mapA := make(map[string]AuthorizationRole)
	mapB := make(map[string]AuthorizationRole)

	for _, auth := range a {
		mapA[auth.Subject] = auth.Role
	}

	for _, auth := range b {
		mapB[auth.Subject] = auth.Role
	}

	// Check if all entries in mapA exist with same role in mapB
	for subject, role := range mapA {
		if mapB[subject] != role {
			return false
		}
	}

	// Check if all entries in mapB exist with same role in mapA
	for subject, role := range mapB {
		if mapA[subject] != role {
			return false
		}
	}

	return true
}

// validatePatchedThreatModel performs validation on the patched threat model
func validatePatchedThreatModel(original, patched ThreatModel, userName string) error {
	// Add debug logging
	fmt.Printf("[DEBUG] Validating patched threat model: %+v\n", patched)

	// 1. Ensure ID is not changed
	if patched.Id != original.Id {
		return fmt.Errorf("cannot change threat model ID")
	}

	// 2. Check if user has the owner role (either by being the owner or having the owner role in authorization)
	hasOwnerRole := (original.Owner == userName)
	if !hasOwnerRole {
		for _, auth := range original.Authorization {
			if auth.Subject == userName && auth.Role == RoleOwner {
				hasOwnerRole = true
				break
			}
		}
	}

	// Only users with owner role can change the owner field
	if !hasOwnerRole && patched.Owner != original.Owner {
		return fmt.Errorf("only the owner can transfer ownership")
	}

	// 3. Ensure creation date is not changed
	if !patched.CreatedAt.Equal(original.CreatedAt) {
		return fmt.Errorf("creation timestamp cannot be modified")
	}

	// 4. Validate required fields
	if patched.Name == "" {
		return fmt.Errorf("name is required")
	}

	// 5. Validate authorization entries
	for _, auth := range patched.Authorization {
		if auth.Subject == "" {
			return fmt.Errorf("authorization subject cannot be empty")
		}
	}

	// According to the new rules, we don't need to check that:
	// - The owner field needs to match an entry in authorization
	// - Multiple owner roles are not allowed

	return nil
}

// Helper functions for threat model patching

// getExistingThreatModel retrieves the existing threat model from context or store
func (h *ThreatModelHandler) getExistingThreatModel(c *gin.Context, id string) (ThreatModel, error) {
	var zero ThreatModel

	// Try to get from context first (set by middleware)
	existingTMValue, exists := c.Get("threatModel")
	if exists {
		if tm, ok := existingTMValue.(ThreatModel); ok {
			return tm, nil
		}
	}

	// If not in context, fetch it directly
	tm, err := ThreatModelStore.Get(id)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Threat model not found",
		}
	}

	return tm, nil
}

// preserveThreatModelCriticalFields preserves critical fields that shouldn't change during patching
func (h *ThreatModelHandler) preserveThreatModelCriticalFields(modified, original ThreatModel) ThreatModel {
	// Preserve original timestamps and ID to avoid JSON marshaling precision issues
	modified.CreatedAt = original.CreatedAt
	modified.Id = original.Id
	return modified
}

// applyThreatModelBusinessRules applies threat model-specific business rules
func (h *ThreatModelHandler) applyThreatModelBusinessRules(modifiedTM *ThreatModel, existingTM ThreatModel, ownerChanging, authChanging bool) error {
	// Check for duplicate authorization subjects
	if authChanging || ownerChanging {
		if err := ValidateDuplicateSubjects(modifiedTM.Authorization); err != nil {
			return err
		}
	}

	// Custom rule 1: If owner is changing, add original owner to authorization with owner role
	if ownerChanging {
		modifiedTM.Authorization = ApplyOwnershipTransferRule(modifiedTM.Authorization, existingTM.Owner, modifiedTM.Owner)
	}

	return nil
}
