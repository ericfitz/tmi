package api

import (
	"context"
	"errors"
	"sync"
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

	rt1, err := core.Get(context.Background())
	require.NoError(t, err)
	ms.AddSetting("timmy.llm_api_key", "sk-b", "string") // rotate key
	rt2, err := core.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&builds), "rebuilt after key rotation")
	assert.NotSame(t, rt1, rt2, "rebuild yields a new runtime")
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

func TestTimmyCore_Get_ConcurrentStable(t *testing.T) {
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

	const N = 50
	var wg sync.WaitGroup
	results := make([]*TimmyRuntime, N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = core.Get(context.Background())
		}(i)
	}
	wg.Wait()
	for i := 0; i < N; i++ {
		require.NoError(t, errs[i])
		assert.Same(t, results[0], results[i], "all concurrent callers get the same runtime")
	}
	// The double-check inside the write lock means only the FIRST writer builds
	// and the rest short-circuit, so the build runs exactly once even under the
	// initial race.
	assert.Equal(t, int32(1), atomic.LoadInt32(&builds), "no per-call rebuild")
}

func TestTimmyCore_Get_StopsOldVectorManagerOnRebuild(t *testing.T) {
	ms := NewMockSettingsService()
	ms.AddSetting("timmy.llm_provider", "openai", "string")
	ms.AddSetting("timmy.llm_model", "gpt-5.5", "string")
	ms.AddSetting("timmy.llm_api_key", "sk-a", "string")
	ms.AddSetting("timmy.text_embedding_provider", "openai", "string")
	ms.AddSetting("timmy.text_embedding_model", "text-embedding-3-large", "string")

	provider := NewTimmyConfigProvider(ms)
	var managers []*VectorIndexManager
	builder := func(ctx context.Context, cfg config.TimmyConfig) (*TimmyRuntime, error) {
		vm := NewVectorIndexManager(nil, 64, 300)
		managers = append(managers, vm)
		return &TimmyRuntime{VectorManager: vm}, nil
	}
	core := NewTimmyCore(provider, builder)

	_, err := core.Get(context.Background())
	require.NoError(t, err)
	ms.AddSetting("timmy.llm_api_key", "sk-b", "string") // force rebuild
	_, err = core.Get(context.Background())
	require.NoError(t, err)

	require.Len(t, managers, 2)
	// The first (discarded) manager must have been stopped on rebuild so its
	// eviction goroutine and ticker are released; the new one must still run.
	assert.True(t, managers[0].IsStopped(), "old vector manager stopped on rebuild")
	assert.False(t, managers[1].IsStopped(), "new vector manager still running")
}
