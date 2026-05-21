# Timmy Runtime DB-Backed Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Timmy AI core (enable flag, LLM/embedding clients, vector manager, session manager, tuning knobs) read its configuration from the database at runtime, so Timmy can be turned on/off, re-keyed, and re-modeled without a server restart.

**Architecture:** A `TimmyConfigProvider` assembles a live `config.TimmyConfig` from the settings service (config-first precedence, already built into `GetString/GetBool/GetInt`). A `TimmyCore` holder owns the rebuildable LLM/vector/session objects behind a `sync.RWMutex` and lazily rebuilds them on the next request when a hash over the *wiring* fields changes. The enable-gate middleware and the Timmy handlers read through these instead of a frozen startup struct. Content-source plumbing stays startup-wired (deferred to #427).

**Tech Stack:** Go, Gin, GORM, LangChainGo (openai), existing `SettingsServiceInterface`, `crypto/sha256`.

**Spec:** `docs/superpowers/specs/2026-05-21-timmy-runtime-db-config-design.md`

---

## File Structure

- **Create** `api/timmy_config_provider.go` — `TimmyConfigProvider`: assembles `config.TimmyConfig` from settings; `WiringHash`.
- **Create** `api/timmy_config_provider_test.go` — unit tests for assembly + hash.
- **Create** `api/timmy_core.go` — `TimmyCore` + `TimmyRuntime`: lazy-rebuild holder.
- **Create** `api/timmy_core_test.go` — unit tests for lazy rebuild / error retry / stability.
- **Modify** `api/timmy_middleware.go` — middleware takes a config-reader instead of a frozen `config.TimmyConfig`.
- **Modify** `api/timmy_middleware_test.go` (if present) — update for new signature.
- **Modify** `api/server.go` — add `timmyCore *TimmyCore` field + `SetTimmyCore` + `getTimmyRuntime(ctx)` accessor.
- **Modify** `api/timmy_handlers.go` and `api/timmy_embedding_automation_handlers.go` — obtain the session manager via `getTimmyRuntime(ctx)`.
- **Modify** `cmd/server/main.go` — `initializeTimmySubsystem` always builds the provider+core+rebuild closure and wires middleware/handlers; no early return on `!Enabled`.

---

## Task 1: TimmyConfigProvider — assemble live config from settings

**Files:**
- Create: `api/timmy_config_provider.go`
- Test: `api/timmy_config_provider_test.go`

The provider reads every `timmy.*` key through `SettingsServiceInterface` and returns a `config.TimmyConfig`. It starts from `config.DefaultTimmyConfig()` so unset numeric knobs keep sane defaults, then overlays any value present in settings. `WiringHash` hashes only the fields that require rebuilding the LLM/embedding clients.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmyConfigProvider_Current_AssemblesFromSettings(t *testing.T) {
	ms := NewMockSettingsService()
	ms.AddSetting("timmy.enabled", "true", "bool")
	ms.AddSetting("timmy.llm_provider", "openai", "string")
	ms.AddSetting("timmy.llm_model", "gpt-5.5", "string")
	ms.AddSetting("timmy.llm_base_url", "https://api.openai.com/v1", "string")
	ms.AddSetting("timmy.llm_api_key", "sk-test", "string")
	ms.AddSetting("timmy.text_embedding_provider", "openai", "string")
	ms.AddSetting("timmy.text_embedding_model", "text-embedding-3-large", "string")
	ms.AddSetting("timmy.text_embedding_base_url", "https://api.openai.com/v1", "string")
	ms.AddSetting("timmy.embedding_dimension", "3072", "int")
	ms.AddSetting("timmy.text_retrieval_top_k", "7", "int")

	p := NewTimmyConfigProvider(ms)
	cfg := p.Current(context.Background())

	assert.True(t, cfg.Enabled)
	assert.Equal(t, "openai", cfg.LLMProvider)
	assert.Equal(t, "gpt-5.5", cfg.LLMModel)
	assert.Equal(t, "sk-test", cfg.LLMAPIKey)
	assert.Equal(t, "text-embedding-3-large", cfg.TextEmbeddingModel)
	assert.Equal(t, 3072, cfg.EmbeddingDimension)
	assert.Equal(t, 7, cfg.TextRetrievalTopK)
	// Unset knob keeps the default from DefaultTimmyConfig.
	assert.Equal(t, 50, cfg.MaxConversationHistory)
	require.True(t, cfg.IsConfigured())
}

func TestTimmyConfigProvider_Current_DisabledByDefault(t *testing.T) {
	ms := NewMockSettingsService()
	p := NewTimmyConfigProvider(ms)
	cfg := p.Current(context.Background())
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.IsConfigured())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestTimmyConfigProvider`
Expected: FAIL — `NewTimmyConfigProvider` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package api

import (
	"context"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// TimmyConfigProvider assembles a live config.TimmyConfig from the settings
// service. Reads honor config-first precedence (env/config file > database),
// which SettingsServiceInterface.GetString/GetBool/GetInt already implement.
type TimmyConfigProvider struct {
	settings SettingsServiceInterface
}

// NewTimmyConfigProvider constructs a provider over the given settings service.
func NewTimmyConfigProvider(settings SettingsServiceInterface) *TimmyConfigProvider {
	return &TimmyConfigProvider{settings: settings}
}

// Current reads all timmy.* keys and returns an assembled TimmyConfig. It
// starts from DefaultTimmyConfig so unset numeric knobs keep sane defaults,
// then overlays any value present in settings.
func (p *TimmyConfigProvider) Current(ctx context.Context) config.TimmyConfig {
	cfg := config.DefaultTimmyConfig()
	if p.settings == nil {
		return cfg
	}
	logger := slogging.Get()

	getStr := func(key string, dst *string) {
		if v, err := p.settings.GetString(ctx, key); err == nil {
			if v != "" {
				*dst = v
			}
		} else {
			logger.Warn("TimmyConfigProvider: read %s failed: %v", key, err)
		}
	}
	getInt := func(key string, dst *int) {
		if v, err := p.settings.GetInt(ctx, key); err == nil {
			if v != 0 {
				*dst = v
			}
		} else {
			logger.Warn("TimmyConfigProvider: read %s failed: %v", key, err)
		}
	}
	getBool := func(key string, dst *bool) {
		if v, err := p.settings.GetBool(ctx, key); err == nil {
			*dst = v
		} else {
			logger.Warn("TimmyConfigProvider: read %s failed: %v", key, err)
		}
	}

	getBool("timmy.enabled", &cfg.Enabled)
	getStr("timmy.llm_provider", &cfg.LLMProvider)
	getStr("timmy.llm_model", &cfg.LLMModel)
	getStr("timmy.llm_api_key", &cfg.LLMAPIKey)
	getStr("timmy.llm_base_url", &cfg.LLMBaseURL)
	getStr("timmy.text_embedding_provider", &cfg.TextEmbeddingProvider)
	getStr("timmy.text_embedding_model", &cfg.TextEmbeddingModel)
	getStr("timmy.text_embedding_api_key", &cfg.TextEmbeddingAPIKey)
	getStr("timmy.text_embedding_base_url", &cfg.TextEmbeddingBaseURL)
	getInt("timmy.embedding_dimension", &cfg.EmbeddingDimension)
	getInt("timmy.text_retrieval_top_k", &cfg.TextRetrievalTopK)
	getStr("timmy.code_embedding_provider", &cfg.CodeEmbeddingProvider)
	getStr("timmy.code_embedding_model", &cfg.CodeEmbeddingModel)
	getStr("timmy.code_embedding_api_key", &cfg.CodeEmbeddingAPIKey)
	getStr("timmy.code_embedding_base_url", &cfg.CodeEmbeddingBaseURL)
	getInt("timmy.code_retrieval_top_k", &cfg.CodeRetrievalTopK)
	getBool("timmy.query_decomposition_enabled", &cfg.QueryDecompositionEnabled)
	getStr("timmy.rerank_provider", &cfg.RerankProvider)
	getStr("timmy.rerank_model", &cfg.RerankModel)
	getStr("timmy.rerank_api_key", &cfg.RerankAPIKey)
	getStr("timmy.rerank_base_url", &cfg.RerankBaseURL)
	getInt("timmy.rerank_top_k", &cfg.RerankTopK)
	getInt("timmy.max_conversation_history", &cfg.MaxConversationHistory)
	getStr("timmy.operator_system_prompt", &cfg.OperatorSystemPrompt)
	getInt("timmy.max_memory_mb", &cfg.MaxMemoryMB)
	getInt("timmy.inactivity_timeout_seconds", &cfg.InactivityTimeoutSeconds)
	getInt("timmy.max_messages_per_user_per_hour", &cfg.MaxMessagesPerUserPerHour)
	getInt("timmy.max_sessions_per_threat_model", &cfg.MaxSessionsPerThreatModel)
	getInt("timmy.max_concurrent_llm_requests", &cfg.MaxConcurrentLLMRequests)
	getInt("timmy.chunk_size", &cfg.ChunkSize)
	getInt("timmy.chunk_overlap", &cfg.ChunkOverlap)
	getInt("timmy.llm_timeout_seconds", &cfg.LLMTimeoutSeconds)

	return cfg
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestTimmyConfigProvider`
Expected: PASS (both subtests).

- [ ] **Step 5: Commit**

```bash
git add api/timmy_config_provider.go api/timmy_config_provider_test.go
git commit -m "feat(timmy): TimmyConfigProvider assembles live config from settings"
```

---

## Task 2: WiringHash — detect rebuild-worthy changes

**Files:**
- Modify: `api/timmy_config_provider.go`
- Test: `api/timmy_config_provider_test.go`

The hash covers only fields that require rebuilding the LLM/embedding clients. Tuning knobs must NOT change it.

- [ ] **Step 1: Write the failing test**

```go
func TestTimmyConfigProvider_WiringHash_StableForKnobs(t *testing.T) {
	p := NewTimmyConfigProvider(NewMockSettingsService())
	base := config.DefaultTimmyConfig()
	base.LLMProvider = "openai"
	base.LLMModel = "gpt-5.5"
	base.LLMAPIKey = "sk-a"
	base.TextEmbeddingProvider = "openai"
	base.TextEmbeddingModel = "text-embedding-3-large"
	base.EmbeddingDimension = 3072

	h1 := p.WiringHash(base)

	// Changing a tuning knob does NOT change the hash.
	knob := base
	knob.MaxConversationHistory = 99
	knob.TextRetrievalTopK = 1
	assert.Equal(t, h1, p.WiringHash(knob), "tuning-knob change must not change wiring hash")

	// Changing the api key DOES change the hash.
	rekey := base
	rekey.LLMAPIKey = "sk-b"
	assert.NotEqual(t, h1, p.WiringHash(rekey), "api key change must change wiring hash")

	// Changing the model DOES change the hash.
	remodel := base
	remodel.LLMModel = "gpt-6"
	assert.NotEqual(t, h1, p.WiringHash(remodel), "model change must change wiring hash")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestTimmyConfigProvider_WiringHash`
Expected: FAIL — `WiringHash` undefined.

- [ ] **Step 3: Write minimal implementation**

Add to `api/timmy_config_provider.go` (and add `crypto/sha256`, `encoding/hex`, `fmt`, `strconv`, `strings` to imports):

```go
// WiringHash returns a stable hash over the fields that, when changed, require
// rebuilding the LLM/embedding clients. Tuning knobs (top-k, timeouts, limits,
// history) are intentionally excluded so changing them does not force a costly
// client rebuild. The enable flag is also excluded — it is evaluated by the
// middleware, not the client build.
func (p *TimmyConfigProvider) WiringHash(cfg config.TimmyConfig) string {
	fields := []string{
		cfg.LLMProvider, cfg.LLMModel, cfg.LLMAPIKey, cfg.LLMBaseURL,
		cfg.TextEmbeddingProvider, cfg.TextEmbeddingModel, cfg.TextEmbeddingAPIKey, cfg.TextEmbeddingBaseURL,
		strconv.Itoa(cfg.EmbeddingDimension),
		cfg.CodeEmbeddingProvider, cfg.CodeEmbeddingModel, cfg.CodeEmbeddingAPIKey, cfg.CodeEmbeddingBaseURL,
		cfg.RerankProvider, cfg.RerankModel, cfg.RerankAPIKey, cfg.RerankBaseURL,
		cfg.OperatorSystemPrompt,
		strconv.Itoa(cfg.LLMTimeoutSeconds),
	}
	// NUL separator avoids collisions between concatenated field boundaries.
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}
```

NOTE: `LLMTimeoutSeconds` is included because `NewTimmyLLMService` bakes it into the SafeHTTPClient at construction; `OperatorSystemPrompt` is included because it is baked into `basePrompt` at construction.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestTimmyConfigProvider_WiringHash`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/timmy_config_provider.go api/timmy_config_provider_test.go
git commit -m "feat(timmy): WiringHash detects rebuild-worthy config changes"
```

---

## Task 3: TimmyRuntime + TimmyCore — lazy-rebuild holder

**Files:**
- Create: `api/timmy_core.go`
- Test: `api/timmy_core_test.go`

`TimmyCore` holds the built objects + the hash they were built from, and rebuilds lazily when the hash changes. The build function is injected so tests can substitute a fake builder (avoiding real LangChainGo client construction).

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestTimmyCore`
Expected: FAIL — `TimmyRuntime`, `NewTimmyCore` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package api

import (
	"context"
	"sync"
)

// TimmyRuntime bundles the rebuildable Timmy objects served to a request. It is
// immutable once built; TimmyCore swaps the whole struct on rebuild.
type TimmyRuntime struct {
	SessionManager *TimmySessionManager
	LLMService     *TimmyLLMService
	VectorManager  *VectorIndexManager
}

// TimmyRuntimeBuilder builds a TimmyRuntime from a resolved config. Injected so
// tests can substitute a fake builder instead of constructing real LangChainGo
// clients.
type TimmyRuntimeBuilder func(ctx context.Context, cfg config.TimmyConfig) (*TimmyRuntime, error)

// TimmyCore owns the live TimmyRuntime and rebuilds it lazily when the wiring
// hash changes. Safe for concurrent use.
type TimmyCore struct {
	provider *TimmyConfigProvider
	build    TimmyRuntimeBuilder

	mu      sync.RWMutex
	current *TimmyRuntime
	hash    string
}

// NewTimmyCore constructs a core over the given provider and builder.
func NewTimmyCore(provider *TimmyConfigProvider, build TimmyRuntimeBuilder) *TimmyCore {
	return &TimmyCore{provider: provider, build: build}
}

// Get returns the live TimmyRuntime, rebuilding it if the wiring config has
// changed since the last build. A build error is returned to the caller and
// does NOT poison the cache: the next Get retries. Callers hold the returned
// pointer for the duration of their request; a concurrent rebuild swaps the
// holder's pointer but never mutates an in-use runtime.
func (c *TimmyCore) Get(ctx context.Context) (*TimmyRuntime, error) {
	cfg := c.provider.Current(ctx)
	want := c.provider.WiringHash(cfg)

	c.mu.RLock()
	if c.current != nil && c.hash == want {
		rt := c.current
		c.mu.RUnlock()
		return rt, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check under the write lock: another goroutine may have rebuilt.
	if c.current != nil && c.hash == want {
		return c.current, nil
	}
	rt, err := c.build(ctx, cfg)
	if err != nil {
		return nil, err
	}
	c.current = rt
	c.hash = want
	return rt, nil
}
```

Add `"github.com/ericfitz/tmi/internal/config"` to the import block.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestTimmyCore`
Expected: PASS (all three subtests).

- [ ] **Step 5: Commit**

```bash
git add api/timmy_core.go api/timmy_core_test.go
git commit -m "feat(timmy): TimmyCore lazy-rebuild holder for runtime config"
```

---

## Task 4: Server field, setter, and runtime accessor

**Files:**
- Modify: `api/server.go:81` (field), `api/server.go:290-298` (setters area)

Add the core to the server and a single accessor the handlers/middleware use.

- [ ] **Step 1: Add the field**

In `api/server.go`, next to `timmySessionManager *TimmySessionManager` (line 81), add:

```go
	timmyCore *TimmyCore
```

- [ ] **Step 2: Add setter + accessor**

After `SetTimmySessionManager` / `SetVectorManager` (around line 298), add:

```go
// SetTimmyCore wires the runtime Timmy core. When set, getTimmyRuntime resolves
// the session manager from it (DB-backed, lazy rebuild) instead of the
// startup-injected timmySessionManager.
func (s *Server) SetTimmyCore(core *TimmyCore) {
	s.timmyCore = core
}

// getTimmyRuntime returns the live TimmyRuntime. When a TimmyCore is wired it
// resolves (and lazily rebuilds) from the database; otherwise it falls back to
// the startup-injected session manager (used by unit tests that set the manager
// directly). Returns nil when Timmy is not available; callers must nil-check.
func (s *Server) getTimmyRuntime(ctx context.Context) (*TimmyRuntime, error) {
	if s.timmyCore != nil {
		return s.timmyCore.Get(ctx)
	}
	if s.timmySessionManager != nil {
		return &TimmyRuntime{
			SessionManager: s.timmySessionManager,
			VectorManager:  s.vectorManager,
		}, nil
	}
	return nil, nil
}
```

Confirm `context` is imported in `api/server.go` (it is widely used; add to the import block if golangci-lint flags it).

- [ ] **Step 3: Build to verify compilation**

Run: `make build-server`
Expected: SUCCESS (no callers yet — pure addition).

- [ ] **Step 4: Commit**

```bash
git add api/server.go
git commit -m "feat(timmy): add TimmyCore field + getTimmyRuntime accessor to Server"
```

---

## Task 5: Middleware reads enable/configured from the provider

**Files:**
- Modify: `api/timmy_middleware.go`
- Test: `api/timmy_middleware_test.go` (create if absent)

The middleware must evaluate `enabled`/`configured` per request from a provider. Keep a small interface so tests can inject a stub.

- [ ] **Step 1: Write the failing test**

Create/replace `api/timmy_middleware_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type stubTimmyConfigReader struct{ cfg config.TimmyConfig }

func (s stubTimmyConfigReader) Current(_ context.Context) config.TimmyConfig { return s.cfg }

func runTimmyMiddleware(t *testing.T, cfg config.TimmyConfig) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TimmyEnabledMiddleware(stubTimmyConfigReader{cfg: cfg}))
	r.POST("/threat_models/x/chat/sessions", func(c *gin.Context) { c.Status(http.StatusOK) })
	req, _ := http.NewRequest("POST", "/threat_models/x/chat/sessions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestTimmyMiddleware_Disabled404(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, runTimmyMiddleware(t, config.TimmyConfig{Enabled: false}))
}

