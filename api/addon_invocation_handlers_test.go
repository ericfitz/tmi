package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock AddonInvocationStore for invocation handler tests
// =============================================================================

// MockAddonInvocationStore is a mock implementation of AddonInvocationStore
type MockAddonInvocationStore struct {
	mock.Mock
}

func (m *MockAddonInvocationStore) Create(ctx context.Context, invocation *AddonInvocation) error {
	args := m.Called(ctx, invocation)
	return args.Error(0)
}

func (m *MockAddonInvocationStore) Get(ctx context.Context, id uuid.UUID) (*AddonInvocation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*AddonInvocation), args.Error(1)
}

func (m *MockAddonInvocationStore) Update(ctx context.Context, invocation *AddonInvocation) error {
	args := m.Called(ctx, invocation)
	return args.Error(0)
}

func (m *MockAddonInvocationStore) List(ctx context.Context, userID *uuid.UUID, addonID *uuid.UUID, status string, limit, offset int) ([]AddonInvocation, int, error) {
	args := m.Called(ctx, userID, addonID, status, limit, offset)
	return args.Get(0).([]AddonInvocation), args.Int(1), args.Error(2)
}

func (m *MockAddonInvocationStore) CountActive(ctx context.Context, addonID uuid.UUID) (int, error) {
	args := m.Called(ctx, addonID)
	return args.Int(0), args.Error(1)
}

func (m *MockAddonInvocationStore) GetActiveForUser(ctx context.Context, userID uuid.UUID) (*AddonInvocation, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*AddonInvocation), args.Error(1)
}

func (m *MockAddonInvocationStore) ListActiveForUser(ctx context.Context, userID uuid.UUID, limit int) ([]AddonInvocation, error) {
	args := m.Called(ctx, userID, limit)
	return args.Get(0).([]AddonInvocation), args.Error(1)
}

func (m *MockAddonInvocationStore) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockAddonInvocationStore) ListStale(ctx context.Context, timeout time.Duration) ([]AddonInvocation, error) {
	args := m.Called(ctx, timeout)
	return args.Get(0).([]AddonInvocation), args.Error(1)
}

// =============================================================================
// Helper functions for invocation handler tests
// =============================================================================

// saveAndClearInvocationGlobals saves current global store state and sets test mocks.
// Returns a cleanup function that must be deferred.
func saveAndClearInvocationGlobals(
	addonStore AddonStore,
	invocationStore AddonInvocationStore,
	webhookStore WebhookSubscriptionStoreInterface,
) func() {
	origAddonStore := GlobalAddonStore
	origInvocationStore := GlobalAddonInvocationStore
	origWebhookStore := GlobalWebhookSubscriptionStore
	origRateLimiter := GlobalAddonRateLimiter
	origWorker := GlobalAddonInvocationWorker
	origGroupMemberStore := GlobalGroupMemberStore

	GlobalAddonStore = addonStore
	GlobalAddonInvocationStore = invocationStore
	GlobalWebhookSubscriptionStore = webhookStore
	GlobalAddonRateLimiter = nil
	GlobalAddonInvocationWorker = nil
	// nil GroupMemberStore means IsUserAdministrator returns false
	GlobalGroupMemberStore = nil

	return func() {
		GlobalAddonStore = origAddonStore
		GlobalAddonInvocationStore = origInvocationStore
		GlobalWebhookSubscriptionStore = origWebhookStore
		GlobalAddonRateLimiter = origRateLimiter
		GlobalAddonInvocationWorker = origWorker
		GlobalGroupMemberStore = origGroupMemberStore
	}
}

// setAuthContext sets the standard authenticated user context on a gin.Context.
func setAuthContext(c *gin.Context, email, providerID string, internalUUID uuid.UUID) {
	c.Set("userEmail", email)
	c.Set("userID", providerID)
	c.Set("userInternalUUID", internalUUID)
}

// =============================================================================
// InvokeAddon Tests
// =============================================================================

func TestInvokeAddon_InvalidAddonID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	c, w := CreateTestGinContext("POST", "/addons/not-a-uuid/invoke")
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}

	InvokeAddon(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid add-on ID format")
}

