package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// GetUserAPIQuota retrieves the API quota for a specific user (admin only)
func (s *Server) GetUserAPIQuota(c *gin.Context) {
	_ = slogging.Get().WithContext(c)

	// Get user ID from path parameter
	userIDParam := c.Param("user_id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

	// Get quota (or default)
	quota := GlobalUserAPIQuotaStore.GetOrDefault(userID.String())

	c.JSON(http.StatusOK, quota)
}

// UpdateUserAPIQuota creates or updates the API quota for a specific user (admin only)
func (s *Server) UpdateUserAPIQuota(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get user ID from path parameter
	userIDParam := c.Param("user_id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

	// Parse request body
	var req struct {
		MaxRequestsPerMinute int  `json:"max_requests_per_minute" binding:"required,min=1"`
		MaxRequestsPerHour   *int `json:"max_requests_per_hour,omitempty"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid request body: " + err.Error()})
		return
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
func (s *Server) DeleteUserAPIQuota(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get user ID from path parameter
	userIDParam := c.Param("user_id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

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

// GetWebhookQuota retrieves the webhook quota for a specific user (admin only)
func (s *Server) GetWebhookQuota(c *gin.Context) {
	_ = slogging.Get().WithContext(c)

	// Get user ID from path parameter
	userIDParam := c.Param("user_id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

	// Get quota (or default)
	quota := GlobalWebhookQuotaStore.GetOrDefault(userID.String())

	c.JSON(http.StatusOK, quota)
}

// UpdateWebhookQuota creates or updates the webhook quota for a specific user (admin only)
func (s *Server) UpdateWebhookQuota(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get user ID from path parameter
	userIDParam := c.Param("user_id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

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

	// Try to get existing quota
	existingQuota, err := GlobalWebhookQuotaStore.Get(userID.String())
	if err != nil {
		// Doesn't exist, create new one
		newQuota := WebhookQuota{
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
func (s *Server) DeleteWebhookQuota(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get user ID from path parameter
	userIDParam := c.Param("user_id")
	userID, err := uuid.Parse(userIDParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: "invalid user ID format"})
		return
	}

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
