package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// addWebhookRateLimitHeaders adds rate limit headers to webhook responses
// SEM@c9e35649a1646f1c8eef600df7a69b674a98b675: attach rate-limit headers to a webhook API response for the current user
func (s *Server) addWebhookRateLimitHeaders(c *gin.Context, userID string) {
	if s.webhookRateLimiter != nil {
		limit, remaining, resetAt, err := s.webhookRateLimiter.GetSubscriptionRateLimitInfo(c.Request.Context(), userID)
		if err == nil {
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))
		}
	}
}

// ListWebhookSubscriptions lists webhook subscriptions (admin only)
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: list all webhook subscriptions with pagination, optionally filtered by threat model (reads DB)
func (s *Server) ListWebhookSubscriptions(c *gin.Context, params ListWebhookSubscriptionsParams) {
	logger := slogging.Get().WithContext(c)

	// Parse pagination parameters
	offset := 0
	limit := 20 // Default limit per OpenAPI spec
	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = min(*params.Limit,
			// Cap at maximum per OpenAPI spec
			100)
	}

	// Get subscriptions (admins see all)
	var subscriptions []DBWebhookSubscription

	if params.ThreatModelId != nil {
		// Filter by threat model
		allSubs, tmErr := GlobalWebhookSubscriptionStore.ListByThreatModel(c.Request.Context(), params.ThreatModelId.String(), offset, limit)
		if tmErr != nil {
			logger.Error("failed to list subscriptions by threat model: %v", tmErr)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to list subscriptions"})
			return
		}
		subscriptions = allSubs
	} else {
		// Get all subscriptions with pagination (nil filter = no filtering)
		subscriptions = GlobalWebhookSubscriptionStore.List(c.Request.Context(), offset, limit, nil)
	}

	// Convert to API response types (don't include secrets in list)
	items := make([]WebhookSubscription, 0, len(subscriptions))
	for _, sub := range subscriptions {
		items = append(items, dbWebhookSubscriptionToAPI(sub, false))
	}

	// Get total count for pagination
	total := GlobalWebhookSubscriptionStore.Count(c.Request.Context())

	c.JSON(http.StatusOK, ListWebhookSubscriptionsResponse{
		Subscriptions: items,
		Total:         total,
		Limit:         limit,
		Offset:        offset,
	})
}

