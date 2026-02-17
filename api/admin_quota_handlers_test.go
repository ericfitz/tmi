package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Stores for Admin Quota Handler Tests
// =============================================================================

// --- mockUserAPIQuotaStore ---

type mockUserAPIQuotaStore struct {
	quotas    map[string]UserAPIQuota
	getErr    error
	listErr   error
	countErr  error
	createErr error
	updateErr error
	deleteErr error
}

func newMockUserAPIQuotaStore() *mockUserAPIQuotaStore {
	return &mockUserAPIQuotaStore{
		quotas: make(map[string]UserAPIQuota),
	}
}

func (m *mockUserAPIQuotaStore) Get(userID string) (UserAPIQuota, error) {
	if m.getErr != nil {
		return UserAPIQuota{}, m.getErr
	}
	if q, ok := m.quotas[userID]; ok {
		return q, nil
	}
	return UserAPIQuota{}, errors.New("not found")
}

func (m *mockUserAPIQuotaStore) GetOrDefault(userID string) UserAPIQuota {
	if q, ok := m.quotas[userID]; ok {
		return q
	}
	uid := uuid.MustParse(userID)
	return UserAPIQuota{
		UserId:               uid,
		MaxRequestsPerMinute: DefaultMaxRequestsPerMinute,
		MaxRequestsPerHour:   nil,
	}
}

