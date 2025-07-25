package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
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
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Filter by user access
	filter := func(tm ThreatModel) bool {
		// If no user is authenticated, only show public threat models (if any)
		if userName == "" {
			return false
		}

		// Check if the user is the owner
		if tm.Owner == userName {
			return true
		}

		// Check if the user has access through authorization
		for _, auth := range tm.Authorization {
			if auth.Subject == userName {
				return true
			}
		}

		return false
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

	// Get threat model from store
	tm, err := ThreatModelStore.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Authorization is handled by middleware
	// The middleware has already verified the user has appropriate access
	c.JSON(http.StatusOK, tm)
}

// CreateThreatModel creates a new threat model
func (h *ThreatModelHandler) CreateThreatModel(c *gin.Context) {
	var request struct {
		Name          string          `json:"name" binding:"required"`
		Description   *string         `json:"description,omitempty"`
		Authorization []Authorization `json:"authorization,omitempty"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: err.Error(),
		})
		return
	}

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok || userName == "" {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return
	}

	// Create new threat model
	now := time.Now().UTC()
	threatIDs := []Threat{}

	// Check for duplicate authorization subjects in the request itself first
	if len(request.Authorization) > 0 {
		authMap := make(map[string]bool)
		for _, auth := range request.Authorization {
			if _, exists := authMap[auth.Subject]; exists {
				c.JSON(http.StatusBadRequest, Error{
					Error:   "invalid_input",
					ErrorDescription: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
				})
				return
			}
			authMap[auth.Subject] = true
		}
	}

	// Create authorizations array with owner as first entry
	authorizations := []Authorization{
		{
			Subject: userName,
			Role:    RoleOwner,
		},
	}

	// Add any additional authorization subjects from the request, checking for duplicates with owner
	if len(request.Authorization) > 0 {
		for _, auth := range request.Authorization {
			if auth.Subject == userName {
				c.JSON(http.StatusBadRequest, Error{
					Error:   "invalid_input",
					ErrorDescription: fmt.Sprintf("Duplicate authorization subject with owner: %s", auth.Subject),
				})
				return
			}
			authorizations = append(authorizations, auth)
		}
	}

	tm := ThreatModel{
		Name:          request.Name,
		Description:   request.Description,
		CreatedAt:     now,
		ModifiedAt:    now,
		Owner:         userName,
		Authorization: authorizations,
		Metadata:      &[]Metadata{},
		Threats:       &threatIDs,
	}

	// Add to store
	idSetter := func(tm ThreatModel, id string) ThreatModel {
		uuid, _ := ParseUUID(id)
		tm.Id = uuid
		return tm
	}

	createdTM, err := ThreatModelStore.Create(tm, idSetter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to create threat model",
		})
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

	// Copy the request body for debugging before binding
	bodyBytes, err := c.GetRawData()
	if err != nil {
		fmt.Printf("[DEBUG HANDLER] Error reading request body: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: "Failed to read request body: " + err.Error(),
		})
		return
	}

	// Log the raw request body
	if len(bodyBytes) > 0 {
		fmt.Printf("[DEBUG HANDLER] Request body: %s\n", string(bodyBytes))
		// Reset the body for later binding
		c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
	} else {
		fmt.Printf("[DEBUG HANDLER] Empty request body received\n")
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: "Request body is empty",
		})
		return
	}

	var request ThreatModel
	if err := c.ShouldBindJSON(&request); err != nil {
		fmt.Printf("[DEBUG HANDLER] JSON binding error: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: err.Error(),
		})
		return
	}

	fmt.Printf("[DEBUG HANDLER] Successfully parsed request: %+v\n", request)

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	_, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return
	}

	// Get existing threat model - should be available from middleware
	existingTM, exists := c.Get("threatModel")
	if !exists {
		// If not in context, fetch it directly
		var err error
		existingTM, err = ThreatModelStore.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, Error{
				Error:   "not_found",
				ErrorDescription: "Threat model not found",
			})
			return
		}
	}

	tm, ok := existingTM.(ThreatModel)
	if !ok {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to process threat model",
		})
		return
	}

	// Ensure ID in the URL matches the one in the body
	uuid, err := ParseUUID(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_id",
			ErrorDescription: "Invalid ID format",
		})
		return
	}
	request.Id = uuid

	// Preserve creation time but update modification time
	request.CreatedAt = tm.CreatedAt
	request.ModifiedAt = time.Now().UTC()

	// Get user role from context - should be set by middleware
	roleValue, exists := c.Get("userRole")
	userRole, ok := roleValue.(Role)
	if !exists || !ok {
		fmt.Printf("[DEBUG HANDLER] User role not found in context\n")
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to determine user role",
		})
		return
	}

	fmt.Printf("[DEBUG HANDLER] User role: %s\n", userRole)
	fmt.Printf("[DEBUG HANDLER] Checking authorization for changes to owner/auth fields\n")

	// Rule 2: Writers cannot modify owner or authorization fields
	ownerChanging := request.Owner != "" && request.Owner != tm.Owner
	// If writer is trying to change auth field, that's a problem
	authChanging := (len(request.Authorization) > 0) && (!authorizationEqual(request.Authorization, tm.Authorization))

	// For writer access check, we need to be more stringent - even presence of the auth field is an issue
	if userRole == RoleWriter && len(request.Authorization) > 0 {
		authChanging = true
	}

	if (ownerChanging || authChanging) && userRole != RoleOwner {
		fmt.Printf("[DEBUG HANDLER] Access denied: non-owner trying to change owner/auth fields\n")
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "Only the owner can change ownership or authorization",
		})
		return
	}

	// Rule 6: Check for duplicate authorization subjects
	fmt.Printf("[DEBUG HANDLER] Checking for duplicate authorization subjects\n")
	subjectMap := make(map[string]bool)
	for _, auth := range request.Authorization {
		if _, exists := subjectMap[auth.Subject]; exists {
			fmt.Printf("[DEBUG HANDLER] Duplicate subject found: %s\n", auth.Subject)
			c.JSON(http.StatusBadRequest, Error{
				Error:   "invalid_input",
				ErrorDescription: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
			})
			return
		}
		subjectMap[auth.Subject] = true
	}

	// Rule 5: If owner is changing, add original owner to authorization with owner role
	if ownerChanging {
		fmt.Printf("[DEBUG HANDLER] Owner changing from %s to %s\n", tm.Owner, request.Owner)
		// Check if the original owner is already in the authorization list
		originalOwnerFound := false
		for i := range request.Authorization {
			if request.Authorization[i].Subject == tm.Owner {
				fmt.Printf("[DEBUG HANDLER] Original owner found in auth list, ensuring owner role\n")
				// Make sure the original owner has the Owner role
				request.Authorization[i].Role = RoleOwner
				originalOwnerFound = true
				break
			}
		}

		// If the original owner isn't in the list, add them
		if !originalOwnerFound {
			fmt.Printf("[DEBUG HANDLER] Adding original owner to auth list with owner role\n")
			request.Authorization = append(request.Authorization, Authorization{
				Subject: tm.Owner,
				Role:    RoleOwner,
			})
		}
	}

	// Update in store
	if err := ThreatModelStore.Update(id, request); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to update threat model",
		})
		return
	}

	c.JSON(http.StatusOK, request)
}

// PatchThreatModel partially updates a threat model
func (h *ThreatModelHandler) PatchThreatModel(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")
	fmt.Printf("[DEBUG HANDLER] PatchThreatModel called for ID: %s\n", id)

	// Copy the request body for debugging before binding
	bodyBytes, err := c.GetRawData()
	if err != nil {
		fmt.Printf("[DEBUG HANDLER] Error reading PATCH request body: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: "Failed to read request body: " + err.Error(),
		})
		return
	}

	// Log the raw request body
	if len(bodyBytes) > 0 {
		fmt.Printf("[DEBUG HANDLER] PATCH request body: %s\n", string(bodyBytes))
		// Reset the body for later binding
		c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
	} else {
		fmt.Printf("[DEBUG HANDLER] Empty PATCH request body received\n")
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: "Request body is empty",
		})
		return
	}

	var operations []PatchOperation
	if err := c.ShouldBindJSON(&operations); err != nil {
		fmt.Printf("[DEBUG HANDLER] PATCH JSON binding error: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: "Invalid JSON Patch format: " + err.Error(),
		})
		return
	}

	fmt.Printf("[DEBUG HANDLER] Successfully parsed PATCH request with %d operations\n", len(operations))

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok || userName == "" {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return
	}

	// Get existing threat model - should be available from middleware
	existingTMValue, exists := c.Get("threatModel")
	if !exists {
		// If not in context, fetch it directly
		var err error
		existingTMValue, err = ThreatModelStore.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, Error{
				Error:   "not_found",
				ErrorDescription: "Threat model not found",
			})
			return
		}
	}

	existingTM, ok := existingTMValue.(ThreatModel)
	if !ok {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to process threat model",
		})
		return
	}

	// Get user role from context - should be set by middleware
	roleValue, exists := c.Get("userRole")
	userRole, ok := roleValue.(Role)

	// Convert operations to RFC6902 JSON Patch format
	patchBytes, err := convertOperationsToJSONPatch(operations)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_format",
			ErrorDescription: "Failed to convert patch operations: " + err.Error(),
		})
		return
	}

	// Convert threat model to JSON
	originalBytes, err := json.Marshal(existingTM)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to serialize threat model",
		})
		return
	}

	// Create patch object
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_patch",
			ErrorDescription: "Invalid JSON Patch: " + err.Error(),
		})
		return
	}

	// Apply patch
	modifiedBytes, err := patch.Apply(originalBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "patch_failed",
			ErrorDescription: "Failed to apply patch: " + err.Error(),
		})
		return
	}

	// Deserialize back into threat model
	var modifiedTM ThreatModel
	if err := json.Unmarshal(modifiedBytes, &modifiedTM); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to deserialize patched threat model",
		})
		return
	}

	// Check if owner or authorization is changing, which requires owner role
	ownerChanging := modifiedTM.Owner != existingTM.Owner
	authChanging := !authorizationEqual(existingTM.Authorization, modifiedTM.Authorization)

	if (ownerChanging || authChanging) && (!exists || !ok || userRole != RoleOwner) {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "Only the owner can change ownership or authorization",
		})
		return
	}

	// Check for duplicate authorization subjects
	if authChanging || ownerChanging {
		subjectMap := make(map[string]bool)
		for _, auth := range modifiedTM.Authorization {
			if _, exists := subjectMap[auth.Subject]; exists {
				c.JSON(http.StatusBadRequest, Error{
					Error:   "invalid_input",
					ErrorDescription: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
				})
				return
			}
			subjectMap[auth.Subject] = true
		}
	}

	// Custom rule 1: If owner is changing, add original owner to authorization with owner role
	if ownerChanging {
		// Check if the original owner is already in the authorization list
		originalOwnerFound := false
		for i := range modifiedTM.Authorization {
			if modifiedTM.Authorization[i].Subject == existingTM.Owner {
				// Make sure the original owner has the Owner role
				modifiedTM.Authorization[i].Role = RoleOwner
				originalOwnerFound = true
				break
			}
		}

		// If the original owner isn't in the list, add them
		if !originalOwnerFound {
			modifiedTM.Authorization = append(modifiedTM.Authorization, Authorization{
				Subject: existingTM.Owner,
				Role:    RoleOwner,
			})
		}
	}

	// Validate patched threat model
	if err := validatePatchedThreatModel(existingTM, modifiedTM, userName); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "validation_failed",
			ErrorDescription: err.Error(),
		})
		return
	}

	// Preserve the original ID
	modifiedTM.Id = existingTM.Id

	// Preserve creation time but update modification time
	modifiedTM.CreatedAt = existingTM.CreatedAt
	modifiedTM.ModifiedAt = time.Now().UTC()

	// Update in store
	if err := ThreatModelStore.Update(id, modifiedTM); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
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

	// Get the user making the request
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok || userName == "" {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return
	}

	// Get threat model directly
	tm, err := ThreatModelStore.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check authorization in handler for delete
	// Verify owner access directly to ensure test compatibility
	// For deletion, only the owner can delete the resource
	if tm.Owner != userName {
		// Check if the user has owner role in the authorization list
		hasOwnerRole := false
		for _, auth := range tm.Authorization {
			if auth.Subject == userName && auth.Role == RoleOwner {
				hasOwnerRole = true
				break
			}
		}

		if !hasOwnerRole {
			c.JSON(http.StatusForbidden, Error{
				Error:   "forbidden",
				ErrorDescription: "Only the owner can delete a threat model",
			})
			return
		}
	}

	// Delete from store
	if err := ThreatModelStore.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to delete threat model",
		})
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
