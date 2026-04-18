//go:build dev || test || integration

package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/testhelpers"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// =============================================================================
// Integration test helpers
// =============================================================================

// openIntegrationDB opens a GORM connection to the PostgreSQL integration database
// (using TEST_DB_* env vars) or falls back to an in-memory SQLite database when
// those vars are not set. The SQLite fallback lets the test compile and run in
// unit-test mode, though SELECT … FOR UPDATE is a no-op there.
func openIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")

	if host == "" || port == "" || user == "" || dbname == "" {
		t.Log("TEST_DB_* vars not set; falling back to SQLite (SELECT FOR UPDATE is a no-op)")
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		require.NoError(t, err, "open SQLite fallback")
		require.NoError(t, db.AutoMigrate(&models.User{}, &models.UserContentToken{}))
		return db
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		host, port, user, password, dbname,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "open PostgreSQL integration DB")

	// AutoMigrate only the tables this test touches.
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.UserContentToken{}))

	return db
}

// openIntegrationRedis returns a Redis client for integration tests.
// Uses TEST_REDIS_* env vars when available, otherwise starts a miniredis server.
func openIntegrationRedis(t *testing.T) redis.UniversalClient {
	t.Helper()

	host := os.Getenv("TEST_REDIS_HOST")
	port := os.Getenv("TEST_REDIS_PORT")

	if host == "" || port == "" {
		t.Log("TEST_REDIS_* vars not set; using miniredis")
		mr := miniredis.RunT(t)
		return redis.NewClient(&redis.Options{Addr: mr.Addr()})
	}

	return redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port),
	})
}

// createIntegrationUser inserts a minimal User row and returns its InternalUUID.
// Uses a fixed deterministic prefix so test rows are easy to identify and clean up.
func createIntegrationUser(t *testing.T, db *gorm.DB, label string) string {
	t.Helper()
	id := uuid.New().String()
	email := fmt.Sprintf("integration-test-%s@tmi.local", label)
	u := models.User{
		InternalUUID: id,
		Provider:     "test",
		Email:        email,
		Name:         fmt.Sprintf("Integration Test User (%s)", label),
	}
	require.NoError(t, db.Create(&u).Error, "create test user")
	t.Cleanup(func() {
		// Best-effort cleanup; ignore errors (e.g., row already deleted by CASCADE).
		db.Where("internal_uuid = ?", id).Delete(&models.User{}) //nolint:errcheck
	})
	return id
}

// newIntegrationTestEncryptor creates a fresh ContentTokenEncryptor with a random key.
func newIntegrationTestEncryptor(t *testing.T) *ContentTokenEncryptor {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	enc, err := NewContentTokenEncryptor(hex.EncodeToString(key))
	require.NoError(t, err)
	return enc
}

// buildIntegrationHandlers wires up a ContentOAuthHandlers backed by real Postgres
// and real (or mini) Redis, using the given stub as the mock OAuth provider.
// callbackURL is the URL the provider should redirect to after authorization
// (i.e. the /oauth2/content_callback endpoint on the test Gin router).
func buildIntegrationHandlers(
	t *testing.T,
	db *gorm.DB,
	rdb redis.UniversalClient,
	stub *testhelpers.StubOAuthProvider,
	userID string,
	callbackURL string,
	clientCallbackURL string,
) (*ContentOAuthHandlers, *GormContentTokenRepository, *ContentOAuthProviderRegistry) {
	t.Helper()

	enc := newIntegrationTestEncryptor(t)
	tokenRepo := NewGormContentTokenRepository(db, enc)

	providerCfg := config.ContentOAuthProviderConfig{
		ClientID:       "integration-cid",
		ClientSecret:   "integration-sec",
		AuthURL:        stub.AuthURL(),
		TokenURL:       stub.TokenURL(),
		RevocationURL:  stub.RevokeURL(),
		UserinfoURL:    stub.UserinfoURL(),
		RequiredScopes: []string{"read:mock"},
	}
	provider := NewBaseContentOAuthProvider("mock", providerCfg)

	registry := NewContentOAuthProviderRegistry()
	registry.Register(provider)

	stateStore := NewContentOAuthStateStore(rdb)

	allowList := NewClientCallbackAllowList([]string{clientCallbackURL, clientCallbackURL + "*"})

	cfg := config.ContentOAuthConfig{
		CallbackURL:            callbackURL,
		AllowedClientCallbacks: []string{clientCallbackURL, clientCallbackURL + "*"},
	}

	h := &ContentOAuthHandlers{
		Cfg:           cfg,
		Registry:      registry,
		StateStore:    stateStore,
		Tokens:        tokenRepo,
		CallbackAllow: allowList,
		UserLookup: func(c *gin.Context) (string, bool) {
			return userID, true
		},
	}

	return h, tokenRepo, registry
}