func TestInvokeAddon_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	c, w := CreateTestGinContext("POST", "/addons/"+addonID.String()+"/invoke")
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	// No user context set

	InvokeAddon(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "Authentication required")
}

func TestInvokeAddon_NoInternalUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	c, w := CreateTestGinContext("POST", "/addons/"+addonID.String()+"/invoke")
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	// Set email and providerID but NOT internalUUID
	c.Set("userEmail", "alice@example.com")
	c.Set("userID", "alice-provider-id")

	InvokeAddon(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "User identity not available")
}

func TestInvokeAddon_InvalidRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	userUUID := uuid.New()

	c, w := CreateTestGinContextWithBody("POST", "/addons/"+addonID.String()+"/invoke",
		"application/json", []byte(`{invalid json`))
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	InvokeAddon(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestInvokeAddon_PayloadTooLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	userUUID := uuid.New()
	threatModelID := uuid.New()

	// Build a payload that when JSON-serialized exceeds 1024 bytes
	largeValue := strings.Repeat("x", 1025)
	payload := map[string]interface{}{
		"data": largeValue,
	}

	reqBody := map[string]interface{}{
		"threat_model_id": threatModelID.String(),
		"payload":         payload,
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("POST", "/addons/"+addonID.String()+"/invoke",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	InvokeAddon(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Payload exceeds maximum size")
}

func TestInvokeAddon_AddonNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	userUUID := uuid.New()
	threatModelID := uuid.New()

	reqBody := map[string]interface{}{
		"threat_model_id": threatModelID.String(),
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("POST", "/addons/"+addonID.String()+"/invoke",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	mockAddonStore.On("Get", mock.Anything, addonID).Return(nil, errors.New("not found"))

	InvokeAddon(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Add-on not found")
	mockAddonStore.AssertExpectations(t)
}

func TestInvokeAddon_InvalidObjectType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	userUUID := uuid.New()
	threatModelID := uuid.New()
	webhookID := uuid.New()

	reqBody := map[string]interface{}{
		"threat_model_id": threatModelID.String(),
		"object_type":     "diagram",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("POST", "/addons/"+addonID.String()+"/invoke",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	// Addon only supports "threat_model" and "threat" types
	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
		Objects:   []string{"threat_model", "threat"},
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)

	InvokeAddon(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Object type not supported by this add-on")
	mockAddonStore.AssertExpectations(t)
}

func TestInvokeAddon_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	userUUID := uuid.New()
	threatModelID := uuid.New()
	webhookID := uuid.New()

	reqBody := map[string]interface{}{
		"threat_model_id": threatModelID.String(),
		"object_type":     "threat_model",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("POST", "/addons/"+addonID.String()+"/invoke",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
		Objects:   []string{"threat_model", "threat"},
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)
	mockInvStore.On("Create", mock.Anything, mock.AnythingOfType("*api.AddonInvocation")).Return(nil)

	InvokeAddon(c)

	assert.Equal(t, http.StatusAccepted, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.NotEmpty(t, response["invocation_id"])
	assert.Equal(t, "pending", response["status"])

	mockAddonStore.AssertExpectations(t)
	mockInvStore.AssertExpectations(t)
}

func TestInvokeAddon_SuccessNoObjectType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	addonID := uuid.New()
	userUUID := uuid.New()
	threatModelID := uuid.New()
	webhookID := uuid.New()

	// Request without object_type - should succeed even if addon has Objects constraint
	reqBody := map[string]interface{}{
		"threat_model_id": threatModelID.String(),
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("POST", "/addons/"+addonID.String()+"/invoke",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: addonID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
		Objects:   []string{"threat_model"},
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)
	mockInvStore.On("Create", mock.Anything, mock.AnythingOfType("*api.AddonInvocation")).Return(nil)

	InvokeAddon(c)

	assert.Equal(t, http.StatusAccepted, w.Code)
	mockAddonStore.AssertExpectations(t)
	mockInvStore.AssertExpectations(t)
}

// =============================================================================
// GetInvocation Tests
// =============================================================================

func TestGetInvocation_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	c, w := CreateTestGinContext("GET", "/invocations/not-a-uuid")
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}

	GetInvocation(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid invocation ID format")
}

func TestGetInvocation_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	c, w := CreateTestGinContext("GET", "/invocations/"+invocationID.String())
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}
	// No user context set

	GetInvocation(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetInvocation_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	userUUID := uuid.New()

	c, w := CreateTestGinContext("GET", "/invocations/"+invocationID.String())
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	mockInvStore.On("Get", mock.Anything, invocationID).Return(nil, errors.New("not found"))

	GetInvocation(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Invocation not found")
	mockInvStore.AssertExpectations(t)
}

func TestGetInvocation_AccessDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()
	// GlobalGroupMemberStore is nil, so IsUserAdministrator returns false

	invocationID := uuid.New()
	userUUID := uuid.New()
	otherUserUUID := uuid.New()

	c, w := CreateTestGinContext("GET", "/invocations/"+invocationID.String())
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	invocation := &AddonInvocation{
		ID:            invocationID,
		AddonID:       uuid.New(),
		ThreatModelID: uuid.New(),
		InvokedByUUID: otherUserUUID, // Different user
		Status:        InvocationStatusPending,
		CreatedAt:     time.Now(),
	}
	mockInvStore.On("Get", mock.Anything, invocationID).Return(invocation, nil)

	GetInvocation(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "Access denied")
	mockInvStore.AssertExpectations(t)
}

func TestGetInvocation_SuccessOwnInvocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	userUUID := uuid.New()
	addonID := uuid.New()
	tmID := uuid.New()

	c, w := CreateTestGinContext("GET", "/invocations/"+invocationID.String())
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	invocation := &AddonInvocation{
		ID:              invocationID,
		AddonID:         addonID,
		ThreatModelID:   tmID,
		InvokedByUUID:   userUUID, // Same user
		InvokedByID:     "alice-provider-id",
		InvokedByEmail:  "alice@example.com",
		InvokedByName:   "Alice",
		Status:          InvocationStatusInProgress,
		StatusPercent:   50,
		StatusMessage:   "Processing...",
		CreatedAt:       time.Now(),
		StatusUpdatedAt: time.Now(),
	}
	mockInvStore.On("Get", mock.Anything, invocationID).Return(invocation, nil)

	GetInvocation(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, invocationID.String(), response["id"])
	assert.Equal(t, "in_progress", response["status"])
	assert.Equal(t, float64(50), response["status_percent"])

	mockInvStore.AssertExpectations(t)
}

// =============================================================================
// ListInvocations Tests
// =============================================================================

func TestListInvocations_Unauthenticated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	c, w := CreateTestGinContext("GET", "/invocations")
	// No user context set

	ListInvocations(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListInvocations_InvalidAddonIDQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	userUUID := uuid.New()
	c, w := CreateTestGinContext("GET", "/invocations?addon_id=not-a-uuid")
	c.Request.URL.RawQuery = "addon_id=not-a-uuid"
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	ListInvocations(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid addon_id format")
}

func TestListInvocations_SuccessWithInvocations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	userUUID := uuid.New()
	c, w := CreateTestGinContext("GET", "/invocations")
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	invocations := []AddonInvocation{
		{
			ID:              uuid.New(),
			AddonID:         uuid.New(),
			ThreatModelID:   uuid.New(),
			InvokedByUUID:   userUUID,
			InvokedByID:     "alice-provider-id",
			InvokedByEmail:  "alice@example.com",
			InvokedByName:   "Alice",
			Status:          InvocationStatusPending,
			CreatedAt:       time.Now(),
			StatusUpdatedAt: time.Now(),
		},
		{
			ID:              uuid.New(),
			AddonID:         uuid.New(),
			ThreatModelID:   uuid.New(),
			InvokedByUUID:   userUUID,
			InvokedByID:     "alice-provider-id",
			InvokedByEmail:  "alice@example.com",
			InvokedByName:   "Alice",
			Status:          InvocationStatusCompleted,
			CreatedAt:       time.Now(),
			StatusUpdatedAt: time.Now(),
		},
	}

	// Non-admin: filtered by userUUID
	mockInvStore.On("List", mock.Anything,
		mock.MatchedBy(func(uid *uuid.UUID) bool {
			return uid != nil && *uid == userUUID
		}),
		(*uuid.UUID)(nil), "", 50, 0,
	).Return(invocations, 2, nil)

	ListInvocations(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, float64(2), response["total"])
	assert.Equal(t, float64(50), response["limit"])
	assert.Equal(t, float64(0), response["offset"])

	items := response["invocations"].([]interface{})
	assert.Len(t, items, 2)

	mockInvStore.AssertExpectations(t)
}

func TestListInvocations_StoreError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	userUUID := uuid.New()
	c, w := CreateTestGinContext("GET", "/invocations")
	setAuthContext(c, "alice@example.com", "alice-provider-id", userUUID)

	mockInvStore.On("List", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return([]AddonInvocation{}, 0, errors.New("redis error"))

	ListInvocations(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to list invocations")
	mockInvStore.AssertExpectations(t)
}

// =============================================================================
// UpdateInvocationStatus Tests
// =============================================================================

func TestUpdateInvocationStatus_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	c, w := CreateTestGinContext("PATCH", "/invocations/not-a-uuid/status")
	c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid invocation ID format")
}

func TestUpdateInvocationStatus_InvalidRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", []byte(`{invalid`))
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid request body")
}

func TestUpdateInvocationStatus_InvalidStatusValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	reqBody := map[string]interface{}{
		"status": "invalid_status",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid status")
}

func TestUpdateInvocationStatus_InvalidStatusPercentOver100(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	percent := 101
	reqBody := map[string]interface{}{
		"status":         "in_progress",
		"status_percent": percent,
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Status percent must be between 0 and 100")
}

func TestUpdateInvocationStatus_InvalidStatusPercentNegative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	percent := -1
	reqBody := map[string]interface{}{
		"status":         "in_progress",
		"status_percent": percent,
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Status percent must be between 0 and 100")
}

func TestUpdateInvocationStatus_StatusMessageTooLong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	longMessage := strings.Repeat("a", 1025)
	reqBody := map[string]interface{}{
		"status":         "in_progress",
		"status_message": longMessage,
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Status message exceeds maximum length")
}

func TestUpdateInvocationStatus_InvocationNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	reqBody := map[string]interface{}{
		"status": "in_progress",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	mockInvStore.On("Get", mock.Anything, invocationID).Return(nil, errors.New("not found"))

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "Invocation not found")
	mockInvStore.AssertExpectations(t)
}

func TestUpdateInvocationStatus_CannotUpdateCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	webhookStore := newMockWebhookSubscriptionStore()
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, webhookStore)
	defer cleanup()

	invocationID := uuid.New()
	addonID := uuid.New()
	webhookID := uuid.New()

	reqBody := map[string]interface{}{
		"status": "in_progress",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	// Invocation is already completed
	invocation := &AddonInvocation{
		ID:              invocationID,
		AddonID:         addonID,
		ThreatModelID:   uuid.New(),
		InvokedByUUID:   uuid.New(),
		Status:          InvocationStatusCompleted,
		CreatedAt:       time.Now(),
		StatusUpdatedAt: time.Now(),
	}
	mockInvStore.On("Get", mock.Anything, invocationID).Return(invocation, nil)

	// The handler gets the addon to look up its webhook
	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)

	// The handler gets the webhook for HMAC verification
	webhookStore.subscriptions[webhookID.String()] = DBWebhookSubscription{
		Id:     webhookID,
		Secret: "", // No secret, skip HMAC
	}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "already completed or failed")
	mockInvStore.AssertExpectations(t)
	mockAddonStore.AssertExpectations(t)
}