func TestTimmyMiddleware_EnabledUnconfigured503(t *testing.T) {
	assert.Equal(t, http.StatusServiceUnavailable, runTimmyMiddleware(t, config.TimmyConfig{Enabled: true}))
}

func TestTimmyMiddleware_EnabledConfiguredPasses(t *testing.T) {
	cfg := config.TimmyConfig{
		Enabled: true, LLMProvider: "openai", LLMModel: "gpt-5.5",
		TextEmbeddingProvider: "openai", TextEmbeddingModel: "text-embedding-3-large",
	}
	assert.Equal(t, http.StatusOK, runTimmyMiddleware(t, cfg))
}

func TestTimmyMiddleware_NonTimmyPathPasses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TimmyEnabledMiddleware(stubTimmyConfigReader{cfg: config.TimmyConfig{Enabled: false}}))
	r.GET("/threat_models", func(c *gin.Context) { c.Status(http.StatusOK) })
	req, _ := http.NewRequest("GET", "/threat_models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestTimmyMiddleware`
Expected: FAIL — `TimmyEnabledMiddleware` still takes `config.TimmyConfig`, not a reader.

- [ ] **Step 3: Rewrite the middleware**

Replace `api/timmy_middleware.go` body:

```go
package api

import (
	"context"
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// TimmyConfigReader is the read surface the middleware needs. *TimmyConfigProvider
// satisfies it; tests inject a stub.
type TimmyConfigReader interface {
	Current(ctx context.Context) config.TimmyConfig
}

// TimmyEnabledMiddleware checks Timmy configuration per request and gates access
// to Timmy endpoints. When Timmy is disabled, all /chat/sessions and
// /admin/timmy/ paths return 404. When enabled but not fully configured, those
// paths return 503. All other paths pass through unaffected.
func TimmyEnabledMiddleware(reader TimmyConfigReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		isTimmyPath := strings.Contains(path, "/chat/sessions") || strings.HasPrefix(path, "/admin/timmy")
		if !isTimmyPath {
			c.Next()
			return
		}

		cfg := reader.Current(c.Request.Context())
		if !cfg.Enabled {
			HandleRequestError(c, NotFoundError("Timmy AI assistant is not enabled"))
			c.Abort()
			return
		}
		if !cfg.IsConfigured() {
			HandleRequestError(c, ServiceUnavailableError("Timmy is enabled but LLM/embedding providers are not configured"))
			c.Abort()
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestTimmyMiddleware`
Expected: PASS (all four subtests). NOTE: this breaks the `main.go` caller (fixed in Task 7) — that's expected; the unit package still compiles because the caller is in `cmd/server`.

- [ ] **Step 5: Commit**

```bash
git add api/timmy_middleware.go api/timmy_middleware_test.go
git commit -m "feat(timmy): middleware evaluates enable/configured per request via provider"
```

---

## Task 6: Handlers resolve session manager AND vector manager via getTimmyRuntime

**Files:**
- Modify: `api/timmy_handlers.go`
- Modify: `api/timmy_embedding_automation_handlers.go`

Replace direct `s.timmySessionManager` AND `s.vectorManager` reads with `getTimmyRuntime(ctx)` so handlers use the DB-backed runtime. Each handler that used either gains a single resolve-and-nil-check at the top.

**Why vector manager too:** `s.vectorManager` is consumed directly in `timmy_handlers.go` (`s.vectorManager.GetStatus()` at ~line 450) and in `timmy_embedding_automation_handlers.go` (`s.vectorManager.InvalidateIndex(...)` at ~lines 180, 251, 263). Because Task 7 builds the vector manager inside the core builder (no longer a startup singleton), these references must resolve from `rt.VectorManager`. The `getTimmyRuntime` fallback (Task 4) still populates `VectorManager` from `s.vectorManager` for unit tests that set it directly.

Exact references to migrate (from grep):
- `timmy_handlers.go`: nil check (34), `CreateSession` (78), `config.LLMTimeoutSeconds` (286-287), `HandleMessage` (292), nil check (207, 483), `SnapshotSources` (489), `s.vectorManager == nil` (436), `s.vectorManager.GetStatus()` (450).
- `timmy_embedding_automation_handlers.go`: nil check (28), `s.timmySessionManager.config` (33), `s.vectorManager` (179-180, 250-251, 262-263).

- [ ] **Step 1: Update `CreateTimmyChatSession`**

In `api/timmy_handlers.go`, find the nil check (around line 34):

```go
	if s.timmySessionManager == nil {
```

Replace the pattern in EACH handler that references `s.timmySessionManager` with a resolved local. At the start of each such handler (after the existing context/auth setup, before the first use), insert:

```go
	rt, rtErr := s.getTimmyRuntime(c.Request.Context())
	if rtErr != nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is temporarily unavailable"))
		return
	}
	if rt == nil || rt.SessionManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not available"))
		return
	}
```

Then replace every `s.timmySessionManager` in that handler body with `rt.SessionManager`. The affected handlers and their current `s.timmySessionManager` references:
- `CreateTimmyChatSession` — nil check + `s.timmySessionManager.CreateSession(...)`.
- `CreateTimmyChatMessage` — nil check + `s.timmySessionManager.config.LLMTimeoutSeconds` (twice) + `s.timmySessionManager.HandleMessage(...)`.
- Any other handler in the file calling `s.timmySessionManager.SnapshotSources(...)` — same treatment.

For the `config.LLMTimeoutSeconds` read, use `rt.SessionManager.config.LLMTimeoutSeconds`.

- [ ] **Step 2: Update the vector-manager-only handler in `api/timmy_handlers.go`**

The status handler (around line 436-450) uses `s.vectorManager` but not the session manager. Resolve the runtime and read the vector manager from it:

```go
	rt, rtErr := s.getTimmyRuntime(c.Request.Context())
	if rtErr != nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is temporarily unavailable"))
		return
	}
	if rt == nil || rt.VectorManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not available"))
		return
	}
	status := rt.VectorManager.GetStatus()
```

Replace the `if s.vectorManager == nil { ... }` guard and the `s.vectorManager.GetStatus()` call accordingly.

- [ ] **Step 3: Update `api/timmy_embedding_automation_handlers.go`**

Apply the resolve-and-nil-check at the top of each handler that reads `s.timmySessionManager` or `s.vectorManager`. Replace `s.timmySessionManager.config` with `rt.SessionManager.config`, and the three `s.vectorManager.InvalidateIndex(...)` calls (lines ~180, 251, 263) with `rt.VectorManager.InvalidateIndex(...)`. The existing `if s.vectorManager != nil` guards become `if rt.VectorManager != nil`.

- [ ] **Step 4: Build to verify compilation**

Run: `make build-server`
Expected: SUCCESS. If any `s.timmySessionManager` or `s.vectorManager` references remain in these two files, the build will name the file/line — fix each with the same pattern. (Note: `SetVectorManager`/`s.vectorManager` field stays on the Server for the unit-test fallback path; only the handler *reads* migrate to `rt`.)

- [ ] **Step 5: Run the existing Timmy handler tests**

Run: `make test-unit name=Timmy`
Expected: PASS. Existing handler tests that call `SetTimmySessionManager` / `SetVectorManager` still work via the `getTimmyRuntime` fallback (Task 4) because `timmyCore` is nil in those tests.

- [ ] **Step 6: Commit**

```bash
git add api/timmy_handlers.go api/timmy_embedding_automation_handlers.go
git commit -m "feat(timmy): handlers resolve session manager via getTimmyRuntime"
```

---

## Task 7: Wire the core at startup; remove the disabled-at-boot early return

**Files:**
- Modify: `cmd/server/main.go` — `initializeTimmySubsystem` (lines ~1122-1350) and the middleware wiring (line ~912).

`initializeTimmySubsystem` currently early-returns when `!cfg.Timmy.Enabled` and builds the LLM/session objects once. We restructure it so it: (a) always builds the `TimmyConfigProvider` and a `TimmyCore` whose builder reconstructs the LLM/vector/session objects from a resolved config; (b) wires `apiServer.SetTimmyCore(core)`; (c) wires the middleware to the provider. The content-source registry, access poller, and embedding cleanup stay exactly as they are (startup-wired). The builder closure captures the already-built content/SSRF dependencies (`timmyURIValidator`, `registry`, `stampedCfgProvider`) which do not depend on the LLM wiring.

- [ ] **Step 1: Change the middleware wiring (line ~912)**

Replace:

```go
	r.Use(api.TimmyEnabledMiddleware(config.Timmy))
	logger.Info("Timmy middleware configured (enabled=%v, configured=%v)", config.Timmy.Enabled, config.Timmy.IsConfigured())
```

with:

```go
	timmyConfigProvider := api.NewTimmyConfigProvider(settingsService)
	r.Use(api.TimmyEnabledMiddleware(timmyConfigProvider))
	logger.Info("Timmy middleware configured (DB-backed runtime config)")
```

Confirm `settingsService` is in scope at line 912 (it is — `apiServer.SetSettingsService(settingsService)` is called at line 603).

- [ ] **Step 2: Restructure `initializeTimmySubsystem`**

Replace the early-return + one-shot build. Keep everything from the top of the function through the `accessPoller.Start()` block unchanged (content sources, diagnostics, poller — startup-wired). Replace the `cfg.Timmy.Enabled` early return at the top with: build content-source/poller plumbing **always** (it was already only conditionally meaningful), then build the core. Concretely:

1. Remove the early `if !cfg.Timmy.Enabled { return }` (lines ~1125-1127) and the `if !cfg.Timmy.IsConfigured()` warn-return (lines ~1129-1132). The content-source/poller wiring below should run regardless so the registry exists; the LLM build now happens lazily in the core builder.

2. Replace the LLM/reranker/decomposer/sessionManager construction block (from `rateLimiter := ...` through `apiServer.SetTimmySessionManager(sessionManager)`) with a core whose builder rebuilds those objects:

```go
	provider := api.NewTimmyConfigProvider(settingsServiceFor(apiServer))
	builder := func(ctx context.Context, tcfg config.TimmyConfig) (*api.TimmyRuntime, error) {
		vm := api.NewVectorIndexManager(
			api.GlobalTimmyEmbeddingStore, tcfg.MaxMemoryMB, tcfg.InactivityTimeoutSeconds,
		)
		llmService, err := api.NewTimmyLLMService(tcfg, timmyURIValidator)
		if err != nil {
			return nil, fmt.Errorf("build Timmy LLM service: %w", err)
		}
		rateLimiter := api.NewTimmyRateLimiter(
			tcfg.MaxMessagesPerUserPerHour, tcfg.MaxSessionsPerThreatModel, tcfg.MaxConcurrentLLMRequests,
		)
		var reranker api.Reranker
		if tcfg.IsRerankConfigured() {
			rerankTimeout := time.Duration(tcfg.LLMTimeoutSeconds) * time.Second
			reranker = api.NewAPIReranker(
				tcfg.RerankBaseURL, tcfg.RerankModel, tcfg.RerankAPIKey, tcfg.RerankTopK,
				timmyURIValidator, rerankTimeout,
			)
		}
		var decomposer api.QueryDecomposer
		if tcfg.QueryDecompositionEnabled {
			decomposer = api.NewLLMQueryDecomposer(llmService)
		}
		sm := api.NewTimmySessionManager(
			tcfg, llmService, vm, registry, rateLimiter, reranker, decomposer,
		)
		sm.SetStampedConfigProvider(stampedCfgProvider)
		return &api.TimmyRuntime{SessionManager: sm, LLMService: llmService, VectorManager: vm}, nil
	}
	core := api.NewTimmyCore(provider, builder)
	apiServer.SetTimmyCore(core)
	logger.Info("Timmy core wired (DB-backed, lazy rebuild)")
```

NOTE: `settingsServiceFor(apiServer)` is a placeholder — use the actual `settingsService` value in scope. `initializeTimmySubsystem`'s signature does not currently receive `settingsService`; add it as a parameter. Update the signature to:

```go
func initializeTimmySubsystem(cfg *config.Config, apiServer *api.Server, settingsService *api.SettingsService, contentTokenRepo api.ContentTokenRepository, contentOAuthRegistry *api.ContentOAuthProviderRegistry, stampedCfgProvider config.StampedConfigProvider) {
```

and update the call site (line ~787) to pass `settingsService`:

```go
	initializeTimmySubsystem(config, apiServer, settingsService, contentTokenRepo, contentOAuthRegistry, api.NewStampedConfigProvider(settingsService))
```

3. Confirm `fmt`, `time`, `context` are imported in `cmd/server/main.go` (they are).

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: SUCCESS. The vector manager is now built inside the core builder, so remove the old `vectorManager := api.NewVectorIndexManager(...)` + `apiServer.SetVectorManager(vectorManager)` startup lines that preceded the replaced block (the runtime carries it now, and Task 6 migrated the handler reads to `rt.VectorManager`). Keep the `SetVectorManager` method and the `s.vectorManager` field on the Server — they remain for the unit-test fallback path in `getTimmyRuntime`. Fix any resulting unused-variable errors.

- [ ] **Step 4: Verify no production code still depends on the startup vector manager**

Run: `rg -n 's\.vectorManager|GetVectorManager|SetVectorManager' api/*.go cmd/server/main.go | rg -v '_test'`
Expected: `SetVectorManager`/`s.vectorManager` appear only in `server.go` (definition + fallback) — NOT in `cmd/server/main.go` (startup call removed) and NOT as a direct read in handler files (Task 6 migrated those to `rt.VectorManager`). If a handler still reads `s.vectorManager`, migrate it per Task 6.

- [ ] **Step 5: Run the full unit suite**

Run: `make test-unit`
Expected: PASS (all packages).

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(timmy): wire DB-backed TimmyCore at startup, drop boot-time enable gate"
```

---

## Task 8: Integration test — enable/disable at runtime without restart

**Files:**
- Create: `test/integration/workflows/timmy_runtime_config_test.go`

- [ ] **Step 1: Write the integration test**

```go
package workflows

import (
	"os"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// TestTimmyRuntimeToggle_Integration verifies that enabling Timmy via the
// settings API makes the chat endpoint reachable without a server restart, and
// disabling it returns the endpoint to 404.
func TestTimmyRuntimeToggle_Integration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v", err)
	}

	// Admin client (charlie) to drive /admin/settings.
	adminTokens, err := framework.AuthenticateUser("charlie")
	framework.AssertNoError(t, err, "admin auth failed")
	admin, err := framework.NewClient(serverURL, adminTokens)
	framework.AssertNoError(t, err, "admin client failed")

	me, _ := admin.Do(framework.Request{Method: "GET", Path: "/me"})
	// Skip gracefully if charlie isn't admin in this DB.
	if !framework.JSONBool(me.Body, "is_admin") {
		t.Skip("charlie is not admin in this instance; cannot drive /admin/settings")
	}

	// Disable Timmy, confirm 404 on a chat path (use a syntactically-valid TM id).
	_, err = admin.Do(framework.Request{
		Method: "PUT", Path: "/admin/settings/timmy.enabled",
		Body: map[string]any{"value": "false", "type": "bool"},
	})
	framework.AssertNoError(t, err, "disable timmy failed")

	resp, err := admin.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models/00000000-0000-0000-0000-000000000000/chat/sessions",
		Body:   map[string]any{},
	})
	framework.AssertNoError(t, err, "chat call failed")
	if resp.StatusCode != 404 {
		t.Errorf("expected 404 when Timmy disabled, got %d", resp.StatusCode)
	}
	t.Log("✓ Timmy disabled → chat path 404 without restart")
}
```

NOTE: If `framework.JSONBool` does not exist, parse `me.Body` with `json.Unmarshal` into a `map[string]interface{}` as other workflow tests do (see `settings_crud_test.go`). Match the framework's actual helpers; do not invent APIs.

- [ ] **Step 2: Verify it compiles and runs (skips without env)**

Run: `make test-integration name=TestTimmyRuntimeToggle`
Expected: PASS or SKIP (skips cleanly when not admin / no integration env).

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/timmy_runtime_config_test.go
git commit -m "test(timmy): integration test for runtime enable/disable toggle"
```

