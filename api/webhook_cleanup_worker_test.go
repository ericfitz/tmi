package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalMockAddonStore implements AddonStore with only the methods needed for cleanup worker tests
type minimalMockAddonStore struct {
	addons       map[string]*Addon
	deleteErr    error
	deletedCount int
}

func newMinimalMockAddonStore() *minimalMockAddonStore {
	return &minimalMockAddonStore{
		addons: make(map[string]*Addon),
	}
}

func (m *minimalMockAddonStore) Create(_ context.Context, addon *Addon) error {
	m.addons[addon.ID.String()] = addon
	return nil
}

func (m *minimalMockAddonStore) Get(_ context.Context, id uuid.UUID) (*Addon, error) {
	if a, ok := m.addons[id.String()]; ok {
		return a, nil
	}
	return nil, nil
}

func (m *minimalMockAddonStore) List(_ context.Context, _, _ int, _ *uuid.UUID) ([]Addon, int, error) {
	return nil, 0, nil
}

func (m *minimalMockAddonStore) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.addons, id.String())
	return nil
}

func (m *minimalMockAddonStore) GetByWebhookID(_ context.Context, webhookID uuid.UUID) ([]Addon, error) {
	var result []Addon
	for _, a := range m.addons {
		if a.WebhookID == webhookID {
			result = append(result, *a)
		}
	}
	return result, nil
}

func (m *minimalMockAddonStore) CountActiveInvocations(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (m *minimalMockAddonStore) DeleteByWebhookID(_ context.Context, webhookID uuid.UUID) (int, error) {
	if m.deleteErr != nil {
		return 0, m.deleteErr
	}
	count := 0
	for id, a := range m.addons {
		if a.WebhookID == webhookID {
			delete(m.addons, id)
			count++
		}
	}
	m.deletedCount += count
	return count, nil
}

func TestDeletePendingSubscriptions_CascadeDeletesChildRecords(t *testing.T) {
	// Save and restore global stores
	origSubStore := GlobalWebhookSubscriptionStore
	origAddonStore := GlobalAddonStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalAddonStore = origAddonStore
	}()

	subStore := newMockWebhookSubscriptionStore()
	addonStore := newMinimalMockAddonStore()
	GlobalWebhookSubscriptionStore = subStore
	GlobalAddonStore = addonStore

	// Create a subscription in pending_delete status
	subID := uuid.New()
	sub := DBWebhookSubscription{
		Id:      subID,
		OwnerId: uuid.New(),
		Name:    "Test Webhook",
		Url:     "https://example.com/webhook",
		Events:  []string{"threat.created"},
		Status:  "pending_delete",
	}
	subStore.subscriptions[subID.String()] = sub

	// Create an addon referencing the subscription
	addonID := uuid.New()
	err := addonStore.Create(context.Background(), &Addon{
		ID:        addonID,
		WebhookID: subID,
		Name:      "Test Addon",
	})
	require.NoError(t, err)

	// Run the cleanup worker deletion
	worker := NewWebhookCleanupWorker()
	count, err := worker.deletePendingSubscriptions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count, "should have deleted 1 subscription")

	// Verify subscription is deleted
	_, getErr := subStore.Get(context.Background(), subID.String())
	assert.Error(t, getErr, "subscription should be deleted")

	// Verify addons are deleted
	assert.Empty(t, addonStore.addons, "all addons should be deleted")
}

func TestDeletePendingSubscriptions_SkipsOnAddonDeleteError(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origAddonStore := GlobalAddonStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalAddonStore = origAddonStore
	}()

	subStore := newMockWebhookSubscriptionStore()
	addonStore := newMinimalMockAddonStore()
	addonStore.deleteErr = assert.AnError // Force addon deletion to fail
	GlobalWebhookSubscriptionStore = subStore
	GlobalAddonStore = addonStore

	subID := uuid.New()
	subStore.subscriptions[subID.String()] = DBWebhookSubscription{
		Id:      subID,
		OwnerId: uuid.New(),
		Name:    "Test Webhook",
		Url:     "https://example.com/webhook",
		Events:  []string{"threat.created"},
		Status:  "pending_delete",
	}

	worker := NewWebhookCleanupWorker()
	count, err := worker.deletePendingSubscriptions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count, "should not have deleted subscription when addon delete fails")

	// Subscription should still exist
	_, getErr := subStore.Get(context.Background(), subID.String())
	assert.NoError(t, getErr, "subscription should still exist")
}

