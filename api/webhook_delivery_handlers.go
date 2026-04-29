package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// WebhookDeliveryPayload represents the unified payload sent to webhook endpoints.
// Used for all webhook deliveries (resource-change events and addon invocations).
type WebhookDeliveryPayload struct {
	EventType     string          `json:"event_type"`
	ThreatModelID uuid.UUID       `json:"threat_model_id"`
	Timestamp     time.Time       `json:"timestamp"`
	ObjectType    string          `json:"object_type,omitempty"`
	ObjectID      *uuid.UUID      `json:"object_id,omitempty"`
	Data          json.RawMessage `json:"data"`
}

// WebhookDeliveryData contains addon-specific fields within the unified payload data.
type WebhookDeliveryData struct {
	AddonID  *uuid.UUID       `json:"addon_id,omitempty"`
	UserData *json.RawMessage `json:"user_data,omitempty"`
}

// VerifySignature verifies the HMAC signature of a request.
// Delegates to the consolidated crypto package.
func VerifySignature(payload []byte, signature string, secret string) bool {
	return crypto.VerifyHMACSignature(payload, signature, secret)
}

// GetWebhookDeliveryStatus retrieves a webhook delivery record.
// Supports dual auth: JWT (admin, subscription owner, or addon invoker) or HMAC (webhook receiver).
func GetWebhookDeliveryStatus(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get delivery ID from path
	deliveryIDStr := c.Param("delivery_id")
	deliveryID, err := uuid.Parse(deliveryIDStr)
	if err != nil {
		logger.Error("Invalid delivery ID: %s", deliveryIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid delivery ID format",
		})
		return
	}

	// Get delivery record
	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery store not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Delivery tracking not available",
		})
		return
	}

	record, err := GlobalWebhookDeliveryRedisStore.Get(c.Request.Context(), deliveryID)
	if err != nil {
		logger.Error("Failed to get delivery record: id=%s, error=%v", deliveryID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Delivery record not found or expired",
		})
		return
	}

	// Dual auth: try HMAC first, then JWT
	hmacSignature := c.GetHeader("X-Webhook-Signature")
	if hmacSignature != "" {
		// HMAC auth: verify against subscription secret
		if err := verifyDeliveryHMAC(c, record, hmacSignature, deliveryIDStr); err != nil {
			HandleRequestError(c, err)
			return
		}
	} else {
		// JWT auth: must be admin, subscription owner, or addon invoker
		if err := verifyDeliveryJWTAccess(c, record); err != nil {
			HandleRequestError(c, err)
			return
		}
	}

	// Return response
	response := deliveryRecordToWebhookDelivery(record)
	c.JSON(http.StatusOK, response)
}

