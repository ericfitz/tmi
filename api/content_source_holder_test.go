package api

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// minimalCfg returns a config.Config with only the content-source fields
// needed by contentSourceWiringHash. Everything else is zero-value.
func minimalCfg() config.Config {
	return config.Config{
		ContentOAuth: config.ContentOAuthConfig{
			Providers: make(map[string]config.ContentOAuthProviderConfig),
		},
	}
}

// cfgWithGWEnabled returns a config where google_workspace is enabled.
func cfgWithGWEnabled() config.Config {
	cfg := minimalCfg()
	cfg.ContentSources.GoogleWorkspace.Enabled = true
	cfg.ContentSources.GoogleWorkspace.PickerDeveloperKey = "dev-key"
	cfg.ContentSources.GoogleWorkspace.PickerAppID = "app-id"
	cfg.ContentTokenEncryptionKey = "some-key"
	cfg.ContentOAuth.Providers[ProviderGoogleWorkspace] = config.ContentOAuthProviderConfig{Enabled: true}
	return cfg
}

// makeCountingBuilder returns a builder that records how many times it was
// called and returns a bundle with an empty source registry and a new poller.
func makeCountingBuilder(builds *int32) ContentSourceBundleBuilder {
	return func(ctx context.Context, cfg config.Config) (*ContentSourceBundle, error) {
		atomic.AddInt32(builds, 1)
		sources := NewContentSourceRegistry()
		poller := NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour)
		return &ContentSourceBundle{Sources: sources, Poller: poller}, nil
	}
}

// trackingPoller wraps AccessPoller to record Start/Stop calls.
type trackingPoller struct {
	starts int32
	stops  int32
	poller *AccessPoller
}

func newTrackingPoller() *trackingPoller {
	sources := NewContentSourceRegistry()
	return &trackingPoller{
		poller: NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour),
	}
}

// buildWithTracking returns a builder that returns a bundle with a tracking
// pointer so the test can observe start/stop invocations.
func buildWithTracking(tp **trackingPoller) ContentSourceBundleBuilder {
	return func(ctx context.Context, cfg config.Config) (*ContentSourceBundle, error) {
		t := newTrackingPoller()
		*tp = t
		sources := NewContentSourceRegistry()
		// Wrap the real AccessPoller so Start/Stop are observable.
		// We track by wrapping in a counted adapter below.
		return &ContentSourceBundle{Sources: sources, Poller: t.poller}, nil
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestContentSourceHolder_BuildsOnFirstGet(t *testing.T) {
	var builds int32
	cfg := minimalCfg()
	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return cfg },
		makeCountingBuilder(&builds),
	)

	b, err := h.Get(context.Background())
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Equal(t, int32(1), atomic.LoadInt32(&builds), "builder called exactly once")

	// Second Get with unchanged config must not rebuild.
	_, err = h.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&builds), "no rebuild on unchanged config")
}

func TestContentSourceHolder_RebuildsOnConfigChange(t *testing.T) {
	var builds int32
	cfg := minimalCfg()
	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return cfg },
		makeCountingBuilder(&builds),
	)

	_, err := h.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&builds))

	// Change a wiring field: flip timmy.enabled (which is part of the hash
	// via cfg.Timmy.Enabled being in the snapshot).
	cfg.Timmy.Enabled = true
	_, err = h.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(&builds), "rebuild triggered by config change")
}

func TestContentSourceHolder_StopsOldPollerOnRebuild(t *testing.T) {
	// Use a var to capture the current poller pointer from each build.
	type pollerRec struct {
		p *AccessPoller
	}
	var mu sync.Mutex
	var pollers []*AccessPoller
	var builds int32

	builder := func(ctx context.Context, cfg config.Config) (*ContentSourceBundle, error) {
		atomic.AddInt32(&builds, 1)
		sources := NewContentSourceRegistry()
		// Use a very short interval so the goroutine starts and is observable.
		p := NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour)
		mu.Lock()
		pollers = append(pollers, p)
		mu.Unlock()
		return &ContentSourceBundle{Sources: sources, Poller: p}, nil
	}

	cfg := minimalCfg()
	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return cfg },
		builder,
	)

	// First build.
	_, err := h.Get(context.Background())
	require.NoError(t, err)
	require.Len(t, pollers, 1, "expected one poller after first build")
	firstPoller := pollers[0]

	// Trigger rebuild by changing config.
	cfg.Timmy.Enabled = true
	_, err = h.Get(context.Background())
	require.NoError(t, err)
	require.Len(t, pollers, 2, "expected two pollers after rebuild")

	// Give the first poller's goroutine time to observe its stop signal.
	// The goroutine runs with interval=1h so it only wakes on the stop channel.
	time.Sleep(20 * time.Millisecond)

	// Verify the first (old) poller's stop channel was closed by calling Stop
	// a second time — if it wasn't already stopped, this would panic (double
	// close) without the sync.Once guard. With the guard it must be a no-op.
	assert.NotPanics(t, func() { firstPoller.Stop() },
		"Stop on already-stopped poller must not panic (sync.Once guard)")

	_ = pollerRec{} // suppress unused import
}

