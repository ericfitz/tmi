package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// InvokeAddonRequest represents the request to invoke an add-on
type InvokeAddonRequest struct {
	ThreatModelID uuid.UUID  `json:"threat_model_id" binding:"required"`
	ObjectType    string     `json:"object_type,omitempty"`
	ObjectID      *uuid.UUID `json:"object_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// InvokeAddonResponse represents the response after invoking an add-on
type InvokeAddonResponse struct {
	InvocationID uuid.UUID `json:"invocation_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// InvocationResponse represents a single invocation in responses
type InvocationResponse struct {
	ID              uuid.UUID  `json:"id"`
	AddonID         uuid.UUID  `json:"addon_id"`
	ThreatModelID   uuid.UUID  `json:"threat_model_id"`
	ObjectType      string     `json:"object_type,omitempty"`
	ObjectID        *uuid.UUID `json:"object_id,omitempty"`
	InvokedBy       uuid.UUID  `json:"invoked_by"`
	Payload         string     `json:"payload"`
	Status          string     `json:"status"`
	StatusPercent   int        `json:"status_percent"`
	StatusMessage   string     `json:"status_message,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StatusUpdatedAt time.Time  `json:"status_updated_at"`
}

// ListInvocationsResponse represents paginated invocation list
type ListInvocationsResponse struct {
	Invocations []InvocationResponse `json:"invocations"`
	Total       int                  `json:"total"`
	Limit       int                  `json:"limit"`
	Offset      int                  `json:"offset"`
}

// UpdateInvocationStatusRequest represents request to update invocation status
type UpdateInvocationStatusRequest struct {
	Status        string `json:"status" binding:"required"`
	StatusPercent int    `json:"status_percent"`
	StatusMessage string `json:"status_message,omitempty"`
}

// UpdateInvocationStatusResponse represents response after status update
type UpdateInvocationStatusResponse struct {
	ID              uuid.UUID `json:"id"`
	Status          string    `json:"status"`
	StatusPercent   int       `json:"status_percent"`
	StatusUpdatedAt time.Time `json:"status_updated_at"`
}

// InvokeAddon invokes an add-on (authenticated users)
func InvokeAddon(c *gin.Context) {
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

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Get user ID from context
	var userID uuid.UUID
	if userIDInterface, exists := c.Get("userID"); exists {
		if userIDStr, ok := userIDInterface.(string); ok {
			userID, err = uuid.Parse(userIDStr)
			if err != nil {
				logger.Error("Invalid user ID in context: %s", userIDStr)
				HandleRequestError(c, &RequestError{
					Status:  http.StatusInternalServerError,
					Code:    "server_error",
					Message: "Invalid user context",
				})
				return
			}
		}
	}
	if userID == uuid.Nil {
		logger.Error("User ID not found in context for email: %s", userEmail)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "User ID not available",
		})
		return
	}

	// Parse request
	var req InvokeAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Failed to parse invoke add-on request: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Validate payload size (max 1KB = 1024 bytes)
	if len(req.Payload) > 1024 {
		logger.Error("Payload too large: %d bytes (max 1024)", len(req.Payload))
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Payload exceeds maximum size of 1024 bytes",
		})
		return
	}

	// Get add-on to validate and get details
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

	// Validate object_type if provided
	if req.ObjectType != "" && len(addon.Objects) > 0 {
		validObjectType := false
		for _, obj := range addon.Objects {
			if obj == req.ObjectType {
				validObjectType = true
				break
			}
		}
		if !validObjectType {
			logger.Error("Invalid object_type '%s' for add-on (allowed: %v)", req.ObjectType, addon.Objects)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Object type not supported by this add-on",
			})
			return
		}
	}

	// Check rate limits
	if GlobalAddonRateLimiter != nil {
		// Check active invocation limit (1 concurrent)
		if err := GlobalAddonRateLimiter.CheckActiveInvocationLimit(c.Request.Context(), userID); err != nil {
			logger.Warn("Active invocation limit exceeded for user %s", userID)
			HandleRequestError(c, err)
			return
		}

		// Check hourly rate limit
		if err := GlobalAddonRateLimiter.CheckHourlyRateLimit(c.Request.Context(), userID); err != nil {
			logger.Warn("Hourly rate limit exceeded for user %s", userID)
			HandleRequestError(c, err)
			return
		}

		// Record invocation in sliding window
		if err := GlobalAddonRateLimiter.RecordInvocation(c.Request.Context(), userID); err != nil {
			logger.Error("Failed to record invocation for rate limiting: %v", err)
			// Continue despite error - don't block the invocation
		}
	}

	// Create invocation
	invocation := &AddonInvocation{
		AddonID:       addonID,
		ThreatModelID: req.ThreatModelID,
		ObjectType:    req.ObjectType,
		ObjectID:      req.ObjectID,
		InvokedBy:     userID,
		Payload:       string(req.Payload),
		Status:        InvocationStatusPending,
		StatusPercent: 0,
		CreatedAt:     time.Now(),
	}

	if err := GlobalAddonInvocationStore.Create(c.Request.Context(), invocation); err != nil {
		logger.Error("Failed to create invocation: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to create invocation",
		})
		return
	}

	// Queue invocation for webhook worker
	if GlobalAddonInvocationWorker != nil {
		GlobalAddonInvocationWorker.QueueInvocation(invocation.ID)
	} else {
		logger.Warn("GlobalAddonInvocationWorker not initialized, invocation will not be sent to webhook")
	}

	// Return response
	response := InvokeAddonResponse{
		InvocationID: invocation.ID,
		Status:       invocation.Status,
		CreatedAt:    invocation.CreatedAt,
	}

	logger.Info("Add-on invoked: addon_id=%s, invocation_id=%s, user=%s",
		addonID, invocation.ID, userID)

	c.JSON(http.StatusAccepted, response)
}

// GetInvocation retrieves a single invocation by ID
func GetInvocation(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get invocation ID from path
	invocationIDStr := c.Param("invocation_id")
	invocationID, err := uuid.Parse(invocationIDStr)
	if err != nil {
		logger.Error("Invalid invocation ID: %s", invocationIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid invocation ID format",
		})
		return
	}

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Get user ID
	var userID uuid.UUID
	if userIDInterface, exists := c.Get("userID"); exists {
		if userIDStr, ok := userIDInterface.(string); ok {
			userID, _ = uuid.Parse(userIDStr)
		}
	}

	// Check if user is admin
	isAdmin := false
	if GlobalAdministratorStore != nil {
		var groups []string
		if groupsInterface, exists := c.Get("userGroups"); exists {
			groups, _ = groupsInterface.([]string)
		}
		isAdmin, _ = GlobalAdministratorStore.IsAdmin(c.Request.Context(), &userID, userEmail, groups)
	}

	// Get invocation
	invocation, err := GlobalAddonInvocationStore.Get(c.Request.Context(), invocationID)
	if err != nil {
		logger.Error("Failed to get invocation: id=%s, error=%v", invocationID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Invocation not found or expired",
		})
		return
	}

	// Authorization: user can only see their own invocations unless admin
	if !isAdmin && invocation.InvokedBy != userID {
		logger.Warn("User %s attempted to access invocation belonging to %s",
			userID, invocation.InvokedBy)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Access denied",
		})
		return
	}

	// Return response
	response := InvocationResponse{
		ID:              invocation.ID,
		AddonID:         invocation.AddonID,
		ThreatModelID:   invocation.ThreatModelID,
		ObjectType:      invocation.ObjectType,
		ObjectID:        invocation.ObjectID,
		InvokedBy:       invocation.InvokedBy,
		Payload:         invocation.Payload,
		Status:          invocation.Status,
		StatusPercent:   invocation.StatusPercent,
		StatusMessage:   invocation.StatusMessage,
		CreatedAt:       invocation.CreatedAt,
		StatusUpdatedAt: invocation.StatusUpdatedAt,
	}

	c.JSON(http.StatusOK, response)
}

// ListInvocations lists invocations with pagination and filtering
func ListInvocations(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	userEmail, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Get user ID
	var userID uuid.UUID
	if userIDInterface, exists := c.Get("userID"); exists {
		if userIDStr, ok := userIDInterface.(string); ok {
			userID, _ = uuid.Parse(userIDStr)
		}
	}

	// Check if user is admin
	isAdmin := false
	if GlobalAdministratorStore != nil {
		var groups []string
		if groupsInterface, exists := c.Get("userGroups"); exists {
			groups, _ = groupsInterface.([]string)
		}
		isAdmin, _ = GlobalAdministratorStore.IsAdmin(c.Request.Context(), &userID, userEmail, groups)
	}

	// Parse query parameters
	limit := 50
	offset := 0
	status := c.Query("status")

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := parsePositiveInt(limitStr); err == nil {
			if parsedLimit > 500 {
				parsedLimit = 500
			}
			limit = parsedLimit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := parsePositiveInt(offsetStr); err == nil {
			offset = parsedOffset
		}
	}

	// Filter by user unless admin
	var filterUserID *uuid.UUID
	if !isAdmin {
		filterUserID = &userID
	}

	// List invocations
	invocations, total, err := GlobalAddonInvocationStore.List(
		c.Request.Context(),
		filterUserID,
		status,
		limit,
		offset,
	)
	if err != nil {
		logger.Error("Failed to list invocations: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to list invocations",
		})
		return
	}

	// Convert to response format
	var responses []InvocationResponse
	for _, inv := range invocations {
		responses = append(responses, InvocationResponse{
			ID:              inv.ID,
			AddonID:         inv.AddonID,
			ThreatModelID:   inv.ThreatModelID,
			ObjectType:      inv.ObjectType,
			ObjectID:        inv.ObjectID,
			InvokedBy:       inv.InvokedBy,
			Payload:         inv.Payload,
			Status:          inv.Status,
			StatusPercent:   inv.StatusPercent,
			StatusMessage:   inv.StatusMessage,
			CreatedAt:       inv.CreatedAt,
			StatusUpdatedAt: inv.StatusUpdatedAt,
		})
	}

	// Return paginated response
	response := ListInvocationsResponse{
		Invocations: responses,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
	}

	c.JSON(http.StatusOK, response)
}

// UpdateInvocationStatus updates the status of an invocation (HMAC authenticated)
func UpdateInvocationStatus(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get invocation ID from path
	invocationIDStr := c.Param("invocation_id")
	invocationID, err := uuid.Parse(invocationIDStr)
	if err != nil {
		logger.Error("Invalid invocation ID: %s", invocationIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid invocation ID format",
		})
		return
	}

	// Parse request
	var req UpdateInvocationStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Failed to parse status update request: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Validate status
	validStatuses := map[string]bool{
		InvocationStatusInProgress: true,
		InvocationStatusCompleted:  true,
		InvocationStatusFailed:     true,
	}
	if !validStatuses[req.Status] {
		logger.Error("Invalid status: %s", req.Status)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid status. Must be: in_progress, completed, or failed",
		})
		return
	}

	// Validate status_percent
	if req.StatusPercent < 0 || req.StatusPercent > 100 {
		logger.Error("Invalid status_percent: %d", req.StatusPercent)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Status percent must be between 0 and 100",
		})
		return
	}

	// Get invocation
	invocation, err := GlobalAddonInvocationStore.Get(c.Request.Context(), invocationID)
	if err != nil {
		logger.Error("Failed to get invocation: id=%s, error=%v", invocationID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Invocation not found or expired",
		})
		return
	}

	// Get addon to get webhook details
	addon, err := GlobalAddonStore.Get(c.Request.Context(), invocation.AddonID)
	if err != nil {
		logger.Error("Failed to get addon: id=%s, error=%v", invocation.AddonID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify invocation",
		})
		return
	}

	// Get webhook to verify signature
	webhook, err := GlobalWebhookSubscriptionStore.Get(addon.WebhookID.String())
	if err != nil {
		logger.Error("Failed to get webhook: id=%s, error=%v", addon.WebhookID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify invocation",
		})
		return
	}

	// Verify HMAC signature
	if webhook.Secret != "" {
		// Get request body for signature verification
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			logger.Error("Failed to read request body: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request",
				Message: "Failed to read request body",
			})
			return
		}

		// Get signature from header
		signature := c.GetHeader("X-Webhook-Signature")
		if signature == "" {
			logger.Warn("Missing HMAC signature for invocation status update: %s", invocationID)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Missing webhook signature",
			})
			return
		}

		// Verify signature
		if !VerifySignature(bodyBytes, signature, webhook.Secret) {
			logger.Warn("Invalid HMAC signature for invocation status update: %s", invocationID)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Invalid webhook signature",
			})
			return
		}

		logger.Debug("HMAC signature verified for invocation status update: %s", invocationID)
	} else {
		logger.Warn("Webhook has no secret, skipping HMAC verification for invocation: %s", invocationID)
	}

	// Validate status transition
	if invocation.Status == InvocationStatusCompleted || invocation.Status == InvocationStatusFailed {
		logger.Warn("Cannot update completed/failed invocation: id=%s, current_status=%s",
			invocationID, invocation.Status)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: "Cannot update invocation that is already completed or failed",
		})
		return
	}

	// Update invocation
	invocation.Status = req.Status
	invocation.StatusPercent = req.StatusPercent
	invocation.StatusMessage = req.StatusMessage

	if err := GlobalAddonInvocationStore.Update(c.Request.Context(), invocation); err != nil {
		logger.Error("Failed to update invocation: id=%s, error=%v", invocationID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to update invocation status",
		})
		return
	}

	// Return response
	response := UpdateInvocationStatusResponse{
		ID:              invocation.ID,
		Status:          invocation.Status,
		StatusPercent:   invocation.StatusPercent,
		StatusUpdatedAt: invocation.StatusUpdatedAt,
	}

	logger.Info("Invocation status updated: id=%s, status=%s, percent=%d",
		invocationID, req.Status, req.StatusPercent)

	c.JSON(http.StatusOK, response)
}
