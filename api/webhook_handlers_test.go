package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Stores for Webhook Handler Tests
// =============================================================================

// mockWebhookSubscriptionStore implements WebhookSubscriptionStoreInterface for testing
type mockWebhookSubscriptionStore struct {
	subscriptions map[string]DBWebhookSubscription
	ownerCounts   map[string]int
	err           error
}

func newMockWebhookSubscriptionStore() *mockWebhookSubscriptionStore {
	return &mockWebhookSubscriptionStore{
		subscriptions: make(map[string]DBWebhookSubscription),
		ownerCounts:   make(map[string]int),
	}
}

func (m *mockWebhookSubscriptionStore) Get(id string) (DBWebhookSubscription, error) {
	if m.err != nil {
		return DBWebhookSubscription{}, m.err
	}
	if sub, ok := m.subscriptions[id]; ok {
		return sub, nil
	}
	return DBWebhookSubscription{}, errors.New("not found")
}

func (m *mockWebhookSubscriptionStore) List(offset, limit int, filter func(DBWebhookSubscription) bool) []DBWebhookSubscription {
	var result []DBWebhookSubscription
	for _, sub := range m.subscriptions {
		if filter == nil || filter(sub) {
			result = append(result, sub)
		}
	}
	if offset > len(result) {
		return []DBWebhookSubscription{}
	}
	end := offset + limit
	if limit == 0 || end > len(result) {
		end = len(result)
	}
	return result[offset:end]
}

func (m *mockWebhookSubscriptionStore) ListByOwner(ownerID string, offset, limit int) ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []DBWebhookSubscription
	for _, sub := range m.subscriptions {
		if sub.OwnerId.String() == ownerID {
			result = append(result, sub)
		}
	}
	if offset > len(result) {
		return []DBWebhookSubscription{}, nil
	}
	end := offset + limit
	if limit == 0 || end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockWebhookSubscriptionStore) ListByThreatModel(threatModelID string, offset, limit int) ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []DBWebhookSubscription
	for _, sub := range m.subscriptions {
		if sub.ThreatModelId != nil && sub.ThreatModelId.String() == threatModelID {
			result = append(result, sub)
		}
	}
	if offset > len(result) {
		return []DBWebhookSubscription{}, nil
	}
	end := offset + limit
	if limit == 0 || end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockWebhookSubscriptionStore) ListActiveByOwner(ownerID string) ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []DBWebhookSubscription
	for _, sub := range m.subscriptions {
		if sub.OwnerId.String() == ownerID && sub.Status == "active" {
			result = append(result, sub)
		}
	}
	return result, nil
}

func (m *mockWebhookSubscriptionStore) Create(sub DBWebhookSubscription, idSetter func(DBWebhookSubscription, string) DBWebhookSubscription) (DBWebhookSubscription, error) {
	if m.err != nil {
		return DBWebhookSubscription{}, m.err
	}
	id := uuid.New().String()
	if idSetter != nil {
		sub = idSetter(sub, id)
	} else {
		sub.Id = uuid.MustParse(id)
	}
	sub.CreatedAt = time.Now().UTC()
	sub.ModifiedAt = time.Now().UTC()
	m.subscriptions[sub.Id.String()] = sub
	return sub, nil
}

func (m *mockWebhookSubscriptionStore) Update(id string, sub DBWebhookSubscription) error {
	if m.err != nil {
		return m.err
	}
	m.subscriptions[id] = sub
	return nil
}

func (m *mockWebhookSubscriptionStore) Delete(id string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.subscriptions, id)
	return nil
}

func (m *mockWebhookSubscriptionStore) Count() int {
	return len(m.subscriptions)
}

func (m *mockWebhookSubscriptionStore) CountByOwner(ownerID string) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	if count, ok := m.ownerCounts[ownerID]; ok {
		return count, nil
	}
	count := 0
	for _, sub := range m.subscriptions {
		if sub.OwnerId.String() == ownerID {
			count++
		}
	}
	return count, nil
}

