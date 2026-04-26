//go:build dev || test || integration

package api

// TestConfluenceDelegated_EndToEnd_Integration exercises the Confluence
// delegated content provider end-to-end:
//
//  1. Authorize a content token for confluence (PKCE → callback → token row).
//  2. Verify the token row is persisted with provider_account_label upgraded
//     to the matched accessible-resources site URL.
//  3. ValidateAccess against a Confluence page URL succeeds.
//  4. Fetch returns the page's view-format HTML.
//  5. Force token expiry; subsequent Fetch triggers a refresh and succeeds.
//  6. DELETE /me/content_tokens/confluence removes the row.
//
// Requires: TEST_DB_* and TEST_REDIS_* env vars for a live PostgreSQL + Redis,
// or falls back to SQLite + miniredis. The Atlassian API endpoints
// (accessible-resources, pages/{id}) are mocked via a local httptest.Server.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/testhelpers"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAtlassianAPI is a miniature Atlassian REST API stub used by the Confluence
// integration test. It serves /oauth/token/accessible-resources and
// /ex/confluence/{cloud_id}/wiki/api/v2/pages/{id} (with and without
// body-format=view).
type stubAtlassianAPI struct {
	server         *httptest.Server
	resources      []map[string]string
	pageHTML       string
	pageStatus     int
	resourcesCalls atomic.Int32
	contentCalls   atomic.Int32
	metaCalls      atomic.Int32
}