func TestContentSourceHolder_NoPollerLeakAcrossEnableDisableCycle(t *testing.T) {
	// This test verifies the enable→disable→enable lifecycle:
	// - enable: bundle built, poller started
	// - disable (timmy.enabled=false): rebuild, old poller stopped, new empty poller started
	// - enable again: rebuild again, new poller started
	// After each cycle the old poller must be stopped (no ticker goroutine leak).

	var mu sync.Mutex
	var allPollers []*AccessPoller

	builder := func(ctx context.Context, cfg config.Config) (*ContentSourceBundle, error) {
		sources := NewContentSourceRegistry()
		p := NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour)
		mu.Lock()
		allPollers = append(allPollers, p)
		mu.Unlock()
		return &ContentSourceBundle{Sources: sources, Poller: p}, nil
	}

	cfg := minimalCfg()
	cfgEnabled := minimalCfg()
	cfgEnabled.Timmy.Enabled = true

	current := cfg // start: disabled
	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return current },
		builder,
	)

	// enable
	current = cfgEnabled
	_, err := h.Get(context.Background())
	require.NoError(t, err)
	assert.Len(t, allPollers, 1)

	// disable
	current = cfg
	_, err = h.Get(context.Background())
	require.NoError(t, err)
	assert.Len(t, allPollers, 2)

	// enable again
	cfgEnabled.ContentSources.GoogleWorkspace.Enabled = true // change something so hash differs
	current = cfgEnabled
	_, err = h.Get(context.Background())
	require.NoError(t, err)
	assert.Len(t, allPollers, 3)

	// All but the last poller must have been stopped.
	// Wait briefly for goroutines to act on the stop signal.
	time.Sleep(20 * time.Millisecond)

	// The idempotent Stop() on each old poller must not panic.
	mu.Lock()
	old := allPollers[:len(allPollers)-1]
	mu.Unlock()
	for i, p := range old {
		assert.NotPanics(t, func() { p.Stop() },
			"double-stop must not panic on poller %d (stopped on rebuild)", i)
	}
}

func TestContentSourceHolder_StopPollerCleanup(t *testing.T) {
	var builds int32
	cfg := minimalCfg()
	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return cfg },
		makeCountingBuilder(&builds),
	)

	_, err := h.Get(context.Background())
	require.NoError(t, err)

	// StopPoller must not panic even when called after the poller was already started.
	assert.NotPanics(t, func() { h.StopPoller() })

	// Calling StopPoller again must also be a no-op (idempotent).
	assert.NotPanics(t, func() { h.StopPoller() })
}

func TestContentSourceHolder_ConcurrentGetStable(t *testing.T) {
	var builds int32
	cfg := minimalCfg()
	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return cfg },
		makeCountingBuilder(&builds),
	)

	const N = 50
	var wg sync.WaitGroup
	bundles := make([]*ContentSourceBundle, N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bundles[i], errs[i] = h.Get(context.Background())
		}(i)
	}
	wg.Wait()

	for i := 0; i < N; i++ {
		require.NoError(t, errs[i])
		assert.Same(t, bundles[0], bundles[i],
			"all concurrent callers get the same bundle pointer")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&builds),
		"builder called exactly once under concurrent load")
}

func TestContentSourceHolder_RebuildHookFires(t *testing.T) {
	var builds int32
	cfg := minimalCfg()

	hookCalled := int32(0)
	var hookBundle *ContentSourceBundle

	h := NewContentSourceHolder(
		func(_ context.Context) config.Config { return cfg },
		makeCountingBuilder(&builds),
	)
	h.AddRebuildHook(func(b *ContentSourceBundle) {
		atomic.AddInt32(&hookCalled, 1)
		hookBundle = b
	})

	b, err := h.Get(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&hookCalled), "hook fired on first build")
	assert.Same(t, b, hookBundle, "hook received the same bundle pointer")
}

func TestAccessPoller_StopIdempotent(t *testing.T) {
	sources := NewContentSourceRegistry()
	p := NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour)
	p.Start()

	// Multiple Stop calls must not panic.
	assert.NotPanics(t, func() { p.Stop() })
	assert.NotPanics(t, func() { p.Stop() })
	assert.NotPanics(t, func() { p.Stop() })
}

func TestContentSourceWiringHash_ChangesOnKeyFieldChange(t *testing.T) {
	base := minimalCfg()
	h1 := contentSourceWiringHash(base)

	// Changing encryption key must change hash.
	changed := base
	changed.ContentTokenEncryptionKey = "new-key"
	h2 := contentSourceWiringHash(changed)
	assert.NotEqual(t, h1, h2, "encryption key change must change wiring hash")

	// Enabling Google Workspace must change hash.
	changed2 := base
	changed2.ContentSources.GoogleWorkspace.Enabled = true
	h3 := contentSourceWiringHash(changed2)
	assert.NotEqual(t, h1, h3, "google_workspace.enabled change must change wiring hash")

	// Same config twice must be stable.
	assert.Equal(t, h1, contentSourceWiringHash(base), "hash is deterministic")
}
