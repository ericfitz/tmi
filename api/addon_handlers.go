package api

import (
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CreateAddonRequest represents the request to create an add-on
type CreateAddonRequest struct {
	Name          string     `json:"name" binding:"required"`
	WebhookID     uuid.UUID  `json:"webhook_id" binding:"required"`
	Description   string     `json:"description,omitempty"`
	Icon          string     `json:"icon,omitempty"`
	Objects       []string   `json:"objects,omitempty"`
	ThreatModelID *uuid.UUID `json:"threat_model_id,omitempty"`
}

// AddonResponse represents the response for add-on operations
type AddonResponse struct {
	ID            uuid.UUID  `json:"id"`
	CreatedAt     time.Time  `json:"created_at"`
	Name          string     `json:"name"`
	WebhookID     uuid.UUID  `json:"webhook_id"`
	Description   string     `json:"description,omitempty"`
	Icon          string     `json:"icon,omitempty"`
	Objects       []string   `json:"objects,omitempty"`
	ThreatModelID *uuid.UUID `json:"threat_model_id,omitempty"`
}

// ListAddonsResponse represents the paginated list response
type ListAddonsResponse struct {
	Addons []AddonResponse `json:"addons"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// CreateAddon creates a new add-on (admin only)
func CreateAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse request
	var req CreateAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Failed to parse create add-on request: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Validate name
	if err := ValidateAddonName(req.Name); err != nil {
		logger.Error("Invalid add-on name: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate description
	if err := ValidateAddonDescription(req.Description); err != nil {
		logger.Error("Invalid add-on description: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate icon
	if err := ValidateIcon(req.Icon); err != nil {
		logger.Error("Invalid add-on icon: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Validate objects
	if err := ValidateObjects(req.Objects); err != nil {
		logger.Error("Invalid add-on objects: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Create add-on
	addon := &Addon{
		CreatedAt:     time.Now(),
		Name:          req.Name,
		WebhookID:     req.WebhookID,
		Description:   req.Description,
		Icon:          req.Icon,
		Objects:       req.Objects,
		ThreatModelID: req.ThreatModelID,
	}

	if err := GlobalAddonStore.Create(c.Request.Context(), addon); err != nil {
		logger.Error("Failed to create add-on: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to create add-on",
		})
		return
	}

	// Return response
	response := AddonResponse{
		ID:            addon.ID,
		CreatedAt:     addon.CreatedAt,
		Name:          addon.Name,
		WebhookID:     addon.WebhookID,
		Description:   addon.Description,
		Icon:          addon.Icon,
		Objects:       addon.Objects,
		ThreatModelID: addon.ThreatModelID,
	}

	logger.Info("Add-on created: id=%s, name=%s", addon.ID, addon.Name)
	c.JSON(http.StatusCreated, response)
}

// GetAddon retrieves a single add-on by ID
func GetAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get addon ID from path
	addonIDStr := c.Param("addon_id")
	addonID, err := uuid.Parse(addonIDStr)
	if err != nil {
		logger.Error("Invalid add-on ID: %s", addonIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid add-on ID format",
		})
		return
	}

	// Get add-on
	addon, err := GlobalAddonStore.Get(c.Request.Context(), addonID)
	if err != nil {
		logger.Error("Failed to get add-on: id=%s, error=%v", addonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Add-on not found",
		})
		return
	}

	// Return response
	response := AddonResponse{
		ID:            addon.ID,
		CreatedAt:     addon.CreatedAt,
		Name:          addon.Name,
		WebhookID:     addon.WebhookID,
		Description:   addon.Description,
		Icon:          addon.Icon,
		Objects:       addon.Objects,
		ThreatModelID: addon.ThreatModelID,
	}

	c.JSON(http.StatusOK, response)
}

// ListAddons retrieves add-ons with pagination
func ListAddons(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse query parameters
	limit := 50 // default
	offset := 0 // default

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := parsePositiveInt(limitStr); err == nil {
			if parsedLimit > 500 {
				parsedLimit = 500 // max limit
			}
			limit = parsedLimit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := parsePositiveInt(offsetStr); err == nil {
			offset = parsedOffset
		}
	}

	// Optional threat model filter
	var threatModelID *uuid.UUID
	if tmIDStr := c.Query("threat_model_id"); tmIDStr != "" {
		if tmID, err := uuid.Parse(tmIDStr); err == nil {
			threatModelID = &tmID
		} else {
			logger.Error("Invalid threat_model_id: %s", tmIDStr)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Invalid threat_model_id format",
			})
			return
		}
	}

	// List add-ons
	addons, total, err := GlobalAddonStore.List(c.Request.Context(), limit, offset, threatModelID)
	if err != nil {
		logger.Error("Failed to list add-ons: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list add-ons",
		})
		return
	}

	// Convert to response format
	var responses []AddonResponse
	for _, addon := range addons {
		responses = append(responses, AddonResponse(addon))
	}

	// Return paginated response
	response := ListAddonsResponse{
		Addons: responses,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}

	c.JSON(http.StatusOK, response)
}

// DeleteAddon deletes an add-on (admin only)
func DeleteAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get addon ID from path
	addonIDStr := c.Param("addon_id")
	addonID, err := uuid.Parse(addonIDStr)
	if err != nil {
		logger.Error("Invalid add-on ID: %s", addonIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid add-on ID format",
		})
		return
	}

	// Check for active invocations
	activeCount, err := GlobalAddonStore.CountActiveInvocations(c.Request.Context(), addonID)
	if err != nil {
		logger.Error("Failed to count active invocations for add-on: id=%s, error=%v", addonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify add-on status",
		})
		return
	}

	if activeCount > 0 {
		logger.Warn("Cannot delete add-on with active invocations: id=%s, count=%d", addonID, activeCount)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: "Cannot delete add-on with active invocations. Please wait for invocations to complete.",
		})
		return
	}

	// Delete add-on
	if err := GlobalAddonStore.Delete(c.Request.Context(), addonID); err != nil {
		logger.Error("Failed to delete add-on: id=%s, error=%v", addonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Add-on not found",
		})
		return
	}

	logger.Info("Add-on deleted: id=%s", addonID)
	c.Status(http.StatusNoContent)
}
