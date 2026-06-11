package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Handler guard tests for operator-pinned webhook subscriptions (#395)
// =============================================================================

// TestWebhookPinnedDelete_403 verifies that DELETE on a pinned subscription
// returns 403 Forbidden.
func TestWebhookPinnedDelete_403(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberRepository = origAdminStore
	}()

	mockSubStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = mockSubStore
	GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

	ownerUUID := uuid.New()
	// Create a pinned subscription
	sub, err := mockSubStore.Create(context.Background(), DBWebhookSubscription{
		OwnerId:        ownerUUID,
		Name:           "Pinned Alert Sink",
		Url:            "https://internal.example.com/alert",
		Events:         []string{EventSystemAuditAdminWrite},
		Status:         "active",
		OperatorPinned: true,
	}, nil)
	require.NoError(t, err)

	adminUUID := uuid.New()
	r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

	req, _ := http.NewRequest("DELETE", "/admin/webhooks/subscriptions/"+sub.Id.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)

	var errResp Error
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Contains(t, errResp.Error, "operator-pinned")

	// Verify the subscription was NOT deleted
	_, getErr := mockSubStore.Get(context.Background(), sub.Id.String())
	assert.NoError(t, getErr, "pinned subscription must remain after a 403 DELETE")
}

// TestWebhookPinnedDelete_NonPinned_OK verifies that DELETE on a non-pinned
// subscription still returns 204 (normal path unaffected by the guard).
func TestWebhookPinnedDelete_NonPinned_OK(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberRepository = origAdminStore
	}()

	mockSubStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = mockSubStore
	GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

	ownerUUID := uuid.New()
	// Create a regular (non-pinned) subscription
	sub, err := mockSubStore.Create(context.Background(), DBWebhookSubscription{
		OwnerId:        ownerUUID,
		Name:           "Normal Webhook",
		Url:            "https://example.com/webhook",
		Events:         []string{"threat.created"},
		Status:         "active",
		OperatorPinned: false,
	}, nil)
	require.NoError(t, err)

	adminUUID := uuid.New()
	r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

	req, _ := http.NewRequest("DELETE", "/admin/webhooks/subscriptions/"+sub.Id.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify it was actually deleted
	_, getErr := mockSubStore.Get(context.Background(), sub.Id.String())
	assert.Error(t, getErr, "non-pinned subscription should have been deleted")
}

// TestWebhookPinnedList_URLRedacted verifies that GET /admin/webhooks/subscriptions
// returns pinned rows with a redacted URL while leaving non-pinned URLs intact.
func TestWebhookPinnedList_URLRedacted(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberRepository = origAdminStore
	}()

	mockSubStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = mockSubStore
	GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

	ownerUUID := uuid.New()

	// Create one pinned and one non-pinned subscription
	_, err := mockSubStore.Create(context.Background(), DBWebhookSubscription{
		OwnerId:        ownerUUID,
		Name:           "Pinned Alert Sink",
		Url:            "https://internal.example.com/alert",
		Events:         []string{EventSystemAuditAdminWrite},
		Status:         "active",
		OperatorPinned: true,
	}, nil)
	require.NoError(t, err)

	_, err = mockSubStore.Create(context.Background(), DBWebhookSubscription{
		OwnerId:        ownerUUID,
		Name:           "Regular Webhook",
		Url:            "https://example.com/webhook",
		Events:         []string{"threat.created"},
		Status:         "active",
		OperatorPinned: false,
	}, nil)
	require.NoError(t, err)

	adminUUID := uuid.New()
	r, _ := setupWebhookRouter("admin@example.com", adminUUID.String(), true)

	req, _ := http.NewRequest("GET", "/admin/webhooks/subscriptions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response ListWebhookSubscriptionsResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Subscriptions, 2)

	pinnedCount := 0
	normalCount := 0
	for _, sub := range response.Subscriptions {
		switch sub.Name {
		case "Pinned Alert Sink":
			assert.Equal(t, "(operator-pinned)", sub.Url, "pinned subscription URL must be redacted")
			pinnedCount++
		case "Regular Webhook":
			assert.Equal(t, "https://example.com/webhook", sub.Url, "non-pinned subscription URL must not be redacted")
			normalCount++
		}
	}
	assert.Equal(t, 1, pinnedCount, "expected exactly one pinned subscription in list")
	assert.Equal(t, 1, normalCount, "expected exactly one non-pinned subscription in list")
}

// TestWebhookPinnedGet_URLRedacted verifies that GET /admin/webhooks/subscriptions/{id}
// for a pinned row returns a redacted URL.
func TestWebhookPinnedGet_URLRedacted(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberRepository
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberRepository = origAdminStore
	}()

	mockSubStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = mockSubStore
	GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

	ownerUUID := uuid.New()
	sub, err := mockSubStore.Create(context.Background(), DBWebhookSubscription{
		OwnerId:        ownerUUID,
		Name:           "Pinned Alert Sink",
		Url:            "https://internal.example.com/alert",
		Events:         []string{EventSystemAuditAdminWrite},
		Status:         "active",
		OperatorPinned: true,
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
	assert.Equal(t, "(operator-pinned)", response.Url, "pinned subscription URL must be redacted in GET response")
}

// TestWebhookPinnedCreate_SystemAuditEventAllowed verifies that POST
// /admin/webhooks/subscriptions with event type system_audit.admin_write succeeds (201).
func TestWebhookPinnedCreate_SystemAuditEventAllowed(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origQuotaStore := GlobalWebhookQuotaStore
	origAdminStore := GlobalGroupMemberRepository
	origDenyListStore := GlobalWebhookUrlDenyListStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalWebhookQuotaStore = origQuotaStore
		GlobalGroupMemberRepository = origAdminStore
		GlobalWebhookUrlDenyListStore = origDenyListStore
	}()

	mockSubStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = mockSubStore
	GlobalWebhookQuotaStore = newMockWebhookQuotaStore()
	GlobalWebhookUrlDenyListStore = &mockDenyListStore{entries: []WebhookUrlDenyListEntry{}}

	userUUID := uuid.New()
	r, _ := setupWebhookRouter("admin@example.com", userUUID.String(), true)

	reqBody := map[string]any{
		"name":   "Audit Alert Sink",
		"url":    "https://siem.example.com/audit-alerts",
		"events": []string{EventSystemAuditAdminWrite},
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, "creating a subscription for system_audit.admin_write must succeed")

	var response WebhookSubscription
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "Audit Alert Sink", response.Name)
	assert.Equal(t, "https://siem.example.com/audit-alerts", response.Url)
}