// CreateWebhookSubscription creates a new webhook subscription (admin only)
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: validate and store a new webhook subscription, enforcing rate limits and URL safety (reads DB)
func (s *Server) CreateWebhookSubscription(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Get user's internal UUID from context (set by JWT middleware)
	userID, err := GetUserInternalUUID(c)
	if err != nil {
		logger.Error("failed to get user internal UUID: %v", err)
		HandleRequestError(c, err)
		return
	}

	// Check rate limits if webhook rate limiter is available
	if s.webhookRateLimiter != nil {
		// Add rate limit headers
		s.addWebhookRateLimitHeaders(c, userID)

		// Check subscription count limit
		if err := s.webhookRateLimiter.CheckSubscriptionLimit(c.Request.Context(), userID); err != nil {
			logger.Warn("subscription limit check failed for user %s: %v", userID, err)
			// Get quota for retry-after calculation
			quota := GlobalWebhookQuotaStore.GetOrDefault(c.Request.Context(), userID)
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, Error{
				Error:            "rate_limit_exceeded",
				ErrorDescription: fmt.Sprintf("%v (limit: %d)", err, quota.MaxSubscriptions),
			})
			return
		}

		// Check subscription request rate limit
		if err := s.webhookRateLimiter.CheckSubscriptionRequestLimit(c.Request.Context(), userID); err != nil {
			logger.Warn("subscription request rate limit exceeded for user %s: %v", userID, err)
			// Get quota for retry-after calculation
			quota := GlobalWebhookQuotaStore.GetOrDefault(c.Request.Context(), userID)
			c.Header("Retry-After", "60")
			c.JSON(http.StatusTooManyRequests, Error{
				Error:            "rate_limit_exceeded",
				ErrorDescription: fmt.Sprintf("%v (limit: %d/minute)", err, quota.MaxSubscriptionRequestsPerMinute),
			})
			return
		}
	}

	// Parse request body
	var input WebhookSubscriptionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		logger.Error("failed to parse request body: %v", err)
		c.JSON(http.StatusBadRequest, Error{Error: "invalid request body"})
		return
	}

	// Validate required fields
	if input.Name == "" {
		c.JSON(http.StatusBadRequest, Error{Error: "name is required"})
		return
	}
	if input.Url == "" {
		c.JSON(http.StatusBadRequest, Error{Error: "url is required"})
		return
	}
	if len(input.Events) == 0 {
		c.JSON(http.StatusBadRequest, Error{Error: "at least one event type is required"})
		return
	}

	// Validate webhook URL (scheme, hostname, deny list)
	urlValidator := NewWebhookUrlValidatorWithHTTP(GlobalWebhookUrlDenyListStore, s.allowHTTPWebhooks)
	if err := urlValidator.ValidateWebhookURL(c.Request.Context(), input.Url); err != nil {
		c.JSON(http.StatusBadRequest, Error{Error: fmt.Sprintf("invalid webhook URL: %s", err.Error())})
		return
	}

	// Generate secret if not provided
	var secret string
	if input.Secret != nil {
		secret = *input.Secret
	} else {
		secret = generateRandomHex(32)
	}

	// Generate challenge token for verification
	challenge := generateRandomHex(32)

	// Convert threat model ID if provided
	var threatModelID *uuid.UUID
	if input.ThreatModelId != nil {
		threatModelID = input.ThreatModelId
	}

	// Create subscription in database
	ownerUUID, err := uuid.Parse(userID)
	if err != nil {
		logger.Error("invalid user ID format in authentication context: %v", err)
		// Invalid UUID in auth context indicates corrupted authentication state
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication state - please re-authenticate")
		c.JSON(http.StatusUnauthorized, Error{Error: "invalid authentication state", ErrorDescription: "Please re-authenticate"})
		return
	}

	subscription := DBWebhookSubscription{
		OwnerId:        ownerUUID,
		ThreatModelId:  threatModelID,
		Name:           input.Name,
		Url:            input.Url,
		Events:         input.Events,
		Secret:         secret,
		Status:         "pending_verification",
		Challenge:      challenge,
		ChallengesSent: 0,
	}

	created, err := GlobalWebhookSubscriptionStore.Create(c.Request.Context(), subscription, func(sub DBWebhookSubscription, id string) DBWebhookSubscription {
		parsedID, _ := uuid.Parse(id)
		sub.Id = parsedID
		return sub
	})
	if err != nil {
		logger.Error("failed to create subscription: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to create subscription"})
		return
	}

	userIdentity := GetUserIdentityForLogging(c)
	logger.Info("created webhook subscription %s for %s", created.Id, userIdentity)

	// Convert to API response type
	response := dbWebhookSubscriptionToAPI(created, true) // Include secret in creation response

	c.JSON(http.StatusCreated, response)
}

// GetWebhookSubscription gets a specific webhook subscription (admin only)
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: fetch a single webhook subscription by ID, redacting the URL if operator-pinned (reads DB)
func (s *Server) GetWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get subscription from database
	subscription, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), webhookId.String())
	if err != nil {
		logger.Error("failed to get subscription %s: %v", webhookId, err)
		c.JSON(http.StatusNotFound, Error{Error: "subscription not found"})
		return
	}

	// Convert to API response type (no secret in GET response)
	response := dbWebhookSubscriptionToAPI(subscription, false)

	c.JSON(http.StatusOK, response)
}

