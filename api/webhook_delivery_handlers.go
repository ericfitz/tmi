package api

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// WebhookDeliveryPayload represents the unified payload sent to webhook endpoints.
// Used for all webhook deliveries (resource-change events and addon invocations).
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: unified webhook event payload sent to subscriber endpoints for all delivery types
type WebhookDeliveryPayload struct {
	EventType     string          `json:"event_type"`
	ThreatModelID uuid.UUID       `json:"threat_model_id"`
	Timestamp     time.Time       `json:"timestamp"`
	ObjectType    string          `json:"object_type,omitempty"`
	ObjectID      *uuid.UUID      `json:"object_id,omitempty"`
	Data          json.RawMessage `json:"data"`
}

// WebhookDeliveryData contains addon-specific fields within the unified payload data.
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: addon-specific fields embedded within a webhook delivery payload
type WebhookDeliveryData struct {
	AddonID  *uuid.UUID       `json:"addon_id,omitempty"`
	UserData *json.RawMessage `json:"user_data,omitempty"`
}

// VerifySignature verifies the HMAC signature of a request.
// Delegates to the consolidated crypto package.
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: validate HMAC signature of a webhook payload against a shared secret (pure)
func VerifySignature(payload []byte, signature string, secret string) bool {
	return crypto.VerifyHMACSignature(payload, signature, secret)
}

// GetWebhookDeliveryStatus retrieves a webhook delivery record.
// Supports dual auth: JWT (admin, subscription owner, or addon invoker) or HMAC (webhook receiver).
// SEM@e64d904fcb8ba57e094190bac4395e83cec9abc1: fetch a webhook delivery record with dual HMAC or JWT authorization (reads DB)
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

	// Fetch the owning subscription so pinned LastError is sanitized on this path
	// too. This endpoint accepts JWT auth (admin, subscription owner, or addon
	// invoker) in addition to HMAC, so callers do NOT necessarily know the
	// subscription secret or its destination URL — an admin must not be able to
	// recover an operator-pinned sink URL from an unsanitized LastError. Pass nil
	// only when the subscription no longer exists (deleted after the delivery was
	// recorded); the record then carries no pinned URL context to redact against,
	// and the generic URL-pattern redaction is unavailable without the sub anyway.
	var sub *DBWebhookSubscription
	if GlobalWebhookSubscriptionStore != nil {
		if fetched, subErr := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), record.SubscriptionID.String()); subErr == nil {
			sub = &fetched
		} else {
			logger.Warn("Subscription %s not found for delivery %s; returning delivery without pinned-URL redaction context: %v",
				record.SubscriptionID, record.ID, subErr)
		}
	}

	response := deliveryRecordToWebhookDelivery(record, sub)
	c.JSON(http.StatusOK, response)
}

// UpdateWebhookDeliveryStatus updates the status of a webhook delivery (HMAC authenticated).
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: update a webhook delivery status via HMAC-authenticated callback, resetting timeouts on success (mutates shared state)
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

	// Validate status transition: terminal states (delivered/failed/cancelled) are final
	if isTerminalDeliveryStatus(record.Status) {
		logger.Warn("Cannot update terminal delivery: id=%s, current_status=%s",
			deliveryID, record.Status)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusConflict,
			Code:    "conflict",
			Message: "Cannot update delivery that is already delivered, failed, or cancelled",
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
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: convert callback request status enum to internal delivery status string (pure)
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
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: authorize delivery access by verifying HMAC signature against subscription secret (reads DB)
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
// Allows access for admins, subscription owners, addon invokers, or the
// client-credentials identity linked to the delivery's addon.
// SEM@411a53c663401d55a0f66913e00979599a208c93: authorize delivery access for admins, owners, invokers, or the linked addon identity via JWT (reads DB)
func verifyDeliveryJWTAccess(c *gin.Context, record *WebhookDeliveryRecord) error {
	logger := slogging.Get().WithContext(c)

	// Validate JWT auth
	if _, err := GetAuthenticatedUser(c); err != nil {
		logger.Error("Authentication failed: %v", err)
		return err
	}
	if deliveryVisibleToCaller(c, record) {
		return nil
	}
	logger.Warn("Caller denied access to delivery %s", record.ID)
	return &RequestError{
		Status:  http.StatusForbidden,
		Code:    "forbidden",
		Message: "Access denied",
	}
}

// deliveryVisibleToCaller applies the delivery access rule to an already
// authenticated caller: admin, addon invoker, linked addon identity, or
// subscription owner.
// SEM@411a53c663401d55a0f66913e00979599a208c93: report whether the authenticated caller may see a delivery record (reads DB)
func deliveryVisibleToCaller(c *gin.Context, record *WebhookDeliveryRecord) bool {
	if isAdmin, _ := IsUserAdministrator(c); isAdmin {
		return true
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

	// Addon invoker
	if record.InvokedByUUID != nil && *record.InvokedByUUID == userUUID {
		return true
	}

	// #913: a client-credentials identity linked to the delivery's addon
	// (tmi_addon_id claim, see cmd/server/jwt_auth.go) owns that addon's deliveries
	if record.AddonID != nil && SourceAddonIDFromContext(c.Request.Context()) == record.AddonID.String() {
		return true
	}

	// Subscription owner
	if GlobalWebhookSubscriptionStore != nil {
		webhook, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), record.SubscriptionID.String())
		if err == nil && webhook.OwnerId == userUUID {
			return true
		}
	}
	return false
}

