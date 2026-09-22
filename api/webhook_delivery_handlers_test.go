package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Delivery Redis Store for Webhook Delivery Handler Tests
// =============================================================================

// mockDeliveryRedisStore implements WebhookDeliveryRedisStoreInterface for testing
type mockDeliveryRedisStore struct {
	records map[uuid.UUID]*WebhookDeliveryRecord
	err     error
}

func newMockDeliveryRedisStore() *mockDeliveryRedisStore {
	return &mockDeliveryRedisStore{
		records: make(map[uuid.UUID]*WebhookDeliveryRecord),
	}
}

func (m *mockDeliveryRedisStore) Create(_ context.Context, record *WebhookDeliveryRecord) error {
	if m.err != nil {
		return m.err
	}
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	m.records[record.ID] = record
	return nil
}

func (m *mockDeliveryRedisStore) Get(_ context.Context, id uuid.UUID) (*WebhookDeliveryRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	if r, ok := m.records[id]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("delivery record not found or expired: %s", id)
}

func (m *mockDeliveryRedisStore) Update(_ context.Context, record *WebhookDeliveryRecord) error {
	if m.err != nil {
		return m.err
	}
	m.records[record.ID] = record
	return nil
}

func (m *mockDeliveryRedisStore) UpdateStatus(_ context.Context, id uuid.UUID, status string, deliveredAt *time.Time) error {
	if m.err != nil {
		return m.err
	}
	if r, ok := m.records[id]; ok {
		r.Status = status
		if deliveredAt != nil {
			r.DeliveredAt = deliveredAt
		}
		return nil
	}
	return fmt.Errorf("not found")
}

func (m *mockDeliveryRedisStore) UpdateRetry(_ context.Context, id uuid.UUID, attempts int, nextRetryAt *time.Time, lastError string) error {
	if m.err != nil {
		return m.err
	}
	if r, ok := m.records[id]; ok {
		r.Attempts = attempts
		r.NextRetryAt = nextRetryAt
		r.LastError = lastError
		return nil
	}
	return fmt.Errorf("not found")
}

func (m *mockDeliveryRedisStore) Delete(_ context.Context, id uuid.UUID) error {
	if m.err != nil {
		return m.err
	}
	delete(m.records, id)
	return nil
}

func (m *mockDeliveryRedisStore) ListPending(_ context.Context, _ int) ([]WebhookDeliveryRecord, error) {
	return nil, m.err
}

func (m *mockDeliveryRedisStore) ListReadyForRetry(_ context.Context) ([]WebhookDeliveryRecord, error) {
	return nil, m.err
}

func (m *mockDeliveryRedisStore) ListStale(_ context.Context, _ time.Duration) ([]WebhookDeliveryRecord, error) {
	return nil, m.err
}

func (m *mockDeliveryRedisStore) ListBySubscription(_ context.Context, _ uuid.UUID, _, _ int) ([]WebhookDeliveryRecord, int, error) {
	return nil, 0, m.err
}

func (m *mockDeliveryRedisStore) ListAll(_ context.Context, _, _ int) ([]WebhookDeliveryRecord, int, error) {
	if m.err != nil {
		return nil, 0, m.err
	}
	out := make([]WebhookDeliveryRecord, 0, len(m.records))
	for _, r := range m.records {
		out = append(out, *r)
	}
	return out, len(out), nil
}

func (m *mockDeliveryRedisStore) CountActiveByAddon(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, m.err
}

// =============================================================================
// Test Helpers
// =============================================================================

// setupDeliveryHandlerTest creates a gin router with auth context for delivery handler tests.
// Returns the router and the user's internal UUID.
func setupDeliveryHandlerTest(isAdmin bool, userUUID uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: isAdmin}

	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userID", "test-provider-id")
		c.Set("userInternalUUID", userUUID.String())
		c.Set("userProvider", "test")
		c.Set("userRole", RoleOwner)
		c.Next()
	})

	r.GET("/webhook_deliveries/:delivery_id", GetWebhookDeliveryStatus)
	r.PUT("/webhook_deliveries/:delivery_id/status", UpdateWebhookDeliveryStatus)
	r.DELETE("/webhook_deliveries/:delivery_id", CancelWebhookDelivery)
	r.GET("/webhook_deliveries", func(c *gin.Context) { ListMyWebhookDeliveries(c, ListMyWebhookDeliveriesParams{}) })

	return r
}

// createTestDeliveryRecord creates a WebhookDeliveryRecord with sensible defaults for testing.
func createTestDeliveryRecord(subscriptionID uuid.UUID, status string) *WebhookDeliveryRecord {
	now := time.Now().UTC()
	id := uuid.New()
	return &WebhookDeliveryRecord{
		ID:             id,
		SubscriptionID: subscriptionID,
		EventType:      "addon.invoked",
		Payload:        `{"event_type":"addon.invoked"}`,
		Status:         status,
		CreatedAt:      now,
		LastActivityAt: now,
	}
}

