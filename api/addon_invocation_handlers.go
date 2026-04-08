package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// invokerContext holds the authenticated user context for an addon invocation
type invokerContext struct {
	userEmail string
	userUUID  uuid.UUID
	userName  string
}

// extractInvokerContext extracts and validates the authenticated user context from a gin context.
// Returns an error suitable for HandleRequestError if validation fails.
func extractInvokerContext(c *gin.Context) (*invokerContext, error) {
	logger := slogging.Get().WithContext(c)

	userEmail, providerID, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		return nil, err
	}
	_ = providerID // available if needed for logging

	var userUUID uuid.UUID
	if internalUUIDInterface, exists := c.Get("userInternalUUID"); exists {
		if uuidVal, ok := internalUUIDInterface.(uuid.UUID); ok {
			userUUID = uuidVal
		} else if uuidStr, ok := internalUUIDInterface.(string); ok {
			userUUID, err = uuid.Parse(uuidStr)
			if err != nil {
				logger.Error("Invalid user internal UUID in context: %s", uuidStr)
				return nil, &RequestError{
					Status:  http.StatusUnauthorized,
					Code:    "unauthorized",
					Message: "Invalid authentication context",
				}
			}
		}
	}
	if userUUID == uuid.Nil {
		logger.Error("User internal UUID not found in context for email: %s", userEmail)
		return nil, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "User identity not available",
		}
	}

	userName := userEmail
	if userNameInterface, exists := c.Get("userDisplayName"); exists {
		if nameStr, ok := userNameInterface.(string); ok && nameStr != "" {
			userName = nameStr
		}
	}

	return &invokerContext{
		userEmail: userEmail,
		userUUID:  userUUID,
		userName:  userName,
	}, nil
}

// validateAddonInvocationRequest validates the request payload and addon configuration.
// Returns the parsed request, payload string, and addon, or an error.
func validateAddonInvocationRequest(c *gin.Context, addonID uuid.UUID) (*InvokeAddonRequest, string, *Addon, error) {
	logger := slogging.Get().WithContext(c)

	var req InvokeAddonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error("Failed to parse invoke add-on request: %v", err)
		return nil, "", nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		}
	}

	payloadStr := payloadToString(req.Data)
	if len(payloadStr) > 1024 {
		logger.Error("Payload too large: %d bytes (max 1024)", len(payloadStr))
		return nil, "", nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Payload exceeds maximum size of 1024 bytes",
		}
	}

	addon, err := GlobalAddonStore.Get(c.Request.Context(), addonID)
	if err != nil {
		logger.Error("Failed to get add-on: id=%s, error=%v", addonID, err)
		return nil, "", nil, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Add-on not found",
		}
	}

	if req.ObjectType != nil && *req.ObjectType != "" && len(addon.Objects) > 0 {
		if !slices.Contains(addon.Objects, string(*req.ObjectType)) {
			logger.Error("Invalid object_type '%s' for add-on (allowed: %v)", string(*req.ObjectType), addon.Objects)
			return nil, "", nil, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Object type not supported by this add-on",
			}
		}
	}

	if len(addon.Parameters) > 0 {
		var dataMap map[string]interface{}
		if req.Data != nil {
			dataMap = *req.Data
		}
		if err := ValidateInvocationData(dataMap, addon.Parameters); err != nil {
			logger.Error("Invalid invocation data for add-on parameters: %v", err)
			return nil, "", nil, err
		}
	}

	return &req, payloadStr, addon, nil
}