// UpdateWebhookDeliveryStatus updates the status of a webhook delivery (HMAC authenticated).
func UpdateWebhookDeliveryStatus(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get delivery ID from path
	deliveryIDStr := c.Param("delivery_id")
	deliveryID, err := uuid.Parse(deliveryIDStr)
	if err != nil {
		logger.Error("Invalid delivery ID: %s", deliveryIDStr)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid delivery ID format",
		})
		return
	}

	// Read request body for HMAC verification (must be read before binding)
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

	// Parse request from body bytes
	var req UpdateWebhookDeliveryStatusRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		logger.Error("Failed to parse status update request: %v", err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "Invalid request body",
		})
		return
	}

	// Validate status
	validStatuses := map[UpdateWebhookDeliveryStatusRequestStatus]bool{
		UpdateWebhookDeliveryStatusRequestStatusInProgress: true,
		UpdateWebhookDeliveryStatusRequestStatusCompleted:  true,
		UpdateWebhookDeliveryStatusRequestStatusFailed:     true,
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
	if req.StatusPercent != nil && (*req.StatusPercent < 0 || *req.StatusPercent > 100) {
		logger.Error("Invalid status_percent: %d", *req.StatusPercent)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Status percent must be between 0 and 100",
		})
		return
	}

	// Validate status_message length (max 1024 characters)
	const maxStatusMessageLength = 1024
	if req.StatusMessage != nil && len(*req.StatusMessage) > maxStatusMessageLength {
		logger.Error("Status message too long: %d characters", len(*req.StatusMessage))
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Status message exceeds maximum length of 1024 characters",
		})
		return
	}

	// Get delivery record
	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery store not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Delivery tracking not available",
		})
		return
	}

	record, err := GlobalWebhookDeliveryRedisStore.Get(c.Request.Context(), deliveryID)
	if err != nil {
		logger.Error("Failed to get delivery record: id=%s, error=%v", deliveryID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Delivery record not found or expired",
		})
		return
	}

	// Get subscription to verify HMAC signature
	if GlobalWebhookSubscriptionStore == nil {
		logger.Error("Webhook subscription store not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Webhook service not available",
		})
		return
	}

	webhook, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), record.SubscriptionID.String())
	if err != nil {
		logger.Error("Failed to get webhook: id=%s, error=%v", record.SubscriptionID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify delivery",
		})
		return
	}

	// Verify HMAC signature (required for status updates)
	signature := c.GetHeader("X-Webhook-Signature")
	if webhook.Secret != "" {
		if signature == "" {
			logger.Warn("Missing HMAC signature for delivery status update: %s", deliveryID)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Missing webhook signature",
			})
			return
		}

		if !VerifySignature(bodyBytes, signature, webhook.Secret) {
			logger.Warn("Invalid HMAC signature for delivery status update: %s", deliveryID)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Invalid webhook signature",
			})
			return
		}

		logger.Debug("HMAC signature verified for delivery status update: %s", deliveryID)
	} else {
		logger.Warn("Webhook has no secret, skipping HMAC verification for delivery: %s", deliveryID)
	}

	// Validate status transition: can't update delivered/failed
	if record.Status == DeliveryStatusDelivered || record.Status == DeliveryStatusFailed {
		logger.Warn("Cannot update delivered/failed delivery: id=%s, current_status=%s",
			deliveryID, record.Status)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: "Cannot update delivery that is already delivered or failed",
		})
		return
	}

	// Map callback status to internal status
	newStatus := mapCallbackStatus(req.Status)

	// Update record fields
	now := time.Now().UTC()
	record.Status = newStatus
	record.LastActivityAt = now
	if req.StatusPercent != nil {
		record.StatusPercent = *req.StatusPercent
	}
	if req.StatusMessage != nil {
		record.StatusMessage = *req.StatusMessage
	}
	if newStatus == DeliveryStatusDelivered {
		record.DeliveredAt = &now
		record.StatusPercent = 100
	}

	if err := GlobalWebhookDeliveryRedisStore.Update(c.Request.Context(), record); err != nil {
		logger.Error("Failed to update delivery record: id=%s, error=%v", deliveryID, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to update delivery status",
		})
		return
	}

	// Reset webhook timeout count on successful delivery
	if newStatus == DeliveryStatusDelivered && GlobalWebhookSubscriptionStore != nil {
		if err := GlobalWebhookSubscriptionStore.ResetTimeouts(c.Request.Context(), webhook.Id.String()); err != nil {
			logger.Error("Failed to reset timeout count for webhook %s: %v", webhook.Id, err)
			// Don't fail the status update for this
		} else {
			logger.Debug("Reset timeout count for webhook %s after successful delivery", webhook.Id)
		}
	}

	// Return response
	response := UpdateWebhookDeliveryStatusResponse{
		Id:              record.ID,
		Status:          UpdateWebhookDeliveryStatusResponseStatus(newStatus),
		StatusPercent:   record.StatusPercent,
		StatusUpdatedAt: now,
	}

	logger.Info("Delivery status updated: id=%s, status=%s, percent=%d",
		deliveryID, newStatus, record.StatusPercent)

	c.JSON(http.StatusOK, response)
}

// mapCallbackStatus maps callback request status to internal delivery status.
// The callback uses "completed" but internally we track "delivered".
func mapCallbackStatus(s UpdateWebhookDeliveryStatusRequestStatus) string {
	switch s {
	case UpdateWebhookDeliveryStatusRequestStatusCompleted:
		return DeliveryStatusDelivered
	case UpdateWebhookDeliveryStatusRequestStatusFailed:
		return DeliveryStatusFailed
	case UpdateWebhookDeliveryStatusRequestStatusInProgress:
		return DeliveryStatusInProgress
	default:
		return string(s)
	}
}

