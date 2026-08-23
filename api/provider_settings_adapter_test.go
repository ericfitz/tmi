package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check that adapter satisfies the auth interface
var _ auth.ProviderSettingsReader = (*ProviderSettingsReaderAdapter)(nil)

func TestProviderSettingsReaderAdapter(t *testing.T) {
	mock := NewMockSettingsService()
	mock.AddSetting("auth.oauth.providers.azure.client_id", "az-id", "string")
	mock.AddSetting("auth.oauth.providers.azure.enabled", "true", "bool")
	mock.AddSetting("websocket.max_participants", "10", "int")

	adapter := NewProviderSettingsReaderAdapter(mock)

	t.Run("returns matching settings", func(t *testing.T) {
		result, err := adapter.ListByPrefix(context.Background(), "auth.oauth.providers.")
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("excludes non-matching settings", func(t *testing.T) {
		result, err := adapter.ListByPrefix(context.Background(), "websocket.")
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, "websocket.max_participants", result[0].Key)
	})

	t.Run("returns empty for no matches", func(t *testing.T) {
		result, err := adapter.ListByPrefix(context.Background(), "nonexistent.")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}
