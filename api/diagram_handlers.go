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

// DiagramHandler provides handlers for diagram operations
type DiagramHandler struct {
	// Could add dependencies like logger, metrics, etc.
}

// NewDiagramHandler creates a new diagram handler
func NewDiagramHandler() *DiagramHandler {
	return &DiagramHandler{}
}

// GetDiagrams returns a list of diagrams
func (h *DiagramHandler) GetDiagrams(c *gin.Context) {
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
	filter := func(d Diagram) bool {
		// If no user is authenticated, only show public diagrams (if any)
		if userName == "" {
			return false
		}
		
		// Check if the user is the owner
		if d.Owner == userName {
			return true
		}
		
		// Check if the user has access through authorization
		for _, auth := range d.Authorization {
			if auth.Subject == userName {
				return true
			}
		}
		
		return false
	}
	
	// Get diagrams from store with filtering
	diagrams := DiagramStore.List(offset, limit, filter)
	
	// Convert to list items for API response
	items := make([]ListItem, 0, len(diagrams))
	for _, d := range diagrams {
		items = append(items, ListItem{
			Id:   d.Id,
			Name: d.Name,
		})
	}
	
	c.JSON(http.StatusOK, items)
}

// GetDiagramByID retrieves a specific diagram
func (h *DiagramHandler) GetDiagramByID(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")
	
	// Get diagram from store
	d, err := DiagramStore.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:   "not_found",
			Message: "Diagram not found",
		})
		return
	}
	
	// Authorization is handled by middleware
	// The middleware has already verified the user has appropriate access
	c.JSON(http.StatusOK, d)
}

// CreateDiagram creates a new diagram
func (h *DiagramHandler) CreateDiagram(c *gin.Context) {
	fmt.Printf("[DEBUG DIAGRAM HANDLER] CreateDiagram called\n")
	
	// Copy the request body for debugging before binding
	bodyBytes, err := c.GetRawData()
	if err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Error reading request body: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Failed to read request body: " + err.Error(),
		})
		return
	}
	
	// Log the raw request body
	if len(bodyBytes) > 0 {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Request body: %s\n", string(bodyBytes))
		// Reset the body for later binding
		c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
	} else {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Empty request body received\n")
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Request body is empty",
		})
		return
	}
	
	var request struct {
		Name          string           `json:"name" binding:"required"`
		Description   *string          `json:"description,omitempty"`
		Authorization []Authorization  `json:"authorization,omitempty"`
	}
	
	if err := c.ShouldBindJSON(&request); err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] JSON binding error: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: err.Error(),
		})
		return
	}
	
	fmt.Printf("[DEBUG DIAGRAM HANDLER] Successfully parsed request: %+v\n", request)
	
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			Message: "Authentication required",
		})
		return
	}
	
	// Check for duplicate authorization subjects in the request itself first
	if len(request.Authorization) > 0 {
		authMap := make(map[string]bool)
		for _, auth := range request.Authorization {
			if _, exists := authMap[auth.Subject]; exists {
				c.JSON(http.StatusBadRequest, Error{
					Error:   "invalid_input",
					Message: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
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
					Message: fmt.Sprintf("Duplicate authorization subject with owner: %s", auth.Subject),
				})
				return
			}
			authorizations = append(authorizations, auth)
		}
	}
	
	// Create new diagram
	now := time.Now().UTC()
	components := []DiagramComponent{}
	metadata := []Metadata{}
	
	d := Diagram{
		Name:          request.Name,
		Description:   request.Description,
		CreatedAt:     now,
		ModifiedAt:    now,
		Owner:         userName,
		Authorization: authorizations,
		Components:    &components,
		Metadata:      &metadata,
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
			Message: "Failed to create diagram",
		})
		return
	}
	
	// Set the Location header
	c.Header("Location", "/diagrams/"+createdDiagram.Id.String())
	c.JSON(http.StatusCreated, createdDiagram)
}