func (m *mockWebhookSubscriptionStore) UpdateStatus(id, status string) error {
	if m.err != nil {
		return m.err
	}
	if sub, ok := m.subscriptions[id]; ok {
		sub.Status = status
		m.subscriptions[id] = sub
		return nil
	}
	return errors.New("not found")
}

func (m *mockWebhookSubscriptionStore) UpdateChallenge(id, challenge string, challengesSent int) error {
	if m.err != nil {
		return m.err
	}
	if sub, ok := m.subscriptions[id]; ok {
		sub.Challenge = challenge
		sub.ChallengesSent = challengesSent
		m.subscriptions[id] = sub
		return nil
	}
	return errors.New("not found")
}

func (m *mockWebhookSubscriptionStore) UpdatePublicationStats(id string, success bool) error {
	if m.err != nil {
		return m.err
	}
	if sub, ok := m.subscriptions[id]; ok {
		if success {
			now := time.Now().UTC()
			sub.LastSuccessfulUse = &now
			sub.PublicationFailures = 0
		} else {
			sub.PublicationFailures++
		}
		m.subscriptions[id] = sub
		return nil
	}
	return errors.New("not found")
}

func (m *mockWebhookSubscriptionStore) ListPendingVerification() ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []DBWebhookSubscription
	for _, sub := range m.subscriptions {
		if sub.Status == "pending_verification" {
			result = append(result, sub)
		}
	}
	return result, nil
}

func (m *mockWebhookSubscriptionStore) ListIdle(daysIdle int) ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []DBWebhookSubscription{}, nil
}

func (m *mockWebhookSubscriptionStore) ListBroken(minFailures, daysSinceSuccess int) ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	return []DBWebhookSubscription{}, nil
}

func (m *mockWebhookSubscriptionStore) ListPendingDelete() ([]DBWebhookSubscription, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []DBWebhookSubscription
	for _, sub := range m.subscriptions {
		if sub.Status == "pending_delete" {
			result = append(result, sub)
		}
	}
	return result, nil
}

func (m *mockWebhookSubscriptionStore) IncrementTimeouts(id string) error {
	return nil
}

func (m *mockWebhookSubscriptionStore) ResetTimeouts(id string) error {
	return nil
}

// mockWebhookQuotaStore implements WebhookQuotaStoreInterface for testing
type mockWebhookQuotaStore struct {
	quotas map[string]DBWebhookQuota
}

func newMockWebhookQuotaStore() *mockWebhookQuotaStore {
	return &mockWebhookQuotaStore{
		quotas: make(map[string]DBWebhookQuota),
	}
}

func (m *mockWebhookQuotaStore) Get(ownerID string) (DBWebhookQuota, error) {
	if q, ok := m.quotas[ownerID]; ok {
		return q, nil
	}
	return DBWebhookQuota{}, errors.New("not found")
}

func (m *mockWebhookQuotaStore) GetOrDefault(ownerID string) DBWebhookQuota {
	if q, ok := m.quotas[ownerID]; ok {
		return q
	}
	return DBWebhookQuota{
		MaxSubscriptions:                 10,
		MaxSubscriptionRequestsPerMinute: 10,
		MaxEventsPerMinute:               100,
	}
}

func (m *mockWebhookQuotaStore) List(offset, limit int) ([]DBWebhookQuota, error) {
	var result []DBWebhookQuota
	for _, q := range m.quotas {
		result = append(result, q)
	}
	return result, nil
}

func (m *mockWebhookQuotaStore) Create(item DBWebhookQuota) (DBWebhookQuota, error) {
	m.quotas[item.OwnerId.String()] = item
	return item, nil
}

func (m *mockWebhookQuotaStore) Update(ownerID string, item DBWebhookQuota) error {
	m.quotas[ownerID] = item
	return nil
}

func (m *mockWebhookQuotaStore) Delete(ownerID string) error {
	delete(m.quotas, ownerID)
	return nil
}

