//go:build dev || test || integration

package api

// TestMicrosoftDelegated_PickerGrantThenFetch exercises Experience 2 end-to-end:
// the user has linked their Microsoft account; tmi-ux POSTs to the picker-grant
// endpoint with (drive_id, item_id); the server calls the Graph permissions API to
// grant the TMI app per-file read access; subsequent fetches via
// DelegatedMicrosoftSource succeed against the same stub Graph server.
//
// TestMicrosoftDelegated_PasteURL_Forbidden exercises Experience 1 degraded
// state: the user pastes a SharePoint URL TMI doesn't have permission to,
// ValidateAccess returns false with no error (403 → (false, nil)).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Stub Graph server
// =============================================================================

// stubMicrosoftGraphServer is a minimal Microsoft Graph mock for the integration test.
// It handles:
//   - POST /drives/{driveId}/items/{itemId}/permissions  → 201 {"id":"perm-123"}
//   - GET  /drives/{driveId}/items/{itemId}              → metadata JSON
//   - GET  /drives/{driveId}/items/{itemId}/content      → file body
type stubMicrosoftGraphServer struct {
	server     *httptest.Server
	URL        string
	grantCalls map[string]bool // "driveID:itemID" → true
}

func newStubMicrosoftGraphServer(t *testing.T) *stubMicrosoftGraphServer {
	t.Helper()
	s := &stubMicrosoftGraphServer{grantCalls: map[string]bool{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// Grant call: POST /drives/{driveId}/items/{itemId}/permissions
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/permissions"):
			// Parse driveID/itemID from /drives/{driveId}/items/{itemId}/permissions.
			// Path segments: ["", "drives", driveId, "items", itemId, "permissions"]
			parts := strings.Split(r.URL.Path, "/")
			if len(parts) >= 6 {
				driveID := parts[2]
				itemID := parts[4]
				s.grantCalls[driveID+":"+itemID] = true
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"perm-123"}`))

		// Item content: GET /drives/{driveId}/items/{itemId}/content
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content"):
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("hello"))

		// Item metadata: GET /drives/{driveId}/items/{itemId}
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/drives/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"01XYZ","name":"hello.txt","file":{"mimeType":"text/plain"}}`))

		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	s.URL = s.server.URL
	t.Cleanup(s.server.Close)
	return s
}

func (s *stubMicrosoftGraphServer) hadGrantCall(driveID, itemID string) bool {
	return s.grantCalls[driveID+":"+itemID]
}

// =============================================================================
// Test: Experience 2 — picker grant + fetch
// =============================================================================

// TestMicrosoftDelegated_PickerGrantThenFetch exercises Experience 2 end-to-end:
//  1. A user's Microsoft token is seeded in the (mock) token repo.
//  2. POST /me/microsoft/picker_grants with (drive_id, item_id) calls Graph
//     permissions API on the stub server and returns the permission id.
//  3. DelegatedMicrosoftSource.fetchByDriveItem retrieves the file from the
//     same stub Graph server using the user's token.
func TestMicrosoftDelegated_PickerGrantThenFetch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Stub Graph server.
	graph := newStubMicrosoftGraphServer(t)

	// 2. Seed token repo with an active Microsoft token for user "u1".
	expiry := time.Now().Add(1 * time.Hour)
	tok := &ContentToken{
		ID:           "ms-tok-1",
		UserID:       "u1",
		ProviderID:   ProviderMicrosoft,
		AccessToken:  "valid-ms-token",
		RefreshToken: "ms-refresh-token",
		Status:       ContentTokenStatusActive,
		ExpiresAt:    &expiry,
	}
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			if userID == "u1" && providerID == ProviderMicrosoft {
				return tok, nil
			}
			return nil, ErrContentTokenNotFound
		},
	}

	// 3. Picker-grant handler invocation.
	grantHandler := NewMicrosoftPickerGrantHandler(
		repo,
		NewContentOAuthProviderRegistry(), // empty registry is fine — token is not expired
		"tmi-app-object-id",
		graph.URL,
		func(_ *gin.Context) (string, bool) { return "u1", true },
	)

	reqBody, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, http.MethodPost, "/me/microsoft/picker_grants", bytes.NewReader(reqBody))
	grantHandler.Handle(c)

	require.Equal(t, http.StatusOK, rec.Code, "picker-grant response: %s", rec.Body.String())

	var grantResp MicrosoftPickerGrantResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &grantResp))
	assert.Equal(t, "perm-123", grantResp.PermissionId, "permission id must match stub response")
	assert.Equal(t, "b!abc", grantResp.DriveId)
	assert.Equal(t, "01XYZ", grantResp.ItemId)
	assert.True(t, graph.hadGrantCall("b!abc", "01XYZ"),
		"picker-grant must have called Graph permissions API")

	// 4. DelegatedMicrosoftSource fetches file content using the same token
	//    and stub Graph server.  We call fetchByDriveItem directly to exercise
	//    the picker path (Graph /drives/{id}/items/{id}/content) without
	//    needing the share-id resolution that the public Fetch method uses.
	source := NewDelegatedMicrosoftSource(repo, NewContentOAuthProviderRegistry())
	source.GraphBaseURL = graph.URL
	source.httpClient = graph.server.Client()

	ctx := WithUserID(context.Background(), "u1")
	data, contentType, err := source.fetchByDriveItem(ctx, "valid-ms-token", "b!abc", "01XYZ")
	require.NoError(t, err)
	assert.Equal(t, "text/plain", contentType)
	assert.Equal(t, "hello", string(data))
}