func TestUpdateInvocationStatus_CannotUpdateFailed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	webhookStore := newMockWebhookSubscriptionStore()
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, webhookStore)
	defer cleanup()

	invocationID := uuid.New()
	addonID := uuid.New()
	webhookID := uuid.New()

	reqBody := map[string]interface{}{
		"status": "completed",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	invocation := &AddonInvocation{
		ID:              invocationID,
		AddonID:         addonID,
		ThreatModelID:   uuid.New(),
		InvokedByUUID:   uuid.New(),
		Status:          InvocationStatusFailed,
		CreatedAt:       time.Now(),
		StatusUpdatedAt: time.Now(),
	}
	mockInvStore.On("Get", mock.Anything, invocationID).Return(invocation, nil)

	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)

	webhookStore.subscriptions[webhookID.String()] = DBWebhookSubscription{
		Id:     webhookID,
		Secret: "",
	}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "already completed or failed")
	mockInvStore.AssertExpectations(t)
	mockAddonStore.AssertExpectations(t)
}

func TestUpdateInvocationStatus_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	webhookStore := newMockWebhookSubscriptionStore()
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, webhookStore)
	defer cleanup()

	invocationID := uuid.New()
	addonID := uuid.New()
	webhookID := uuid.New()

	percent := 75
	message := "Almost done"
	reqBody := map[string]interface{}{
		"status":         "in_progress",
		"status_percent": percent,
		"status_message": message,
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	invocation := &AddonInvocation{
		ID:              invocationID,
		AddonID:         addonID,
		ThreatModelID:   uuid.New(),
		InvokedByUUID:   uuid.New(),
		Status:          InvocationStatusPending,
		CreatedAt:       time.Now(),
		StatusUpdatedAt: time.Now(),
	}
	mockInvStore.On("Get", mock.Anything, invocationID).Return(invocation, nil)
	mockInvStore.On("Update", mock.Anything, mock.AnythingOfType("*api.AddonInvocation")).Return(nil)

	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)

	// Webhook with no secret (HMAC skipped)
	webhookStore.subscriptions[webhookID.String()] = DBWebhookSubscription{
		Id:     webhookID,
		Secret: "",
	}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, invocationID.String(), response["id"])
	assert.Equal(t, "in_progress", response["status"])
	assert.Equal(t, float64(75), response["status_percent"])

	mockInvStore.AssertExpectations(t)
	mockAddonStore.AssertExpectations(t)
}

