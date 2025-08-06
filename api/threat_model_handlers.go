package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

	// Get threat models from store with filtering and counts
	modelsWithCounts := ThreatModelStore.ListWithCounts(offset, limit, filter)

	// Convert to TMListItems for API response
	items := make([]TMListItem, 0, len(modelsWithCounts))
	for _, tmWithCounts := range modelsWithCounts {
		tm := tmWithCounts.ThreatModel
		// Convert framework to TMListItem enum
		var framework TMListItemThreatModelFramework
		if tm.ThreatModelFramework != "" {
			switch tm.ThreatModelFramework {
			case ThreatModelThreatModelFrameworkCIA:
				framework = TMListItemThreatModelFrameworkCIA
			case ThreatModelThreatModelFrameworkDIE:
				framework = TMListItemThreatModelFrameworkDIE
			case ThreatModelThreatModelFrameworkLINDDUN:
				framework = TMListItemThreatModelFrameworkLINDDUN
			case ThreatModelThreatModelFrameworkPLOT4ai:
				framework = TMListItemThreatModelFrameworkPLOT4ai
			case ThreatModelThreatModelFrameworkSTRIDE:
				framework = TMListItemThreatModelFrameworkSTRIDE
			default:
				framework = TMListItemThreatModelFrameworkSTRIDE // Default fallback
			}
		} else {
			framework = TMListItemThreatModelFrameworkSTRIDE // Default fallback
		}

		items = append(items, TMListItem{
			Id:                   tm.Id,
			Name:                 tm.Name,
			Description:          tm.Description,
			CreatedAt:            tm.CreatedAt,
			ModifiedAt:           tm.ModifiedAt,
			Owner:                tm.Owner,
			CreatedBy:            tm.CreatedBy,
			ThreatModelFramework: framework,
			IssueUrl:             tm.IssueUrl,
			// Count fields from database
			DocumentCount: tmWithCounts.DocumentCount,
			SourceCount:   tmWithCounts.SourceCount,
			DiagramCount:  tmWithCounts.DiagramCount,
			ThreatCount:   tmWithCounts.ThreatCount,
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

	// First, check for prohibited fields by parsing raw JSON
	var rawRequest map[string]interface{}
	if err := c.ShouldBindJSON(&rawRequest); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid JSON format"))
		return
	}

	// Check for calculated/prohibited fields
	prohibitedFields := []string{
		"document_count", "source_count", "diagram_count", "threat_count",
		"id", "created_at", "modified_at", "created_by", "owner",
		"diagrams", "documents", "threats", "sourceCode",
	}

	for _, field := range prohibitedFields {
		if _, exists := rawRequest[field]; exists {
			HandleRequestError(c, InvalidInputError(fmt.Sprintf(
				"Field '%s' is not allowed in POST requests. %s",
				field, getFieldErrorMessage(field))))
			return
		}
	}

	// Parse the validated raw request into our restricted struct
	var request CreateThreatModelRequest
	rawJSON, err := json.Marshal(rawRequest)
	if err != nil {
		HandleRequestError(c, InvalidInputError("Failed to process request"))
		return
	}

	if err := json.Unmarshal(rawJSON, &request); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request format: "+err.Error()))
		return
	}

	// Validate required fields manually
	if request.Name == "" {
		HandleRequestError(c, InvalidInputError("Field 'name' is required"))
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
		// Log the actual error for debugging
		fmt.Printf("[ERROR] Failed to create threat model: %v\n", err)
		HandleRequestError(c, ServerError("Failed to create threat model"))
		return
	}

	// Initialize counts for new threat model (all start at 0)
	if err := ThreatModelStore.UpdateCountsWithValues(createdTM.Id.String(), 0, 0, 0, 0); err != nil {
		// Log error but don't fail the operation
		fmt.Printf("[WARNING] Failed to initialize counts for threat model %s: %v\n", createdTM.Id.String(), err)
	}

	// Set the Location header
	c.Header("Location", "/threat_models/"+createdTM.Id.String())
	c.JSON(http.StatusCreated, createdTM)
}