// sanitizePinnedLastError removes URL substrings from LastError for operator-pinned subscriptions.
// The pinned URL (if known) is replaced first, then any remaining https?://\S+ patterns are
// replaced with "(operator-pinned)" so url.Error format strings cannot leak the address.
// SEM@a870b93778753735e380098f91f8c25076bbb50a: redact destination URLs from operator-pinned delivery error strings (pure)
func sanitizePinnedLastError(lastError, pinnedURL string) string {
	result := lastError
	if pinnedURL != "" {
		result = strings.ReplaceAll(result, pinnedURL, "(operator-pinned)")
	}
	// Also redact any remaining URL-shaped substrings
	urlPattern := regexp.MustCompile(`https?://\S+`)
	result = urlPattern.ReplaceAllString(result, "(operator-pinned)")
	return result
}

// deliveryRecordToWebhookDelivery converts a WebhookDeliveryRecord to the API response type.
// sub is the owning subscription, used to redact the LastError field for operator-pinned
// subscriptions. Pass nil to skip redaction (fail-open).
// SEM@411a53c663401d55a0f66913e00979599a208c93: convert a webhook delivery record to the API response DTO, sanitizing pinned URLs and filling invoker identity (pure)
func deliveryRecordToWebhookDelivery(r *WebhookDeliveryRecord, sub *DBWebhookSubscription) WebhookDelivery {
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
		lastErr := r.LastError
		if sub != nil && sub.OperatorPinned {
			lastErr = sanitizePinnedLastError(r.LastError, sub.Url)
		}
		delivery.LastError = strPtr(lastErr)
	}

	// Parse payload JSON back to map
	if r.Payload != "" {
		var payloadMap map[string]interface{}
		if err := json.Unmarshal([]byte(r.Payload), &payloadMap); err == nil {
			delivery.Payload = &payloadMap
		}
	}

	// Build InvokedBy user if addon-specific fields are populated. Records
	// written before #913 lack the provider identity; fall back to the
	// internal UUID so the User schema (provider_id minLength 1) still holds.
	if r.InvokedByEmail != "" {
		provider, providerID := r.InvokedByProvider, r.InvokedByProviderID
		if provider == "" {
			provider = "unknown"
		}
		if providerID == "" && r.InvokedByUUID != nil {
			providerID = r.InvokedByUUID.String()
		}
		delivery.InvokedBy = &User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      provider,
			ProviderId:    providerID,
			DisplayName:   r.InvokedByName,
			Email:         openapi_types.Email(r.InvokedByEmail),
		}
	}

	return delivery
}

