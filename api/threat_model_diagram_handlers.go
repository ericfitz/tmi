package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ThreatModelDiagramHandler provides handlers for diagram operations within threat models
type ThreatModelDiagramHandler struct {
	// Could add dependencies like logger, metrics, etc.
}

// NewThreatModelDiagramHandler creates a new handler for diagrams within threat models
func NewThreatModelDiagramHandler() *ThreatModelDiagramHandler {
	return &ThreatModelDiagramHandler{}
}

// GetDiagrams returns a list of diagrams for a threat model
func (h *ThreatModelDiagramHandler) GetDiagrams(c *gin.Context, threatModelId string) {
	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleReader); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to access this threat model",
		})
		return
	}

	// Get diagrams associated with this threat model
	var diagrams []DfdDiagram
	if tm.Diagrams != nil {
		for _, diagramID := range *tm.Diagrams {
			diagram, err := DiagramStore.Get(diagramID.String())
			if err == nil {
				diagrams = append(diagrams, diagram)
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

	var paginatedDiagrams []Diagram
	if start < end {
		paginatedDiagrams = diagrams[start:end]
	} else {
		paginatedDiagrams = []Diagram{}
	}

	// Convert to list items for API response
	items := make([]ListItem, 0, len(paginatedDiagrams))
	for _, d := range paginatedDiagrams {
		items = append(items, ListItem{
			Id:   d.Id,
			Name: d.Name,
		})
	}

	c.JSON(http.StatusOK, items)
}

// CreateDiagram creates a new diagram for a threat model
func (h *ThreatModelDiagramHandler) CreateDiagram(c *gin.Context, threatModelId string) {
	var request struct {
		Name        string  `json:"name" binding:"required"`
		Description *string `json:"description,omitempty"`
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

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has write access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleWriter); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to create diagrams in this threat model",
		})
		return
	}

	// Create new diagram
	now := time.Now().UTC()
	cells := []Cell{}
	metadata := []Metadata{}

	d := Diagram{
		Name:        request.Name,
		Description: request.Description,
		CreatedAt:   now,
		ModifiedAt:  now,
		GraphData:   &cells,
		Metadata:    &metadata,
	}

	// Add to store
	idSetter := func(d Diagram, id string) Diagram {
		uuid, _ := ParseUUID(id)
		d.Id = uuid
		return d
	}

	createdDiagram, err := DiagramStore.Create(d, idSetter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to create diagram",
		})
		return
	}

	// Add diagram ID to threat model's diagrams array
	if tm.Diagrams == nil {
		diagrams := []TypesUUID{createdDiagram.Id}
		tm.Diagrams = &diagrams
	} else {
		*tm.Diagrams = append(*tm.Diagrams, createdDiagram.Id)
	}

	// Update threat model in store
	tm.ModifiedAt = now
	if err := ThreatModelStore.Update(threatModelId, tm); err != nil {
		// If updating the threat model fails, delete the created diagram
		if deleteErr := DiagramStore.Delete(createdDiagram.Id.String()); deleteErr != nil {
			// Log the error but continue with the main error response
			fmt.Printf("Failed to delete diagram after threat model update failure: %v\n", deleteErr)
		}
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to update threat model with new diagram",
		})
		return
	}

	// Set the Location header
	c.Header("Location", fmt.Sprintf("/threat_models/%s/diagrams/%s", threatModelId, createdDiagram.Id.String()))
	c.JSON(http.StatusCreated, createdDiagram)
}

// GetDiagramByID retrieves a specific diagram within a threat model
func (h *ThreatModelDiagramHandler) GetDiagramByID(c *gin.Context, threatModelId, diagramId string) {
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleReader); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to access this threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// Get diagram from store
	diagram, err := DiagramStore.Get(diagramId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found",
		})
		return
	}

	c.JSON(http.StatusOK, diagram)
}

// UpdateDiagram fully updates a diagram within a threat model
func (h *ThreatModelDiagramHandler) UpdateDiagram(c *gin.Context, threatModelId, diagramId string) {
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has write access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleWriter); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to update diagrams in this threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// Get existing diagram
	existingDiagram, err := DiagramStore.Get(diagramId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found",
		})
		return
	}

	// Parse the updated diagram from request body
	var updatedDiagram Diagram
	if err := c.ShouldBindJSON(&updatedDiagram); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: err.Error(),
		})
		return
	}

	// Ensure ID matches
	uuid, err := ParseUUID(diagramId)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_id",
			ErrorDescription: "Invalid diagram ID format",
		})
		return
	}
	updatedDiagram.Id = uuid

	// Preserve creation time but update modification time
	updatedDiagram.CreatedAt = existingDiagram.CreatedAt
	updatedDiagram.ModifiedAt = time.Now().UTC()

	// Update in store
	if err := DiagramStore.Update(diagramId, updatedDiagram); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to update diagram",
		})
		return
	}

	c.JSON(http.StatusOK, updatedDiagram)
}

