package api

import (
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ThreatModelDiagramHandler provides handlers for diagram operations within threat models
type ThreatModelDiagramHandler struct {
	// WebSocket hub for collaboration sessions
	wsHub *WebSocketHub
}

// buildWebSocketURL constructs the absolute WebSocket URL from request context
func (h *ThreatModelDiagramHandler) buildWebSocketURL(c *gin.Context, threatModelId, diagramId string) string {
	// Get config information from the context
	tlsEnabled := false
	tlsSubjectName := ""
	serverPort := "8080"

	// Try to extract from request context
	if val, exists := c.Get("tlsEnabled"); exists {
		if enabled, ok := val.(bool); ok {
			tlsEnabled = enabled
		}
	}

	if val, exists := c.Get("tlsSubjectName"); exists {
		if name, ok := val.(string); ok {
			tlsSubjectName = name
		}
	}

	if val, exists := c.Get("serverPort"); exists {
		if port, ok := val.(string); ok {
			serverPort = port
		}
	}

	// Determine websocket protocol
	scheme := "ws"
	if tlsEnabled {
		scheme = "wss"
	}

	// Determine host
	host := c.Request.Host
	if tlsSubjectName != "" && tlsEnabled {
		// Use configured subject name if available
		host = tlsSubjectName
		// Add port if not the default HTTPS port
		if serverPort != "443" {
			host = fmt.Sprintf("%s:%s", host, serverPort)
		}
	}

	// Build WebSocket URL with the specific path
	return fmt.Sprintf("%s://%s/threat_models/%s/diagrams/%s/ws", scheme, host, threatModelId, diagramId)
}

// NewThreatModelDiagramHandler creates a new handler for diagrams within threat models
func NewThreatModelDiagramHandler(wsHub *WebSocketHub) *ThreatModelDiagramHandler {
	return &ThreatModelDiagramHandler{
		wsHub: wsHub,
	}
}

// GetDiagrams returns a list of diagrams for a threat model
func (h *ThreatModelDiagramHandler) GetDiagrams(c *gin.Context, threatModelId string) {
	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For listing endpoints, we allow unauthenticated users but return empty results
		userEmail = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has access to the threat model using new utilities
	hasAccess, err := CheckResourceAccess(userEmail, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasAccess {
		HandleRequestError(c, ForbiddenError("You don't have sufficient permissions to access this threat model"))
		return
	}

	// Get diagrams associated with this threat model
	var diagrams []DfdDiagram
	if tm.Diagrams != nil {
		for _, diagramUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to extract ID
			if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
				// Since we already have the DfdDiagram, we can use it directly instead of querying the store
				diagrams = append(diagrams, dfdDiag)
			}
		}
	}

	// Apply pagination
	start := offset
	end := offset + limit
	if start >= len(diagrams) {
		start = len(diagrams)
	}
	if end > len(diagrams) {
		end = len(diagrams)
	}

	var paginatedDiagrams []DfdDiagram
	if start < end {
		paginatedDiagrams = diagrams[start:end]
	} else {
		paginatedDiagrams = []DfdDiagram{}
	}

	// Convert to list items for API response
	// NOTE: ListItem type removed with diagram endpoints - this code is now inactive
	items := make([]map[string]interface{}, 0, len(paginatedDiagrams))
	for _, d := range paginatedDiagrams {
		items = append(items, map[string]interface{}{
			"id":   d.Id,
			"name": d.Name,
			"type": d.Type,
		})
	}

	c.JSON(http.StatusOK, items)
}