func TestUpdateInvocationStatus_SuccessCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	webhookStore := newMockWebhookSubscriptionStore()
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, webhookStore)
	defer cleanup()

	invocationID := uuid.New()
	addonID := uuid.New()
	webhookID := uuid.New()

	percent := 100
	message := "Done"
	reqBody := map[string]interface{}{
		"status":         "completed",
		"status_percent": percent,
		"status_message": message,
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", "/invocations/"+invocationID.String()+"/status",
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	invocation := &AddonInvocation{
		ID:              invocationID,
		AddonID:         addonID,
		ThreatModelID:   uuid.New(),
		InvokedByUUID:   uuid.New(),
		Status:          InvocationStatusInProgress,
		CreatedAt:       time.Now(),
		StatusUpdatedAt: time.Now(),
	}
	mockInvStore.On("Get", mock.Anything, invocationID).Return(invocation, nil)
	mockInvStore.On("Update", mock.Anything, mock.AnythingOfType("*api.AddonInvocation")).Return(nil)

	addon := &Addon{
		ID:        addonID,
		Name:      "Test Addon",
		WebhookID: webhookID,
	}
	mockAddonStore.On("Get", mock.Anything, addonID).Return(addon, nil)

	webhookStore.subscriptions[webhookID.String()] = DBWebhookSubscription{
		Id:     webhookID,
		Secret: "",
	}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "completed", response["status"])

	mockInvStore.AssertExpectations(t)
	mockAddonStore.AssertExpectations(t)
}

func TestAddonInvocationHandlers_PendingStatusNotAllowedInUpdate(t *testing.T) {
	// "pending" is not a valid status for UpdateInvocationStatus
	gin.SetMode(gin.TestMode)
	mockAddonStore := &MockAddonStore{}
	mockInvStore := &MockAddonInvocationStore{}
	cleanup := saveAndClearInvocationGlobals(mockAddonStore, mockInvStore, nil)
	defer cleanup()

	invocationID := uuid.New()
	reqBody := map[string]interface{}{
		"status": "pending",
	}
	body, _ := json.Marshal(reqBody)

	c, w := CreateTestGinContextWithBody("PATCH", fmt.Sprintf("/invocations/%s/status", invocationID),
		"application/json", body)
	c.Params = gin.Params{{Key: "id", Value: invocationID.String()}}

	UpdateInvocationStatus(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid status")
}