// UpdateDiagram fully updates a diagram
func (h *DiagramHandler) UpdateDiagram(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")
	fmt.Printf("[DEBUG DIAGRAM HANDLER] UpdateDiagram called for ID: %s\n", id)
	
	// Copy the request body for debugging before binding
	bodyBytes, err := c.GetRawData()
	if err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Error reading request body: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Failed to read request body: " + err.Error(),
		})
		return
	}
	
	// Log the raw request body
	if len(bodyBytes) > 0 {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Request body: %s\n", string(bodyBytes))
		// Reset the body for later binding
		c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
	} else {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Empty request body received\n")
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Request body is empty",
		})
		return
	}
	
	var request Diagram
	if err := c.ShouldBindJSON(&request); err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] JSON binding error: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: err.Error(),
		})
		return
	}
	
	fmt.Printf("[DEBUG DIAGRAM HANDLER] Successfully parsed request: %+v\n", request)
	
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	_, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			Message: "Authentication required",
		})
		return
	}
	
	// Get existing diagram - should be available from middleware
	existingDiagram, exists := c.Get("diagram")
	if !exists {
		// If not in context, fetch it directly
		var err error
		existingDiagram, err = DiagramStore.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, Error{
				Error:   "not_found",
				Message: "Diagram not found",
			})
			return
		}
	}
	
	d, ok := existingDiagram.(Diagram)
	if !ok {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to process diagram",
		})
		return
	}
	
	// Ensure ID in the URL matches the one in the body
	uuid, err := ParseUUID(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_id",
			Message: "Invalid ID format",
		})
		return
	}
	request.Id = uuid
	
	// Preserve creation time but update modification time
	request.CreatedAt = d.CreatedAt
	request.ModifiedAt = time.Now().UTC()
	
	// Get user role from context - should be set by middleware
	roleValue, exists := c.Get("userRole")
	userRole, ok := roleValue.(Role)
	
	// Rule 2: Writers cannot modify owner or authorization fields
	ownerChanging := request.Owner != "" && request.Owner != d.Owner
	// If writer is trying to change auth field, that's a problem 
	authChanging := (len(request.Authorization) > 0) && (!authorizationEqual(request.Authorization, d.Authorization))
	
	// For writer access check, we need to be more stringent - even presence of the auth field is an issue
	if userRole == RoleWriter && len(request.Authorization) > 0 {
		authChanging = true
	}
	
	if (ownerChanging || authChanging) && userRole != RoleOwner {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			Message: "Only the owner can change ownership or authorization",
		})
		return
	}
	
	// Check for duplicate authorization subjects
	subjectMap := make(map[string]bool)
	for _, auth := range request.Authorization {
		if _, exists := subjectMap[auth.Subject]; exists {
			c.JSON(http.StatusBadRequest, Error{
				Error:   "invalid_input",
				Message: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
			})
			return
		}
		subjectMap[auth.Subject] = true
	}
	
	// Custom rule 1: If owner is changing, add original owner to authorization with owner role
	if request.Owner != d.Owner {
		// Check if the original owner is already in the authorization list
		originalOwnerFound := false
		for i := range request.Authorization {
			if request.Authorization[i].Subject == d.Owner {
				// Make sure the original owner has the Owner role
				request.Authorization[i].Role = Owner
				originalOwnerFound = true
				break
			}
		}
		
		// If the original owner isn't in the list, add them
		if !originalOwnerFound {
			request.Authorization = append(request.Authorization, Authorization{
				Subject: d.Owner,
				Role:    RoleOwner,
			})
		}
	}
	
	// Update in store
	if err := DiagramStore.Update(id, request); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to update diagram",
		})
		return
	}
	
	c.JSON(http.StatusOK, request)
}