// UpdateThreatModel fully updates a threat model
func (h *ThreatModelHandler) UpdateThreatModel(c *gin.Context) {
	// Define allowed fields for PUT requests - excludes calculated and read-only fields
	type UpdateThreatModelRequest struct {
		Name                 string                          `json:"name" binding:"required"`
		Description          *string                         `json:"description,omitempty"`
		Owner                string                          `json:"owner" binding:"required"`
		ThreatModelFramework ThreatModelThreatModelFramework `json:"threat_model_framework" binding:"required"`
		IssueUrl             *string                         `json:"issue_url,omitempty"`
		Authorization        []Authorization                 `json:"authorization" binding:"required"`
		Metadata             *[]Metadata                     `json:"metadata,omitempty"`
	}

	// Parse ID from URL parameter
	id := c.Param("id")
	fmt.Printf("[DEBUG HANDLER] UpdateThreatModel called for ID: %s\n", id)

	// First, check for prohibited fields by parsing raw JSON
	var rawRequest map[string]interface{}
	if err := c.ShouldBindJSON(&rawRequest); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid JSON format"))
		return
	}

	// Check for calculated/prohibited fields
	prohibitedFields := []string{
		"document_count", "source_count", "diagram_count", "threat_count",
		"id", "created_at", "modified_at", "created_by",
		"diagrams", "documents", "threats", "sourceCode",
	}

	for _, field := range prohibitedFields {
		if _, exists := rawRequest[field]; exists {
			HandleRequestError(c, InvalidInputError(fmt.Sprintf(
				"Field '%s' is not allowed in PUT requests. %s",
				field, getFieldErrorMessage(field))))
			return
		}
	}

	// Parse the validated raw request into our restricted struct
	var request UpdateThreatModelRequest
	rawJSON, err := json.Marshal(rawRequest)
	if err != nil {
		HandleRequestError(c, InvalidInputError("Failed to process request"))
		return
	}

	if err := json.Unmarshal(rawJSON, &request); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request format: "+err.Error()))
		return
	}

	// Validate required fields manually since we bypassed gin's binding
	if request.Name == "" {
		HandleRequestError(c, InvalidInputError("Field 'name' is required"))
		return
	}
	if request.Owner == "" {
		HandleRequestError(c, InvalidInputError("Field 'owner' is required"))
		return
	}
	if len(request.Authorization) == 0 {
		HandleRequestError(c, InvalidInputError("Field 'authorization' is required"))
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

	// Build full threat model from request
	updatedTM := ThreatModel{
		Id:                   &uuid,
		Name:                 request.Name,
		Description:          request.Description,
		Owner:                request.Owner,
		ThreatModelFramework: request.ThreatModelFramework,
		IssueUrl:             request.IssueUrl,
		Authorization:        request.Authorization,
		Metadata:             request.Metadata,
		// Preserve server-controlled fields
		CreatedAt:  tm.CreatedAt,
		ModifiedAt: time.Now().UTC(),
		CreatedBy:  tm.CreatedBy,
		// Preserve sub-entity arrays (managed separately)
		Diagrams:   tm.Diagrams,
		Documents:  tm.Documents,
		Threats:    tm.Threats,
		SourceCode: tm.SourceCode,
	}

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
	ownerChanging := updatedTM.Owner != "" && updatedTM.Owner != tm.Owner
	authChanging := (len(updatedTM.Authorization) > 0) && (!authorizationEqual(updatedTM.Authorization, tm.Authorization))

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
		if err := ValidateAuthorizationEntriesWithFormat(updatedTM.Authorization); err != nil {
			HandleRequestError(c, err)
			return
		}

		// Check for duplicate authorization subjects
		if err := ValidateDuplicateSubjects(updatedTM.Authorization); err != nil {
			HandleRequestError(c, err)
			return
		}

		// If owner is changing, apply ownership transfer rule
		if ownerChanging {
			updatedTM.Authorization = ApplyOwnershipTransferRule(updatedTM.Authorization, tm.Owner, updatedTM.Owner)
		}
	}

	// Count sub-entities from payload for PUT operations
	docCount, srcCount, diagCount, threatCount := ThreatModelStore.CountSubEntitiesFromPayload(updatedTM)

	// Update in store
	if err := ThreatModelStore.Update(id, updatedTM); err != nil {
		HandleRequestError(c, ServerError("Failed to update threat model"))
		return
	}

	// Update counts in database using the counted values from payload
	if err := ThreatModelStore.UpdateCountsWithValues(id, docCount, srcCount, diagCount, threatCount); err != nil {
		// Log error but don't fail the operation
		fmt.Printf("[WARNING] Failed to update counts for threat model %s: %v\n", id, err)
	}

	c.JSON(http.StatusOK, updatedTM)
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

	// Validate patch operations against prohibited fields
	prohibitedPaths := []string{
		"/document_count", "/source_count", "/diagram_count", "/threat_count",
		"/id", "/created_at", "/modified_at", "/created_by",
		"/diagrams", "/documents", "/threats", "/sourceCode",
	}

	for _, op := range operations {
		for _, prohibitedPath := range prohibitedPaths {
			if op.Path == prohibitedPath {
				fieldName := strings.TrimPrefix(prohibitedPath, "/")
				HandleRequestError(c, InvalidInputError(fmt.Sprintf(
					"Field '%s' is not allowed in PATCH requests. %s",
					fieldName, getFieldErrorMessage(fieldName))))
				return
			}
		}
	}

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

	// For PATCH operations, recompute absolute counts from database
	if err := ThreatModelStore.UpdateCounts(id); err != nil {
		// Log error but don't fail the operation
		fmt.Printf("[WARNING] Failed to update counts for threat model %s: %v\n", id, err)
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

// getFieldErrorMessage returns a descriptive error message for prohibited fields
func getFieldErrorMessage(field string) string {
	switch field {
	case "document_count", "source_count", "diagram_count", "threat_count":
		return "Count fields are calculated automatically and cannot be set directly."
	case "id":
		return "The ID is read-only and set by the server."
	case "created_at":
		return "Creation timestamp is read-only and set by the server."
	case "modified_at":
		return "Modification timestamp is managed automatically by the server."
	case "created_by":
		return "The creator field is read-only and set during creation."
	case "owner":
		return "The owner field is set automatically to the authenticated user during creation."
	case "diagrams":
		return "Diagrams must be managed via the /threat_models/:id/diagrams sub-entity endpoints."
	case "documents":
		return "Documents must be managed via the /threat_models/:id/documents sub-entity endpoints."
	case "threats":
		return "Threats must be managed via the /threat_models/:id/threats sub-entity endpoints."
	case "sourceCode":
		return "Source code entries must be managed via the /threat_models/:id/sources sub-entity endpoints."
	default:
		return "This field is not allowed in this request."
	}
}

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