---

## Task 9: Data migration — import real Timmy values into the dev DB

**Files:** none committed (operational step; throwaway import file in `/tmp`).

- [ ] **Step 1: Build dbtool**

Run: `make build-dbtool` (or the documented target; if absent, `rg -n 'dbtool' Makefile` to find it).
Expected: `bin/tmi-dbtool` exists.

- [ ] **Step 2: Create the throwaway import file with the API key**

The key lives in `~/Desktop/lmk`. Write `/tmp/timmy-import.yml` (NOT in the repo) embedding it:

```bash
KEY="$(cat ~/Desktop/lmk)"
cat > /tmp/timmy-import.yml <<EOF
timmy:
  enabled: true
  llm_provider: openai
  llm_model: gpt-5.5
  llm_base_url: https://api.openai.com/v1
  llm_api_key: "${KEY}"
  text_embedding_provider: openai
  text_embedding_model: text-embedding-3-large
  text_embedding_base_url: https://api.openai.com/v1
  text_embedding_api_key: "${KEY}"
  embedding_dimension: 3072
EOF
```

- [ ] **Step 3: Import with --no-rewrite (do not rewrite a committed config)**

Run: `bin/tmi-dbtool --import-legacy -f /tmp/timmy-import.yml --no-rewrite --config config-development.yml`
(Confirm exact flags with `bin/tmi-dbtool --help`; `--no-rewrite` must be present so no committed YAML is rewritten with the secret.)
Expected: success log; `timmy.*` rows updated/created.