func TestDeletePendingSubscriptions_WorksWithNilStores(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origAddonStore := GlobalAddonStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalAddonStore = origAddonStore
	}()

	subStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = subStore
	GlobalAddonStore = nil

	subID := uuid.New()
	subStore.subscriptions[subID.String()] = DBWebhookSubscription{
		Id:      subID,
		OwnerId: uuid.New(),
		Name:    "Test Webhook",
		Url:     "https://example.com/webhook",
		Events:  []string{"threat.created"},
		Status:  "pending_delete",
	}

	worker := NewWebhookCleanupWorker()
	count, err := worker.deletePendingSubscriptions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count, "should delete subscription even with nil addon store")

	_, getErr := subStore.Get(context.Background(), subID.String())
	assert.Error(t, getErr, "subscription should be deleted")
}

func TestMarkIdleSubscriptions_SkipsPinned(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	defer func() { GlobalWebhookSubscriptionStore = origSubStore }()

	subStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = subStore

	// Pinned subscription — should survive
	pinnedID := uuid.New()
	subStore.subscriptions[pinnedID.String()] = DBWebhookSubscription{
		Id:             pinnedID,
		OwnerId:        uuid.New(),
		Name:           "Pinned Alert Sink",
		Url:            "https://alert.example.com/hook",
		Events:         []string{"system_audit.admin_write"},
		Status:         string(WebhookSubscriptionStatusActive),
		OperatorPinned: true,
	}

	// Non-pinned subscription — should be marked
	normalID := uuid.New()
	subStore.subscriptions[normalID.String()] = DBWebhookSubscription{
		Id:             normalID,
		OwnerId:        uuid.New(),
		Name:           "Normal Webhook",
		Url:            "https://example.com/hook",
		Events:         []string{"threat.created"},
		Status:         string(WebhookSubscriptionStatusActive),
		OperatorPinned: false,
	}

	worker := NewWebhookCleanupWorker()
	// markIdleSubscriptions calls ListIdle — the mock now excludes pinned rows
	count, err := worker.markIdleSubscriptions(context.Background(), 0) // 0 days = everything is idle
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only one (non-pinned) subscription should be marked")

	// Pinned still active
	pinned, err := subStore.Get(context.Background(), pinnedID.String())
	require.NoError(t, err)
	assert.Equal(t, string(WebhookSubscriptionStatusActive), pinned.Status, "pinned subscription must stay active")

	// Normal subscription is now pending_delete
	normal, err := subStore.Get(context.Background(), normalID.String())
	require.NoError(t, err)
	assert.Equal(t, "pending_delete", normal.Status)
}

func TestDeletePendingSubscriptions_SkipsPinnedRows(t *testing.T) {
	origSubStore := GlobalWebhookSubscriptionStore
	origAddonStore := GlobalAddonStore
	defer func() {
		GlobalWebhookSubscriptionStore = origSubStore
		GlobalAddonStore = origAddonStore
	}()

	subStore := newMockWebhookSubscriptionStore()
	GlobalWebhookSubscriptionStore = subStore
	GlobalAddonStore = nil

	// A pinned row that somehow has pending_delete status (should not happen but test the guard)
	pinnedID := uuid.New()
	subStore.subscriptions[pinnedID.String()] = DBWebhookSubscription{
		Id:             pinnedID,
		OwnerId:        uuid.New(),
		Name:           "Pinned Alert Sink",
		Url:            "https://alert.example.com/hook",
		Events:         []string{"system_audit.admin_write"},
		Status:         "pending_delete",
		OperatorPinned: true,
	}

	// A normal pending_delete row
	normalID := uuid.New()
	subStore.subscriptions[normalID.String()] = DBWebhookSubscription{
		Id:             normalID,
		OwnerId:        uuid.New(),
		Name:           "Normal Webhook",
		Url:            "https://example.com/hook",
		Events:         []string{"threat.created"},
		Status:         "pending_delete",
		OperatorPinned: false,
	}

	worker := NewWebhookCleanupWorker()
	count, err := worker.deletePendingSubscriptions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only non-pinned subscription should be deleted")

	// Pinned row must still exist
	_, getErr := subStore.Get(context.Background(), pinnedID.String())
	assert.NoError(t, getErr, "pinned subscription must not be deleted")

	// Normal row must be gone
	_, getErr = subStore.Get(context.Background(), normalID.String())
	assert.Error(t, getErr, "normal pending_delete subscription must be deleted")
}
