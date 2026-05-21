package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmyCore_Get_StableWhenHashUnchanged(t *testing.T) {
	ms := NewMockSettingsService()
	ms.AddSetting("timmy.llm_provider", "openai", "string")
	ms.AddSetting("timmy.llm_model", "gpt-5.5", "string")
	ms.AddSetting("timmy.llm_api_key", "sk-a", "string")
	ms.AddSetting("timmy.text_embedding_provider", "openai", "string")
	ms.AddSetting("timmy.text_embedding_model", "text-embedding-3-large", "string")

	provider := NewTimmyConfigProvider(ms)
	var builds int32
	builder := func(ctx context.Context, cfg config.TimmyConfig) (*TimmyRuntime, error) {
		atomic.AddInt32(&builds, 1)
		return &TimmyRuntime{}, nil
	}
	core := NewTimmyCore(provider, builder)

	rt1, err := core.Get(context.Background())
	require.NoError(t, err)
	rt2, err := core.Get(context.Background())
	require.NoError(t, err)
	assert.Same(t, rt1, rt2, "same runtime returned when wiring unchanged")
	assert.Equal(t, int32(1), atomic.LoadInt32(&builds), "built exactly once")
}

func TestTimmyCore_Get_RebuildsWhenWiringChanges(t *testing.T) {
	ms := NewMockSettingsService()
	ms.AddSetting("timmy.llm_provider", "openai", "string")
	ms.AddSetting("timmy.llm_model", "gpt-5.5", "string")
	ms.AddSetting("timmy.llm_api_key", "sk-a", "string")
	ms.AddSetting("timmy.text_embedding_provider", "openai", "string")
	ms.AddSetting("timmy.text_embedding_model", "text-embedding-3-large", "string")

	provider := NewTimmyConfigProvider(ms)
	var builds int32
	builder := func(ctx context.Context, cfg config.TimmyConfig) (*TimmyRuntime, error) {
		atomic.AddInt32(&builds, 1)
		return &TimmyRuntime{}, nil
	}
	core := NewTimmyCore(provider, builder)

	_, err := core.Get(context.Background())
	require.NoError(t, err)
	ms.AddSetting("timmy.llm_api_key", "sk-b", "string") // rotate key
	_, err = core.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&builds), "rebuilt after key rotation")
}

func TestTimmyCore_Get_RetriesAfterBuildError(t *testing.T) {
	ms := NewMockSettingsService()
	ms.AddSetting("timmy.llm_provider", "openai", "string")
	ms.AddSetting("timmy.llm_model", "gpt-5.5", "string")
	ms.AddSetting("timmy.llm_api_key", "sk-a", "string")
	ms.AddSetting("timmy.text_embedding_provider", "openai", "string")
	ms.AddSetting("timmy.text_embedding_model", "text-embedding-3-large", "string")

	provider := NewTimmyConfigProvider(ms)
	var builds int32
	builder := func(ctx context.Context, cfg config.TimmyConfig) (*TimmyRuntime, error) {
		n := atomic.AddInt32(&builds, 1)
		if n == 1 {
			return nil, errors.New("bad key")
		}
		return &TimmyRuntime{}, nil
	}
	core := NewTimmyCore(provider, builder)

	_, err := core.Get(context.Background())
	require.Error(t, err, "first build fails")
	rt, err := core.Get(context.Background())
	require.NoError(t, err, "second build succeeds — error did not poison cache")
	require.NotNil(t, rt)
	assert.Equal(t, int32(2), atomic.LoadInt32(&builds))
}