// PatchDiagram partially updates a diagram
func (h *DiagramHandler) PatchDiagram(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")
	fmt.Printf("[DEBUG DIAGRAM HANDLER] PatchDiagram called for ID: %s\n", id)
	
	// Copy the request body for debugging before binding
	bodyBytes, err := c.GetRawData()
	if err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Error reading PATCH request body: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Failed to read request body: " + err.Error(),
		})
		return
	}
	
	// Log the raw request body
	if len(bodyBytes) > 0 {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] PATCH request body: %s\n", string(bodyBytes))
		// Reset the body for later binding
		c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
	} else {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Empty PATCH request body received\n")
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Request body is empty",
		})
		return
	}
	
	var operations []PatchOperation
	if err := c.ShouldBindJSON(&operations); err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] PATCH JSON binding error: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_input",
			Message: "Invalid JSON Patch format: " + err.Error(),
		})
		return
	}
	
	fmt.Printf("[DEBUG DIAGRAM HANDLER] Successfully parsed PATCH request with %d operations\n", len(operations))
	
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, Error{
			Error:   "unauthorized",
			Message: "Authentication required",
		})
		return
	}
	
	// Get existing diagram - should be available from middleware
	existingDiagramValue, exists := c.Get("diagram")
	if !exists {
		// If not in context, fetch it directly
		var err error
		existingDiagramValue, err = DiagramStore.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, Error{
				Error:   "not_found",
				Message: "Diagram not found",
			})
			return
		}
	}
	
	existingDiagram, ok := existingDiagramValue.(Diagram)
	if !ok {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to process diagram",
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
			Message: "Failed to convert patch operations: " + err.Error(),
		})
		return
	}
	
	// Convert diagram to JSON
	originalBytes, err := json.Marshal(existingDiagram)
	if err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to serialize diagram",
		})
		return
	}
	
	// Create patch object
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_patch",
			Message: "Invalid JSON Patch: " + err.Error(),
		})
		return
	}
	
	// Apply patch
	fmt.Printf("[DEBUG DIAGRAM HANDLER] Applying patch: %s to original: %s\n", 
		string(patchBytes), string(originalBytes))
	modifiedBytes, err := patch.Apply(originalBytes)
	if err != nil {
		fmt.Printf("[DEBUG DIAGRAM HANDLER] Patch apply error: %v\n", err)
		c.JSON(http.StatusBadRequest, Error{
			Error:   "patch_failed",
			Message: "Failed to apply patch: " + err.Error(),
		})
		return
	}
	fmt.Printf("[DEBUG DIAGRAM HANDLER] Modified JSON after patch: %s\n", string(modifiedBytes))
	
	// Deserialize back into diagram
	var modifiedDiagram Diagram
	if err := json.Unmarshal(modifiedBytes, &modifiedDiagram); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to deserialize patched diagram",
		})
		return
	}
	
	// Check if owner or authorization is changing, which requires owner role
	ownerChanging := modifiedDiagram.Owner != existingDiagram.Owner
	authChanging := !authorizationEqual(existingDiagram.Authorization, modifiedDiagram.Authorization)
	
	if (ownerChanging || authChanging) && (!exists || !ok || userRole != RoleOwner) {
		c.JSON(http.StatusForbidden, Error{
			Error:   "forbidden",
			Message: "Only the owner can change ownership or authorization",
		})
		return
	}
	
	// Check for duplicate authorization subjects
	if authChanging || ownerChanging {
		subjectMap := make(map[string]bool)
		for _, auth := range modifiedDiagram.Authorization {
			if _, exists := subjectMap[auth.Subject]; exists {
				c.JSON(http.StatusBadRequest, Error{
					Error:   "invalid_input",
					Message: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
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
		for i := range modifiedDiagram.Authorization {
			if modifiedDiagram.Authorization[i].Subject == existingDiagram.Owner {
				// Make sure the original owner has the Owner role
				modifiedDiagram.Authorization[i].Role = Owner
				originalOwnerFound = true
				break
			}
		}
		
		// If the original owner isn't in the list, add them
		if !originalOwnerFound {
			modifiedDiagram.Authorization = append(modifiedDiagram.Authorization, Authorization{
				Subject: existingDiagram.Owner,
				Role:    RoleOwner,
			})
		}
	}
	
	// Validate patched diagram
	if err := validatePatchedDiagram(existingDiagram, modifiedDiagram, userName); err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "validation_failed",
			Message: err.Error(),
		})
		return
	}
	
	// Preserve the original ID
	modifiedDiagram.Id = existingDiagram.Id
	
	// Preserve creation time but update modification time
	modifiedDiagram.CreatedAt = existingDiagram.CreatedAt
	modifiedDiagram.ModifiedAt = time.Now().UTC()
	
	// Update in store
	if err := DiagramStore.Update(id, modifiedDiagram); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to update diagram",
		})
		return
	}
	
	c.JSON(http.StatusOK, modifiedDiagram)
}