// verifyDeliveryHMAC verifies HMAC signature for delivery access
func verifyDeliveryHMAC(c *gin.Context, record *WebhookDeliveryRecord, signature string, deliveryIDStr string) error {
	logger := slogging.Get().WithContext(c)

	if GlobalWebhookSubscriptionStore == nil {
		logger.Error("Webhook subscription store not initialized")
		return &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Webhook service not available",
		}
	}

	sub, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), record.SubscriptionID.String())
	if err != nil {
		logger.Error("Failed to get webhook for HMAC verification: %v", err)
		return &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to verify delivery",
		}
	}

	if sub.Secret == "" {
		logger.Warn("Webhook has no secret, cannot verify HMAC signature")
		return &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Webhook secret not configured",
		}
	}

	// For GET requests, verify HMAC over the delivery ID
	if !VerifySignature([]byte(deliveryIDStr), signature, sub.Secret) {
		logger.Warn("Invalid HMAC signature for delivery access: %s", record.ID)
		return &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Invalid webhook signature",
		}
	}

	logger.Debug("HMAC signature verified for delivery access: %s", record.ID)
	return nil
}

// verifyDeliveryJWTAccess verifies JWT-based access to a delivery record.
// Allows access for admins, subscription owners, or addon invokers.
func verifyDeliveryJWTAccess(c *gin.Context, record *WebhookDeliveryRecord) error {
	logger := slogging.Get().WithContext(c)

	// Validate JWT auth
	_, err := GetAuthenticatedUser(c)
	if err != nil {
		logger.Error("Authentication failed: %v", err)
		return err
	}

	// Check if user is admin
	isAdmin, _ := IsUserAdministrator(c)
	if isAdmin {
		return nil
	}

	// Get user's internal UUID
	var userUUID uuid.UUID
	if internalUUIDInterface, exists := c.Get("userInternalUUID"); exists {
		if uuidVal, ok := internalUUIDInterface.(uuid.UUID); ok {
			userUUID = uuidVal
		} else if uuidStr, ok := internalUUIDInterface.(string); ok {
			userUUID, _ = uuid.Parse(uuidStr)
		}
	}

	// Check if user is the addon invoker
	if record.InvokedByUUID != nil && *record.InvokedByUUID == userUUID {
		return nil
	}

	// Check if user owns the subscription
	if GlobalWebhookSubscriptionStore != nil {
		webhook, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), record.SubscriptionID.String())
		if err == nil && webhook.OwnerId == userUUID {
			return nil
		}
	}

	logger.Warn("User %s denied access to delivery %s", userUUID, record.ID)
	return &RequestError{
		Status:  http.StatusForbidden,
		Code:    "forbidden",
		Message: "Access denied",
	}
}

// deliveryRecordToWebhookDelivery converts a WebhookDeliveryRecord to the API response type.
func deliveryRecordToWebhookDelivery(r *WebhookDeliveryRecord) WebhookDelivery {
	delivery := WebhookDelivery{
		Id:             r.ID,
		SubscriptionId: r.SubscriptionID,
		EventType:      WebhookEventType(r.EventType),
		Status:         WebhookDeliveryStatus(r.Status),
		Attempts:       r.Attempts,
		CreatedAt:      r.CreatedAt,
		DeliveredAt:    r.DeliveredAt,
		LastActivityAt: &r.LastActivityAt,
		NextRetryAt:    r.NextRetryAt,
		AddonId:        r.AddonID,
		StatusPercent:  intPtr(r.StatusPercent),
	}

	if r.StatusMessage != "" {
		delivery.StatusMessage = strPtr(r.StatusMessage)
	}
	if r.LastError != "" {
		delivery.LastError = strPtr(r.LastError)
	}

	// Parse payload JSON back to map
	if r.Payload != "" {
		var payloadMap map[string]interface{}
		if err := json.Unmarshal([]byte(r.Payload), &payloadMap); err == nil {
			delivery.Payload = &payloadMap
		}
	}

	// Build InvokedBy user if addon-specific fields are populated
	if r.InvokedByEmail != "" {
		delivery.InvokedBy = &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "unknown",
			ProviderId:    "",
			DisplayName:   r.InvokedByName,
			Email:         openapi_types.Email(r.InvokedByEmail),
		}
	}

	return delivery
}

// intPtr converts an int to a pointer.
func intPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}
