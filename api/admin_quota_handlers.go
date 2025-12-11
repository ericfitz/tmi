package api

import (
	"context"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ListUserAPIQuotas retrieves all custom user API quotas (admin only)
func (s *Server) ListUserAPIQuotas(c *gin.Context, params ListUserAPIQuotasParams) {
	logger := slogging.Get().WithContext(c)

	// Set default values if not provided
	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Get quotas
	quotas, err := GlobalUserAPIQuotaStore.List(offset, limit)
	if err != nil {
		logger.Error("failed to list user API quotas: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to list quotas"})
		return
	}

	c.JSON(http.StatusOK, quotas)
}

// GetUserAPIQuota retrieves the API quota for a specific user (admin only)
func (s *Server) GetUserAPIQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Validate user ID format (should be done by OpenAPI, but defensive check)
	if userID.String() == "" {
		logger.Error("Invalid user ID in GetUserAPIQuota: empty UUID")
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

	// Get quota (or default) with panic recovery
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in GetUserAPIQuota for user %s: %v", userID, r)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to retrieve quota"})
		}
	}()

	// Get quota (or default)
	quota := GlobalUserAPIQuotaStore.GetOrDefault(userID.String())

	c.JSON(http.StatusOK, quota)
}

// UpdateUserAPIQuota creates or updates the API quota for a specific user (admin only)
func (s *Server) UpdateUserAPIQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Parse request body
	var req struct {
		MaxRequestsPerMinute int  `json:"max_requests_per_minute" binding:"required,min=1"`
		MaxRequestsPerHour   *int `json:"max_requests_per_hour,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid request body: " + err.Error()})
		return
	}

	// Validate quota bounds
	if err := ValidateQuotaValue(req.MaxRequestsPerMinute, 1, MaxRequestsPerMinute, "max_requests_per_minute"); err != nil {
		HandleRequestError(c, err)
		return
	}
	if req.MaxRequestsPerHour != nil {
		if err := ValidateQuotaValue(*req.MaxRequestsPerHour, 1, MaxRequestsPerHour, "max_requests_per_hour"); err != nil {
			HandleRequestError(c, err)
			return
		}
	}

	// Try to get existing quota
	existingQuota, err := GlobalUserAPIQuotaStore.Get(userID.String())
	if err != nil {
		// Doesn't exist, create new one
		newQuota := UserAPIQuota{
			UserId:               userID,
			MaxRequestsPerMinute: req.MaxRequestsPerMinute,
			MaxRequestsPerHour:   req.MaxRequestsPerHour,
		}

		createdQuota, err := GlobalUserAPIQuotaStore.Create(newQuota)
		if err != nil {
			logger.Error("failed to create user API quota for %s: %v", userID, err)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to create quota"})
			return
		}

		logger.Info("created user API quota for user %s: %d req/min", userID, req.MaxRequestsPerMinute)
		c.JSON(http.StatusCreated, createdQuota)
		return
	}

	// Update existing quota
	existingQuota.MaxRequestsPerMinute = req.MaxRequestsPerMinute
	existingQuota.MaxRequestsPerHour = req.MaxRequestsPerHour

	if err := GlobalUserAPIQuotaStore.Update(userID.String(), existingQuota); err != nil {
		logger.Error("failed to update user API quota for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to update quota"})
		return
	}

	// Invalidate cache for this user
	if GlobalQuotaCache != nil {
		GlobalQuotaCache.InvalidateUserAPIQuota(userID.String())
	}

	// Get updated quota
	updatedQuota := GlobalUserAPIQuotaStore.GetOrDefault(userID.String())

	logger.Info("updated user API quota for user %s: %d req/min", userID, req.MaxRequestsPerMinute)
	c.JSON(http.StatusOK, updatedQuota)
}

// DeleteUserAPIQuota deletes the API quota for a specific user, reverting to defaults (admin only)
func (s *Server) DeleteUserAPIQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Delete quota
	if err := GlobalUserAPIQuotaStore.Delete(userID.String()); err != nil {
		logger.Error("failed to delete user API quota for %s: %v", userID, err)
		c.JSON(http.StatusNotFound, Error{Error: "quota not found"})
		return
	}

	// Invalidate cache for this user
	if GlobalQuotaCache != nil {
		GlobalQuotaCache.InvalidateUserAPIQuota(userID.String())
	}

	logger.Info("deleted user API quota for user %s (reverted to defaults)", userID)
	c.Status(http.StatusNoContent)
}

// ListWebhookQuotas retrieves all custom webhook quotas (admin only)
func (s *Server) ListWebhookQuotas(c *gin.Context, params ListWebhookQuotasParams) {
	logger := slogging.Get().WithContext(c)

	// Set default values if not provided
	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Get quotas
	quotas, err := GlobalWebhookQuotaStore.List(offset, limit)
	if err != nil {
		logger.Error("failed to list webhook quotas: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to list quotas"})
		return
	}

	c.JSON(http.StatusOK, quotas)
}

// GetWebhookQuota retrieves the webhook quota for a specific user (admin only)
func (s *Server) GetWebhookQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Validate user ID format (should be done by OpenAPI, but defensive check)
	if userID.String() == "" {
		logger.Error("Invalid user ID in GetWebhookQuota: empty UUID")
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

	// Get quota (or default) with error handling
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in GetWebhookQuota for user %s: %v", userID, r)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to retrieve quota"})
		}
	}()

	quota := GlobalWebhookQuotaStore.GetOrDefault(userID.String())

	c.JSON(http.StatusOK, quota)
}

// UpdateWebhookQuota creates or updates the webhook quota for a specific user (admin only)
func (s *Server) UpdateWebhookQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Parse request body
	var req struct {
		MaxSubscriptions                 int `json:"max_subscriptions" binding:"required,min=1"`
		MaxEventsPerMinute               int `json:"max_events_per_minute" binding:"required,min=1"`
		MaxSubscriptionRequestsPerMinute int `json:"max_subscription_requests_per_minute" binding:"required,min=1"`
		MaxSubscriptionRequestsPerDay    int `json:"max_subscription_requests_per_day" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid request body: " + err.Error()})
		return
	}

	// Validate quota bounds
	if err := ValidateQuotaValue(req.MaxSubscriptions, 1, MaxSubscriptions, "max_subscriptions"); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := ValidateQuotaValue(req.MaxEventsPerMinute, 1, MaxEventsPerMinute, "max_events_per_minute"); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := ValidateQuotaValue(req.MaxSubscriptionRequestsPerMinute, 1, MaxSubscriptionRequestsPerMinute, "max_subscription_requests_per_minute"); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := ValidateQuotaValue(req.MaxSubscriptionRequestsPerDay, 1, MaxSubscriptionRequestsPerDay, "max_subscription_requests_per_day"); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Try to get existing quota
	existingQuota, err := GlobalWebhookQuotaStore.Get(userID.String())
	if err != nil {
		// Doesn't exist, create new one
		newQuota := DBWebhookQuota{
			OwnerId:                          userID,
			MaxSubscriptions:                 req.MaxSubscriptions,
			MaxEventsPerMinute:               req.MaxEventsPerMinute,
			MaxSubscriptionRequestsPerMinute: req.MaxSubscriptionRequestsPerMinute,
			MaxSubscriptionRequestsPerDay:    req.MaxSubscriptionRequestsPerDay,
		}

		createdQuota, err := GlobalWebhookQuotaStore.Create(newQuota)
		if err != nil {
			logger.Error("failed to create webhook quota for %s: %v", userID, err)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to create quota"})
			return
		}

		logger.Info("created webhook quota for user %s", userID)
		c.JSON(http.StatusCreated, createdQuota)
		return
	}

	// Update existing quota
	existingQuota.MaxSubscriptions = req.MaxSubscriptions
	existingQuota.MaxEventsPerMinute = req.MaxEventsPerMinute
	existingQuota.MaxSubscriptionRequestsPerMinute = req.MaxSubscriptionRequestsPerMinute
	existingQuota.MaxSubscriptionRequestsPerDay = req.MaxSubscriptionRequestsPerDay

	if err := GlobalWebhookQuotaStore.Update(userID.String(), existingQuota); err != nil {
		logger.Error("failed to update webhook quota for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to update quota"})
		return
	}

	// Invalidate cache for this user
	if GlobalQuotaCache != nil {
		GlobalQuotaCache.InvalidateWebhookQuota(userID.String())
	}

	// Get updated quota
	updatedQuota := GlobalWebhookQuotaStore.GetOrDefault(userID.String())

	logger.Info("updated webhook quota for user %s", userID)
	c.JSON(http.StatusOK, updatedQuota)
}