// buildIntegrationRouter creates a Gin test router with the content OAuth routes registered.
func buildIntegrationRouter(h *ContentOAuthHandlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/me/content_tokens/:provider_id/authorize", h.Authorize)
	r.GET("/oauth2/content_callback", h.Callback)
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)
	r.GET("/me/content_tokens", h.List)
	return r
}

// doAuthorizeFlow performs POST /me/content_tokens/mock/authorize and returns
// the authorization_url from the JSON response.
func doAuthorizeFlow(t *testing.T, router *gin.Engine, clientCallbackURL string) string {
	t.Helper()

	body, err := json.Marshal(map[string]string{"client_callback": clientCallbackURL})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/me/content_tokens/mock/authorize",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "authorize response: %s", rec.Body.String())

	var resp struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotEmpty(t, resp.AuthorizationURL, "authorization_url must not be empty")

	return resp.AuthorizationURL
}

// followAuthorizeAndCallback:
//  1. Follows the authorization_url (one redirect hop, no further following).
//  2. Extracts code+state from the Location header.
//  3. Issues GET /oauth2/content_callback?code=...&state=... against the router.
//  4. Returns the final Location redirect URL.
func followAuthorizeAndCallback(t *testing.T, authURL string, router *gin.Engine) string {
	t.Helper()

	// Step 1: Follow the stub /authorize endpoint (it immediately 302s to our callback).
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(authURL) //nolint:gosec // G107 - test URL from stub server
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode, "stub /authorize must 302")

	// Step 2: Extract code and state from Location.
	location := resp.Header.Get("Location")
	require.NotEmpty(t, location, "stub /authorize must provide a Location header")

	parsed, err := url.Parse(location)
	require.NoError(t, err)
	q := parsed.Query()
	code := q.Get("code")
	state := q.Get("state")
	require.NotEmpty(t, code, "code must be present in Location")
	require.NotEmpty(t, state, "state must be present in Location")

	// Step 3: Deliver the callback to our Gin router.
	callbackURL := fmt.Sprintf("/oauth2/content_callback?code=%s&state=%s",
		url.QueryEscape(code), url.QueryEscape(state))
	cbReq := httptest.NewRequest(http.MethodGet, callbackURL, nil)
	cbRec := httptest.NewRecorder()
	router.ServeHTTP(cbRec, cbReq)
	require.Equal(t, http.StatusFound, cbRec.Code,
		"callback must 302 to client URL; body: %s", cbRec.Body.String())

	// Step 4: Return the redirect destination.
	finalLocation := cbRec.Header().Get("Location")
	require.NotEmpty(t, finalLocation, "callback must set a Location header")
	return finalLocation
}

// =============================================================================
// End-to-end test
// =============================================================================