// CancelWebhookDelivery marks a non-terminal delivery as cancelled (#913).
// JWT only: admin, subscription owner, addon invoker, or the addon-linked credential.
// SEM@411a53c663401d55a0f66913e00979599a208c93: cancel a webhook delivery for an authorized JWT caller, rejecting terminal states (mutates shared state)
func CancelWebhookDelivery(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	deliveryID, err := uuid.Parse(c.Param("delivery_id"))
	if err != nil {
		HandleRequestError(c, &RequestError{Status: http.StatusBadRequest, Code: "invalid_input", Message: "Invalid delivery ID format"})
		return
	}
	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery store not initialized")
		HandleRequestError(c, &RequestError{Status: http.StatusServiceUnavailable, Code: "service_unavailable", Message: "Delivery tracking not available"})
		return
	}
	record, err := GlobalWebhookDeliveryRedisStore.Get(c.Request.Context(), deliveryID)
	if err != nil {
		HandleRequestError(c, &RequestError{Status: http.StatusNotFound, Code: "not_found", Message: "Delivery record not found or expired"})
		return
	}
	if err := verifyDeliveryJWTAccess(c, record); err != nil {
		HandleRequestError(c, err)
		return
	}
	if isTerminalDeliveryStatus(record.Status) {
		HandleRequestError(c, &RequestError{Status: http.StatusConflict, Code: "conflict", Message: "Cannot cancel delivery that is already delivered, failed, or cancelled"})
		return
	}
	// ponytail: get-then-update race with the delivery worker; a delivery that is
	// mid-flight keeps running until the addon polls and sees cancelled, which is
	// the agreed contract. Add a Redis WATCH/MULTI if a lost cancel ever matters.
	record.Status = DeliveryStatusCancelled
	if err := GlobalWebhookDeliveryRedisStore.Update(c.Request.Context(), record); err != nil {
		logger.Error("Failed to cancel delivery record: id=%s, error=%v", deliveryID, err)
		HandleRequestError(c, &RequestError{Status: http.StatusInternalServerError, Code: "server_error", Message: "Failed to cancel delivery"})
		return
	}
	logger.Info("Delivery cancelled: id=%s", deliveryID)
	c.Status(http.StatusNoContent)
}

// ListMyWebhookDeliveries lists the deliveries the JWT caller may see (#913):
// everything for admins; otherwise the same rule as verifyDeliveryJWTAccess.
// SEM@411a53c663401d55a0f66913e00979599a208c93: list webhook deliveries visible to the caller with pagination (reads DB)
func ListMyWebhookDeliveries(c *gin.Context, params ListMyWebhookDeliveriesParams) {
	logger := slogging.Get().WithContext(c)

	if _, err := GetAuthenticatedUser(c); err != nil {
		HandleRequestError(c, err)
		return
	}
	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("Webhook delivery store not initialized")
		HandleRequestError(c, &RequestError{Status: http.StatusServiceUnavailable, Code: "service_unavailable", Message: "Delivery tracking not available"})
		return
	}
	offset, limit := 0, 20
	if params.Offset != nil {
		offset = max(*params.Offset, 0)
	}
	if params.Limit != nil {
		limit = min(max(*params.Limit, 1), 100)
	}

	ctx := c.Request.Context()
	// ponytail: the Redis store is a full SCAN either way (ListAll does the same);
	// fetch everything, filter by access, paginate in memory. Index by
	// addon/invoker if delivery volume ever makes this slow.
	all, _, err := GlobalWebhookDeliveryRedisStore.ListAll(ctx, 1<<30, 0)
	if err != nil {
		logger.Error("Failed to list delivery records: %v", err)
		HandleRequestError(c, &RequestError{Status: http.StatusInternalServerError, Code: "server_error", Message: "Failed to list deliveries"})
		return
	}
	visible := all[:0]
	for i := range all {
		if deliveryVisibleToCaller(c, &all[i]) {
			visible = append(visible, all[i])
		}
	}
	// Newest first: callers want their current jobs, not the oldest history
	sort.Slice(visible, func(i, j int) bool { return visible[i].CreatedAt.After(visible[j].CreatedAt) })
	total := len(visible)
	end := min(offset+limit, total)
	page := []WebhookDeliveryRecord{}
	if offset < total {
		page = visible[offset:end]
	}

	subCache := map[string]*DBWebhookSubscription{}
	items := make([]WebhookDelivery, 0, len(page))
	for i := range page {
		subID := page[i].SubscriptionID.String()
		sub, ok := subCache[subID]
		if !ok && GlobalWebhookSubscriptionStore != nil {
			if fetched, fetchErr := GlobalWebhookSubscriptionStore.Get(ctx, subID); fetchErr == nil {
				sub = &fetched
			}
			subCache[subID] = sub
		}
		items = append(items, deliveryRecordToWebhookDelivery(&page[i], sub))
	}
	c.JSON(http.StatusOK, ListWebhookDeliveriesResponse{Deliveries: items, Total: total, Limit: limit, Offset: offset})
}

// intPtr converts an int to a pointer.
// SEM@ca61a567c4babc9270ee913396aaa4fb530505a3: convert an int to a pointer, returning nil for zero (pure)
func intPtr(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}
