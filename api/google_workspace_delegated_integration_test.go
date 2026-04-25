//go:build dev || test || integration

package api

// TestGoogleWorkspaceDelegated_EndToEnd_Integration exercises the Google
// Workspace delegated-picker surface end-to-end:
//
//  1. Authorize a content token for google_workspace (drive PKCE → callback → token row).
//  2. Mint a picker token via POST /me/picker_tokens/google_workspace.
//  3. Attach a document with picker_registration → DB row has picker columns set,
//     access_status = "unknown".
//  4. GET that document → response carries access_diagnostics and
//     access_status_updated_at when access is not yet confirmed.
//  5. Build per-viewer diagnostics for no_accessible_source reason — caller has
//     google_workspace linked → exactly 2 remediations in order:
//     share_with_service_account, repick_after_share.
//  6. Un-link cascade: DELETE /me/content_tokens/google_workspace → owned picker
//     docs revert to access_status = "unknown" and have NULL picker columns.
//  7. Multi-user view: user A picker-attaches doc; user B (with no linked token)
//     views → access_diagnostics.remediations[0].action == "share_with_service_account",
//     no repick_after_share (because B has no token).
//
// Requires: TEST_DB_* and TEST_REDIS_* env vars pointing at a live PostgreSQL +
// Redis instance. Falls back to SQLite + miniredis in unit-test mode, but
// SELECT ... FOR UPDATE serialization is not exercised there. The DoFetch
// callback on the integration source is mocked so no real Google Drive API
// calls are made.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/testhelpers"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// =============================================================================
// gwIntegrationInfra — per-test infrastructure bundle
// =============================================================================

type gwIntegrationInfra struct {
	t                 *testing.T
	db                *gorm.DB
	tokenRepo         *GormContentTokenRepository
	registry          *ContentOAuthProviderRegistry
	docStore          *GormDocumentStore
	docHandler        *DocumentSubResourceHandler
	contentOAuthH     *ContentOAuthHandlers
	pickerHandler     *PickerTokenHandler
	stub              *testhelpers.StubOAuthProvider
	userID            string // InternalUUID for the primary test user
	clientCallbackURL string
}

const (
	gwTestServiceAccountEmail = "indexer@tmi.iam.gserviceaccount.com"
	gwTestPickerDeveloperKey  = "AIza-test-dev-key"
	gwTestPickerAppID         = "123456789"
	gwTestClientCallback      = "http://localhost:55456/gw-cb"
)