// TestDelegatedContentProvider_EndToEnd_Integration exercises the full
// delegated-content-provider infrastructure: authorize → callback → persist →
// fetch → lazy refresh → concurrent refresh serialization → revoke.
//
// Requires: TEST_DB_* and TEST_REDIS_* env vars pointing at a live PostgreSQL
// + Redis instance (set automatically by scripts/run-integration-tests-pg.sh).
// Falls back to SQLite + miniredis in unit-test mode, but SELECT … FOR UPDATE
// serialization is not exercised in that configuration.
func TestDelegatedContentProvider_EndToEnd_Integration(t *testing.T) {
	ctx := context.Background()

	db := openIntegrationDB(t)
	rdb := openIntegrationRedis(t)

	// Detect whether we have a real Postgres connection (for the concurrent-refresh sub-test).
	usingPostgres := os.Getenv("TEST_DB_HOST") != ""

	// Client callback URL used in all sub-tests.
	const clientCallback = "http://localhost:55123/cb"

	// -------------------------------------------------------------------------
	// Helper: stand up fresh infrastructure for a sub-test.
	// -------------------------------------------------------------------------
	newInfra := func(t *testing.T) (
		stub *testhelpers.StubOAuthProvider,
		router *gin.Engine,
		tokenRepo *GormContentTokenRepository,
		registry *ContentOAuthProviderRegistry,
		userID string,
	) {
		t.Helper()

		// Each sub-test gets its own stub server so knobs are independent.
		stub = testhelpers.NewStubOAuthProvider(t)

		// Stand up a temporary httptest.Server just to discover a free port for
		// the callback URL; we then use that port as the "base" of our test router.
		// In practice we use httptest.NewRecorder directly, so any non-empty URL works.
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(ts.Close)
		callbackURL := ts.URL + "/oauth2/content_callback"

		userID = createIntegrationUser(t, db, t.Name())

		h, repo, reg := buildIntegrationHandlers(t, db, rdb, stub, userID, callbackURL, clientCallback)
		r := buildIntegrationRouter(h)
		return stub, r, repo, reg, userID
	}

	// =========================================================================
	// Sub-test 1: Happy path — authorize → callback → fetch → refresh → revoke
	// =========================================================================
	t.Run("happy_path", func(t *testing.T) {
		stub, router, tokenRepo, registry, userID := newInfra(t)

		// Set deterministic first access token so we can assert on it later.
		stub.SetNextAccess("integration-at-1")

		// --- Step 1: Authorize ---
		authURL := doAuthorizeFlow(t, router, clientCallback)

		// --- Step 2 & 3: Follow authorize + callback ---
		finalLocation := followAuthorizeAndCallback(t, authURL, router)

		parsedFinal, err := url.Parse(finalLocation)
		require.NoError(t, err)
		assert.Equal(t, "success", parsedFinal.Query().Get("status"),
			"callback must redirect with status=success; location=%s", finalLocation)
		assert.Equal(t, "mock", parsedFinal.Query().Get("provider_id"),
			"provider_id must be present in redirect")

		// --- Step 4: Assert row persisted ---
		tok, err := tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
		require.NoError(t, err, "token must be persisted after callback")
		assert.Equal(t, ContentTokenStatusActive, tok.Status)
		assert.Equal(t, "integration-at-1", tok.AccessToken,
			"access token must match what stub issued")
		assert.NotEmpty(t, tok.RefreshToken, "refresh token must be persisted")

		// --- Step 5: Fetch via MockDelegatedSource ---
		mockSource := NewMockDelegatedSource(tokenRepo, registry)
		mockSource.Contents["doc1"] = []byte("hello from integration test")

		data, contentType, err := mockSource.FetchForUser(ctx, userID, "mock://doc/doc1")
		require.NoError(t, err, "FetchForUser must succeed with valid token")
		assert.Equal(t, []byte("hello from integration test"), data)
		assert.Equal(t, "text/plain", contentType)

		// --- Step 6: Force expiry + refresh ---
		stub.SetNextAccess("integration-at-2")
		expired := time.Now().Add(-2 * time.Hour)
		tok.ExpiresAt = &expired
		require.NoError(t, tokenRepo.Upsert(ctx, tok))

		data2, _, err := mockSource.FetchForUser(ctx, userID, "mock://doc/doc1")
		require.NoError(t, err, "FetchForUser must trigger lazy refresh")
		assert.Equal(t, []byte("hello from integration test"), data2)

		assert.Equal(t, 1, stub.RefreshCalls(),
			"exactly one refresh call must have been made")
		tok2, err := tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
		require.NoError(t, err)
		assert.Equal(t, "integration-at-2", tok2.AccessToken,
			"repository must hold the refreshed access token")

		// --- Step 7: Concurrent refresh ---
		if usingPostgres {
			stub.ResetRefreshCalls()
			stub.SetNextAccess("integration-at-3")

			// Re-expire the token.
			tok3, err := tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
			require.NoError(t, err)
			expiredAgain := time.Now().Add(-2 * time.Hour)
			tok3.ExpiresAt = &expiredAgain
			require.NoError(t, tokenRepo.Upsert(ctx, tok3))

			const goroutines = 5
			var wg sync.WaitGroup
			wg.Add(goroutines)
			errs := make([]error, goroutines)
			results := make([][]byte, goroutines)

			for i := 0; i < goroutines; i++ {
				go func(idx int) {
					defer wg.Done()
					d, _, fetchErr := mockSource.FetchForUser(ctx, userID, "mock://doc/doc1")
					errs[idx] = fetchErr
					results[idx] = d
				}(i)
			}
			wg.Wait()

			for i, e := range errs {
				assert.NoError(t, e, "goroutine %d must succeed", i)
			}
			for i, d := range results {
				assert.Equal(t, []byte("hello from integration test"), d,
					"goroutine %d must return correct data", i)
			}
			assert.Equal(t, 1, stub.RefreshCalls(),
				"SELECT FOR UPDATE must serialize: exactly 1 provider refresh call")
		} else {
			t.Log("skipping concurrent-refresh sub-check (requires PostgreSQL)")
		}

		// --- Step 8: Revoke + delete ---
		revokeBeforeCallCount := stub.RevokeCalls()
		tokenBeforeRevoke, err := tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
		require.NoError(t, err)
		expectedRevokedToken := tokenBeforeRevoke.AccessToken

		req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/mock", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code, "DELETE must return 204")

		assert.Equal(t, revokeBeforeCallCount+1, stub.RevokeCalls(),
			"provider revoke must have been called once")
		revokedTokens := stub.RevokedTokens()
		assert.Contains(t, revokedTokens, expectedRevokedToken,
			"revoked token must match what was stored")

		_, err = tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
		assert.True(t, errors.Is(err, ErrContentTokenNotFound),
			"token row must be gone after DELETE")
	})

	// =========================================================================
	// Sub-test 2: Permanent refresh failure path
	// =========================================================================
	t.Run("permanent_refresh_failure", func(t *testing.T) {
		stub, router, tokenRepo, registry, userID := newInfra(t)

		// --- Step a: Authorize + callback ---
		stub.SetNextAccess("prf-at-1")
		authURL := doAuthorizeFlow(t, router, clientCallback)
		finalLocation := followAuthorizeAndCallback(t, authURL, router)

		parsedFinal, err := url.Parse(finalLocation)
		require.NoError(t, err)
		assert.Equal(t, "success", parsedFinal.Query().Get("status"),
			"callback must redirect with status=success")

		// --- Step b: Configure stub to fail refresh ---
		stub.RefreshStatus = http.StatusBadRequest
		stub.RefreshSucceeds = false

		// --- Step c: Force expiry ---
		tok, err := tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
		require.NoError(t, err)
		expired := time.Now().Add(-2 * time.Hour)
		tok.ExpiresAt = &expired
		require.NoError(t, tokenRepo.Upsert(ctx, tok))

		// Wire up a mock source for this sub-test.
		mockSource := NewMockDelegatedSource(tokenRepo, registry)
		mockSource.Contents["docY"] = []byte("data")

		// --- Step d: FetchForUser must return ErrAuthRequired ---
		_, _, err = mockSource.FetchForUser(ctx, userID, "mock://doc/docY")
		assert.ErrorIs(t, err, ErrAuthRequired,
			"permanent refresh failure must surface as ErrAuthRequired")

		// --- Step e: Assert row status is failed_refresh ---
		tok2, err := tokenRepo.GetByUserAndProvider(ctx, userID, "mock")
		require.NoError(t, err)
		assert.Equal(t, ContentTokenStatusFailedRefresh, tok2.Status,
			"token status must be failed_refresh after permanent failure")

		// --- Step f: Second call must return ErrAuthRequired with no additional refresh ---
		refreshCallsBefore := stub.RefreshCalls()
		_, _, err = mockSource.FetchForUser(ctx, userID, "mock://doc/docY")
		assert.ErrorIs(t, err, ErrAuthRequired,
			"second call must also return ErrAuthRequired")
		assert.Equal(t, refreshCallsBefore, stub.RefreshCalls(),
			"no additional refresh calls must be made when status=failed_refresh")
	})
}
