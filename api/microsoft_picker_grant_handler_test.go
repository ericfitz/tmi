package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPickerGrantGinContext builds a bare gin.Context backed by an httptest
// recorder. It does NOT go through a router, so gin.Param routing is
// unavailable — but the handler reads only request body and the result of
// userLookup, so this is sufficient.
func newPickerGrantGinContext(t *testing.T, method, path string, body *bytes.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	return c, rec
}

// timePtrGrant returns a pointer to the given time (avoids shadowing timePtr
// defined in picker_token_handler_test.go in the same package).
func timePtrGrant(t time.Time) *time.Time { return &t }

// newPickerGrantRepo builds a mockContentTokenRepo that serves a fixed token
// map keyed by "userID:providerID". It re-uses the shared mock from
// content_oauth_handlers_test.go.
func newPickerGrantRepo(tokens map[string]*ContentToken) *mockContentTokenRepo {
	return &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			if tok, ok := tokens[userID+":"+providerID]; ok {
				return tok, nil
			}
			return nil, ErrContentTokenNotFound
		},
	}
}

// =============================================================================
// Tests
// =============================================================================

// Case 1: Happy path — linked active token, Graph returns 201.
func TestMicrosoftPickerGrantHandler_Handle_Success(t *testing.T) {
	// Stub Graph: POST /drives/{driveId}/items/{itemId}/permissions → 201.
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/drives/b!abc/items/01XYZ/permissions", r.URL.Path)

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, []any{"read"}, body["roles"])

		grants, ok := body["grantedToIdentities"].([]any)
		require.True(t, ok)
		require.Len(t, grants, 1)
		entry := grants[0].(map[string]any)
		app := entry["application"].(map[string]any)
		assert.Equal(t, "tmi-app-object-id", app["id"])

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"perm-123"}`))
	}))
	defer graph.Close()

	repo := newPickerGrantRepo(map[string]*ContentToken{
		"u1:microsoft": {
			ID:          "tok-1",
			UserID:      "u1",
			ProviderID:  ProviderMicrosoft,
			AccessToken: "valid-token",
			Status:      ContentTokenStatusActive,
			ExpiresAt:   timePtrGrant(time.Now().Add(1 * time.Hour)),
		},
	})

	registry := NewContentOAuthProviderRegistry()
	h := NewMicrosoftPickerGrantHandler(repo, registry, "tmi-app-object-id", graph.URL,
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp MicrosoftPickerGrantResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "perm-123", resp.PermissionId)
	assert.Equal(t, "b!abc", resp.DriveId)
	assert.Equal(t, "01XYZ", resp.ItemId)
}

// Case 2: No applicationObjectID → 422 picker_grants_not_configured.
func TestMicrosoftPickerGrantHandler_Handle_NoAppObjectID(t *testing.T) {
	repo := newPickerGrantRepo(nil)
	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "", "",
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "picker_grants_not_configured", resp["code"])
}

// Case 3: No authenticated user → 401 unauthenticated.
func TestMicrosoftPickerGrantHandler_Handle_Unauthenticated(t *testing.T) {
	repo := newPickerGrantRepo(nil)
	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", "",
		func(_ *gin.Context) (string, bool) { return "", false })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "unauthenticated", resp["code"])
}

// Case 4: No linked Microsoft token → 404 not_linked.
func TestMicrosoftPickerGrantHandler_Handle_NotLinked(t *testing.T) {
	repo := newPickerGrantRepo(nil) // empty — no tokens
	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", "",
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "not_linked", resp["code"])
}

// Case 5: Graph returns 403 → 422 grant_failed.
func TestMicrosoftPickerGrantHandler_Handle_GraphReturns403(t *testing.T) {
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer graph.Close()

	repo := newPickerGrantRepo(map[string]*ContentToken{
		"u1:microsoft": {
			ID:          "tok-1",
			UserID:      "u1",
			ProviderID:  ProviderMicrosoft,
			AccessToken: "valid-token",
			Status:      ContentTokenStatusActive,
			ExpiresAt:   timePtrGrant(time.Now().Add(1 * time.Hour)),
		},
	})

	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", graph.URL,
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "grant_failed", resp["code"])
}

// Case 6: Token in failed_refresh status → 401 token_refresh_failed (no Graph call).
func TestMicrosoftPickerGrantHandler_Handle_FailedRefreshStatus(t *testing.T) {
	repo := newPickerGrantRepo(map[string]*ContentToken{
		"u1:microsoft": {
			ID:         "tok-2",
			UserID:     "u1",
			ProviderID: ProviderMicrosoft,
			Status:     ContentTokenStatusFailedRefresh,
			ExpiresAt:  timePtrGrant(time.Now().Add(1 * time.Hour)),
		},
	})

	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", "",
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "token_refresh_failed", resp["code"])
}

// Case 7: Missing drive_id → 400 invalid_request.
func TestMicrosoftPickerGrantHandler_Handle_MissingDriveID(t *testing.T) {
	repo := newPickerGrantRepo(map[string]*ContentToken{
		"u1:microsoft": {
			ID:          "tok-1",
			UserID:      "u1",
			ProviderID:  ProviderMicrosoft,
			AccessToken: "valid-token",
			Status:      ContentTokenStatusActive,
			ExpiresAt:   timePtrGrant(time.Now().Add(1 * time.Hour)),
		},
	})

	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", "",
		func(_ *gin.Context) (string, bool) { return "u1", true })

	// Only item_id provided; drive_id is empty.
	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "invalid_request", resp["code"])
}

// Case 8: Transport error reaching Graph (connection closed without response) → 503 transient_failure.
func TestMicrosoftPickerGrantHandler_Handle_TransportError(t *testing.T) {
	// Hijack the connection and close it immediately without writing a response,
	// so the HTTP client sees an EOF — a canonical transport-level failure.
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijacker not supported")
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close() // close without writing → client sees EOF
	}))
	defer graph.Close()

	repo := newPickerGrantRepo(map[string]*ContentToken{
		"u1:microsoft": {
			ID:          "tok-1",
			UserID:      "u1",
			ProviderID:  ProviderMicrosoft,
			AccessToken: "valid-token",
			Status:      ContentTokenStatusActive,
			ExpiresAt:   timePtrGrant(time.Now().Add(1 * time.Hour)),
		},
	})

	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", graph.URL,
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "transient_failure", resp["code"])
}

// Case 9: Graph returns 500 → 503 transient_failure.
func TestMicrosoftPickerGrantHandler_Handle_GraphReturns500(t *testing.T) {
	graph := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer graph.Close()

	repo := newPickerGrantRepo(map[string]*ContentToken{
		"u1:microsoft": {
			ID:          "tok-1",
			UserID:      "u1",
			ProviderID:  ProviderMicrosoft,
			AccessToken: "valid-token",
			Status:      ContentTokenStatusActive,
			ExpiresAt:   timePtrGrant(time.Now().Add(1 * time.Hour)),
		},
	})

	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", graph.URL,
		func(_ *gin.Context) (string, bool) { return "u1", true })

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "transient_failure", resp["code"])
}
