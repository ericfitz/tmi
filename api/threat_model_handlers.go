package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// ThreatModelHandler provides handlers for threat model operations
type ThreatModelHandler struct {
	// WebSocket hub for collaboration sessions
	wsHub *WebSocketHub
}

// NewThreatModelHandler creates a new threat model handler
func NewThreatModelHandler(wsHub *WebSocketHub) *ThreatModelHandler {
	return &ThreatModelHandler{
		wsHub: wsHub,
	}
}

// GetThreatModels returns a list of threat models
func (h *ThreatModelHandler) GetThreatModels(c *gin.Context) {
	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Parse filter parameters
	filters := parseThreatModelFilters(c)

	// Get username from JWT claim
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For listing endpoints, we allow unauthenticated users but return empty results
		userEmail = ""
	}

	// Get user provider ID, internal UUID, IdP and groups from context for group-based authorization
	userProviderID := ""
	if providerID, exists := c.Get("userID"); exists {
		userProviderID, _ = providerID.(string)
	}

	userInternalUUID := ""
	if internalUUID, exists := c.Get("userInternalUUID"); exists {
		userInternalUUID, _ = internalUUID.(string)
	}

	userIdP := ""
	if idp, exists := c.Get("userIdP"); exists {
		userIdP, _ = idp.(string)
	}

	var userGroups []string
	if groups, exists := c.Get("userGroups"); exists {
		userGroups, _ = groups.([]string)
	}

	// Filter by user access using authorization utilities with group support
	filter := func(tm ThreatModel) bool {
		// If no user is authenticated, only show public threat models (if any)
		if userEmail == "" {
			return false
		}

		// Create authorization data for the threat model
		authData := AuthorizationData{
			Type:          AuthTypeTMI10,
			Owner:         tm.Owner,
			Authorization: tm.Authorization,
		}

		// Check if user has at least reader access (including group-based access like "everyone")
		return AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleReader, authData)
	}

	// Get threat models from store with filtering and counts
	modelsWithCounts, total := ThreatModelStore.ListWithCounts(offset, limit, filter, filters)

	// Convert to TMListItems for API response
	items := make([]TMListItem, 0, len(modelsWithCounts))
	for _, tmWithCounts := range modelsWithCounts {
		tm := tmWithCounts.ThreatModel
		// Set default framework if empty
		framework := tm.ThreatModelFramework
		if framework == "" {
			framework = "STRIDE" // Default fallback
		}

		var createdAt time.Time
		if tm.CreatedAt != nil {
			createdAt = *tm.CreatedAt
		}
		var modifiedAt time.Time
		if tm.ModifiedAt != nil {
			modifiedAt = *tm.ModifiedAt
		}
		// Get created_by User (dereference pointer, use empty User if nil)
		var createdBy User
		if tm.CreatedBy != nil {
			createdBy = *tm.CreatedBy
		}

		items = append(items, TMListItem{
			Id:                   tm.Id,
			Name:                 tm.Name,
			Description:          tm.Description,
			CreatedAt:            createdAt,
			ModifiedAt:           modifiedAt,
			Owner:                tm.Owner,
			CreatedBy:            createdBy,
			SecurityReviewer:     tm.SecurityReviewer,
			ThreatModelFramework: framework,
			IssueUri:             tm.IssueUri,
			Status:               tm.Status,
			StatusUpdated:        tm.StatusUpdated,
			// Count fields from database
			DocumentCount: tmWithCounts.DocumentCount,
			RepoCount:     tmWithCounts.SourceCount,
			DiagramCount:  tmWithCounts.DiagramCount,
			ThreatCount:   tmWithCounts.ThreatCount,
			NoteCount:     tmWithCounts.NoteCount,
			AssetCount:    tmWithCounts.AssetCount,
		})
	}

	// Return wrapped response with pagination metadata
	c.JSON(http.StatusOK, ListThreatModelsResponse{
		ThreatModels: items,
		Total:        total,
		Limit:        limit,
		Offset:       offset,
	})
}