// DeleteWebhookQuota deletes the webhook quota for a specific user, reverting to defaults (admin only)
func (s *Server) DeleteWebhookQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Delete quota
	if err := GlobalWebhookQuotaStore.Delete(userID.String()); err != nil {
		logger.Error("failed to delete webhook quota for %s: %v", userID, err)
		c.JSON(http.StatusNotFound, Error{Error: "quota not found"})
		return
	}

	// Invalidate cache for this user
	if GlobalQuotaCache != nil {
		GlobalQuotaCache.InvalidateWebhookQuota(userID.String())
	}

	logger.Info("deleted webhook quota for user %s (reverted to defaults)", userID)
	c.Status(http.StatusNoContent)
}

// ListAddonInvocationQuotas retrieves all custom addon invocation quotas (admin only)
func (s *Server) ListAddonInvocationQuotas(c *gin.Context, params ListAddonInvocationQuotasParams) {
	logger := slogging.Get().WithContext(c)

	// Set default values if not provided
	limit := 50
	offset := 0
	if params.Limit != nil {
		limit = *params.Limit
	}
	if params.Offset != nil {
		offset = *params.Offset
	}

	// Get quotas
	quotas, err := GlobalAddonInvocationQuotaStore.List(context.Background(), offset, limit)
	if err != nil {
		logger.Error("failed to list addon invocation quotas: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to list quotas"})
		return
	}

	// Convert to API response format
	responseQuotas := make([]AddonInvocationQuota, len(quotas))
	for i, q := range quotas {
		responseQuotas[i] = *q
	}

	c.JSON(http.StatusOK, responseQuotas)
}