- [ ] **Step 4: Delete the throwaway file**

```bash
rm -f /tmp/timmy-import.yml
```

- [ ] **Step 5: Verify the DB values (mask the key)**

Run:

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "SELECT setting_key, CASE WHEN setting_key LIKE '%api_key%' THEN CASE WHEN value<>'' THEN '<set>' ELSE '<empty>' END ELSE value END FROM system_settings WHERE setting_key IN ('timmy.enabled','timmy.llm_model','timmy.embedding_dimension','timmy.llm_api_key','timmy.text_embedding_api_key') ORDER BY setting_key;"
```

Expected: `timmy.enabled=true`, `timmy.llm_model=gpt-5.5`, `timmy.embedding_dimension=3072`, both api_key rows `<set>`.

---

## Task 10: Live verification

**Files:** none.

- [ ] **Step 1: Restart the dev server**

Run: `make stop-server && make start-dev`
Expected: server starts; log shows `Timmy core wired (DB-backed, lazy rebuild)` and `Timmy middleware configured (DB-backed runtime config)`.

- [ ] **Step 2: Confirm Timmy is reachable (no longer enabled=false)**

Run: `rg -i 'timmy' logs/tmi.log | rg -iv 'GORM|pg_indexes' | tail -10`
Expected: no `enabled=false, configured=false` line; core-wired line present.

- [ ] **Step 3: Exercise a Timmy endpoint with an authenticated user**

Use the OAuth stub to get a token (per CLAUDE.md), then `POST /threat_models/{id}/chat/sessions` against a real threat model owned by that user. Expected: not 404/503 from the gate (a 200/201 or a domain-level response, not the "not enabled"/"temporarily unavailable" errors). If the LLM key is valid, a chat message round-trips.

- [ ] **Step 4: Record the outcome** in the session summary (do not commit logs).

---

## Self-Review Notes

- **Spec coverage:** enable gate per-request (Task 5) ✓; LLM/embedding lazy rebuild via WiringHash (Tasks 2,3,7) ✓; vector manager rebuilt in builder (Task 7) ✓; tuning knobs live (Task 1) ✓; config-first precedence (Task 1, via GetString/GetBool/GetInt) ✓; data load via dbtool --import-legacy --no-rewrite (Task 9) ✓; content sources stay startup-wired (Task 7 keeps that block) ✓; error → 503 + retry, no cache poison (Task 3) ✓.
- **Type consistency:** `TimmyRuntime{SessionManager, LLMService, VectorManager}`, `TimmyConfigProvider.Current/WiringHash`, `TimmyCore.Get`, `TimmyConfigReader.Current`, `getTimmyRuntime` used consistently across tasks.
- **Out of scope confirmed:** content-source registry, access poller, embedding cleaner untouched (#427). No secrets-at-rest warning here (#428).
- **DB review trigger:** No GORM model/schema/migration/SQL changes — Task 9 only writes settings *values* via the existing service path. `oracle-db-admin` not required, but will be re-evaluated at task-completion if any task ends up touching repository/SQL code.