// GetThreatModelByID retrieves a specific threat model
func (h *ThreatModelHandler) GetThreatModelByID(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("threat_model_id")

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
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Check authorization using new utilities with group support
	hasAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleReader)
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
		Name                 string          `json:"name" binding:"required"`
		Description          *string         `json:"description,omitempty"`
		ThreatModelFramework *string         `json:"threat_model_framework,omitempty"`
		IssueUri             *string         `json:"issue_uri,omitempty"`
		IsConfidential       *bool           `json:"is_confidential,omitempty"`
		SecurityReviewer     *User           `json:"security_reviewer,omitempty"`
		Metadata             *[]Metadata     `json:"metadata,omitempty"`
		Authorization        []Authorization `json:"authorization,omitempty"`
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["threat_model_create"]
	request, err := ValidateAndParseRequest[CreateThreatModelRequest](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get user identity from JWT claims
	userEmail, providerID, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get user identity provider from context
	userIdpInterface, _ := c.Get("userIdP")
	userIdp, _ := userIdpInterface.(string)
	if userIdp == "" {
		userIdp = "unknown" // Fallback
	}

	// Get user display name from context
	userDisplayNameInterface, _ := c.Get("userDisplayName")
	userDisplayName, _ := userDisplayNameInterface.(string)
	if userDisplayName == "" {
		userDisplayName = userEmail // Fallback to email
	}

	// Create new threat model
	now := time.Now().UTC()
	threatIDs := []Threat{}

	// Strip response-only fields from authorization entries before validation
	request.Authorization = StripResponseOnlyAuthFields(request.Authorization)

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

	// Create authorizations array with owner as first entry
	authorizations := []Authorization{
		{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      userIdp,
			ProviderId:    providerID, // Use OAuth provider ID from JWT "sub" claim
			Role:          RoleOwner,
		},
	}

	// Add any additional authorization subjects from the request
	authorizations = append(authorizations, request.Authorization...)

	// Create User object for owner and created_by
	userObj := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      userIdp,
		ProviderId:    providerID, // Use OAuth provider ID from JWT "sub" claim
		DisplayName:   userDisplayName,
		Email:         openapi_types.Email(userEmail),
	}

	// Set metadata - use provided value or default to empty array
	metadata := &[]Metadata{}
	if request.Metadata != nil {
		metadata = request.Metadata
	}

	// Set threat_model_framework - use provided value or default to empty string
	var framework string
	if request.ThreatModelFramework != nil {
		framework = *request.ThreatModelFramework
	}

	tm := ThreatModel{
		Name:                 request.Name,
		Description:          request.Description,
		ThreatModelFramework: framework,
		IssueUri:             request.IssueUri,
		IsConfidential:       request.IsConfidential,
		SecurityReviewer:     request.SecurityReviewer,
		CreatedAt:            &now,
		ModifiedAt:           &now,
		Owner:                userObj,
		CreatedBy:            &userObj,
		Authorization:        authorizations,
		Metadata:             metadata,
		Threats:              &threatIDs,
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
		slogging.Get().WithContext(c).Error("Failed to create threat model: %v", err)

		// Check if this is a foreign key constraint violation (stale user session)
		if isForeignKeyConstraintError(err) {
			// This indicates the user's JWT token is valid but they no longer exist in the database
			// This happens when user account is deleted but JWT hasn't expired yet
			slogging.Get().WithContext(c).Warn("Foreign key constraint violation for user %s - invalidating session", userEmail)

			// Try to blacklist the token to prevent future use
			if tokenStr, err := extractTokenFromRequest(c); err == nil {
				blacklistTokenIfAvailable(c, tokenStr, userEmail)
			}

			HandleRequestError(c, UnauthorizedError("Your session is no longer valid. Please log in again."))
			return
		}

		HandleRequestError(c, ServerError("Failed to create threat model"))
		return
	}

	// Counts are now calculated dynamically - no need to initialize

	// Broadcast notification about new threat model
	BroadcastThreatModelCreated(userEmail, createdTM.Id.String(), createdTM.Name)

	// Emit event for webhook subscriptions
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:     EventThreatModelCreated,
			ThreatModelID: createdTM.Id.String(),
			ResourceID:    createdTM.Id.String(),
			ResourceType:  "threat_model",
			OwnerID:       GetOwnerInternalUUID(c.Request.Context(), createdTM.Owner.Provider, createdTM.Owner.ProviderId),
			Data: map[string]interface{}{
				"name":        createdTM.Name,
				"description": createdTM.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(c.Request.Context(), payload)
	}

	// Set the Location header
	c.Header("Location", "/threat_models/"+createdTM.Id.String())
	c.JSON(http.StatusCreated, createdTM)
}