// DeleteWebhookSubscription deletes a webhook subscription (admin only)
// SEM@4c446bfacf6eb966a03bc2a174728d73b8a6d41e: delete a webhook subscription and cascade-delete its addons, blocking pinned rows (reads DB)
func (s *Server) DeleteWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Get subscription from database
	subscription, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), webhookId.String())
	if err != nil {
		logger.Error("failed to get subscription %s: %v", webhookId, err)
		c.JSON(http.StatusNotFound, Error{Error: "subscription not found"})
		return
	}

	// Operator-pinned subscriptions are managed by server configuration and may
	// not be mutated through the API.
	if subscription.OperatorPinned {
		c.JSON(http.StatusForbidden, Error{Error: "operator-pinned subscription is managed by server configuration and cannot be modified through the API"})
		return
	}

	// First, delete any addons associated with this webhook subscription
	// This is required because addons have a foreign key constraint to webhook_subscriptions
	if GlobalAddonStore != nil {
		deletedCount, delErr := GlobalAddonStore.DeleteByWebhookID(c.Request.Context(), webhookId)
		if delErr != nil {
			logger.Error("failed to delete addons for subscription %s: %v", webhookId, delErr)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to delete associated addons"})
			return
		}
		if deletedCount > 0 {
			logger.Info("cascade deleted %d addons for subscription %s", deletedCount, webhookId)
		}
	}

	// Delete the subscription
	if err := GlobalWebhookSubscriptionStore.Delete(c.Request.Context(), webhookId.String()); err != nil {
		logger.Error("failed to delete subscription %s: %v", webhookId, err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to delete subscription"})
		return
	}

	userIdentity := GetUserIdentityForLogging(c)
	logger.Info("deleted webhook subscription %s for %s", webhookId, userIdentity)

	c.Status(http.StatusNoContent)
}

// webhookSubscriptionPatchPaths is the set of JSON Pointer paths a webhook
// subscription PATCH may target. Every other field (id, owner, status, secret,
// challenge state, counters, timestamps, operator_pinned) is server-managed.
var webhookSubscriptionPatchPaths = []string{"/name", "/events", "/threat_model_id", "/url"}

// PatchAdminWebhookSubscription partially updates a webhook subscription (admin only).
// PATCH /admin/webhooks/subscriptions/{webhook_id}
// SEM@6e6f341493ef17352815b59696dcdead01383e70: handle JSON Patch update of a webhook subscription, re-verifying on URL change (mutates DB)
func (s *Server) PatchAdminWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	if GlobalWebhookSubscriptionStore == nil {
		logger.Error("webhook subscription store not initialized")
		HandleRequestError(c, &RequestError{
			Status:  http.StatusServiceUnavailable,
			Code:    "service_unavailable",
			Message: "Webhook subscriptions are not available",
		})
		return
	}

	existing, err := GlobalWebhookSubscriptionStore.Get(ctx, webhookId.String())
	if err != nil {
		logger.Error("failed to get subscription %s: %v", webhookId, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Subscription not found",
		})
		return
	}

	// Operator-pinned subscriptions are managed by server configuration and may
	// not be mutated through the API.
	if existing.OperatorPinned {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Operator-pinned subscription is managed by server configuration and cannot be modified through the API",
		})
		return
	}

	operations, parseErr := ParsePatchRequest(c)
	if parseErr != nil {
		HandleRequestError(c, parseErr)
		return
	}

	if reqErr := ValidatePatchPathsAllowed(webhookSubscriptionPatchPaths, operations); reqErr != nil {
		HandleRequestError(c, reqErr)
		return
	}

	patched, err := ApplyPatchOperations(existing, operations)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Server-managed fields are not reachable through the allowlist; restore
	// them anyway so a future allowlist change cannot leak identity drift.
	patched.Id = existing.Id
	patched.OwnerId = existing.OwnerId
	patched.Secret = existing.Secret
	patched.OperatorPinned = existing.OperatorPinned

	patched.Name = SanitizePlainText(patched.Name)

	if reqErr := s.validatePatchedWebhookSubscription(ctx, patched, existing, patchedPaths(operations)); reqErr != nil {
		HandleRequestError(c, reqErr)
		return
	}

	// Only the fields this patch actually rewrites are written back, so a
	// concurrent challenge-worker transition is not reverted.
	written := []string{"Name", "URL", "Events", "ThreatModelID"}

	// The destination URL is the trust anchor: a new endpoint must prove
	// ownership again before it receives events. The challenge worker picks the
	// subscription back up from its pending_verification status.
	urlChanged := patched.Url != existing.Url
	if urlChanged {
		patched.Status = "pending_verification"
		patched.Challenge = generateRandomHex(32)
		patched.ChallengesSent = 0
		written = append(written, "Status", "Challenge", "ChallengesSent")
	}

	if err := GlobalWebhookSubscriptionStore.Update(ctx, webhookId.String(), patched, written...); err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "Subscription not found",
			})
			return
		}
		// The referenced threat model can be deleted between validation and
		// write; the resulting FK violation is the caller's 400, not a 500.
		if errors.Is(err, dberrors.ErrConstraint) {
			HandleRequestError(c, InvalidInputError("threat_model_id does not refer to an existing threat model"))
			return
		}
		logger.Error("failed to update subscription %s: %v", webhookId, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to update subscription",
		})
		return
	}

	updated, err := GlobalWebhookSubscriptionStore.Get(ctx, webhookId.String())
	if err != nil {
		logger.Error("failed to re-read subscription %s after update: %v", webhookId, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to update subscription",
		})
		return
	}

	userIdentity := GetUserIdentityForLogging(c)
	logger.Info("patched webhook subscription %s for %s (url_changed=%t)", webhookId, userIdentity, urlChanged)

	c.JSON(http.StatusOK, dbWebhookSubscriptionToAPI(updated, false))
}