// =============================================================================
// Test: Experience 1 (paste-URL) — ValidateAccess returns (false, nil) on 403
// =============================================================================

// TestMicrosoftDelegated_PasteURL_Forbidden exercises the Experience 1 degraded
// path: Graph returns 403 on /shares/{shareId}/driveItem (no per-file permission
// yet). ValidateAccess must return (false, nil) — not an error — so the
// document can be transitioned to pending_access with reason microsoft_not_shared.
func TestMicrosoftDelegated_PasteURL_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Stub Graph that returns 403 on any /shares/ endpoint.
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/shares/") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, "unexpected: "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(graph.Close)

	// Token repo with an active token.
	expiry := time.Now().Add(1 * time.Hour)
	tok := &ContentToken{
		ID:          "ms-tok-2",
		UserID:      "u1",
		ProviderID:  ProviderMicrosoft,
		AccessToken: "valid-ms-token",
		Status:      ContentTokenStatusActive,
		ExpiresAt:   &expiry,
	}
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			if userID == "u1" && providerID == ProviderMicrosoft {
				return tok, nil
			}
			return nil, ErrContentTokenNotFound
		},
	}

	source := NewDelegatedMicrosoftSource(repo, NewContentOAuthProviderRegistry())
	source.GraphBaseURL = graph.URL
	source.httpClient = graph.Client()

	ctx := WithUserID(context.Background(), "u1")
	accessible, err := source.ValidateAccess(ctx,
		"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx")
	require.NoError(t, err, "Graph 403 must yield (false, nil), not an error")
	assert.False(t, accessible, "ValidateAccess must return false when Graph returns 403")
}

// =============================================================================
// Test: Experience 1 (paste-URL) — Fetch returns error on 403
// =============================================================================

// TestMicrosoftDelegated_PasteURL_Fetch_Forbidden verifies that Fetch
// propagates the graph 403 as an error (distinct from ValidateAccess which
// swallows it). The caller decides how to surface the error.
func TestMicrosoftDelegated_PasteURL_Fetch_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	t.Cleanup(graph.Close)

	expiry := time.Now().Add(1 * time.Hour)
	tok := &ContentToken{
		ID:          "ms-tok-3",
		UserID:      "u1",
		ProviderID:  ProviderMicrosoft,
		AccessToken: "valid-ms-token",
		Status:      ContentTokenStatusActive,
		ExpiresAt:   &expiry,
	}
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			if userID == "u1" && providerID == ProviderMicrosoft {
				return tok, nil
			}
			return nil, ErrContentTokenNotFound
		},
	}

	source := NewDelegatedMicrosoftSource(repo, NewContentOAuthProviderRegistry())
	source.GraphBaseURL = graph.URL
	source.httpClient = graph.Client()

	ctx := WithUserID(context.Background(), "u1")
	_, _, err := source.Fetch(ctx,
		"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx")
	require.Error(t, err, "Fetch must return an error when Graph returns 403")
}

// =============================================================================
// Test: no linked token → ErrAuthRequired
// =============================================================================

// TestMicrosoftDelegated_NoToken_ValidateAccess verifies that ValidateAccess
// returns (false, ErrAuthRequired) when no Microsoft token is linked for the user.
func TestMicrosoftDelegated_NoToken_ValidateAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &mockContentTokenRepo{
		// default: returns ErrContentTokenNotFound
	}

	source := NewDelegatedMicrosoftSource(repo, NewContentOAuthProviderRegistry())

	ctx := WithUserID(context.Background(), "u1")
	accessible, err := source.ValidateAccess(ctx,
		"https://contoso.sharepoint.com/sites/Marketing/Doc.docx")
	assert.False(t, accessible)
	assert.ErrorIs(t, err, ErrAuthRequired,
		"missing token must surface as ErrAuthRequired, not a generic error")
}

// =============================================================================
// Test: picker-grant handler — not linked → 404
// =============================================================================

// TestMicrosoftDelegated_PickerGrant_NotLinked verifies that the picker-grant
// handler returns 404 with code "not_linked" when no Microsoft token exists.
func TestMicrosoftDelegated_PickerGrant_NotLinked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &mockContentTokenRepo{
		// default: returns ErrContentTokenNotFound
	}

	handler := NewMicrosoftPickerGrantHandler(
		repo,
		NewContentOAuthProviderRegistry(),
		"tmi-app-object-id",
		"http://graph-stub.local",
		func(_ *gin.Context) (string, bool) { return "u1", true },
	)

	reqBody, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, http.MethodPost, "/me/microsoft/picker_grants", bytes.NewReader(reqBody))
	handler.Handle(c)

	require.Equal(t, http.StatusNotFound, rec.Code, "response: %s", rec.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "not_linked", body["code"])
}