// newGWIntegrationInfra wires up the complete integration infrastructure for
// a single parent test. Call this once per parent test; sub-tests share infra.
func newGWIntegrationInfra(t *testing.T) *gwIntegrationInfra {
	t.Helper()

	// ---- DB ----------------------------------------------------------------
	db := openIntegrationDB(t)

	// Also migrate Document and ThreatModel tables (openIntegrationDB only
	// migrates User + UserContentToken).
	require.NoError(t, db.AutoMigrate(&models.ThreatModel{}, &models.Document{}),
		"AutoMigrate Document + ThreatModel")

	// ---- Redis / Token repo ------------------------------------------------
	rdb := openIntegrationRedis(t)

	enc := newIntegrationTestEncryptor(t)
	tokenRepo := NewGormContentTokenRepository(db, enc)

	// ---- Stub OAuth provider registered under "google_workspace" ----------
	stub := testhelpers.NewStubOAuthProvider(t)

	providerCfg := config.ContentOAuthProviderConfig{
		ClientID:       "gw-integration-cid",
		ClientSecret:   "gw-integration-sec",
		AuthURL:        stub.AuthURL(),
		TokenURL:       stub.TokenURL(),
		RevocationURL:  stub.RevokeURL(),
		UserinfoURL:    stub.UserinfoURL(),
		RequiredScopes: []string{"https://www.googleapis.com/auth/drive.file"},
	}
	gwProvider := NewBaseContentOAuthProvider(ProviderGoogleWorkspace, providerCfg)

	registry := NewContentOAuthProviderRegistry()
	registry.Register(gwProvider)

	// ---- Users -------------------------------------------------------------
	userID := createIntegrationUser(t, db, "gw-primary")

	// ---- Document store + handler ----------------------------------------
	docStore := NewGormDocumentStore(db, nil, nil)

	docHandler := NewDocumentSubResourceHandler(docStore, nil, nil, nil)
	docHandler.SetContentTokens(tokenRepo)
	docHandler.SetServiceAccountEmail(gwTestServiceAccountEmail)
	docHandler.SetContentOAuthRegistry(registry)

	// ---- Picker token handler ---------------------------------------------
	pickerConfigs := map[string]PickerTokenConfig{
		ProviderGoogleWorkspace: {
			DeveloperKey: gwTestPickerDeveloperKey,
			AppID:        gwTestPickerAppID,
		},
	}

	// The test server URL is only needed for deriving a callback URL; actual
	// routing goes through httptest.NewRecorder so any non-empty base is fine.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(ts.Close)
	callbackURL := ts.URL + "/oauth2/content_callback"

	stateStore := NewContentOAuthStateStore(rdb)
	allowList := NewClientCallbackAllowList([]string{gwTestClientCallback, gwTestClientCallback + "*"})

	contentOAuthH := &ContentOAuthHandlers{
		Cfg: config.ContentOAuthConfig{
			CallbackURL:            callbackURL,
			AllowedClientCallbacks: []string{gwTestClientCallback, gwTestClientCallback + "*"},
		},
		Registry:      registry,
		StateStore:    stateStore,
		Tokens:        tokenRepo,
		CallbackAllow: allowList,
		Documents:     docStore,
		UserLookup: func(c *gin.Context) (string, bool) {
			uid, _ := c.Get("userInternalUUID")
			s, ok := uid.(string)
			return s, ok && s != ""
		},
	}

	pickerHandler := NewPickerTokenHandler(
		tokenRepo,
		registry,
		pickerConfigs,
		func(c *gin.Context) (string, bool) {
			uid, _ := c.Get("userInternalUUID")
			s, ok := uid.(string)
			return s, ok && s != ""
		},
	)

	infra := &gwIntegrationInfra{
		t:                 t,
		db:                db,
		tokenRepo:         tokenRepo,
		registry:          registry,
		docStore:          docStore,
		docHandler:        docHandler,
		contentOAuthH:     contentOAuthH,
		pickerHandler:     pickerHandler,
		stub:              stub,
		userID:            userID,
		clientCallbackURL: gwTestClientCallback,
	}
	return infra
}

// =============================================================================
// Per-test fixture helpers
// =============================================================================

// createThreatModelRow inserts a minimal ThreatModel row via GORM.
// ownerInternalUUID must already exist in users table.
func (i *gwIntegrationInfra) createThreatModelRow(t *testing.T, ownerInternalUUID string) string {
	t.Helper()
	tmID := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    tmID,
		OwnerInternalUUID:     ownerInternalUUID,
		CreatedByInternalUUID: ownerInternalUUID,
		Name:                  "GW integration test TM",
		ThreatModelFramework:  "STRIDE",
	}
	require.NoError(t, i.db.Create(tm).Error, "create ThreatModel fixture")
	t.Cleanup(func() {
		i.db.Where("id = ?", tmID).Delete(&models.ThreatModel{}) //nolint:errcheck
	})
	return tmID
}