// InvokeAddon invokes an add-on (authenticated users).
// Creates a WebhookDeliveryRecord and emits an addon.invoked event.
func InvokeAddon(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get addon ID from path
	addonIDStr := c.Param("id")
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

	// Extract and validate user context
	invoker, err := extractInvokerContext(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate request and addon
	req, payloadStr, addon, err := validateAddonInvocationRequest(c, addonID)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Check rate limits
	if GlobalAddonRateLimiter != nil {
		if err := GlobalAddonRateLimiter.CheckActiveInvocationLimit(c.Request.Context(), invoker.userUUID); err != nil {
			logger.Warn("Active invocation limit exceeded for user %s", invoker.userUUID)
			HandleRequestError(c, err)
			return
		}

		if err := GlobalAddonRateLimiter.CheckHourlyRateLimit(c.Request.Context(), invoker.userUUID); err != nil {
			logger.Warn("Hourly rate limit exceeded for user %s", invoker.userUUID)
			HandleRequestError(c, err)
			return
		}

		if err := GlobalAddonRateLimiter.RecordInvocation(c.Request.Context(), invoker.userUUID); err != nil {
			logger.Error("Failed to record invocation for rate limiting: %v", err)
		}

		if err := checkInvocationDeduplication(c.Request.Context(), addonID, invoker.userUUID); err != nil {
			logger.Warn("Duplicate invocation detected for user %s, addon %s", invoker.userUUID, addonID)
			HandleRequestError(c, err)
			return
		}
	}

	// Build addon-specific data for the unified payload
	userData := json.RawMessage(payloadStr)
	deliveryData := WebhookDeliveryData{
		AddonID:  &addonID,
		UserData: &userData,
	}
	dataBytes, err := json.Marshal(deliveryData)
	if err != nil {
		logger.Error("Failed to marshal delivery data: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to prepare invocation data",
		})
		return
	}

	// Build unified envelope payload
	envelope := WebhookDeliveryPayload{
		EventType:     "addon.invoked",
		ThreatModelID: req.ThreatModelId,
		ObjectType:    toObjectTypeString(req.ObjectType),
		ObjectID:      req.ObjectId,
		Timestamp:     time.Now().UTC(),
		Data:          json.RawMessage(dataBytes),
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		logger.Error("Failed to marshal envelope payload: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to prepare invocation payload",
		})
		return
	}

	// Create delivery record in the unified webhook delivery store
	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery store not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Delivery tracking not available",
		})
		return
	}

	deliveryRecord := &WebhookDeliveryRecord{
		SubscriptionID: addon.WebhookID,
		EventType:      "addon.invoked",
		Payload:        string(envelopeBytes),
		Status:         DeliveryStatusPending,
		AddonID:        &addonID,
		InvokedByUUID:  &invoker.userUUID,
		InvokedByEmail: invoker.userEmail,
		InvokedByName:  invoker.userName,
	}

	if err := GlobalWebhookDeliveryRedisStore.Create(c.Request.Context(), deliveryRecord); err != nil {
		logger.Error("Failed to create delivery record: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to create invocation",
		})
		return
	}

	// Emit addon.invoked event via the event emitter so the unified delivery worker picks it up
	if GlobalEventEmitter != nil {
		emitErr := GlobalEventEmitter.EmitEvent(c.Request.Context(), EventPayload{
			EventType:     "addon.invoked",
			ThreatModelID: req.ThreatModelId.String(),
			ObjectID:      addonID.String(),
			ObjectType:    "addon",
			OwnerID:       invoker.userUUID.String(),
			Timestamp:     time.Now().UTC(),
			Data: map[string]any{
				"delivery_id":     deliveryRecord.ID.String(),
				"subscription_id": addon.WebhookID.String(),
				"addon_id":        addonID.String(),
			},
		})
		if emitErr != nil {
			logger.Error("Failed to emit addon.invoked event: %v", emitErr)
			// Don't fail the invocation for this — the delivery record is already created
			// and the delivery worker will pick it up on its next poll
		}
	} else {
		logger.Warn("GlobalEventEmitter not initialized, addon invocation will be picked up by delivery worker polling")
	}

	// Return response
	response := InvokeAddonResponse{
		DeliveryId: deliveryRecord.ID,
		Status:     statusToInvokeAddonResponseStatus(deliveryRecord.Status),
		CreatedAt:  deliveryRecord.CreatedAt,
	}

	userIdentity := GetUserIdentityForLogging(c)
	logger.Info("Add-on invoked: addon_id=%s, delivery_id=%s, %s",
		addonID, deliveryRecord.ID, userIdentity)

	c.JSON(http.StatusAccepted, response)
}

// checkInvocationDeduplication checks if the same user has invoked the same addon within the deduplication window
func checkInvocationDeduplication(ctx context.Context, addonID uuid.UUID, userID uuid.UUID) error {
	logger := slogging.Get()

	if GlobalAddonRateLimiter == nil || GlobalAddonRateLimiter.redis == nil {
		logger.Warn("Redis not available for deduplication check")
		return nil // Allow invocation if Redis is not available
	}

	// Create deduplication key
	dedupKey := fmt.Sprintf("addon:dedup:%s:%s", addonID.String(), userID.String())

	// Try to set the key with NX (only if not exists) and 5-second TTL
	client := GlobalAddonRateLimiter.redis.GetClient()
	err := client.SetArgs(ctx, dedupKey, "1", redis.SetArgs{Mode: "NX", TTL: 5 * time.Second}).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		logger.Error("Failed to check deduplication: %v", err)
		return nil // Allow invocation on error - better than blocking legitimate requests
	}

	if errors.Is(err, redis.Nil) {
		// Key already exists — this is a duplicate invocation within the window
		logger.Debug("Duplicate invocation blocked: addon=%s, user=%s", addonID, userID)
		return &RequestError{
			Status:  http.StatusTooManyRequests,
			Code:    "duplicate_invocation",
			Message: "You just invoked this add-on. Please wait a few seconds before invoking again.",
		}
	}

	logger.Debug("Deduplication check passed: addon=%s, user=%s", addonID, userID)
	return nil
}