// UpdateThreatModel fully updates a threat model
func (h *ThreatModelHandler) UpdateThreatModel(c *gin.Context) {
	// Define allowed fields for PUT requests - excludes calculated and read-only fields
	// Per OpenAPI spec (ThreatModelInput), only 'name' is required
	type UpdateThreatModelRequest struct {
		Name                 string          `json:"name" binding:"required"`
		Description          *string         `json:"description,omitempty"`
		Owner                *string         `json:"owner,omitempty"` // Optional: if not provided, preserves existing owner
		ThreatModelFramework *string         `json:"threat_model_framework,omitempty"`
		IssueUri             *string         `json:"issue_uri,omitempty"`
		SecurityReviewer     *User           `json:"security_reviewer,omitempty"`
		Authorization        []Authorization `json:"authorization,omitempty"`
		Metadata             *[]Metadata     `json:"metadata,omitempty"`
	}

	// Parse ID from URL parameter
	id := c.Param("threat_model_id")
	slogging.Get().WithContext(c).Debug("[HANDLER] UpdateThreatModel called for ID: %s", id)

	// Parse and validate request body using OpenAPI validation
	var request UpdateThreatModelRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	slogging.Get().WithContext(c).Debug("[HANDLER] Successfully parsed request: %+v", request)

	// Get username from JWT claim
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Only validate authorization if provided
	if request.Authorization != nil {
		// Strip response-only fields from authorization entries before validation
		request.Authorization = StripResponseOnlyAuthFields(request.Authorization)

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
	}

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

	// Determine owner: use provided owner if specified, otherwise preserve existing
	owner := tm.Owner
	if request.Owner != nil && *request.Owner != "" {
		// Owner is being changed - convert string to User object
		owner = User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "tmi", // TODO: Get provider from auth context
			ProviderId:    *request.Owner,
			DisplayName:   *request.Owner,
			Email:         openapi_types.Email(*request.Owner),
		}
	}

	// Determine threat_model_framework: use provided value or preserve existing
	framework := tm.ThreatModelFramework
	if request.ThreatModelFramework != nil {
		framework = *request.ThreatModelFramework
	}

	// Determine authorization: use provided value or preserve existing
	authorization := tm.Authorization
	if request.Authorization != nil {
		authorization = request.Authorization
	}

	// Determine metadata: use provided value or preserve existing
	metadata := tm.Metadata
	if request.Metadata != nil {
		metadata = request.Metadata
	}

	// Determine security_reviewer: use provided value or preserve existing
	securityReviewer := tm.SecurityReviewer
	if request.SecurityReviewer != nil {
		securityReviewer = request.SecurityReviewer
	}

	// Build full threat model from request
	updatedTM := ThreatModel{
		Id:                   &uuid,
		Name:                 request.Name,
		Description:          request.Description,
		Owner:                owner,
		SecurityReviewer:     securityReviewer,
		ThreatModelFramework: framework,
		IssueUri:             request.IssueUri,
		IsConfidential:       tm.IsConfidential, // Immutable after creation
		Authorization:        authorization,
		Metadata:             metadata,
		// Preserve server-controlled fields
		CreatedAt:  tm.CreatedAt,
		ModifiedAt: func() *time.Time { now := time.Now().UTC(); return &now }(),
		CreatedBy:  tm.CreatedBy,
		// Preserve sub-entity arrays (managed separately)
		Diagrams:     tm.Diagrams,
		Documents:    tm.Documents,
		Threats:      tm.Threats,
		Repositories: tm.Repositories,
	}

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleWriter)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasWriteAccess {
		HandleRequestError(c, ForbiddenError("Insufficient permissions to update this threat model"))
		return
	}

	// Check if user has owner access for sensitive fields
	ownerChanging := updatedTM.Owner.ProviderId != "" && updatedTM.Owner.ProviderId != tm.Owner.ProviderId
	authChanging := (len(updatedTM.Authorization) > 0) && (!authorizationEqual(updatedTM.Authorization, tm.Authorization))

	if ownerChanging || authChanging {
		hasOwnerAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleOwner)
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
	}

	// Apply ownership transfer rule whenever owner is changing
	if ownerChanging {
		updatedTM.Authorization = ApplyOwnershipTransferRule(updatedTM.Authorization, tm.Owner.ProviderId, updatedTM.Owner.ProviderId)
	}

	// Update in store
	if err := ThreatModelStore.Update(id, updatedTM); err != nil {
		slogging.Get().WithContext(c).Error("Failed to update threat model %s in store (user: %s, name: %s): %v", id, userEmail, updatedTM.Name, err)
		HandleRequestError(c, ServerError("Failed to update threat model"))
		return
	}

	// Counts are now calculated dynamically - no need to update

	// Broadcast notification about updated threat model
	BroadcastThreatModelUpdated(userEmail, updatedTM.Id.String(), updatedTM.Name)

	// Emit event for webhook subscriptions
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:     EventThreatModelUpdated,
			ThreatModelID: updatedTM.Id.String(),
			ResourceID:    updatedTM.Id.String(),
			ResourceType:  "threat_model",
			OwnerID:       GetOwnerInternalUUID(c.Request.Context(), updatedTM.Owner.Provider, updatedTM.Owner.ProviderId),
			Data: map[string]interface{}{
				"name":        updatedTM.Name,
				"description": updatedTM.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(c.Request.Context(), payload)
	}

	c.JSON(http.StatusOK, updatedTM)
}