func (m *mockWebhookQuotaStore) Count() (int, error) {
	return len(m.quotas), nil
}

// =============================================================================
// Test Setup Helpers
// =============================================================================

// setupWebhookRouter creates a test router with webhook handlers
func setupWebhookRouter(userID, userInternalUUID string, isAdmin bool) (*gin.Engine, *Server) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Create server with mock stores
	server := &Server{}

	// Set up mock admin store (uses mockGroupMemberStoreForAdmin from authorization_middleware_test.go)
	GlobalGroupMemberStore = &mockGroupMemberStoreForAdmin{
		isAdminResult: isAdmin,
	}

	// Add fake auth middleware
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", userID)
		c.Set("userID", userID)
		c.Set("userInternalUUID", userInternalUUID)
		c.Set("userProvider", "tmi") // Required by RequireAdministrator
		if isAdmin {
			c.Set("isAdmin", true)
		}
		c.Next()
	})

	// Register webhook routes under /admin/ with administrator middleware
	adminGroup := r.Group("/admin")
	adminGroup.Use(AdministratorMiddleware())
	{
		adminGroup.GET("/webhooks/subscriptions", func(c *gin.Context) {
			var params ListWebhookSubscriptionsParams
			if offset := c.Query("offset"); offset != "" {
				var o int
				if err := json.Unmarshal([]byte(offset), &o); err == nil {
					params.Offset = &o
				}
			}
			if limit := c.Query("limit"); limit != "" {
				var l int
				if err := json.Unmarshal([]byte(limit), &l); err == nil {
					params.Limit = &l
				}
			}
			server.ListWebhookSubscriptions(c, params)
		})
		adminGroup.POST("/webhooks/subscriptions", server.CreateWebhookSubscription)
		adminGroup.GET("/webhooks/subscriptions/:webhook_id", func(c *gin.Context) {
			webhookIDStr := c.Param("webhook_id")
			webhookID, _ := uuid.Parse(webhookIDStr)
			server.GetWebhookSubscription(c, webhookID)
		})
		adminGroup.DELETE("/webhooks/subscriptions/:webhook_id", func(c *gin.Context) {
			webhookIDStr := c.Param("webhook_id")
			webhookID, _ := uuid.Parse(webhookIDStr)
			server.DeleteWebhookSubscription(c, webhookID)
		})
		adminGroup.POST("/webhooks/subscriptions/:webhook_id/test", func(c *gin.Context) {
			webhookIDStr := c.Param("webhook_id")
			webhookID, _ := uuid.Parse(webhookIDStr)
			server.TestWebhookSubscription(c, webhookID)
		})
		adminGroup.GET("/webhooks/deliveries", func(c *gin.Context) {
			var params ListWebhookDeliveriesParams
			server.ListWebhookDeliveries(c, params)
		})
		adminGroup.GET("/webhooks/deliveries/:delivery_id", func(c *gin.Context) {
			deliveryIDStr := c.Param("delivery_id")
			deliveryID, _ := uuid.Parse(deliveryIDStr)
			server.GetWebhookDelivery(c, deliveryID)
		})
	}

	return r, server
}

// =============================================================================
// ListWebhookSubscriptions Tests
// =============================================================================