// PatchDiagram partially updates a diagram within a threat model
func (h *ThreatModelDiagramHandler) PatchDiagram(c *gin.Context, threatModelId, diagramId string) {
	// Similar to UpdateDiagram but with JSON Patch operations
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has write access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleWriter); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to update diagrams in this threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// Get existing diagram
	existingDiagram, err := DiagramStore.Get(diagramId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found",
		})
		return
	}

	// Parse patch operations from request body
	var operations []PatchOperation
	if err := c.ShouldBindJSON(&operations); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			ErrorDescription: "Invalid JSON Patch format: " + err.Error(),
		})
		return
	}

	// Apply patch operations (simplified)
	// In a real implementation, you would use a JSON Patch library
	// For now, we'll just return the existing diagram

	// Update modification time
	existingDiagram.ModifiedAt = time.Now().UTC()

	// Update in store
	if err := DiagramStore.Update(diagramId, existingDiagram); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to update diagram",
		})
		return
	}

	c.JSON(http.StatusOK, existingDiagram)
}

// DeleteDiagram deletes a diagram within a threat model
func (h *ThreatModelDiagramHandler) DeleteDiagram(c *gin.Context, threatModelId, diagramId string) {
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has owner access to the threat model
	// Only owners can delete diagrams
	if err := CheckThreatModelAccess(userName, tm, RoleOwner); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "Only the owner can delete diagrams from a threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	var diagramIndex int
	if tm.Diagrams != nil {
		for i, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				diagramIndex = i
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// Delete diagram from store
	if err := DiagramStore.Delete(diagramId); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to delete diagram",
		})
		return
	}

	// Remove diagram ID from threat model's diagrams array
	diagrams := *tm.Diagrams
	*tm.Diagrams = append(diagrams[:diagramIndex], diagrams[diagramIndex+1:]...)

	// Update threat model in store
	tm.ModifiedAt = time.Now().UTC()
	if err := ThreatModelStore.Update(threatModelId, tm); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			ErrorDescription: "Failed to update threat model after diagram deletion",
		})
		return
	}

	c.Status(http.StatusNoContent)
}

// GetDiagramCollaborate gets collaboration session status for a diagram within a threat model
func (h *ThreatModelDiagramHandler) GetDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// Similar to DiagramHandler.GetDiagramCollaborate but with threat model access check
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleReader); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to access this threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// Get diagram from store
	_, err = DiagramStore.Get(diagramId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found",
		})
		return
	}

	// For now, return a placeholder response
	// In a real implementation, you would check for active collaboration sessions
	c.JSON(http.StatusOK, gin.H{
		"session_id":      "placeholder-session-id",
		"threat_model_id": threatModelId,
		"diagram_id":      diagramId,
		"participants":    []interface{}{},
		"websocket_url":   fmt.Sprintf("/threat_models/%s/diagrams/%s/ws", threatModelId, diagramId),
	})
}

// PostDiagramCollaborate joins or starts a collaboration session for a diagram within a threat model
func (h *ThreatModelDiagramHandler) PostDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// Similar to DiagramHandler.PostDiagramCollaborate but with threat model access check
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleReader); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to access this threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// Get diagram from store
	_, err = DiagramStore.Get(diagramId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found",
		})
		return
	}

	// For now, return a placeholder response
	// In a real implementation, you would create or join a collaboration session
	c.JSON(http.StatusOK, gin.H{
		"session_id":      "placeholder-session-id",
		"threat_model_id": threatModelId,
		"diagram_id":      diagramId,
		"participants": []gin.H{
			{
				"user_id":   userName,
				"joined_at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		"websocket_url": fmt.Sprintf("/threat_models/%s/diagrams/%s/ws", threatModelId, diagramId),
	})
}

// DeleteDiagramCollaborate leaves a collaboration session for a diagram within a threat model
func (h *ThreatModelDiagramHandler) DeleteDiagramCollaborate(c *gin.Context, threatModelId, diagramId string) {
	// Similar to DiagramHandler.DeleteDiagramCollaborate but with threat model access check
	// For brevity, this implementation is simplified

	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}

	// Get the threat model to check access
	tm, err := ThreatModelStore.Get(threatModelId)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Threat model not found",
		})
		return
	}

	// Check if user has access to the threat model
	if err := CheckThreatModelAccess(userName, tm, RoleReader); err != nil {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			ErrorDescription: "You don't have sufficient permissions to access this threat model",
		})
		return
	}

	// Check if the diagram is associated with this threat model
	diagramFound := false
	if tm.Diagrams != nil {
		for _, id := range *tm.Diagrams {
			if id.String() == diagramId {
				diagramFound = true
				break
			}
		}
	}

	if !diagramFound {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			ErrorDescription: "Diagram not found in this threat model",
		})
		return
	}

	// For now, just return success
	// In a real implementation, you would remove the user from the collaboration session
	c.Status(http.StatusNoContent)
}