// patchedPaths returns the set of top-level fields a patch touches, as JSON
// Pointer prefixes ("/name"), so validation can skip fields the caller left
// alone. A "move"/"copy" source counts as touched too, since it is rewritten.
// SEM@6e6f341493ef17352815b59696dcdead01383e70: compute the set of top-level fields a JSON Patch touches (pure)
func patchedPaths(operations []PatchOperation) map[string]bool {
	touched := make(map[string]bool, len(operations))
	for _, op := range operations {
		for _, path := range patchOpPaths(op) {
			if path == "" {
				continue
			}
			if idx := strings.Index(path[1:], "/"); idx >= 0 {
				path = path[:idx+1]
			}
			touched[path] = true
		}
	}
	return touched
}

// validatePatchedWebhookSubscription rejects a patched subscription that would
// not have been accepted by CreateWebhookSubscription: empty or over-long name,
// empty or unknown event list, unsafe URL, or a threat model that does not
// exist. Only fields the patch actually touched are re-validated, so a legacy
// row carrying a non-enum event stays patchable on its other fields.
// SEM@6e6f341493ef17352815b59696dcdead01383e70: validate the touched fields of a patched webhook subscription (reads DB)
func (s *Server) validatePatchedWebhookSubscription(ctx context.Context, patched, existing DBWebhookSubscription, touched map[string]bool) *RequestError {
	if touched["/name"] {
		if strings.TrimSpace(patched.Name) == "" {
			return InvalidInputError("name is required")
		}
		if len(patched.Name) > 255 {
			return InvalidInputError("name must be 255 characters or fewer")
		}
	}

	if touched["/events"] {
		if len(patched.Events) == 0 {
			return InvalidInputError("at least one event type is required")
		}
		if len(patched.Events) > 50 {
			return InvalidInputError("at most 50 event types are allowed")
		}
		for _, event := range patched.Events {
			if !WebhookEventType(event).Valid() {
				return InvalidInputError(fmt.Sprintf("unknown event type: %s", event))
			}
		}
	}

	if patched.Url != existing.Url {
		if patched.Url == "" {
			return InvalidInputError("url is required")
		}
		urlValidator := NewWebhookUrlValidatorWithHTTP(GlobalWebhookUrlDenyListStore, s.allowHTTPWebhooks)
		if err := urlValidator.ValidateWebhookURL(ctx, patched.Url); err != nil {
			return InvalidInputError(fmt.Sprintf("invalid webhook URL: %s", err.Error()))
		}
	}

	// A nil threat model ID means "all threat models" and is always allowed.
	if patched.ThreatModelId != nil && ThreatModelStore != nil {
		changed := existing.ThreatModelId == nil || *existing.ThreatModelId != *patched.ThreatModelId
		if changed {
			if _, err := ThreatModelStore.Get(patched.ThreatModelId.String()); err != nil {
				return InvalidInputError("threat_model_id does not refer to an existing threat model")
			}
		}
	}

	return nil
}