// insertDocumentRow inserts a Document row directly via GORM (bypassing
// BeforeSave hooks) and returns the row ID. Used for diagnostics sub-tests
// that need a pre-seeded document without going through CreateDocument.
func (i *gwIntegrationInfra) insertDocumentRow(
	t *testing.T,
	tmID string,
	overrides map[string]interface{},
) string {
	t.Helper()
	docID := uuid.New().String()
	now := time.Now().UTC()

	// Base values; caller can override via overrides map.
	base := map[string]interface{}{
		"id":              docID,
		"threat_model_id": tmID,
		"name":            "integration test doc",
		"uri":             "https://docs.google.com/document/d/abc123/edit",
		"created_at":      now,
		"modified_at":     now,
	}
	for k, v := range overrides {
		base[k] = v
	}
	require.NoError(t,
		i.db.Table("documents").Create(base).Error,
		"insert document fixture",
	)
	t.Cleanup(func() {
		i.db.Table("documents").Where("id = ?", docID).Delete(nil) //nolint:errcheck
	})
	return docID
}

// authorizeGWToken runs the full authorize → callback flow for the given user
// and returns after verifying the token row is persisted. Returns the token.
func (i *gwIntegrationInfra) authorizeGWToken(t *testing.T, userID string) *ContentToken {
	t.Helper()
	ctx := context.Background()

	// The router reads userInternalUUID from context; we need it for Authorize.
	// We post with the right Gin context key pre-set via the request.
	// Since we're using httptest, we set it as a header and the middleware
	// would normally resolve it, but our test middleware reads from context.
	// Instead we build a small per-user router slice.

	// We POST to authorize.
	body, _ := json.Marshal(map[string]string{"client_callback": i.clientCallbackURL})
	req := httptest.NewRequest(http.MethodPost,
		"/me/content_tokens/"+ProviderGoogleWorkspace+"/authorize",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	// Inject userInternalUUID directly into the Gin context before the handler
	// runs. We do this by building a sub-router that sets the context key.
	subR := gin.New()
	subR.Use(func(c *gin.Context) {
		c.Set("userInternalUUID", userID)
		c.Set("userEmail", userID+"@tmi.test")
		c.Set("userID", userID+"-provider")
		c.Set("userRole", RoleWriter)
		c.Next()
	})
	subR.POST("/me/content_tokens/:provider_id/authorize", i.contentOAuthH.Authorize)
	subR.GET("/oauth2/content_callback", i.contentOAuthH.Callback)

	subR.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "authorize: %s", rec.Body.String())

	var authResp struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&authResp))
	require.NotEmpty(t, authResp.AuthorizationURL)

	// Follow auth URL → callback.
	followAuthorizeAndCallback(t, authResp.AuthorizationURL, subR)

	// Assert token persisted.
	tok, err := i.tokenRepo.GetByUserAndProvider(ctx, userID, ProviderGoogleWorkspace)
	require.NoError(t, err, "token must be persisted after callback")
	require.Equal(t, ContentTokenStatusActive, tok.Status)
	return tok
}

// newSubRouter returns a Gin router that injects the given userInternalUUID
// into the context, suitable for multi-user view tests.
func (i *gwIntegrationInfra) newSubRouter(userID string) *gin.Engine {
	i.t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userInternalUUID", userID)
		c.Set("userEmail", userID+"@tmi.test")
		c.Set("userID", userID+"-provider")
		c.Set("userRole", RoleWriter)
		c.Next()
	})
	r.GET("/threat_models/:threat_model_id/documents/:document_id", i.docHandler.GetDocument)
	r.POST("/threat_models/:threat_model_id/documents", i.docHandler.CreateDocument)
	r.POST("/me/picker_tokens/:provider_id", i.pickerHandler.Handle)
	r.POST("/me/content_tokens/:provider_id/authorize", i.contentOAuthH.Authorize)
	r.GET("/oauth2/content_callback", i.contentOAuthH.Callback)
	r.DELETE("/me/content_tokens/:provider_id", i.contentOAuthH.Delete)
	return r
}

// =============================================================================
// Parent test
// =============================================================================