// =============================================================================
// GetWebhookDeliveryStatus Tests
// =============================================================================

func TestGetWebhookDeliveryStatus(t *testing.T) {
	// Save originals
	origDeliveryStore := GlobalWebhookDeliveryRedisStore
	origSubStore := GlobalWebhookSubscriptionStore
	origGroupStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookDeliveryRedisStore = origDeliveryStore
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalGroupMemberRepository = origGroupStore
	}()

	t.Run("Success_AdminCanGet", func(t *testing.T) {
		adminUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(true, adminUUID)

		req := httptest.NewRequest("GET", "/webhook_deliveries/"+record.ID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp WebhookDelivery
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, record.ID, resp.Id)
		assert.Equal(t, WebhookDeliveryStatus(DeliveryStatusPending), resp.Status)
	})

	t.Run("Success_InvokerCanGet", func(t *testing.T) {
		invokerUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusInProgress)
		record.InvokedByUUID = &invokerUUID
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = subStore

		// Non-admin user whose UUID matches InvokedByUUID
		r := setupDeliveryHandlerTest(false, invokerUUID)

		req := httptest.NewRequest("GET", "/webhook_deliveries/"+record.ID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp WebhookDelivery
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, record.ID, resp.Id)
	})

	t.Run("NotFound", func(t *testing.T) {
		userUUID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		GlobalWebhookDeliveryRedisStore = deliveryStore

		r := setupDeliveryHandlerTest(true, userUUID)

		nonexistentID := uuid.New()
		req := httptest.NewRequest("GET", "/webhook_deliveries/"+nonexistentID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Forbidden_NonOwner", func(t *testing.T) {
		otherUserUUID := uuid.New()
		invokerUUID := uuid.New()
		ownerUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		record.InvokedByUUID = &invokerUUID
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		// Subscription owned by a different user
		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: ownerUUID,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		// Non-admin, non-invoker, non-owner user
		r := setupDeliveryHandlerTest(false, otherUserUUID)

		req := httptest.NewRequest("GET", "/webhook_deliveries/"+record.ID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("BadRequest_InvalidDeliveryID", func(t *testing.T) {
		userUUID := uuid.New()
		deliveryStore := newMockDeliveryRedisStore()
		GlobalWebhookDeliveryRedisStore = deliveryStore

		r := setupDeliveryHandlerTest(true, userUUID)

		req := httptest.NewRequest("GET", "/webhook_deliveries/not-a-uuid", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Success_SubscriptionOwnerCanGet", func(t *testing.T) {
		ownerUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusDelivered)
		// InvokedByUUID is someone else (or nil)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: ownerUUID,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		// Non-admin user who owns the subscription
		r := setupDeliveryHandlerTest(false, ownerUUID)

		req := httptest.NewRequest("GET", "/webhook_deliveries/"+record.ID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Pinned_LastErrorSanitized_AdminJWT", func(t *testing.T) {
		// Regression test: this endpoint accepts JWT auth in addition to HMAC.
		// A JWT-authenticated admin must not be able to recover the
		// operator-pinned sink URL from an unsanitized LastError.
		adminUUID := uuid.New()
		subID := uuid.New()
		pinnedURL := "https://real-sink.internal/alert"

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusFailed)
		record.LastError = fmt.Sprintf(`Post "%s": dial tcp: no such host`, pinnedURL)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:             subID,
			OwnerId:        uuid.New(),
			Url:            pinnedURL,
			Status:         "active",
			OperatorPinned: true,
		}
		GlobalWebhookSubscriptionStore = subStore

		// Admin via JWT, no X-Webhook-Signature header
		r := setupDeliveryHandlerTest(true, adminUUID)

		req := httptest.NewRequest("GET", "/webhook_deliveries/"+record.ID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp WebhookDelivery
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.NotNil(t, resp.LastError)
		assert.NotContains(t, *resp.LastError, pinnedURL, "pinned URL must not leak via the JWT-authenticated status endpoint")
		assert.NotContains(t, *resp.LastError, "https://", "no URL should remain in LastError for a pinned subscription")
		assert.Contains(t, *resp.LastError, "(operator-pinned)")
	})
}

// =============================================================================
// UpdateWebhookDeliveryStatus Tests
// =============================================================================

func TestUpdateWebhookDeliveryStatus(t *testing.T) {
	// Save originals
	origDeliveryStore := GlobalWebhookDeliveryRedisStore
	origSubStore := GlobalWebhookSubscriptionStore
	origGroupStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookDeliveryRedisStore = origDeliveryStore
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalGroupMemberRepository = origGroupStore
	}()

	webhookSecret := "test-webhook-secret-12345"

	t.Run("Success_Completed", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  webhookSecret,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusCompleted,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		signature := crypto.GenerateHMACSignature(bodyBytes, webhookSecret)

		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signature)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp UpdateWebhookDeliveryStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, record.ID, resp.Id)
		// "completed" maps to internal "delivered" status
		assert.Equal(t, UpdateWebhookDeliveryStatusResponseStatus(DeliveryStatusDelivered), resp.Status)
		assert.Equal(t, 100, resp.StatusPercent)
	})

	t.Run("Success_InProgress", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  webhookSecret,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		statusPercent := 50
		statusMsg := "Processing threat model"
		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status:        UpdateWebhookDeliveryStatusRequestStatusInProgress,
			StatusPercent: &statusPercent,
			StatusMessage: &statusMsg,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		signature := crypto.GenerateHMACSignature(bodyBytes, webhookSecret)

		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signature)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp UpdateWebhookDeliveryStatusResponse
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, UpdateWebhookDeliveryStatusResponseStatus(DeliveryStatusInProgress), resp.Status)
		assert.Equal(t, 50, resp.StatusPercent)
	})

	t.Run("Conflict_AlreadyDelivered", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusDelivered)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  webhookSecret,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusCompleted,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		signature := crypto.GenerateHMACSignature(bodyBytes, webhookSecret)

		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signature)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("Unauthorized_MissingSignature", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  webhookSecret, // webhook has a secret
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusCompleted,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// No X-Webhook-Signature header
		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("BadRequest_InvalidStatus", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		r := setupDeliveryHandlerTest(false, userUUID)

		// Use raw JSON to send an invalid status value
		bodyBytes := []byte(`{"status":"bogus_status"}`)

		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Unauthorized_InvalidSignature", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  webhookSecret,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusCompleted,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// Use a wrong secret to generate an invalid signature
		badSignature := crypto.GenerateHMACSignature(bodyBytes, "wrong-secret")

		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", badSignature)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("NotFound_DeliveryDoesNotExist", func(t *testing.T) {
		userUUID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		GlobalWebhookDeliveryRedisStore = deliveryStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusCompleted,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		nonexistentID := uuid.New()
		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+nonexistentID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", "sha256=anything")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Conflict_AlreadyFailed", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusFailed)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  webhookSecret,
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusInProgress,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		signature := crypto.GenerateHMACSignature(bodyBytes, webhookSecret)

		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", signature)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("Success_NoSecretSkipsHMAC", func(t *testing.T) {
		userUUID := uuid.New()
		subID := uuid.New()

		deliveryStore := newMockDeliveryRedisStore()
		record := createTestDeliveryRecord(subID, DeliveryStatusPending)
		deliveryStore.records[record.ID] = record
		GlobalWebhookDeliveryRedisStore = deliveryStore

		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{
			Id:      subID,
			OwnerId: userUUID,
			Secret:  "", // No secret configured
			Status:  "active",
		}
		GlobalWebhookSubscriptionStore = subStore

		r := setupDeliveryHandlerTest(false, userUUID)

		reqBody := UpdateWebhookDeliveryStatusRequest{
			Status: UpdateWebhookDeliveryStatusRequestStatusCompleted,
		}
		bodyBytes, err := json.Marshal(reqBody)
		require.NoError(t, err)

		// No signature header needed when webhook has no secret
		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+record.ID.String()+"/status", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// =============================================================================
// #913: CancelWebhookDelivery / ListMyWebhookDeliveries / linked-addon access
// =============================================================================

// withSourceAddon tags every request with the addon id a linked client
// credential would carry (cmd/server/jwt_auth.go sets it the same way).
func withSourceAddon(r *gin.Engine, addonID uuid.UUID) *gin.Engine {
	tagged := gin.New()
	tagged.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(WithSourceAddonID(c.Request.Context(), addonID.String()))
		c.Next()
	})
	tagged.Any("/*path", func(c *gin.Context) { r.HandleContext(c) })
	return tagged
}