// DeleteDiagram deletes a diagram
func (h *DiagramHandler) DeleteDiagram(c *gin.Context) {
	// Parse ID from URL parameter
	id := c.Param("id")
	
	// Get diagram - should be available via middleware
	_, exists := c.Get("diagram")
	if !exists {
		// If not in context, fetch it directly
		_, err := DiagramStore.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, Error{
				Error:   "not_found",
				Message: "Diagram not found",
			})
			return
		}
	}
	
	// Role check is done by middleware
	// The middleware already verifies owner access for delete operations
	
	// Delete from store
	if err := DiagramStore.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, Error{
			Error:   "server_error",
			Message: "Failed to delete diagram",
		})
		return
	}
	
	c.Status(http.StatusNoContent)
}

// validatePatchedDiagram performs validation on the patched diagram
func validatePatchedDiagram(original, patched Diagram, userName string) error {
	// 1. Ensure ID is not changed
	if patched.Id != original.Id {
		return fmt.Errorf("cannot change diagram ID")
	}
	
	// 2. Prevent changing owner if the user doesn't have owner role
	// Check if user is the owner or has owner role in authorization
	hasOwnerRole := (original.Owner == userName)
	if !hasOwnerRole {
		for _, auth := range original.Authorization {
			if auth.Subject == userName && auth.Role == Owner {
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
	
	// 5. Validate authorization entries (only check for empty subjects)
	for _, auth := range patched.Authorization {
		if auth.Subject == "" {
			return fmt.Errorf("authorization subject cannot be empty")
		}
	}
	
	return nil
}

// GetDiagramCollaborate handles diagram collaboration endpoint
func (h *DiagramHandler) GetDiagramCollaborate(c *gin.Context) {
	// Parse ID from URL parameter
	idStr := c.Param("id")
	id, err := ParseUUID(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_id",
			Message: "Invalid ID format",
		})
		return
	}
	
	// Return collaboration session details
	session := CollaborationSession{
		DiagramId:     id,
		SessionId:     NewUUID().String(),
		WebsocketUrl:  fmt.Sprintf("/ws/diagrams/%s", id),
		Participants:  []struct{
			JoinedAt *time.Time `json:"joined_at,omitempty"`
			UserId   *string    `json:"user_id,omitempty"`
		}{},
	}
	
	c.JSON(http.StatusOK, session)
}

// PostDiagramCollaborate initiates a collaboration session
func (h *DiagramHandler) PostDiagramCollaborate(c *gin.Context) {
	// Parse ID from URL parameter
	idStr := c.Param("id")
	id, err := ParseUUID(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{
			Error:   "invalid_id",
			Message: "Invalid ID format",
		})
		return
	}
	
	// Get username from context
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok {
		userName = ""
	}
	
	// Create a new collaboration session
	now := time.Now().UTC()
	session := CollaborationSession{
		DiagramId:    id,
		SessionId:    NewUUID().String(),
		WebsocketUrl: fmt.Sprintf("/ws/diagrams/%s", id),
		Participants: []struct{
			JoinedAt *time.Time `json:"joined_at,omitempty"`
			UserId   *string    `json:"user_id,omitempty"`
		}{
			{
				UserId:   &userName,
				JoinedAt: &now,
			},
		},
	}
	
	c.JSON(http.StatusOK, session)
}

// DeleteDiagramCollaborate leaves a collaboration session
func (h *DiagramHandler) DeleteDiagramCollaborate(c *gin.Context) {
	c.Status(http.StatusNoContent)
}