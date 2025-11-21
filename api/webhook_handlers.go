package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ListWebhookSubscriptions lists webhook subscriptions for the authenticated user
func (s *Server) ListWebhookSubscriptions(c *gin.Context, params ListWebhookSubscriptionsParams) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	_, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// TODO: Implement full handler logic
	c.JSON(http.StatusOK, []WebhookSubscription{})
}

// CreateWebhookSubscription creates a new webhook subscription
func (s *Server) CreateWebhookSubscription(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	userID, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// Check rate limits if webhook rate limiter is available
	if s.webhookRateLimiter != nil {
		// Check subscription count limit
		if err := s.webhookRateLimiter.CheckSubscriptionLimit(c.Request.Context(), userID); err != nil {
			logger.Warn("subscription limit check failed for user %s: %v", userID, err)
			c.JSON(http.StatusTooManyRequests, Error{Error: err.Error()})
			return
		}

		// Check subscription request rate limit
		if err := s.webhookRateLimiter.CheckSubscriptionRequestLimit(c.Request.Context(), userID); err != nil {
			logger.Warn("subscription request rate limit exceeded for user %s: %v", userID, err)
			c.JSON(http.StatusTooManyRequests, Error{Error: err.Error()})
			return
		}
	}

	// TODO: Implement full handler logic
	c.JSON(http.StatusNotImplemented, Error{Error: "not yet implemented"})
}

// GetWebhookSubscription gets a specific webhook subscription
func (s *Server) GetWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	_, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// TODO: Implement full handler logic
	c.JSON(http.StatusNotImplemented, Error{Error: "not yet implemented"})
}

// DeleteWebhookSubscription deletes a webhook subscription
func (s *Server) DeleteWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	_, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// TODO: Implement full handler logic
	c.Status(http.StatusNotImplemented)
}

// TestWebhookSubscription sends a test event to the webhook
func (s *Server) TestWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	_, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// TODO: Implement full handler logic
	c.JSON(http.StatusNotImplemented, Error{Error: "not yet implemented"})
}

// ListWebhookDeliveries lists webhook deliveries for the authenticated user
func (s *Server) ListWebhookDeliveries(c *gin.Context, params ListWebhookDeliveriesParams) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	_, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// TODO: Implement full handler logic
	c.JSON(http.StatusOK, []WebhookDelivery{})
}

// GetWebhookDelivery gets a specific webhook delivery
func (s *Server) GetWebhookDelivery(c *gin.Context, deliveryId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get authenticated user
	_, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("authentication failed: %v", err)
		c.JSON(http.StatusUnauthorized, Error{Error: "authentication required"})
		return
	}

	// TODO: Implement full handler logic
	c.JSON(http.StatusNotImplemented, Error{Error: "not yet implemented"})
}