func newStubAtlassianAPI(t *testing.T) *stubAtlassianAPI {
	t.Helper()
	s := &stubAtlassianAPI{
		pageHTML:   "<p>integration html</p>",
		pageStatus: http.StatusOK,
	}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/oauth/token/accessible-resources":
			s.resourcesCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(s.resources)
		case strings.HasPrefix(r.URL.Path, "/ex/confluence/"):
			if r.URL.Query().Get("body-format") == "view" {
				s.contentCalls.Add(1)
				if s.pageStatus != http.StatusOK {
					http.Error(w, "page failure", s.pageStatus)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "12345",
					"title": "Integration Page",
					"body": map[string]any{
						"view": map[string]string{
							"representation": "view",
							"value":          s.pageHTML,
						},
					},
				})
				return
			}
			s.metaCalls.Add(1)
			if s.pageStatus != http.StatusOK {
				http.Error(w, "page failure", s.pageStatus)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345", "title": "Integration Page"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

// TestConfluenceDelegated_EndToEnd_Integration is the parent end-to-end test
// for the Confluence delegated provider.
func TestConfluenceDelegated_EndToEnd_Integration(t *testing.T) {
	const clientCallback = "http://localhost:55678/cb"
	confluencePageURL := "https://acme.atlassian.net/wiki/spaces/ENG/pages/12345/Integration"

	ctx := context.Background()
	db := openIntegrationDB(t)
	rdb := openIntegrationRedis(t)
	enc := newIntegrationTestEncryptor(t)

	t.Run("happy_path_authorize_fetch_refresh_revoke", func(t *testing.T) {
		stub := testhelpers.NewStubOAuthProvider(t)
		atlassian := newStubAtlassianAPI(t)
		atlassian.resources = []map[string]string{
			{"id": "cloud-acme", "url": "https://acme.atlassian.net"},
		}

		// Per-test user.
		userID := createIntegrationUser(t, db, "confluence-happy")

		tokenRepo := NewGormContentTokenRepository(db, enc)

		// Build OAuth provider via the same code path used at runtime
		// (LoadContentOAuthRegistryFromConfig → buildContentOAuthProvider) so
		// the Confluence wrapper is exercised, including its
		// accessible-resources call during account linking.
		providerCfg := config.ContentOAuthProviderConfig{
			Enabled:        true,
			ClientID:       "confluence-cid",
			ClientSecret:   "confluence-sec",
			AuthURL:        stub.AuthURL(),
			TokenURL:       stub.TokenURL(),
			UserinfoURL:    stub.UserinfoURL(),
			RevocationURL:  stub.RevokeURL(),
			RequiredScopes: []string{"read:confluence-content.all", "offline_access"},
			ExtraAuthorizeParams: map[string]string{
				"audience": "api.atlassian.com",
				"prompt":   "consent",
			},
		}
		// Construct the Confluence wrapper directly and aim its API base
		// at the stub Atlassian API so accessible-resources resolves there.
		confluenceProvider := NewConfluenceContentOAuthProvider(
			NewBaseContentOAuthProvider(ProviderConfluence, providerCfg))
		confluenceProvider.apiBase = atlassian.server.URL
		confluenceProvider.httpClient = atlassian.server.Client()

		registry := NewContentOAuthProviderRegistry()
		registry.Register(confluenceProvider)

		// Build OAuth handlers / router.
		stateStore := NewContentOAuthStateStore(rdb)
		allowList := NewClientCallbackAllowList([]string{clientCallback, clientCallback + "*"})
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		t.Cleanup(ts.Close)
		callbackURL := ts.URL + "/oauth2/content_callback"

		h := &ContentOAuthHandlers{
			Cfg: config.ContentOAuthConfig{
				CallbackURL:            callbackURL,
				AllowedClientCallbacks: []string{clientCallback, clientCallback + "*"},
			},
			Registry:      registry,
			StateStore:    stateStore,
			Tokens:        tokenRepo,
			CallbackAllow: allowList,
			UserLookup: func(_ *gin.Context) (string, bool) {
				return userID, true
			},
		}
		router := buildIntegrationRouter(h)

		// --- Step 1: Authorize ---
		stub.SetNextAccess("confluence-at-1")
		authURL := authorizeForProvider(t, router, ProviderConfluence, clientCallback)

		// Confirm extra params landed on the authorize URL.
		parsedAuth, err := url.Parse(authURL)
		require.NoError(t, err)
		assert.Equal(t, "api.atlassian.com", parsedAuth.Query().Get("audience"),
			"audience must be appended via ExtraAuthorizeParams")
		assert.Equal(t, "consent", parsedAuth.Query().Get("prompt"))

		// --- Step 2: Follow authorize → callback ---
		finalLocation := followAuthorizeAndCallback(t, authURL, router)
		parsedFinal, err := url.Parse(finalLocation)
		require.NoError(t, err)
		assert.Equal(t, "success", parsedFinal.Query().Get("status"),
			"callback must succeed; location=%s", finalLocation)
		assert.Equal(t, ProviderConfluence, parsedFinal.Query().Get("provider_id"))

		// --- Step 3: Token row persisted, label upgraded to site URL ---
		tok, err := tokenRepo.GetByUserAndProvider(ctx, userID, ProviderConfluence)
		require.NoError(t, err)
		assert.Equal(t, ContentTokenStatusActive, tok.Status)
		assert.Equal(t, "confluence-at-1", tok.AccessToken)
		assert.NotEmpty(t, tok.RefreshToken)
		assert.Equal(t, "https://acme.atlassian.net", tok.ProviderAccountLabel,
			"Confluence wrapper should upgrade label to the matched site URL")
		// account_id falls back from /me; stub provides "stub-account-id".
		assert.Equal(t, "stub-account-id", tok.ProviderAccountID)

		// --- Step 4: Build a DelegatedConfluenceSource pointed at the same stub ---
		source := NewDelegatedConfluenceSource(tokenRepo, registry)
		source.apiBase = atlassian.server.URL
		source.httpClient = atlassian.server.Client()

		// --- Step 5: ValidateAccess succeeds ---
		userCtx := WithUserID(ctx, userID)
		ok, err := source.ValidateAccess(userCtx, confluencePageURL)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, int32(1), atlassian.metaCalls.Load(),
			"ValidateAccess must call the metadata-only endpoint exactly once")

		// --- Step 6: Fetch returns view HTML ---
		data, ctype, err := source.Fetch(userCtx, confluencePageURL)
		require.NoError(t, err)
		assert.Equal(t, "text/html", ctype)
		assert.Equal(t, "<p>integration html</p>", string(data))
		assert.Equal(t, int32(1), atlassian.contentCalls.Load())

		// --- Step 7: Force expiry + refresh ---
		stub.SetNextAccess("confluence-at-2")
		expired := time.Now().Add(-1 * time.Hour)
		tok.ExpiresAt = &expired
		require.NoError(t, tokenRepo.Upsert(ctx, tok))

		_, _, err = source.Fetch(userCtx, confluencePageURL)
		require.NoError(t, err, "Fetch after expiry must lazy-refresh")
		assert.Equal(t, 1, stub.RefreshCalls(),
			"exactly one refresh call must have been made")
		tok2, err := tokenRepo.GetByUserAndProvider(ctx, userID, ProviderConfluence)
		require.NoError(t, err)
		assert.Equal(t, "confluence-at-2", tok2.AccessToken,
			"repository must hold the refreshed access token")

		// --- Step 8: Revoke (DELETE removes row; revoke endpoint optional) ---
		req := httptest.NewRequest(http.MethodDelete,
			"/me/content_tokens/"+ProviderConfluence, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNoContent, rec.Code)

		_, err = tokenRepo.GetByUserAndProvider(ctx, userID, ProviderConfluence)
		require.Error(t, err, "token row should be gone after DELETE")
	})

	t.Run("legacy_url_returns_error_without_provider_call", func(t *testing.T) {
		stub := testhelpers.NewStubOAuthProvider(t)
		atlassian := newStubAtlassianAPI(t)
		atlassian.resources = []map[string]string{
			{"id": "cloud-acme", "url": "https://acme.atlassian.net"},
		}
		userID := createIntegrationUser(t, db, "confluence-legacy")
		tokenRepo := NewGormContentTokenRepository(db, enc)

		// Pre-seed an active token so we know any failure isn't an auth issue.
		expiry := time.Now().Add(1 * time.Hour)
		require.NoError(t, tokenRepo.Upsert(ctx, &ContentToken{
			UserID:      userID,
			ProviderID:  ProviderConfluence,
			AccessToken: "preset-at",
			Status:      ContentTokenStatusActive,
			ExpiresAt:   &expiry,
		}))

		registry := NewContentOAuthProviderRegistry()
		// Need a base provider so Refresh paths don't panic on missing entry.
		_ = stub
		base := NewBaseContentOAuthProvider(ProviderConfluence, config.ContentOAuthProviderConfig{
			ClientID: "x", ClientSecret: "y",
			AuthURL: "http://x", TokenURL: "http://x",
		})
		registry.Register(NewConfluenceContentOAuthProvider(base))

		source := NewDelegatedConfluenceSource(tokenRepo, registry)
		source.apiBase = atlassian.server.URL
		source.httpClient = atlassian.server.Client()

		userCtx := WithUserID(ctx, userID)
		_, _, err := source.Fetch(userCtx, "https://acme.atlassian.net/wiki/display/ENG/Home")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not extract page id")
		assert.Equal(t, int32(0), atlassian.contentCalls.Load(),
			"legacy URLs should be rejected before any HTTP call")
		assert.Equal(t, int32(0), atlassian.resourcesCalls.Load(),
			"legacy URLs should be rejected before resolving cloud_id")
	})
}

// authorizeForProvider performs POST /me/content_tokens/{provider}/authorize
// and returns the authorization_url. Mirrors doAuthorizeFlow but takes the
// provider id as a parameter.
func authorizeForProvider(t *testing.T, router *gin.Engine, providerID, clientCallback string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"client_callback": clientCallback})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/me/content_tokens/%s/authorize", providerID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "authorize: %s", rec.Body.String())
	var resp struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.NotEmpty(t, resp.AuthorizationURL)
	return resp.AuthorizationURL
}