// GetAddonInvocationQuota retrieves the addon invocation quota for a specific user (admin only)
func (s *Server) GetAddonInvocationQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Validate user ID format (should be done by OpenAPI, but defensive check)
	if userID.String() == "" {
		logger.Error("Invalid user ID in GetAddonInvocationQuota: empty UUID")
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

	// Get quota (or default) with panic recovery
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in GetAddonInvocationQuota for user %s: %v", userID, r)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to retrieve quota"})
		}
	}()

	// Get quota (or default)
	quota, err := GlobalAddonInvocationQuotaStore.GetOrDefault(context.Background(), userID)
	if err != nil {
		logger.Error("failed to get addon invocation quota for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to get quota"})
		return
	}

	c.JSON(http.StatusOK, quota)
}

// UpdateAddonInvocationQuota creates or updates the addon invocation quota for a specific user (admin only)
func (s *Server) UpdateAddonInvocationQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Parse request body
	var req struct {
		MaxActiveInvocations  int `json:"max_active_invocations" binding:"required,min=1"`
		MaxInvocationsPerHour int `json:"max_invocations_per_hour" binding:"required,min=1"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid request body: " + err.Error()})
		return
	}

	// Validate quota bounds
	if err := ValidateQuotaValue(req.MaxActiveInvocations, 1, MaxActiveInvocations, "max_active_invocations"); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := ValidateQuotaValue(req.MaxInvocationsPerHour, 1, MaxInvocationsPerHour, "max_invocations_per_hour"); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Try to get existing quota
	existingQuota, err := GlobalAddonInvocationQuotaStore.Get(context.Background(), userID)
	isNew := err != nil

	// Create new quota structure
	newQuota := &AddonInvocationQuota{
		OwnerId:               userID,
		MaxActiveInvocations:  req.MaxActiveInvocations,
		MaxInvocationsPerHour: req.MaxInvocationsPerHour,
	}

	// Preserve timestamps if updating
	if !isNew {
		newQuota.CreatedAt = existingQuota.CreatedAt
	}

	// Set quota
	if err := GlobalAddonInvocationQuotaStore.Set(context.Background(), newQuota); err != nil {
		logger.Error("failed to set addon invocation quota for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to set quota"})
		return
	}

	// Get final quota
	finalQuota, err := GlobalAddonInvocationQuotaStore.GetOrDefault(context.Background(), userID)
	if err != nil {
		logger.Error("failed to retrieve addon invocation quota for %s: %v", userID, err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to retrieve quota"})
		return
	}

	logger.Info("set addon invocation quota for user %s: active=%d, hourly=%d", userID, req.MaxActiveInvocations, req.MaxInvocationsPerHour)

	if isNew {
		c.JSON(http.StatusCreated, finalQuota)
	} else {
		c.JSON(http.StatusOK, finalQuota)
	}
}

// DeleteAddonInvocationQuota deletes the addon invocation quota for a specific user, reverting to defaults (admin only)
func (s *Server) DeleteAddonInvocationQuota(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	userID := userId

	// Delete quota
	if err := GlobalAddonInvocationQuotaStore.Delete(context.Background(), userID); err != nil {
		logger.Error("failed to delete addon invocation quota for %s: %v", userID, err)
		c.JSON(http.StatusNotFound, Error{Error: "quota not found"})
		return
	}

	logger.Info("deleted addon invocation quota for user %s (reverted to defaults)", userID)
	c.Status(http.StatusNoContent)
}