func (m *mockUserAPIQuotaStore) List(offset, limit int) ([]UserAPIQuota, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []UserAPIQuota
	for _, q := range m.quotas {
		result = append(result, q)
	}
	if offset > len(result) {
		return []UserAPIQuota{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockUserAPIQuotaStore) Count() (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return len(m.quotas), nil
}

func (m *mockUserAPIQuotaStore) Create(item UserAPIQuota) (UserAPIQuota, error) {
	if m.createErr != nil {
		return UserAPIQuota{}, m.createErr
	}
	item.CreatedAt = time.Now().UTC()
	item.ModifiedAt = time.Now().UTC()
	m.quotas[item.UserId.String()] = item
	return item, nil
}

func (m *mockUserAPIQuotaStore) Update(userID string, item UserAPIQuota) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	item.ModifiedAt = time.Now().UTC()
	m.quotas[userID] = item
	return nil
}

func (m *mockUserAPIQuotaStore) Delete(userID string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.quotas[userID]; !ok {
		return errors.New("not found")
	}
	delete(m.quotas, userID)
	return nil
}

// --- mockAdminWebhookQuotaStore ---
// Named differently from mockWebhookQuotaStore in webhook_handlers_test.go to avoid redefinition.

type mockAdminWebhookQuotaStore struct {
	quotas    map[string]DBWebhookQuota
	getErr    error
	listErr   error
	countErr  error
	createErr error
	updateErr error
	deleteErr error
}

func newMockAdminWebhookQuotaStore() *mockAdminWebhookQuotaStore {
	return &mockAdminWebhookQuotaStore{
		quotas: make(map[string]DBWebhookQuota),
	}
}

func (m *mockAdminWebhookQuotaStore) Get(ownerID string) (DBWebhookQuota, error) {
	if m.getErr != nil {
		return DBWebhookQuota{}, m.getErr
	}
	if q, ok := m.quotas[ownerID]; ok {
		return q, nil
	}
	return DBWebhookQuota{}, errors.New("not found")
}

func (m *mockAdminWebhookQuotaStore) GetOrDefault(ownerID string) DBWebhookQuota {
	if q, ok := m.quotas[ownerID]; ok {
		return q
	}
	uid := uuid.MustParse(ownerID)
	return DBWebhookQuota{
		OwnerId:                          uid,
		MaxSubscriptions:                 10,
		MaxEventsPerMinute:               12,
		MaxSubscriptionRequestsPerMinute: 10,
		MaxSubscriptionRequestsPerDay:    20,
	}
}

func (m *mockAdminWebhookQuotaStore) List(offset, limit int) ([]DBWebhookQuota, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []DBWebhookQuota
	for _, q := range m.quotas {
		result = append(result, q)
	}
	if offset > len(result) {
		return []DBWebhookQuota{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockAdminWebhookQuotaStore) Count() (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return len(m.quotas), nil
}

func (m *mockAdminWebhookQuotaStore) Create(item DBWebhookQuota) (DBWebhookQuota, error) {
	if m.createErr != nil {
		return DBWebhookQuota{}, m.createErr
	}
	item.CreatedAt = time.Now().UTC()
	item.ModifiedAt = time.Now().UTC()
	m.quotas[item.OwnerId.String()] = item
	return item, nil
}

func (m *mockAdminWebhookQuotaStore) Update(ownerID string, item DBWebhookQuota) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	item.ModifiedAt = time.Now().UTC()
	m.quotas[ownerID] = item
	return nil
}

func (m *mockAdminWebhookQuotaStore) Delete(ownerID string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.quotas[ownerID]; !ok {
		return errors.New("not found")
	}
	delete(m.quotas, ownerID)
	return nil
}

// --- mockAddonInvocationQuotaStore ---

type mockAddonInvocationQuotaStore struct {
	quotas        map[string]*AddonInvocationQuota
	getErr        error
	getDefaultErr error
	listErr       error
	countErr      error
	setErr        error
	deleteErr     error
}

func newMockAddonInvocationQuotaStore() *mockAddonInvocationQuotaStore {
	return &mockAddonInvocationQuotaStore{
		quotas: make(map[string]*AddonInvocationQuota),
	}
}

func (m *mockAddonInvocationQuotaStore) Get(_ context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if q, ok := m.quotas[ownerID.String()]; ok {
		return q, nil
	}
	return nil, errors.New("not found")
}

func (m *mockAddonInvocationQuotaStore) GetOrDefault(_ context.Context, ownerID uuid.UUID) (*AddonInvocationQuota, error) {
	if m.getDefaultErr != nil {
		return nil, m.getDefaultErr
	}
	if q, ok := m.quotas[ownerID.String()]; ok {
		return q, nil
	}
	return &AddonInvocationQuota{
		OwnerId:               ownerID,
		MaxActiveInvocations:  DefaultMaxActiveInvocations,
		MaxInvocationsPerHour: DefaultMaxInvocationsPerHour,
		CreatedAt:             time.Now().UTC(),
		ModifiedAt:            time.Now().UTC(),
	}, nil
}

func (m *mockAddonInvocationQuotaStore) List(_ context.Context, offset, limit int) ([]*AddonInvocationQuota, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []*AddonInvocationQuota
	for _, q := range m.quotas {
		result = append(result, q)
	}
	if offset > len(result) {
		return []*AddonInvocationQuota{}, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockAddonInvocationQuotaStore) Count(_ context.Context) (int, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return len(m.quotas), nil
}

func (m *mockAddonInvocationQuotaStore) Set(_ context.Context, quota *AddonInvocationQuota) error {
	if m.setErr != nil {
		return m.setErr
	}
	now := time.Now().UTC()
	if quota.CreatedAt.IsZero() {
		quota.CreatedAt = now
	}
	quota.ModifiedAt = now
	m.quotas[quota.OwnerId.String()] = quota
	return nil
}

func (m *mockAddonInvocationQuotaStore) Delete(_ context.Context, ownerID uuid.UUID) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.quotas[ownerID.String()]; !ok {
		return errors.New("not found")
	}
	delete(m.quotas, ownerID.String())
	return nil
}

// =============================================================================
// Test Setup Helper
// =============================================================================

func setupAdminQuotaTest(t *testing.T) (*Server, *mockUserAPIQuotaStore, *mockAdminWebhookQuotaStore, *mockAddonInvocationQuotaStore, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origUserQuotaStore := GlobalUserAPIQuotaStore
	origWebhookQuotaStore := GlobalWebhookQuotaStore
	origAddonQuotaStore := GlobalAddonInvocationQuotaStore
	origQuotaCache := GlobalQuotaCache

	mockUserQuota := newMockUserAPIQuotaStore()
	mockWebhookQuota := newMockAdminWebhookQuotaStore()
	mockAddonQuota := newMockAddonInvocationQuotaStore()

	GlobalUserAPIQuotaStore = mockUserQuota
	GlobalWebhookQuotaStore = mockWebhookQuota
	GlobalAddonInvocationQuotaStore = mockAddonQuota
	GlobalQuotaCache = nil // Disable cache in tests

	server := &Server{}
	cleanup := func() {
		GlobalUserAPIQuotaStore = origUserQuotaStore
		GlobalWebhookQuotaStore = origWebhookQuotaStore
		GlobalAddonInvocationQuotaStore = origAddonQuotaStore
		GlobalQuotaCache = origQuotaCache
	}
	return server, mockUserQuota, mockWebhookQuota, mockAddonQuota, cleanup
}

// =============================================================================
// ListUserAPIQuotas Tests
// =============================================================================

func TestListUserAPIQuotas(t *testing.T) {
	t.Run("Success_DefaultPagination", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		mockStore.quotas[userID.String()] = UserAPIQuota{
			UserId:               userID,
			MaxRequestsPerMinute: 200,
			CreatedAt:            time.Now().UTC(),
			ModifiedAt:           time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api", nil)

		server.ListUserAPIQuotas(c, ListUserAPIQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListUserQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 1)
		assert.Equal(t, 50, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Equal(t, 1, resp.Total)
	})

	t.Run("Success_WithPagination", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		// Add multiple quotas
		for i := 0; i < 5; i++ {
			uid := uuid.New()
			mockStore.quotas[uid.String()] = UserAPIQuota{
				UserId:               uid,
				MaxRequestsPerMinute: 100 + i,
				CreatedAt:            time.Now().UTC(),
				ModifiedAt:           time.Now().UTC(),
			}
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api?limit=2&offset=0", nil)

		limit := 2
		offset := 0
		server.ListUserAPIQuotas(c, ListUserAPIQuotasParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListUserQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 2)
		assert.Equal(t, 2, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Equal(t, 5, resp.Total)
	})

	t.Run("StoreError_Returns500", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.listErr = errors.New("database error")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api", nil)

		server.ListUserAPIQuotas(c, ListUserAPIQuotasParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("CountError_FallsBackToPageSize", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		uid := uuid.New()
		mockStore.quotas[uid.String()] = UserAPIQuota{
			UserId:               uid,
			MaxRequestsPerMinute: 100,
			CreatedAt:            time.Now().UTC(),
			ModifiedAt:           time.Now().UTC(),
		}
		mockStore.countErr = errors.New("count failed")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api", nil)

		server.ListUserAPIQuotas(c, ListUserAPIQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListUserQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		// Total should fall back to the number of returned items
		assert.Equal(t, len(resp.Quotas), resp.Total)
	})

	t.Run("InvalidPagination_NegativeLimit", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api?limit=-1", nil)

		limit := -1
		server.ListUserAPIQuotas(c, ListUserAPIQuotasParams{
			Limit: &limit,
		})

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyList", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api", nil)

		server.ListUserAPIQuotas(c, ListUserAPIQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListUserQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 0)
		assert.Equal(t, 0, resp.Total)
	})
}

// =============================================================================
// GetUserAPIQuota Tests
// =============================================================================

func TestGetUserAPIQuota(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		mockStore.quotas[userID.String()] = UserAPIQuota{
			UserId:               userID,
			MaxRequestsPerMinute: 500,
			CreatedAt:            time.Now().UTC(),
			ModifiedAt:           time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api/"+userID.String(), nil)

		server.GetUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp UserAPIQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 500, resp.MaxRequestsPerMinute)
		assert.Equal(t, userID, resp.UserId)
	})

	t.Run("NotFound_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		nonExistentID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api/"+nonExistentID.String(), nil)

		server.GetUserAPIQuota(c, nonExistentID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("ZeroUUID_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/api/00000000-0000-0000-0000-000000000000", nil)

		// Zero-value UUID String() is "00000000-...", not "", so handler's
		// empty-string check doesn't fire; falls through to store Get -> not found
		var zeroUUID openapi_types.UUID
		server.GetUserAPIQuota(c, zeroUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// UpdateUserAPIQuota Tests
// =============================================================================

func TestUpdateUserAPIQuota(t *testing.T) {
	t.Run("CreateNew_Returns201", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		body := map[string]interface{}{
			"max_requests_per_minute": 300,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp UserAPIQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 300, resp.MaxRequestsPerMinute)
	})

	t.Run("CreateNew_WithOptionalHour", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		body := map[string]interface{}{
			"max_requests_per_minute": 300,
			"max_requests_per_hour":   5000,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp UserAPIQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 300, resp.MaxRequestsPerMinute)
		require.NotNil(t, resp.MaxRequestsPerHour)
		assert.Equal(t, 5000, *resp.MaxRequestsPerHour)
	})

	t.Run("UpdateExisting_Returns200", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		mockStore.quotas[userID.String()] = UserAPIQuota{
			UserId:               userID,
			MaxRequestsPerMinute: 100,
			CreatedAt:            time.Now().UTC(),
			ModifiedAt:           time.Now().UTC(),
		}

		body := map[string]interface{}{
			"max_requests_per_minute": 500,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp UserAPIQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 500, resp.MaxRequestsPerMinute)
	})

	t.Run("InvalidBody_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBufferString("not json"))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingRequiredField_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		// max_requests_per_minute is required but missing
		body := map[string]interface{}{
			"max_requests_per_hour": 5000,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxRequestsPerMinute_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		body := map[string]interface{}{
			"max_requests_per_minute": MaxRequestsPerMinute + 1,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxRequestsPerHour_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		body := map[string]interface{}{
			"max_requests_per_minute": 100,
			"max_requests_per_hour":   MaxRequestsPerHour + 1,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ForeignKeyError_Returns404", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		// Simulate foreign key error on create (user doesn't exist)
		mockStore.createErr = errors.New("violates foreign key constraint")

		userID := uuid.New()
		body := map[string]interface{}{
			"max_requests_per_minute": 100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("CreateError_NonForeignKey_Returns500", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.createErr = errors.New("database connection lost")

		userID := uuid.New()
		body := map[string]interface{}{
			"max_requests_per_minute": 100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("UpdateError_Returns500", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		mockStore.quotas[userID.String()] = UserAPIQuota{
			UserId:               userID,
			MaxRequestsPerMinute: 100,
			CreatedAt:            time.Now().UTC(),
			ModifiedAt:           time.Now().UTC(),
		}
		mockStore.updateErr = errors.New("database error")

		body := map[string]interface{}{
			"max_requests_per_minute": 500,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/api/"+userID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateUserAPIQuota(c, userID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// DeleteUserAPIQuota Tests
// =============================================================================

func TestDeleteUserAPIQuota(t *testing.T) {
	t.Run("Success_Returns204", func(t *testing.T) {
		server, mockStore, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		userID := uuid.New()
		mockStore.quotas[userID.String()] = UserAPIQuota{
			UserId:               userID,
			MaxRequestsPerMinute: 200,
			CreatedAt:            time.Now().UTC(),
			ModifiedAt:           time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("DELETE", "/admin/quotas/api/"+userID.String(), nil)

		server.DeleteUserAPIQuota(c, userID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
		// Verify quota was deleted
		_, ok := mockStore.quotas[userID.String()]
		assert.False(t, ok)
	})

	t.Run("NotFound_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		nonExistentID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("DELETE", "/admin/quotas/api/"+nonExistentID.String(), nil)

		server.DeleteUserAPIQuota(c, nonExistentID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// ListWebhookQuotas Tests
// =============================================================================

func TestListWebhookQuotas(t *testing.T) {
	t.Run("Success_DefaultPagination", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = DBWebhookQuota{
			OwnerId:                          ownerID,
			MaxSubscriptions:                 20,
			MaxEventsPerMinute:               50,
			MaxSubscriptionRequestsPerMinute: 15,
			MaxSubscriptionRequestsPerDay:    200,
			CreatedAt:                        time.Now().UTC(),
			ModifiedAt:                       time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks", nil)

		server.ListWebhookQuotas(c, ListWebhookQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListWebhookQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 1)
		assert.Equal(t, 50, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Equal(t, 1, resp.Total)
	})

	t.Run("Success_WithPagination", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		for i := 0; i < 5; i++ {
			uid := uuid.New()
			mockStore.quotas[uid.String()] = DBWebhookQuota{
				OwnerId:                          uid,
				MaxSubscriptions:                 10 + i,
				MaxEventsPerMinute:               50,
				MaxSubscriptionRequestsPerMinute: 10,
				MaxSubscriptionRequestsPerDay:    100,
				CreatedAt:                        time.Now().UTC(),
				ModifiedAt:                       time.Now().UTC(),
			}
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks?limit=3&offset=0", nil)

		limit := 3
		offset := 0
		server.ListWebhookQuotas(c, ListWebhookQuotasParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListWebhookQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 3)
		assert.Equal(t, 3, resp.Limit)
		assert.Equal(t, 5, resp.Total)
	})

	t.Run("StoreError_Returns500", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.listErr = errors.New("database error")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks", nil)

		server.ListWebhookQuotas(c, ListWebhookQuotasParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("CountError_FallsBackToPageSize", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		uid := uuid.New()
		mockStore.quotas[uid.String()] = DBWebhookQuota{
			OwnerId:                          uid,
			MaxSubscriptions:                 10,
			MaxEventsPerMinute:               50,
			MaxSubscriptionRequestsPerMinute: 10,
			MaxSubscriptionRequestsPerDay:    100,
			CreatedAt:                        time.Now().UTC(),
			ModifiedAt:                       time.Now().UTC(),
		}
		mockStore.countErr = errors.New("count failed")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks", nil)

		server.ListWebhookQuotas(c, ListWebhookQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListWebhookQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, len(resp.Quotas), resp.Total)
	})

	t.Run("InvalidPagination_NegativeOffset", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks?offset=-1", nil)

		offset := -1
		server.ListWebhookQuotas(c, ListWebhookQuotasParams{
			Offset: &offset,
		})

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// GetWebhookQuota Tests
// =============================================================================

func TestGetWebhookQuota(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = DBWebhookQuota{
			OwnerId:                          ownerID,
			MaxSubscriptions:                 25,
			MaxEventsPerMinute:               100,
			MaxSubscriptionRequestsPerMinute: 20,
			MaxSubscriptionRequestsPerDay:    500,
			CreatedAt:                        time.Now().UTC(),
			ModifiedAt:                       time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks/"+ownerID.String(), nil)

		server.GetWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("NotFound_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		nonExistentID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks/"+nonExistentID.String(), nil)

		server.GetWebhookQuota(c, nonExistentID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("ZeroUUID_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/webhooks/00000000-0000-0000-0000-000000000000", nil)

		// Zero-value UUID String() is "00000000-...", not "", so handler's
		// empty-string check doesn't fire; falls through to store Get -> not found
		var zeroUUID openapi_types.UUID
		server.GetWebhookQuota(c, zeroUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// UpdateWebhookQuota Tests
// =============================================================================

func TestUpdateWebhookQuota(t *testing.T) {
	t.Run("CreateNew_Returns201", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    30,
			"max_events_per_minute":                200,
			"max_subscription_requests_per_minute": 25,
			"max_subscription_requests_per_day":    1000,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("UpdateExisting_Returns200", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = DBWebhookQuota{
			OwnerId:                          ownerID,
			MaxSubscriptions:                 10,
			MaxEventsPerMinute:               50,
			MaxSubscriptionRequestsPerMinute: 10,
			MaxSubscriptionRequestsPerDay:    100,
			CreatedAt:                        time.Now().UTC(),
			ModifiedAt:                       time.Now().UTC(),
		}

		body := map[string]interface{}{
			"max_subscriptions":                    50,
			"max_events_per_minute":                500,
			"max_subscription_requests_per_minute": 50,
			"max_subscription_requests_per_day":    5000,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("InvalidBody_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBufferString("{invalid"))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingRequiredField_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		// Missing max_events_per_minute and others
		body := map[string]interface{}{
			"max_subscriptions": 10,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxSubscriptions_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    MaxSubscriptions + 1,
			"max_events_per_minute":                50,
			"max_subscription_requests_per_minute": 10,
			"max_subscription_requests_per_day":    100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxEventsPerMinute_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    10,
			"max_events_per_minute":                MaxEventsPerMinute + 1,
			"max_subscription_requests_per_minute": 10,
			"max_subscription_requests_per_day":    100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxSubReqPerMinute_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    10,
			"max_events_per_minute":                50,
			"max_subscription_requests_per_minute": MaxSubscriptionRequestsPerMinute + 1,
			"max_subscription_requests_per_day":    100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxSubReqPerDay_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    10,
			"max_events_per_minute":                50,
			"max_subscription_requests_per_minute": 10,
			"max_subscription_requests_per_day":    MaxSubscriptionRequestsPerDay + 1,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ForeignKeyError_Returns404", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.createErr = errors.New("violates foreign key constraint")

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    10,
			"max_events_per_minute":                50,
			"max_subscription_requests_per_minute": 10,
			"max_subscription_requests_per_day":    100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("CreateError_NonForeignKey_Returns500", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.createErr = errors.New("database connection lost")

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_subscriptions":                    10,
			"max_events_per_minute":                50,
			"max_subscription_requests_per_minute": 10,
			"max_subscription_requests_per_day":    100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("UpdateError_Returns500", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = DBWebhookQuota{
			OwnerId:                          ownerID,
			MaxSubscriptions:                 10,
			MaxEventsPerMinute:               50,
			MaxSubscriptionRequestsPerMinute: 10,
			MaxSubscriptionRequestsPerDay:    100,
			CreatedAt:                        time.Now().UTC(),
			ModifiedAt:                       time.Now().UTC(),
		}
		mockStore.updateErr = errors.New("database error")

		body := map[string]interface{}{
			"max_subscriptions":                    20,
			"max_events_per_minute":                100,
			"max_subscription_requests_per_minute": 20,
			"max_subscription_requests_per_day":    200,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/webhooks/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateWebhookQuota(c, ownerID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// DeleteWebhookQuota Tests
// =============================================================================

func TestDeleteWebhookQuota(t *testing.T) {
	t.Run("Success_Returns204", func(t *testing.T) {
		server, _, mockStore, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = DBWebhookQuota{
			OwnerId:                          ownerID,
			MaxSubscriptions:                 10,
			MaxEventsPerMinute:               50,
			MaxSubscriptionRequestsPerMinute: 10,
			MaxSubscriptionRequestsPerDay:    100,
			CreatedAt:                        time.Now().UTC(),
			ModifiedAt:                       time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("DELETE", "/admin/quotas/webhooks/"+ownerID.String(), nil)

		server.DeleteWebhookQuota(c, ownerID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
		_, ok := mockStore.quotas[ownerID.String()]
		assert.False(t, ok)
	})

	t.Run("NotFound_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		nonExistentID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("DELETE", "/admin/quotas/webhooks/"+nonExistentID.String(), nil)

		server.DeleteWebhookQuota(c, nonExistentID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// ListAddonInvocationQuotas Tests
// =============================================================================

func TestListAddonInvocationQuotas(t *testing.T) {
	t.Run("Success_DefaultPagination", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = &AddonInvocationQuota{
			OwnerId:               ownerID,
			MaxActiveInvocations:  5,
			MaxInvocationsPerHour: 50,
			CreatedAt:             time.Now().UTC(),
			ModifiedAt:            time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations", nil)

		server.ListAddonInvocationQuotas(c, ListAddonInvocationQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListAddonQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 1)
		assert.Equal(t, 50, resp.Limit)
		assert.Equal(t, 0, resp.Offset)
		assert.Equal(t, 1, resp.Total)
	})

	t.Run("Success_WithPagination", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		for i := 0; i < 5; i++ {
			uid := uuid.New()
			mockStore.quotas[uid.String()] = &AddonInvocationQuota{
				OwnerId:               uid,
				MaxActiveInvocations:  3 + i,
				MaxInvocationsPerHour: 10 + i,
				CreatedAt:             time.Now().UTC(),
				ModifiedAt:            time.Now().UTC(),
			}
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations?limit=2&offset=1", nil)

		limit := 2
		offset := 1
		server.ListAddonInvocationQuotas(c, ListAddonInvocationQuotasParams{
			Limit:  &limit,
			Offset: &offset,
		})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListAddonQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 2)
		assert.Equal(t, 2, resp.Limit)
		assert.Equal(t, 1, resp.Offset)
		assert.Equal(t, 5, resp.Total)
	})

	t.Run("StoreError_Returns500", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.listErr = errors.New("database error")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations", nil)

		server.ListAddonInvocationQuotas(c, ListAddonInvocationQuotasParams{})

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("CountError_FallsBackToPageSize", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		uid := uuid.New()
		mockStore.quotas[uid.String()] = &AddonInvocationQuota{
			OwnerId:               uid,
			MaxActiveInvocations:  5,
			MaxInvocationsPerHour: 50,
			CreatedAt:             time.Now().UTC(),
			ModifiedAt:            time.Now().UTC(),
		}
		mockStore.countErr = errors.New("count failed")

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations", nil)

		server.ListAddonInvocationQuotas(c, ListAddonInvocationQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListAddonQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, len(resp.Quotas), resp.Total)
	})

	t.Run("InvalidPagination_LimitExceedsMax", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations?limit=99999", nil)

		limit := MaxPaginationLimit + 1
		server.ListAddonInvocationQuotas(c, ListAddonInvocationQuotasParams{
			Limit: &limit,
		})

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("EmptyList", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations", nil)

		server.ListAddonInvocationQuotas(c, ListAddonInvocationQuotasParams{})

		assert.Equal(t, http.StatusOK, w.Code)

		var resp ListAddonQuotasResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp.Quotas, 0)
		assert.Equal(t, 0, resp.Total)
	})
}

// =============================================================================
// GetAddonInvocationQuota Tests
// =============================================================================

func TestGetAddonInvocationQuota(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = &AddonInvocationQuota{
			OwnerId:               ownerID,
			MaxActiveInvocations:  8,
			MaxInvocationsPerHour: 100,
			CreatedAt:             time.Now().UTC(),
			ModifiedAt:            time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations/"+ownerID.String(), nil)

		server.GetAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp AddonInvocationQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 8, resp.MaxActiveInvocations)
		assert.Equal(t, 100, resp.MaxInvocationsPerHour)
	})

	t.Run("NotFound_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		nonExistentID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations/"+nonExistentID.String(), nil)

		server.GetAddonInvocationQuota(c, nonExistentID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("ZeroUUID_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/admin/quotas/addon-invocations/00000000-0000-0000-0000-000000000000", nil)

		// Zero-value UUID String() is "00000000-...", not "", so handler's
		// empty-string check doesn't fire; falls through to store Get -> not found
		var zeroUUID openapi_types.UUID
		server.GetAddonInvocationQuota(c, zeroUUID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// UpdateAddonInvocationQuota Tests
// =============================================================================

func TestUpdateAddonInvocationQuota(t *testing.T) {
	t.Run("CreateNew_Returns201", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_active_invocations":   7,
			"max_invocations_per_hour": 100,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp AddonInvocationQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 7, resp.MaxActiveInvocations)
		assert.Equal(t, 100, resp.MaxInvocationsPerHour)
	})

	t.Run("UpdateExisting_Returns200", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = &AddonInvocationQuota{
			OwnerId:               ownerID,
			MaxActiveInvocations:  3,
			MaxInvocationsPerHour: 10,
			CreatedAt:             time.Now().UTC(),
			ModifiedAt:            time.Now().UTC(),
		}

		body := map[string]interface{}{
			"max_active_invocations":   10,
			"max_invocations_per_hour": 500,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp AddonInvocationQuota
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 10, resp.MaxActiveInvocations)
		assert.Equal(t, 500, resp.MaxInvocationsPerHour)
	})

	t.Run("InvalidBody_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBufferString("not json"))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("MissingRequiredField_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		// Missing max_invocations_per_hour
		body := map[string]interface{}{
			"max_active_invocations": 5,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxActiveInvocations_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_active_invocations":   MaxActiveInvocations + 1,
			"max_invocations_per_hour": 50,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ExceedsMaxInvocationsPerHour_Returns400", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_active_invocations":   5,
			"max_invocations_per_hour": MaxInvocationsPerHour + 1,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ForeignKeyError_Returns404", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.setErr = errors.New("violates foreign key constraint")

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_active_invocations":   5,
			"max_invocations_per_hour": 50,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("SetError_NonForeignKey_Returns500", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.setErr = errors.New("database connection lost")

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_active_invocations":   5,
			"max_invocations_per_hour": 50,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetOrDefaultError_Returns500", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		mockStore.getDefaultErr = errors.New("database error on get")

		ownerID := uuid.New()
		body := map[string]interface{}{
			"max_active_invocations":   5,
			"max_invocations_per_hour": 50,
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/admin/quotas/addon-invocations/"+ownerID.String(), bytes.NewBuffer(bodyBytes))
		c.Request.Header.Set("Content-Type", "application/json")

		server.UpdateAddonInvocationQuota(c, ownerID)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// DeleteAddonInvocationQuota Tests
// =============================================================================

func TestDeleteAddonInvocationQuota(t *testing.T) {
	t.Run("Success_Returns204", func(t *testing.T) {
		server, _, _, mockStore, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		ownerID := uuid.New()
		mockStore.quotas[ownerID.String()] = &AddonInvocationQuota{
			OwnerId:               ownerID,
			MaxActiveInvocations:  5,
			MaxInvocationsPerHour: 50,
			CreatedAt:             time.Now().UTC(),
			ModifiedAt:            time.Now().UTC(),
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("DELETE", "/admin/quotas/addon-invocations/"+ownerID.String(), nil)

		server.DeleteAddonInvocationQuota(c, ownerID)

		// c.Status() doesn't flush to httptest.NewRecorder; check Gin's writer status
		assert.Equal(t, http.StatusNoContent, c.Writer.Status())
		_, ok := mockStore.quotas[ownerID.String()]
		assert.False(t, ok)
	})

	t.Run("NotFound_Returns404", func(t *testing.T) {
		server, _, _, _, cleanup := setupAdminQuotaTest(t)
		defer cleanup()

		nonExistentID := uuid.New()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("DELETE", "/admin/quotas/addon-invocations/"+nonExistentID.String(), nil)

		server.DeleteAddonInvocationQuota(c, nonExistentID)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