func TestGoogleWorkspaceDelegated_EndToEnd_Integration(t *testing.T) {
	infra := newGWIntegrationInfra(t)

	// =========================================================================
	// Sub-test 1: AuthorizeAndCallback_PersistsToken
	// =========================================================================
	t.Run("AuthorizeAndCallback_PersistsToken", func(t *testing.T) {
		ctx := context.Background()

		infra.stub.SetNextAccess("gw-at-auth-1")
		tok := infra.authorizeGWToken(t, infra.userID)

		// Re-read to confirm persistence.
		tok2, err := infra.tokenRepo.GetByUserAndProvider(ctx, infra.userID, ProviderGoogleWorkspace)
		require.NoError(t, err, "GetByUserAndProvider must succeed")
		assert.Equal(t, ContentTokenStatusActive, tok2.Status,
			"token status must be active after callback")
		assert.Equal(t, tok.AccessToken, tok2.AccessToken,
			"access token must match what was authorized")
	})

	// =========================================================================
	// Sub-test 2: MintPickerToken_HappyPath
	// =========================================================================
	t.Run("MintPickerToken_HappyPath", func(t *testing.T) {
		// Ensure token is linked (authorizes if not already present).
		infra.authorizeGWToken(t, infra.userID)

		r := infra.newSubRouter(infra.userID)
		req := httptest.NewRequest(http.MethodPost,
			"/me/picker_tokens/"+ProviderGoogleWorkspace,
			http.NoBody)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code,
			"picker token: %s", rec.Body.String())

		var resp struct {
			AccessToken  string    `json:"access_token"`
			ExpiresAt    time.Time `json:"expires_at"`
			DeveloperKey string    `json:"developer_key"`
			AppID        string    `json:"app_id"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.NotEmpty(t, resp.AccessToken, "access_token must be non-empty")
		assert.True(t, resp.ExpiresAt.After(time.Now()),
			"expires_at must be in the future; got %v", resp.ExpiresAt)
		assert.Equal(t, gwTestPickerDeveloperKey, resp.DeveloperKey)
		assert.Equal(t, gwTestPickerAppID, resp.AppID)
	})

	// =========================================================================
	// Sub-test 3: AttachDocumentWithPicker_PersistsPickerColumns
	// =========================================================================
	t.Run("AttachDocumentWithPicker_PersistsPickerColumns", func(t *testing.T) {
		// Pre-condition: token authorized for this user.
		infra.authorizeGWToken(t, infra.userID)

		tmID := infra.createThreatModelRow(t, infra.userID)

		reqBody := map[string]interface{}{
			"name": "GW Picker Doc",
			"uri":  "https://docs.google.com/document/d/abc123/edit",
			"picker_registration": map[string]interface{}{
				"provider_id": ProviderGoogleWorkspace,
				"file_id":     "abc123",
				"mime_type":   "application/vnd.google-apps.document",
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		r := infra.newSubRouter(infra.userID)
		req := httptest.NewRequest(http.MethodPost,
			"/threat_models/"+tmID+"/documents",
			bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code,
			"create document with picker_registration: %s", rec.Body.String())

		// Parse the created document ID from the response.
		var created struct {
			ID            string `json:"id"`
			AccessStatus  string `json:"access_status"`
			ContentSource string `json:"content_source"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))
		require.NotEmpty(t, created.ID, "response must include document id")

		// Verify picker columns in DB directly.
		var raw models.Document
		require.NoError(t,
			infra.db.Table("documents").Where("id = ?", created.ID).Take(&raw).Error,
			"document row must exist",
		)
		assert.NotNil(t, raw.PickerProviderID, "picker_provider_id must be set")
		if raw.PickerProviderID != nil {
			assert.Equal(t, ProviderGoogleWorkspace, *raw.PickerProviderID)
		}
		assert.NotNil(t, raw.PickerFileID, "picker_file_id must be set")
		if raw.PickerFileID != nil {
			assert.Equal(t, "abc123", *raw.PickerFileID)
		}
		assert.NotNil(t, raw.PickerMimeType, "picker_mime_type must be set")

		assert.NotNil(t, raw.AccessStatus, "access_status must be set")
		if raw.AccessStatus != nil {
			assert.Equal(t, AccessStatusUnknown, *raw.AccessStatus,
				"access_status must be 'unknown' after picker attach")
		}
		assert.NotNil(t, raw.ContentSource, "content_source must be set")
		if raw.ContentSource != nil {
			assert.Equal(t, ProviderGoogleWorkspace, *raw.ContentSource)
		}
	})

	// =========================================================================
	// Sub-test 4: GetDocument_DiagnosticsShape_NoAccessibleSource_TwoRemediations
	// =========================================================================
	t.Run("GetDocument_DiagnosticsShape_NoAccessibleSource_TwoRemediations", func(t *testing.T) {
		// Ensure caller has an active token.
		infra.authorizeGWToken(t, infra.userID)

		tmID := infra.createThreatModelRow(t, infra.userID)

		// Insert a document with access_status = pending_access and reason_code set.
		now := time.Now().UTC()
		docID := infra.insertDocumentRow(t, tmID, map[string]interface{}{
			"uri":                      "https://docs.google.com/document/d/diag-abc/edit",
			"access_status":            AccessStatusPendingAccess,
			"content_source":           ProviderGoogleWorkspace,
			"access_reason_code":       ReasonNoAccessibleSource,
			"access_status_updated_at": now,
		})

		r := infra.newSubRouter(infra.userID)
		req := httptest.NewRequest(http.MethodGet,
			"/threat_models/"+tmID+"/documents/"+docID, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "GET document: %s", rec.Body.String())

		var doc struct {
			AccessStatus          string     `json:"access_status"`
			AccessStatusUpdatedAt *time.Time `json:"access_status_updated_at"`
			AccessDiagnostics     *struct {
				ReasonCode   string `json:"reason_code"`
				Remediations []struct {
					Action string                 `json:"action"`
					Params map[string]interface{} `json:"params"`
				} `json:"remediations"`
			} `json:"access_diagnostics"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))

		assert.Equal(t, AccessStatusPendingAccess, doc.AccessStatus)
		assert.NotNil(t, doc.AccessStatusUpdatedAt, "access_status_updated_at must be present")

		require.NotNil(t, doc.AccessDiagnostics, "access_diagnostics must be present")
		assert.Equal(t, ReasonNoAccessibleSource, doc.AccessDiagnostics.ReasonCode)
		require.Len(t, doc.AccessDiagnostics.Remediations, 2,
			"caller with linked token must get 2 remediations")
		assert.Equal(t, RemediationShareWithServiceAccount,
			doc.AccessDiagnostics.Remediations[0].Action,
			"first remediation must be share_with_service_account")
		params0 := doc.AccessDiagnostics.Remediations[0].Params
		assert.Equal(t, gwTestServiceAccountEmail, params0["service_account_email"],
			"service_account_email must match configured address")

		assert.Equal(t, RemediationRepickAfterShare,
			doc.AccessDiagnostics.Remediations[1].Action,
			"second remediation must be repick_after_share")
	})

	// =========================================================================
	// Sub-test 5: UnlinkCascade_ClearsPickerColumns
	// =========================================================================
	t.Run("UnlinkCascade_ClearsPickerColumns", func(t *testing.T) {
		// Pre-condition: user has an active token.
		infra.authorizeGWToken(t, infra.userID)

		tmID := infra.createThreatModelRow(t, infra.userID)

		// Insert a document with picker metadata pre-set (simulating picker-attach).
		provID := ProviderGoogleWorkspace
		fileID := "cascade-file-456"
		mimeType := "application/vnd.google-apps.document"
		accessStatus := AccessStatusAccessible

		docID := infra.insertDocumentRow(t, tmID, map[string]interface{}{
			"uri":                "https://docs.google.com/document/d/cascade-file-456/edit",
			"picker_provider_id": provID,
			"picker_file_id":     fileID,
			"picker_mime_type":   mimeType,
			"access_status":      accessStatus,
			"content_source":     provID,
		})

		// DELETE /me/content_tokens/google_workspace
		r := infra.newSubRouter(infra.userID)
		req := httptest.NewRequest(http.MethodDelete,
			"/me/content_tokens/"+ProviderGoogleWorkspace, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code,
			"DELETE content token: %s", rec.Body.String())

		// Verify picker columns are NULL and access_status = "unknown".
		var raw models.Document
		require.NoError(t,
			infra.db.Table("documents").Where("id = ?", docID).Take(&raw).Error,
			"document row must still exist",
		)
		assert.Nil(t, raw.PickerProviderID, "picker_provider_id must be cleared after un-link")
		assert.Nil(t, raw.PickerFileID, "picker_file_id must be cleared after un-link")
		assert.Nil(t, raw.PickerMimeType, "picker_mime_type must be cleared after un-link")
		require.NotNil(t, raw.AccessStatus)
		assert.Equal(t, AccessStatusUnknown, *raw.AccessStatus,
			"access_status must revert to 'unknown' after un-link cascade")

		// Confirm token is gone.
		ctx := context.Background()
		_, err := infra.tokenRepo.GetByUserAndProvider(ctx, infra.userID, ProviderGoogleWorkspace)
		assert.ErrorIs(t, err, ErrContentTokenNotFound,
			"token must be deleted after un-link")
	})

	// =========================================================================
	// Sub-test 6: MultiUserView_PerViewerRemediations
	// =========================================================================
	t.Run("MultiUserView_PerViewerRemediations", func(t *testing.T) {
		// User A is the document owner with an active token.
		userA := infra.userID
		infra.authorizeGWToken(t, userA)

		// User B has no linked token.
		userB := createIntegrationUser(t, infra.db, "gw-viewer-b")

		tmID := infra.createThreatModelRow(t, userA)

		// Insert a document with access_status = pending_access, reason_code set.
		now := time.Now().UTC()
		docID := infra.insertDocumentRow(t, tmID, map[string]interface{}{
			"uri":                      "https://docs.google.com/document/d/multi-user-abc/edit",
			"access_status":            AccessStatusPendingAccess,
			"content_source":           ProviderGoogleWorkspace,
			"access_reason_code":       ReasonNoAccessibleSource,
			"access_status_updated_at": now,
		})

		// User B views the document.
		rB := infra.newSubRouter(userB)
		req := httptest.NewRequest(http.MethodGet,
			"/threat_models/"+tmID+"/documents/"+docID, nil)
		rec := httptest.NewRecorder()
		rB.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code,
			"user B GET document: %s", rec.Body.String())

		var doc struct {
			AccessStatus      string `json:"access_status"`
			AccessDiagnostics *struct {
				ReasonCode   string `json:"reason_code"`
				Remediations []struct {
					Action string `json:"action"`
				} `json:"remediations"`
			} `json:"access_diagnostics"`
		}
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&doc))

		require.NotNil(t, doc.AccessDiagnostics,
			"user B must see access_diagnostics")
		assert.Equal(t, ReasonNoAccessibleSource, doc.AccessDiagnostics.ReasonCode)
		require.Len(t, doc.AccessDiagnostics.Remediations, 1,
			"user B (no linked token) must see exactly 1 remediation")
		assert.Equal(t, RemediationShareWithServiceAccount,
			doc.AccessDiagnostics.Remediations[0].Action,
			"user B must see share_with_service_account remediation")

		// Ensure repick_after_share is NOT present for user B.
		for _, r := range doc.AccessDiagnostics.Remediations {
			assert.NotEqual(t, RemediationRepickAfterShare, r.Action,
				"user B must NOT see repick_after_share (no linked token)")
		}
	})
}