func TestWebhookDeliveryCancelAndList(t *testing.T) {
	origDeliveryStore := GlobalWebhookDeliveryRedisStore
	origSubStore := GlobalWebhookSubscriptionStore
	origGroupStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookDeliveryRedisStore = origDeliveryStore
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalGroupMemberRepository = origGroupStore
	}()

	addonID := uuid.New()
	otherAddonID := uuid.New()
	ownerUUID := uuid.New()
	invokerUUID := uuid.New()
	strangerUUID := uuid.New()
	subID := uuid.New()

	seed := func() (*mockDeliveryRedisStore, *WebhookDeliveryRecord, *WebhookDeliveryRecord, *WebhookDeliveryRecord) {
		store := newMockDeliveryRedisStore()
		mine := createTestDeliveryRecord(subID, DeliveryStatusInProgress)
		mine.AddonID = &addonID
		mine.InvokedByUUID = &invokerUUID
		theirs := createTestDeliveryRecord(subID, DeliveryStatusPending)
		theirs.AddonID = &otherAddonID
		done := createTestDeliveryRecord(subID, DeliveryStatusDelivered)
		done.AddonID = &addonID
		for _, rec := range []*WebhookDeliveryRecord{mine, theirs, done} {
			store.records[rec.ID] = rec
		}
		GlobalWebhookDeliveryRedisStore = store
		subStore := newMockWebhookSubscriptionStore()
		subStore.subscriptions[subID.String()] = DBWebhookSubscription{Id: subID, OwnerId: ownerUUID, Status: "active"}
		GlobalWebhookSubscriptionStore = subStore
		return store, mine, theirs, done
	}
	do := func(r *gin.Engine, method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	t.Run("LinkedAddon_GetListCancel", func(t *testing.T) {
		store, mine, theirs, _ := seed()
		r := withSourceAddon(setupDeliveryHandlerTest(false, strangerUUID), addonID)

		assert.Equal(t, http.StatusOK, do(r, "GET", "/webhook_deliveries/"+mine.ID.String()).Code)
		assert.Equal(t, http.StatusForbidden, do(r, "GET", "/webhook_deliveries/"+theirs.ID.String()).Code)

		w := do(r, "GET", "/webhook_deliveries")
		require.Equal(t, http.StatusOK, w.Code)
		var list ListWebhookDeliveriesResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
		assert.Equal(t, 2, list.Total, "own addon's deliveries only (active + delivered)")
		for _, d := range list.Deliveries {
			assert.Equal(t, addonID, *d.AddonId)
		}

		assert.Equal(t, http.StatusNoContent, do(r, "DELETE", "/webhook_deliveries/"+mine.ID.String()).Code)
		assert.Equal(t, DeliveryStatusCancelled, store.records[mine.ID].Status)
		assert.Equal(t, http.StatusConflict, do(r, "DELETE", "/webhook_deliveries/"+mine.ID.String()).Code, "already cancelled")
		assert.Equal(t, http.StatusForbidden, do(r, "DELETE", "/webhook_deliveries/"+theirs.ID.String()).Code)

		// The addon's own status callback after cancel is rejected, status stays cancelled
		req := httptest.NewRequest("PUT", "/webhook_deliveries/"+mine.ID.String()+"/status", strings.NewReader(`{"status":"failed"}`))
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Equal(t, DeliveryStatusCancelled, store.records[mine.ID].Status)
	})

	t.Run("Cancel_TerminalIs409_MissingIs404", func(t *testing.T) {
		_, _, _, done := seed()
		r := setupDeliveryHandlerTest(true, ownerUUID)
		assert.Equal(t, http.StatusConflict, do(r, "DELETE", "/webhook_deliveries/"+done.ID.String()).Code)
		assert.Equal(t, http.StatusNotFound, do(r, "DELETE", "/webhook_deliveries/"+uuid.New().String()).Code)
		assert.Equal(t, http.StatusBadRequest, do(r, "DELETE", "/webhook_deliveries/nope").Code)
	})

	t.Run("List_Visibility", func(t *testing.T) {
		seed()
		count := func(r *gin.Engine) int {
			w := do(r, "GET", "/webhook_deliveries")
			require.Equal(t, http.StatusOK, w.Code)
			var list ListWebhookDeliveriesResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
			return list.Total
		}
		assert.Equal(t, 3, count(setupDeliveryHandlerTest(true, strangerUUID)), "admin sees all")
		assert.Equal(t, 3, count(setupDeliveryHandlerTest(false, ownerUUID)), "subscription owner sees all of its deliveries")
		assert.Equal(t, 1, count(setupDeliveryHandlerTest(false, invokerUUID)), "invoker sees what they invoked")
		assert.Equal(t, 0, count(setupDeliveryHandlerTest(false, strangerUUID)), "stranger sees nothing")
	})

	t.Run("Cancel_InvokerAndOwnerAllowed", func(t *testing.T) {
		_, mine, theirs, _ := seed()
		assert.Equal(t, http.StatusNoContent, do(setupDeliveryHandlerTest(false, invokerUUID), "DELETE", "/webhook_deliveries/"+mine.ID.String()).Code)
		assert.Equal(t, http.StatusNoContent, do(setupDeliveryHandlerTest(false, ownerUUID), "DELETE", "/webhook_deliveries/"+theirs.ID.String()).Code)
	})
}