// CreateDiagram creates a new diagram for a threat model
func (h *ThreatModelDiagramHandler) CreateDiagram(c *gin.Context, threatModelId string) {
	type CreateThreatModelDiagramRequest struct {
		Name        string  `json:"name" binding:"required"`
		Type        string  `json:"type" binding:"required"`
		Description *string `json:"description,omitempty"`
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["diagram_create"]
	request, err := ValidateAndParseRequest[CreateThreatModelDiagramRequest](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has write access to the threat model using new utilities
	hasWriteAccess, err := CheckResourceAccess(userEmail, tm, RoleWriter)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasWriteAccess {
		HandleRequestError(c, ForbiddenError("You don't have sufficient permissions to create diagrams in this threat model"))
		return
	}

	// Create new diagram
	now := time.Now().UTC()
	cells := []DfdDiagram_Cells_Item{}
	metadata := []Metadata{}

	// Create DfdDiagram directly for the store
	d := DfdDiagram{
		Name:       request.Name,
		Type:       DfdDiagramType(request.Type),
		CreatedAt:  now,
		ModifiedAt: now,
		Cells:      cells,
		Metadata:   &metadata,
	}

	// Add to store
	idSetter := func(d DfdDiagram, id string) DfdDiagram {
		uuid, _ := ParseUUID(id)
		d.Id = &uuid
		return d
	}

	createdDiagram, err := DiagramStore.CreateWithThreatModel(d, threatModelId, idSetter)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to create diagram in store for threat model %s (user: %s, diagram type: %s): %v", threatModelId, userEmail, d.Type, err)
		HandleRequestError(c, ServerError("Failed to create diagram"))
		return
	}

	// Add diagram to threat model's diagrams array
	// Convert DfdDiagram to Diagram union type
	var diagramUnion Diagram
	if err := diagramUnion.FromDfdDiagram(createdDiagram); err != nil {
		// Delete the created diagram if we can't add it to the threat model
		if deleteErr := DiagramStore.Delete(createdDiagram.Id.String()); deleteErr != nil {
			slogging.Get().WithContext(c).Error("Failed to delete diagram after union conversion failure: %v", deleteErr)
		}
		HandleRequestError(c, ServerError("Failed to convert diagram: "+err.Error()))
		return
	}

	if tm.Diagrams == nil {
		diagrams := []Diagram{diagramUnion}
		tm.Diagrams = &diagrams
	} else {
		*tm.Diagrams = append(*tm.Diagrams, diagramUnion)
	}

	// Update threat model in store
	tm.ModifiedAt = &now
	if err := ThreatModelStore.Update(threatModelId, tm); err != nil {
		slogging.Get().WithContext(c).Error("Failed to update threat model %s with new diagram %s (user: %s): %v", threatModelId, createdDiagram.Id.String(), userEmail, err)
		// If updating the threat model fails, delete the created diagram
		if deleteErr := DiagramStore.Delete(createdDiagram.Id.String()); deleteErr != nil {
			// Log the error but continue with the main error response
			slogging.Get().WithContext(c).Error("Failed to delete diagram after threat model update failure: %v", deleteErr)
		}
		HandleRequestError(c, ServerError("Failed to update threat model with new diagram"))
		return
	}

	// Set the Location header
	c.Header("Location", fmt.Sprintf("/threat_models/%s/diagrams/%s", threatModelId, createdDiagram.Id.String()))
	c.JSON(http.StatusCreated, createdDiagram)
}

// GetDiagramByID retrieves a specific diagram within a threat model
func (h *ThreatModelDiagramHandler) GetDiagramByID(c *gin.Context, threatModelId, diagramId string) {
	// Validate ID formats
	if _, err := ParseUUID(threatModelId); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}
	if _, err := ParseUUID(diagramId); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format, must be a valid UUID"))
		return
	}

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has access to the threat model using new utilities
	hasAccess, err := CheckResourceAccess(userEmail, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if !hasAccess {
		HandleRequestError(c, ForbiddenError("You don't have sufficient permissions to access this threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Get diagram from store
	diagram, err := DiagramStore.Get(diagramId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Diagram not found"))
		return
	}

	c.JSON(http.StatusOK, diagram)
}

// UpdateDiagram fully updates a diagram within a threat model
func (h *ThreatModelDiagramHandler) UpdateDiagram(c *gin.Context, threatModelId, diagramId string) {
	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccess(userEmail, tm, RoleWriter)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasWriteAccess {
		HandleRequestError(c, ForbiddenError("You don't have sufficient permissions to update diagrams in this threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Get existing diagram
	existingDiagram, err := DiagramStore.Get(diagramId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Diagram not found"))
		return
	}

	// Parse and validate the updated diagram from request body using OpenAPI validation
	var updatedDiagramUnion Diagram
	if err := c.ShouldBindJSON(&updatedDiagramUnion); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Convert union type to DfdDiagram for working with store
	updatedDiagram, err := updatedDiagramUnion.AsDfdDiagram()
	if err != nil {
		HandleRequestError(c, InvalidInputError("Invalid diagram format: "+err.Error()))
		return
	}

	// Ensure ID matches
	uuid, err := ParseUUID(diagramId)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid diagram ID format"))
		return
	}
	updatedDiagram.Id = &uuid

	// Preserve creation time
	updatedDiagram.CreatedAt = existingDiagram.CreatedAt

	// Use centralized update function
	updateFunc := func(diagram DfdDiagram) (DfdDiagram, bool, error) {
		// Return the full updated diagram, incrementing vector only if cells changed
		cellsChanged := !areSlicesEqual(diagram.Cells, updatedDiagram.Cells)
		return updatedDiagram, cellsChanged, nil
	}

	result, err := h.wsHub.UpdateDiagram(diagramId, updateFunc, "rest_api", userEmail)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to update diagram %s via centralized function (user: %s, type: %s): %v", diagramId, userEmail, updatedDiagram.Type, err)
		HandleRequestError(c, ServerError("Failed to update diagram"))
		return
	}

	c.JSON(http.StatusOK, result.UpdatedDiagram)
}

// PatchDiagram partially updates a diagram within a threat model
func (h *ThreatModelDiagramHandler) PatchDiagram(c *gin.Context, threatModelId, diagramId string) {
	// Similar to UpdateDiagram but with JSON Patch operations
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has write access to the threat model
	hasWriteAccess, err := CheckResourceAccess(userEmail, tm, RoleWriter)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasWriteAccess {
		HandleRequestError(c, ForbiddenError("You don't have sufficient permissions to update diagrams in this threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Get existing diagram
	existingDiagram, err := DiagramStore.Get(diagramId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Diagram not found"))
		return
	}

	// Parse patch operations from request body
	var operations []PatchOperation
	if err := c.ShouldBindJSON(&operations); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid JSON Patch format: "+err.Error()))
		return
	}

	// Apply patch operations
	modifiedDiagram, err := ApplyPatchOperations(existingDiagram, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Preserve critical fields that shouldn't change during patching
	modifiedDiagram.Id = existingDiagram.Id
	modifiedDiagram.CreatedAt = existingDiagram.CreatedAt

	// Use centralized update function
	updateFunc := func(diagram DfdDiagram) (DfdDiagram, bool, error) {
		// Check if cells changed to determine if we should increment vector
		cellsChanged := !areSlicesEqual(diagram.Cells, modifiedDiagram.Cells)
		return modifiedDiagram, cellsChanged, nil
	}

	result, err := h.wsHub.UpdateDiagram(diagramId, updateFunc, "rest_api", userEmail)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to update diagram: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, result.UpdatedDiagram)
}

// DeleteDiagram deletes a diagram within a threat model
func (h *ThreatModelDiagramHandler) DeleteDiagram(c *gin.Context, threatModelId, diagramId string) {
	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has owner access to the threat model
	// Only owners can delete diagrams
	hasOwnerAccess, err := CheckResourceAccess(userEmail, tm, RoleOwner)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasOwnerAccess {
		HandleRequestError(c, ForbiddenError("Only the owner can delete diagrams from a threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	var diagramIndex int
	if tm.Diagrams != nil {
		for i, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				diagramIndex = i
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Delete diagram from store
	if err := DiagramStore.Delete(diagramId); err != nil {
		HandleRequestError(c, ServerError("Failed to delete diagram"))
		return
	}

	// Remove diagram ID from threat model's diagrams array
	diagrams := *tm.Diagrams
	*tm.Diagrams = append(diagrams[:diagramIndex], diagrams[diagramIndex+1:]...)

	// Update threat model in store
	now := time.Now().UTC()
	tm.ModifiedAt = &now
	if err := ThreatModelStore.Update(threatModelId, tm); err != nil {
		HandleRequestError(c, ServerError("Failed to update threat model after diagram deletion"))
		return
	}

	c.Status(http.StatusNoContent)
}

// GetDiagramCollaborate gets collaboration session status for a diagram within a threat model
func (h *ThreatModelDiagramHandler) GetDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// Similar to DiagramHandler.GetDiagramCollaborate but with threat model access check
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For collaboration endpoints, allow anonymous users
		// TODO: make this code more readable.  We expect middleware to set userEmail to "anonymous" when unauthenticated
		userEmail = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has access to the threat model
	hasReadAccess, err := CheckResourceAccess(userEmail, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasReadAccess {
		HandleRequestError(c, UnauthorizedError("You don't have sufficient permissions to access this threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Get diagram from store
	_, err = DiagramStore.Get(diagramId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Diagram not found"))
		return
	}

	// Check for existing collaboration session
	session := h.wsHub.GetSession(diagramId)

	if session == nil {
		// No active session
		c.JSON(http.StatusOK, gin.H{
			"session_id":      nil,
			"threat_model_id": threatModelId,
			"diagram_id":      diagramId,
			"participants":    []interface{}{},
			"websocket_url":   h.buildWebSocketURL(c, threatModelId, diagramId),
		})
		return
	}

	// Build proper CollaborationSession response using the same method as PUT
	collaborationSession, err := h.wsHub.buildCollaborationSessionFromDiagramSession(c, diagramId, session, userEmail)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to build collaboration session response: "+err.Error()))
		return
	}

	c.JSON(http.StatusOK, collaborationSession)
}

// CreateDiagramCollaborate creates a new collaboration session for a diagram within a threat model
func (h *ThreatModelDiagramHandler) CreateDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// Similar to DiagramHandler.PostDiagramCollaborate but with threat model access check
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For collaboration endpoints, allow anonymous users
		// TODO: make this code more readable.  We expect middleware to set userEmail to "anonymous" when unauthenticated
		userEmail = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has access to the threat model
	hasReadAccess, err := CheckResourceAccess(userEmail, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasReadAccess {
		HandleRequestError(c, UnauthorizedError("You don't have sufficient permissions to access this threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Get diagram from store
	_, err = DiagramStore.Get(diagramId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Diagram not found"))
		return
	}

	// Try to get or create collaboration session
	session := h.wsHub.GetSession(diagramId)
	statusCode := http.StatusOK // Default for existing session
	if session == nil {
		// Create new collaboration session
		session, err = h.wsHub.CreateSession(diagramId, threatModelId, userEmail)
		if err != nil {
			HandleRequestError(c, ServerError("Failed to create collaboration session"))
			return
		}
		statusCode = http.StatusCreated // New session created
	}

	// Don't add participants here - only when they connect via WebSocket

	// Build proper CollaborationSession response
	collaborationSession, err := h.wsHub.buildCollaborationSessionFromDiagramSession(c, diagramId, session, userEmail)
	if err != nil {
		// Temporarily return detailed error for debugging
		HandleRequestError(c, ServerError("Failed to build collaboration session response: "+err.Error()))
		return
	}

	c.JSON(statusCode, collaborationSession)
}

// PostDiagramCollaborate joins or starts a collaboration session for a diagram within a threat model (DEPRECATED - use CreateDiagramCollaborate)
func (h *ThreatModelDiagramHandler) PostDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// For backward compatibility, delegate to CreateDiagramCollaborate
	h.CreateDiagramCollaborate(c, threatModelId, diagramId)
}

// DeleteDiagramCollaborate leaves a collaboration session for a diagram within a threat model
func (h *ThreatModelDiagramHandler) DeleteDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// Similar to DiagramHandler.DeleteDiagramCollaborate but with threat model access check
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		// For collaboration endpoints, allow anonymous users
		// TODO: make this code more readable.  We expect middleware to set userEmail to "anonymous" when unauthenticated
		userEmail = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Check if user has access to the threat model
	hasReadAccess, err := CheckResourceAccess(userEmail, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasReadAccess {
		HandleRequestError(c, UnauthorizedError("You don't have sufficient permissions to access this threat model"))
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, diagUnion := range *tm.Diagrams {
			// Convert union type to DfdDiagram to get the ID
			if dfdDiag, err := diagUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil && dfdDiag.Id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		HandleRequestError(c, NotFoundError("Diagram not found in this threat model"))
		return
	}

	// Get the session to check if it exists and if user is part of it
	session := h.wsHub.GetSession(diagramId)
	if session == nil {
		// No active session - this is fine, just return success
		c.Status(http.StatusNoContent)
		return
	}

	// Check if the requesting user is the host
	session.mu.RLock()
	isHost := (session.Host == userEmail)
	session.mu.RUnlock()

	if isHost {
		// If the user is the host, close the entire session
		h.wsHub.CloseSession(diagramId)
		slogging.Get().WithContext(c).Info("Collaboration session %s closed by host %s", session.ID, userEmail)
	} else {
		// If user is not the host, they will be removed when their WebSocket disconnects
		// No need to do anything here since we only track active connections
		slogging.Get().WithContext(c).Info("User %s leaving session %s (will disconnect from WebSocket)", userEmail, session.ID)
	}

	c.Status(http.StatusNoContent)
}

// areSlicesEqual compares two slices of DfdDiagram_Cells_Item for equality
func areSlicesEqual(a, b []DfdDiagram_Cells_Item) bool {
	if len(a) != len(b) {
		return false
	}

	// Use deep comparison for complex slice elements
	return reflect.DeepEqual(a, b)
}