// TestWebhookSubscription sends a test event to the webhook (admin only)
// SEM@a870b93778753735e380098f91f8c25076bbb50a: enqueue a test delivery for a webhook subscription and return its delivery ID (reads DB)
func (s *Server) TestWebhookSubscription(c *gin.Context, webhookId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	// Parse optional request body
	var input WebhookTestRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		// Body is optional, so ignore parse errors
		input = WebhookTestRequest{}
	}

	// Get subscription from database
	subscription, err := GlobalWebhookSubscriptionStore.Get(c.Request.Context(), webhookId.String())
	if err != nil {
		logger.Error("failed to get subscription %s: %v", webhookId, err)
		c.JSON(http.StatusNotFound, Error{Error: "subscription not found"})
		return
	}

	// Operator-pinned subscriptions cannot be test-triggered through the API.
	if subscription.OperatorPinned {
		c.JSON(http.StatusForbidden, Error{Error: "operator-pinned subscription is managed by server configuration and cannot be modified through the API"})
		return
	}

	// Determine event type - use provided or first from subscription
	eventType := "webhook.test"
	if input.EventType != nil {
		eventType = string(*input.EventType)
	} else if len(subscription.Events) > 0 {
		eventType = subscription.Events[0]
	}

	// Create test delivery
	testPayload := map[string]any{
		"type":            "test",
		"subscription_id": webhookId.String(),
		"timestamp":       time.Now().UTC().Format(time.RFC3339),
		"message":         "This is a test webhook delivery",
	}

	payloadJSON, err := json.Marshal(testPayload)
	if err != nil {
		logger.Error("failed to marshal test payload: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to create test delivery"})
		return
	}

	// Create delivery record in Redis
	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("webhook delivery Redis store not available")
		c.JSON(http.StatusInternalServerError, Error{Error: "delivery store not available"})
		return
	}

	record := &WebhookDeliveryRecord{
		SubscriptionID: subscription.Id,
		EventType:      eventType,
		Payload:        string(payloadJSON),
		Status:         DeliveryStatusPending,
		Attempts:       0,
	}

	if err := GlobalWebhookDeliveryRedisStore.Create(c.Request.Context(), record); err != nil {
		logger.Error("failed to create test delivery: %v", err)
		c.JSON(http.StatusInternalServerError, Error{Error: "failed to create test delivery"})
		return
	}

	userIdentity := GetUserIdentityForLogging(c)
	logger.Info("created test delivery %s for subscription %s by %s", record.ID, webhookId, userIdentity)

	// Return response with delivery ID
	message := "Test delivery created and queued for sending"
	response := WebhookTestResponse{
		DeliveryId: record.ID,
		Message:    &message,
	}

	c.JSON(http.StatusAccepted, response)
}

// ListWebhookDeliveries lists webhook deliveries (admin only)
// SEM@a870b93778753735e380098f91f8c25076bbb50a: list delivery records with pagination, optionally filtered by subscription (reads DB)
func (s *Server) ListWebhookDeliveries(c *gin.Context, params ListWebhookDeliveriesParams) {
	logger := slogging.Get().WithContext(c)

	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("webhook delivery Redis store not initialized")
		c.JSON(http.StatusServiceUnavailable, Error{Error: "delivery tracking not available"})
		return
	}

	// Parse pagination parameters
	offset := 0
	limit := 20 // Default limit per OpenAPI spec
	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = min(*params.Limit,
			// Cap at maximum per OpenAPI spec
			100)
	}

	ctx := c.Request.Context()
	var records []WebhookDeliveryRecord
	var total int

	// If subscription ID is provided, get deliveries for that subscription
	if params.SubscriptionId != nil {
		// Verify the subscription exists
		_, subErr := GlobalWebhookSubscriptionStore.Get(ctx, params.SubscriptionId.String())
		if subErr != nil {
			logger.Error("failed to get subscription %s: %v", params.SubscriptionId, subErr)
			c.JSON(http.StatusNotFound, Error{Error: "subscription not found"})
			return
		}

		var err error
		records, total, err = GlobalWebhookDeliveryRedisStore.ListBySubscription(ctx, *params.SubscriptionId, limit, offset)
		if err != nil {
			logger.Error("failed to list deliveries for subscription %s: %v", params.SubscriptionId, err)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to list deliveries"})
			return
		}
	} else {
		var err error
		records, total, err = GlobalWebhookDeliveryRedisStore.ListAll(ctx, limit, offset)
		if err != nil {
			logger.Error("failed to list all deliveries: %v", err)
			c.JSON(http.StatusInternalServerError, Error{Error: "failed to list deliveries"})
			return
		}
	}

	// Convert to API response types.
	// Use a lazy cache map to fetch subscriptions for pinned-URL redaction without
	// issuing a query per delivery record.
	subCache := make(map[string]*DBWebhookSubscription)
	items := make([]WebhookDelivery, 0, len(records))
	for i := range records {
		subIDStr := records[i].SubscriptionID.String()
		var sub *DBWebhookSubscription
		if cached, ok := subCache[subIDStr]; ok {
			sub = cached
		} else {
			fetched, fetchErr := GlobalWebhookSubscriptionStore.Get(ctx, subIDStr)
			if fetchErr == nil {
				subCache[subIDStr] = &fetched
				sub = &fetched
			}
		}
		items = append(items, deliveryRecordToWebhookDelivery(&records[i], sub))
	}

	c.JSON(http.StatusOK, ListWebhookDeliveriesResponse{
		Deliveries: items,
		Total:      total,
		Limit:      limit,
		Offset:     offset,
	})
}

