package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWebhookUrlDenyListStore is a test-only deny list store that always
// returns an empty deny list (all URLs are permitted).
type mockWebhookUrlDenyListStore struct{}

func (m *mockWebhookUrlDenyListStore) List(_ context.Context) ([]WebhookUrlDenyListEntry, error) {
	return []WebhookUrlDenyListEntry{}, nil
}

func (m *mockWebhookUrlDenyListStore) Create(_ context.Context, item WebhookUrlDenyListEntry) (WebhookUrlDenyListEntry, error) {
	return item, nil
}

func (m *mockWebhookUrlDenyListStore) Delete(_ context.Context, _ string) error {
	return nil
}

// TestEnsurePinnedAlertSubscription_Enabled verifies that enabling alerting
// creates a new active pinned subscription.
func TestEnsurePinnedAlertSubscription_Enabled(t *testing.T) {
	db := setupWebhookPinnedTestDB(t)
	store := NewGormWebhookSubscriptionStore(db)
	ctx := context.Background()
	denyList := &mockWebhookUrlDenyListStore{}

	cfg := AlertingBootstrap{
		Enabled: true,
		URL:     "https://alerts.example.com/hook",
		Secret:  "super-secret",
	}

	sub, err := EnsurePinnedAlertSubscription(ctx, store, denyList, cfg)
	require.NoError(t, err)
	assert.Equal(t, "active", sub.Status)
	assert.Equal(t, cfg.URL, sub.Url)
	assert.Equal(t, cfg.Secret, sub.Secret)
	assert.True(t, sub.OperatorPinned)
	assert.Contains(t, sub.Events, EventSystemAuditAdminWrite)

	// Verify exactly one subscription in the store.
	all := store.List(ctx, 0, 0, nil)
	assert.Len(t, all, 1)
}

// TestEnsurePinnedAlertSubscription_UpdateInPlace verifies that a second call
// with a different URL updates the existing row in-place (count stays 1).
func TestEnsurePinnedAlertSubscription_UpdateInPlace(t *testing.T) {
	db := setupWebhookPinnedTestDB(t)
	store := NewGormWebhookSubscriptionStore(db)
	ctx := context.Background()
	denyList := &mockWebhookUrlDenyListStore{}

	cfg1 := AlertingBootstrap{Enabled: true, URL: "https://alerts.example.com/hook", Secret: "s1"}
	_, err := EnsurePinnedAlertSubscription(ctx, store, denyList, cfg1)
	require.NoError(t, err)

	cfg2 := AlertingBootstrap{Enabled: true, URL: "https://alerts2.example.com/hook", Secret: "s2"}
	sub2, err := EnsurePinnedAlertSubscription(ctx, store, denyList, cfg2)
	require.NoError(t, err)
	assert.Equal(t, "active", sub2.Status)
	assert.Equal(t, cfg2.URL, sub2.Url)
	assert.Equal(t, cfg2.Secret, sub2.Secret)

	all := store.List(ctx, 0, 0, nil)
	assert.Len(t, all, 1, "second call must update in-place, not create a second row")
}

// TestEnsurePinnedAlertSubscription_Disabled verifies that disabling alerting
// deactivates the existing pinned subscription.
func TestEnsurePinnedAlertSubscription_Disabled(t *testing.T) {
	db := setupWebhookPinnedTestDB(t)
	store := NewGormWebhookSubscriptionStore(db)
	ctx := context.Background()
	denyList := &mockWebhookUrlDenyListStore{}

	// First enable to create the subscription.
	cfg1 := AlertingBootstrap{Enabled: true, URL: "https://alerts.example.com/hook", Secret: "s1"}
	_, err := EnsurePinnedAlertSubscription(ctx, store, denyList, cfg1)
	require.NoError(t, err)

	// Now disable.
	cfg2 := AlertingBootstrap{Enabled: false}
	sub, err := EnsurePinnedAlertSubscription(ctx, store, denyList, cfg2)
	require.NoError(t, err)
	assert.NotEqual(t, "active", sub.Status, "disabled subscription must not be active")

	// Re-read from store to confirm the status was persisted.
	all := store.List(ctx, 0, 0, func(s DBWebhookSubscription) bool {
		return s.OperatorPinned
	})
	require.Len(t, all, 1)
	assert.Equal(t, "inactive", all[0].Status)
}

// TestEnsurePinnedAlertSubscription_DisabledNoOp verifies that disabling when
// no pinned subscription exists is a no-op (no error, no rows created).
func TestEnsurePinnedAlertSubscription_DisabledNoOp(t *testing.T) {
	db := setupWebhookPinnedTestDB(t)
	store := NewGormWebhookSubscriptionStore(db)
	ctx := context.Background()

	cfg := AlertingBootstrap{Enabled: false}
	sub, err := EnsurePinnedAlertSubscription(ctx, store, nil, cfg)
	require.NoError(t, err)
	assert.Equal(t, DBWebhookSubscription{}, sub)

	all := store.List(ctx, 0, 0, nil)
	assert.Len(t, all, 0)
}

// TestEnsurePinnedAlertSubscription_EmptyURLError verifies that enabling
// without a URL returns an error.
func TestEnsurePinnedAlertSubscription_EmptyURLError(t *testing.T) {
	db := setupWebhookPinnedTestDB(t)
	store := NewGormWebhookSubscriptionStore(db)
	ctx := context.Background()

	cfg := AlertingBootstrap{Enabled: true, URL: ""}
	_, err := EnsurePinnedAlertSubscription(ctx, store, nil, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webhook_url is empty")
}