func TestListWebhookSubscriptions(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberStore = origAdminStore
	}()

	t.Run("Success_AdminCanList", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		_, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		// Admin can list all subscriptions
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListWebhookSubscriptionsResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Subscriptions, 1)
		assert.Equal(t, "Test Webhook", response.Subscriptions[0].Name)
	})

	t.Run("EmptyList_Admin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListWebhookSubscriptionsResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Subscriptions, 0)
	})

	t.Run("Forbidden_NonAdmin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		_, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		// Non-admin should be forbidden
		r, _ := setupWebhookRouter("user@example.com", userUUID.String(), false)

		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("AdminSeesAllSubscriptions", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		// Create subscriptions from different users
		user1UUID := uuid.New()
		user2UUID := uuid.New()
		adminUUID := uuid.New()

		_, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: user1UUID,
			Name:    "User1 Webhook",
			Url:     "https://example.com/webhook1",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		_, err = mockSubStore.Create(DBWebhookSubscription{
			OwnerId: user2UUID,
			Name:    "User2 Webhook",
			Url:     "https://example.com/webhook2",
			Events:  []string{"threat.updated"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		// Admin should see all subscriptions
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response ListWebhookSubscriptionsResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Len(t, response.Subscriptions, 2)
	})
}

// =============================================================================
// CreateWebhookSubscription Tests
// =============================================================================

func TestCreateWebhookSubscription(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberStore
	origDenyListStore := GlobalWebhookUrlDenyListStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberStore = origAdminStore
		GlobalWebhookUrlDenyListStore = origDenyListStore
	}()
	GlobalWebhookUrlDenyListStore = &mockDenyListStore{entries: []WebhookUrlDenyListEntry{}}

	t.Run("Success_AdminCanCreate", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		reqBody := map[string]any{
			"name":   "Test Webhook",
			"url":    "https://example.com/webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response WebhookSubscription
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Test Webhook", response.Name)
		assert.Equal(t, "https://example.com/webhook", response.Url)
		assert.NotNil(t, response.Secret)
		assert.Equal(t, WebhookSubscriptionStatusPendingVerification, response.Status)
	})

	t.Run("Forbidden_NonAdmin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("user@example.com", userUUID.String(), false)

		reqBody := map[string]any{
			"name":   "Test Webhook",
			"url":    "https://example.com/webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("MissingName", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		reqBody := map[string]any{
			"url":    "https://example.com/webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingURL", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		reqBody := map[string]any{
			"name":   "Test Webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("NonHTTPSURL", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		reqBody := map[string]any{
			"name":   "Test Webhook",
			"url":    "http://example.com/webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("HTTPURLAllowedWhenConfigured", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, server := setupWebhookRouter("admin@example.com", userUUID.String(), true)
		server.allowHTTPWebhooks = true

		reqBody := map[string]any{
			"name":   "Test Webhook",
			"url":    "http://my-service.default.svc.cluster.local:8080/webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response WebhookSubscription
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "http://my-service.default.svc.cluster.local:8080/webhook", response.Url)
	})

	t.Run("FTPURLRejectedEvenWhenHTTPAllowed", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, server := setupWebhookRouter("admin@example.com", userUUID.String(), true)
		server.allowHTTPWebhooks = true

		reqBody := map[string]any{
			"name":   "Test Webhook",
			"url":    "ftp://example.com/webhook",
			"events": []string{"threat.created"},
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingEvents", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		reqBody := map[string]any{
			"name": "Test Webhook",
			"url":  "https://example.com/webhook",
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("WithCustomSecret", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

		customSecret := "my-custom-secret-key"
		reqBody := map[string]any{
			"name":   "Test Webhook",
			"url":    "https://example.com/webhook",
			"events": []string{"threat.created"},
			"secret": customSecret,
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response WebhookSubscription
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotNil(t, response.Secret)
		assert.Equal(t, customSecret, *response.Secret)
	})
}

// =============================================================================
// GetWebhookSubscription Tests
// =============================================================================

func TestGetWebhookSubscription(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberStore = origAdminStore
	}()

	t.Run("Success_AdminCanGet", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
			Secret:  "secret-key",
		}, nil)
		require.NoError(t, err)

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions/"+sub.Id.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response WebhookSubscription
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Test Webhook", response.Name)
		// Secret should not be included in GET response
		assert.Nil(t, response.Secret)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		nonExistentID := uuid.New()
		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Forbidden_NonAdmin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		// Create subscription
		ownerUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: ownerUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		// Try to access as non-admin (even as owner should be forbidden)
		r, _ := setupWebhookRouter("owner@example.com", ownerUUID.String(), false)

		req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions/"+sub.Id.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

}

// =============================================================================
// DeleteWebhookSubscription Tests
// =============================================================================

func TestDeleteWebhookSubscription(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberStore = origAdminStore
	}()

	t.Run("Success_AdminCanDelete", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		req, _ := http.NewRequest("DELETE", "/admin/webhooks/subscriptions/"+sub.Id.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify deletion
		_, err = mockSubStore.Get(sub.Id.String())
		assert.Error(t, err)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		nonExistentID := uuid.New()
		req, _ := http.NewRequest("DELETE", "/admin/webhooks/subscriptions/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Forbidden_NonAdmin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		ownerUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: ownerUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		// Even owner cannot delete without admin privileges
		r, _ := setupWebhookRouter("owner@example.com", ownerUUID.String(), false)

		req, _ := http.NewRequest("DELETE", "/admin/webhooks/subscriptions/"+sub.Id.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)

		// Verify not deleted
		_, err = mockSubStore.Get(sub.Id.String())
		assert.NoError(t, err)
	})

}

// =============================================================================
// TestWebhookSubscription Tests
// =============================================================================

func TestTestWebhookSubscription(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origRedisDelStore := GlobalWebhookDeliveryRedisStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookDeliveryRedisStore = origRedisDelStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberStore = origAdminStore
	}()

	t.Run("Success_AdminCanTest", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created", "threat.updated"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions/"+sub.Id.String()+"/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var response WebhookTestResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, response.DeliveryId)
	})

	t.Run("WithEventType", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		reqBody := map[string]any{
			"event_type": "threat.updated",
		}
		body, _ := json.Marshal(reqBody)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions/"+sub.Id.String()+"/test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		nonExistentID := uuid.New()
		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions/"+nonExistentID.String()+"/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Forbidden_NonAdmin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		ownerUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: ownerUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		// Even owner cannot test without admin privileges
		r, _ := setupWebhookRouter("owner@example.com", ownerUUID.String(), false)

		req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions/"+sub.Id.String()+"/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// =============================================================================
// GetWebhookDelivery Tests
// =============================================================================

func TestGetWebhookDelivery(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origRedisStore := GlobalWebhookDeliveryRedisStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookDeliveryRedisStore = origRedisStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberStore = origAdminStore
	}()

	t.Run("Success_AdminCanGet", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		userUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: userUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		now := time.Now().UTC()
		deliveryID := uuid.New()
		record := &WebhookDeliveryRecord{
			ID:             deliveryID,
			SubscriptionID: sub.Id,
			EventType:      "threat.created",
			Payload:        `{"test": "data"}`,
			Status:         DeliveryStatusDelivered,
			Attempts:       1,
			CreatedAt:      now,
			DeliveredAt:    &now,
			LastActivityAt: now,
		}
		mockRedisStore.records[deliveryID] = record

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		req, _ := http.NewRequest("GET", "/admin/webhooks/deliveries/"+deliveryID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response WebhookDelivery
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, WebhookEventType("threat.created"), response.EventType)
		assert.Equal(t, WebhookDeliveryStatus("delivered"), response.Status)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		adminUUID := uuid.New()
		r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

		nonExistentID := uuid.New()
		req, _ := http.NewRequest("GET", "/admin/webhooks/deliveries/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("Forbidden_NonAdmin", func(t *testing.T) {
		mockSubStore := newMockWebhookSubscriptionStore()
		mockRedisStore := newMockDeliveryRedisStore()
		GlobalWebhookSubscriptionStore = mockSubStore
		GlobalWebhookDeliveryRedisStore = mockRedisStore
		GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

		ownerUUID := uuid.New()
		sub, err := mockSubStore.Create(DBWebhookSubscription{
			OwnerId: ownerUUID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Status:  "active",
		}, nil)
		require.NoError(t, err)

		now := time.Now().UTC()
		deliveryID := uuid.New()
		record := &WebhookDeliveryRecord{
			ID:             deliveryID,
			SubscriptionID: sub.Id,
			EventType:      "threat.created",
			Payload:        `{"test": "data"}`,
			Status:         DeliveryStatusDelivered,
			CreatedAt:      now,
			LastActivityAt: now,
		}
		mockRedisStore.records[deliveryID] = record

		// Even owner cannot access delivery without admin privileges
		r, _ := setupWebhookRouter("owner@example.com", ownerUUID.String(), false)

		req, _ := http.NewRequest("GET", "/admin/webhooks/deliveries/"+deliveryID.String(), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestDBWebhookSubscriptionToAPI(t *testing.T) {
	now := time.Now().UTC()
	ownerID := uuid.New()
	subID := uuid.New()
	tmID := uuid.New()

	t.Run("BasicConversion", func(t *testing.T) {
		dbSub := DBWebhookSubscription{
			Id:                  subID,
			OwnerId:             ownerID,
			ThreatModelId:       &tmID,
			Name:                "Test Webhook",
			Url:                 "https://example.com/webhook",
			Events:              []string{"threat.created", "threat.updated"},
			Secret:              "secret-key",
			Status:              "active",
			CreatedAt:           now,
			ModifiedAt:          now,
			ChallengesSent:      2,
			PublicationFailures: 1,
		}

		// Without secret
		result := dbWebhookSubscriptionToAPI(dbSub, false)
		assert.Equal(t, subID, result.Id)
		assert.Equal(t, ownerID, result.OwnerId)
		assert.Equal(t, "Test Webhook", result.Name)
		assert.Equal(t, "https://example.com/webhook", result.Url)
		assert.Len(t, result.Events, 2)
		assert.Nil(t, result.Secret)
		assert.Equal(t, WebhookSubscriptionStatusActive, result.Status)
		assert.NotNil(t, result.ThreatModelId)
		assert.Equal(t, &tmID, result.ThreatModelId)
	})

	t.Run("WithSecret", func(t *testing.T) {
		dbSub := DBWebhookSubscription{
			Id:      subID,
			OwnerId: ownerID,
			Name:    "Test Webhook",
			Url:     "https://example.com/webhook",
			Events:  []string{"threat.created"},
			Secret:  "secret-key",
			Status:  "active",
		}

		result := dbWebhookSubscriptionToAPI(dbSub, true)
		assert.NotNil(t, result.Secret)
		assert.Equal(t, "secret-key", *result.Secret)
	})
}

func TestDeliveryRecordToWebhookDelivery(t *testing.T) {
	now := time.Now().UTC()
	deliveryID := uuid.New()
	subID := uuid.New()

	t.Run("BasicConversion", func(t *testing.T) {
		record := &WebhookDeliveryRecord{
			ID:             deliveryID,
			SubscriptionID: subID,
			EventType:      "threat.created",
			Payload:        `{"test": "data"}`,
			Status:         "delivered",
			Attempts:       1,
			CreatedAt:      now,
			DeliveredAt:    &now,
			LastActivityAt: now,
		}

		result := deliveryRecordToWebhookDelivery(record)
		assert.Equal(t, deliveryID, result.Id)
		assert.Equal(t, subID, result.SubscriptionId)
		assert.Equal(t, WebhookEventType("threat.created"), result.EventType)
		assert.Equal(t, WebhookDeliveryStatus("delivered"), result.Status)
		assert.Equal(t, 1, result.Attempts)
		assert.NotNil(t, result.Payload)
		assert.NotNil(t, result.DeliveredAt)
	})

	t.Run("WithError", func(t *testing.T) {
		record := &WebhookDeliveryRecord{
			ID:             deliveryID,
			SubscriptionID: subID,
			EventType:      "threat.created",
			Payload:        `{"test": "data"}`,
			Status:         "failed",
			Attempts:       3,
			CreatedAt:      now,
			LastError:      "connection refused",
			LastActivityAt: now,
		}

		result := deliveryRecordToWebhookDelivery(record)
		assert.Equal(t, WebhookDeliveryStatus("failed"), result.Status)
		assert.NotNil(t, result.LastError)
		assert.Equal(t, "connection refused", *result.LastError)
	})
}