// PatchThreatModel partially updates a threat model
func (h *ThreatModelHandler) PatchThreatModel(c *gin.Context) {
	id := c.Param("threat_model_id")
	slogging.Get().WithContext(c).Debug("[HANDLER] PatchThreatModel called for ID: %s", id)

	// Phase 1: Parse request and validate user
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	slogging.Get().WithContext(c).Debug("[HANDLER] Successfully parsed PATCH request with %d operations", len(operations))

	// Validate patch operations against prohibited fields
	prohibitedPaths := []string{
		"/id", "/created_at", "/modified_at", "/created_by",
		"/diagrams", "/documents", "/threats", "/sourceCode",
		"/is_confidential",
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

	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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

	// Phase 3.5: Enrich authorization entries (sparse -> complete)
	// This happens AFTER patch operations but BEFORE validation

	// Strip response-only fields (like display_name) from authorization entries
	// This allows clients to send back authorization data they received from the server
	modifiedTM.Authorization = StripResponseOnlyAuthFields(modifiedTM.Authorization)

	// First, validate sparse entries (provider + one of provider_id/email)
	if err := ValidateSparseAuthorizationEntries(modifiedTM.Authorization); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Note: Duplicate validation is NOT done here for PATCH operations.
	// Applying multiple patches for the same user is allowed - the database ON CONFLICT
	// will handle it gracefully by updating the role to the latest value.
	// This allows API callers to modify user roles incrementally without error.

	// Get database connection for enrichment
	var db *gorm.DB
	if dbStore, ok := ThreatModelStore.(*GormThreatModelStore); ok {
		db = dbStore.GetDB()
	} else {
		// Fallback for test environments without database
		slogging.Get().WithContext(c).Warn("ThreatModelStore is not a database store, skipping enrichment")
	}

	// Enrich authorization entries if database is available
	if db != nil {
		slogging.Get().WithContext(c).Debug("[HANDLER] Enriching %d authorization entries before save", len(modifiedTM.Authorization))
		for i, auth := range modifiedTM.Authorization {
			slogging.Get().WithContext(c).Debug("[HANDLER]   Before enrich %d: type=%s, provider=%s, provider_id=%s, role=%s",
				i, auth.PrincipalType, auth.Provider, auth.ProviderId, auth.Role)
		}
		if err := EnrichAuthorizationList(c.Request.Context(), db, modifiedTM.Authorization); err != nil {
			HandleRequestError(c, err)
			return
		}
		for i, auth := range modifiedTM.Authorization {
			slogging.Get().WithContext(c).Debug("[HANDLER]   After enrich %d: type=%s, provider=%s, provider_id=%s, role=%s",
				i, auth.PrincipalType, auth.Provider, auth.ProviderId, auth.Role)
		}
	}

	// Phase 3.6: Deduplicate authorization entries
	// For in-memory storage: This ensures that patching the same user multiple times
	// results in a single entry with the latest role (mimics database ON CONFLICT behavior).
	// For database storage: This is a no-op since the database handles it, but it
	// provides consistent behavior and cleaner response data.
	modifiedTM.Authorization = DeduplicateAuthorizationList(modifiedTM.Authorization)

	// Phase 4: Preserve critical fields and validate authorization
	modifiedTM = h.preserveThreatModelCriticalFields(modifiedTM, existingTM)

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccessFromContext(c, userEmail, existingTM, RoleWriter)
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
		hasOwnerAccess, err := CheckResourceAccessFromContext(c, userEmail, existingTM, RoleOwner)
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
	if err := h.applyThreatModelBusinessRules(&modifiedTM, existingTM, ownerChanging); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Phase 6: Validate the patched threat model
	if err := ValidatePatchedEntity(existingTM, modifiedTM, userEmail, validatePatchedThreatModel); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Final update of timestamps
	now := time.Now().UTC()
	modifiedTM.ModifiedAt = &now

	// Update in store
	if err := ThreatModelStore.Update(id, modifiedTM); err != nil {
		// Log the actual error for debugging
		slogging.Get().WithContext(c).Error("Failed to update threat model %s: %v", id, err)

		// Check if this is a foreign key constraint violation
		if isForeignKeyConstraintError(err) {
			// This indicates one of the users in the authorization list doesn't exist in the database
			slogging.Get().WithContext(c).Warn("Foreign key constraint violation when updating threat model %s - one or more users in authorization list do not exist", id)
			HandleRequestError(c, InvalidInputError("One or more users in the authorization list do not exist. Users must log in at least once before they can be added to a threat model."))
			return
		}

		// Generic server error for other cases
		HandleRequestError(c, ServerError("Failed to update threat model"))
		return
	}

	// Counts are now calculated dynamically - no need to update

	// Broadcast notification about updated threat model
	BroadcastThreatModelUpdated(userEmail, modifiedTM.Id.String(), modifiedTM.Name)

	// Emit event for webhook subscriptions
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:     EventThreatModelUpdated,
			ThreatModelID: modifiedTM.Id.String(),
			ResourceID:    modifiedTM.Id.String(),
			ResourceType:  "threat_model",
			OwnerID:       GetOwnerInternalUUID(c.Request.Context(), modifiedTM.Owner.Provider, modifiedTM.Owner.ProviderId),
			Data: map[string]interface{}{
				"name":        modifiedTM.Name,
				"description": modifiedTM.Description,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(c.Request.Context(), payload)
	}

	c.JSON(http.StatusOK, modifiedTM)
}

// DeleteThreatModel deletes a threat model
func (h *ThreatModelHandler) DeleteThreatModel(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("threat_model_id")

	// Validate ID format
	if _, err := ParseUUID(id); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get the user making the request
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
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
	hasOwnerAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleOwner)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasOwnerAccess {
		HandleRequestError(c, ForbiddenError("Only the owner can delete a threat model"))
		return
	}

	// Check if any diagrams in this threat model have active collaboration sessions
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
				if h.wsHub.HasActiveSession(dfdDiag.Id.String()) {
					HandleRequestError(c, ConflictError("Cannot delete threat model while a diagram has an active collaboration session. Please end all collaboration sessions first."))
					return
				}
			}
		}
	}

	// Delete from store
	if err := ThreatModelStore.Delete(id); err != nil {
		slogging.Get().WithContext(c).Error("Failed to delete threat model %s from store (user: %s, name: %s): %v", id, userEmail, tm.Name, err)
		HandleRequestError(c, ServerError("Failed to delete threat model"))
		return
	}

	// Broadcast notification about deleted threat model
	BroadcastThreatModelDeleted(userEmail, tm.Id.String(), tm.Name)

	// Emit event for webhook subscriptions
	if GlobalEventEmitter != nil {
		payload := EventPayload{
			EventType:     EventThreatModelDeleted,
			ThreatModelID: tm.Id.String(),
			ResourceID:    tm.Id.String(),
			ResourceType:  "threat_model",
			OwnerID:       GetOwnerInternalUUID(c.Request.Context(), tm.Owner.Provider, tm.Owner.ProviderId),
			Data: map[string]interface{}{
				"name": tm.Name,
			},
		}
		_ = GlobalEventEmitter.EmitEvent(c.Request.Context(), payload)
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

// parseThreatModelFilters parses filter query parameters from the request
func parseThreatModelFilters(c *gin.Context) *ThreatModelFilters {
	filters := &ThreatModelFilters{}
	hasFilters := false

	if owner := c.Query("owner"); owner != "" {
		filters.Owner = &owner
		hasFilters = true
	}
	if name := c.Query("name"); name != "" {
		filters.Name = &name
		hasFilters = true
	}
	if description := c.Query("description"); description != "" {
		filters.Description = &description
		hasFilters = true
	}
	if issueUri := c.Query("issue_uri"); issueUri != "" {
		filters.IssueUri = &issueUri
		hasFilters = true
	}
	if createdAfter := c.Query("created_after"); createdAfter != "" {
		if t, err := time.Parse(time.RFC3339, createdAfter); err == nil {
			filters.CreatedAfter = &t
			hasFilters = true
		}
	}
	if createdBefore := c.Query("created_before"); createdBefore != "" {
		if t, err := time.Parse(time.RFC3339, createdBefore); err == nil {
			filters.CreatedBefore = &t
			hasFilters = true
		}
	}
	if modifiedAfter := c.Query("modified_after"); modifiedAfter != "" {
		if t, err := time.Parse(time.RFC3339, modifiedAfter); err == nil {
			filters.ModifiedAfter = &t
			hasFilters = true
		}
	}
	if modifiedBefore := c.Query("modified_before"); modifiedBefore != "" {
		if t, err := time.Parse(time.RFC3339, modifiedBefore); err == nil {
			filters.ModifiedBefore = &t
			hasFilters = true
		}
	}
	if status := c.Query("status"); status != "" {
		filters.Status = &status
		hasFilters = true
	}
	if statusUpdatedAfter := c.Query("status_updated_after"); statusUpdatedAfter != "" {
		if t, err := time.Parse(time.RFC3339, statusUpdatedAfter); err == nil {
			filters.StatusUpdatedAfter = &t
			hasFilters = true
		}
	}
	if statusUpdatedBefore := c.Query("status_updated_before"); statusUpdatedBefore != "" {
		if t, err := time.Parse(time.RFC3339, statusUpdatedBefore); err == nil {
			filters.StatusUpdatedBefore = &t
			hasFilters = true
		}
	}

	// Return nil if no filters were provided to avoid unnecessary processing
	if !hasFilters {
		return nil
	}
	return filters
}

// Note: Using the PatchOperation type defined in types.go

// getFieldErrorMessage returns a descriptive error message for prohibited fields
func getFieldErrorMessage(field string) string {
	switch field {
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
		return "Diagrams must be managed via the /threat_models/:threat_model_id/diagrams sub-entity endpoints."
	case "documents":
		return "Documents must be managed via the /threat_models/:threat_model_id/documents sub-entity endpoints."
	case "threats":
		return "Threats must be managed via the /threat_models/:threat_model_id/threats sub-entity endpoints."
	case "sourceCode":
		return "Source code entries must be managed via the /threat_models/:threat_model_id/sources sub-entity endpoints."
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
		mapA[auth.ProviderId] = auth.Role
	}

	for _, auth := range b {
		mapB[auth.ProviderId] = auth.Role
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
func validatePatchedThreatModel(original, patched ThreatModel, userEmail string) error {
	// Add debug logging
	slogging.Get().Debug("Validating patched threat model: %+v", patched)

	// 1. Ensure ID is not changed
	if patched.Id != original.Id {
		return fmt.Errorf("cannot change threat model ID")
	}

	// 2. Check if user has the owner role (either by being the owner or having the owner role in authorization)
	hasOwnerRole := (original.Owner.ProviderId == userEmail)
	if !hasOwnerRole {
		for _, auth := range original.Authorization {
			if auth.ProviderId == userEmail && auth.Role == RoleOwner {
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
	if !patched.CreatedAt.Equal(*original.CreatedAt) {
		return fmt.Errorf("creation timestamp cannot be modified")
	}

	// 4. Validate required fields
	if patched.Name == "" {
		return fmt.Errorf("name is required")
	}

	// 5. Validate authorization entries (after enrichment)
	// Authorization entries must have either provider_id OR email to identify the subject
	// Groups use provider_id, users can use either provider_id (OAuth sub) or email
	for _, auth := range patched.Authorization {
		hasProviderID := auth.ProviderId != ""
		hasEmail := auth.Email != nil && string(*auth.Email) != ""

		if !hasProviderID && !hasEmail {
			return fmt.Errorf("authorization subject must have either provider_id or email")
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
	modified.IsConfidential = original.IsConfidential // Immutable after creation
	return modified
}

// applyThreatModelBusinessRules applies threat model-specific business rules
func (h *ThreatModelHandler) applyThreatModelBusinessRules(modifiedTM *ThreatModel, existingTM ThreatModel, ownerChanging bool) error {
	// Note: Post-enrichment duplicate detection removed.
	// The database ON CONFLICT will handle duplicates gracefully after internal_uuid resolution.
	// Pre-enrichment validation already caught obvious client mistakes.

	// Custom rule 1: If owner is changing, add original owner to authorization with owner role
	if ownerChanging {
		modifiedTM.Authorization = ApplyOwnershipTransferRule(modifiedTM.Authorization, existingTM.Owner.ProviderId, modifiedTM.Owner.ProviderId)
	}

	return nil
}