// GetWebhookDelivery gets a specific webhook delivery (admin only)
// SEM@5ab9bc35c2a66a8d16c0ac1b58f3d345958c9c34: fetch a single delivery record by ID with pinned-URL redaction applied (reads DB)
func (s *Server) GetWebhookDelivery(c *gin.Context, deliveryId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)

	if GlobalWebhookDeliveryRedisStore == nil {
		logger.Error("webhook delivery Redis store not initialized")
		c.JSON(http.StatusServiceUnavailable, Error{Error: "delivery tracking not available"})
		return
	}

	// Get delivery from Redis store
	record, err := GlobalWebhookDeliveryRedisStore.Get(c.Request.Context(), deliveryId)
	if err != nil {
		logger.Error("failed to get delivery %s: %v", deliveryId, err)
		c.JSON(http.StatusNotFound, Error{Error: "delivery not found"})
		return
	}

	// Fetch the owning subscription so pinned LastError is sanitized on this
	// admin endpoint too — an admin must not be able to recover an
	// operator-pinned sink URL from an unsanitized LastError. Pass nil only
	// when the subscription no longer exists (deleted after the delivery was
	// recorded); the record then carries no pinned URL context to redact
	// against, and the generic URL-pattern redaction is unavailable without
	// the sub anyway.
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

// Helper functions for type conversion

// dbWebhookSubscriptionToAPI converts a database webhook subscription to API response type.
// For operator-pinned subscriptions the destination URL is redacted so that admins can
// see the control exists without learning or targeting the internal alert-sink address.
// SEM@4c446bfacf6eb966a03bc2a174728d73b8a6d41e: convert a DB webhook subscription to its API DTO, redacting URL and secret for pinned rows (pure)
func dbWebhookSubscriptionToAPI(db DBWebhookSubscription, includeSecret bool) WebhookSubscription {
	// Convert []string to []WebhookEventType
	events := make([]WebhookEventType, len(db.Events))
	for i, event := range db.Events {
		events[i] = WebhookEventType(event)
	}

	// Redact the destination URL for operator-pinned subscriptions.
	url := db.Url
	if db.OperatorPinned {
		url = "(operator-pinned)"
	}

	response := WebhookSubscription{
		Id:                  db.Id,
		OwnerId:             db.OwnerId,
		Name:                db.Name,
		Url:                 url,
		Events:              events,
		Status:              WebhookSubscriptionStatus(db.Status),
		CreatedAt:           db.CreatedAt,
		ModifiedAt:          db.ModifiedAt,
		ChallengesSent:      &db.ChallengesSent,
		PublicationFailures: &db.PublicationFailures,
	}

	// Include secret only for creation response, never for pinned rows.
	if includeSecret && !db.OperatorPinned && db.Secret != "" {
		response.Secret = &db.Secret
	}

	// Include threat model ID if present
	if db.ThreatModelId != nil {
		response.ThreatModelId = db.ThreatModelId
	}

	// Include last successful use if present
	if db.LastSuccessfulUse != nil {
		response.LastSuccessfulUse = db.LastSuccessfulUse
	}

	return response
}
