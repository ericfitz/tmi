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
	count, err := worker.deletePendingSubscriptions()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "should have deleted 1 subscription")

	// Verify subscription is deleted
	_, getErr := subStore.Get(subID.String())
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
	count, err := worker.deletePendingSubscriptions()
	require.NoError(t, err)
	assert.Equal(t, 0, count, "should not have deleted subscription when addon delete fails")

	// Subscription should still exist
	_, getErr := subStore.Get(subID.String())
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
	count, err := worker.deletePendingSubscriptions()
	require.NoError(t, err)
	assert.Equal(t, 1, count, "should delete subscription even with nil addon store")

	_, getErr := subStore.Get(subID.String())
	assert.Error(t, getErr, "subscription should be deleted")
}
