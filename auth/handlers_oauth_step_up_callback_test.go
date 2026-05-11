package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseCallbackState_ExtractsStepUpFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	state := "test-state-abc"
	stateMap := map[string]string{
		"provider":           "google",
		"client_callback":    "http://localhost:4200/callback",
		"step_up":            "true",
		"original_user_uuid": "uuid-original",
		"original_email":     "alice@example.com",
		"step_up_strength":   "strong",
	}
	stateJSON, err := json.Marshal(stateMap)
	require.NoError(t, err)

	ctx := context.Background()
	err = h.handlers.service.dbManager.Redis().Set(ctx, "oauth_state:"+state, string(stateJSON), 10*time.Minute)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/oauth2/callback?state="+state, nil)

	got, err := h.handlers.parseCallbackState(c, state)
	require.NoError(t, err)
	require.True(t, got.StepUp, "StepUp flag should be set")
	require.Equal(t, "uuid-original", got.OriginalUserUUID)
	require.Equal(t, "alice@example.com", got.OriginalEmail)
	require.Equal(t, "strong", got.StepUpStrength)
	require.Equal(t, "google", got.ProviderID)
	require.Equal(t, "http://localhost:4200/callback", got.ClientCallback)
}

func TestParseCallbackState_NonStepUpStateUnaffected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	state := "test-state-normal"
	stateMap := map[string]string{
		"provider":        "google",
		"client_callback": "http://localhost:4200/callback",
		"login_hint":      "alice",
	}
	stateJSON, err := json.Marshal(stateMap)
	require.NoError(t, err)

	ctx := context.Background()
	err = h.handlers.service.dbManager.Redis().Set(ctx, "oauth_state:"+state, string(stateJSON), 10*time.Minute)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/oauth2/callback?state="+state, nil)

	got, err := h.handlers.parseCallbackState(c, state)
	require.NoError(t, err)
	require.False(t, got.StepUp, "non-step-up state should not flip the flag")
	require.Empty(t, got.OriginalUserUUID)
	require.Empty(t, got.OriginalEmail)
	require.Empty(t, got.StepUpStrength)
	require.Equal(t, "alice", got.UserHint)
}

func TestCallback_StepUpUpstreamAccessDenied_AuditsAndRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	state := "test-state-denied"
	stateMap := map[string]string{
		"provider":           "google",
		"client_callback":    "http://localhost:4200/callback",
		"step_up":            "true",
		"original_user_uuid": "uuid-original",
		"original_email":     "alice@example.com",
		"step_up_strength":   "strong",
	}
	stateJSON, err := json.Marshal(stateMap)
	require.NoError(t, err)

	ctx := context.Background()
	err = h.handlers.service.dbManager.Redis().Set(ctx, "oauth_state:"+state, string(stateJSON), 10*time.Minute)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/callback?error=access_denied&state="+state, nil)

	h.handlers.Callback(c)

	require.Equal(t, 302, w.Code, "expected redirect to client_callback")
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "error=access_denied")
	require.Contains(t, loc, "state=test-state-denied")

	// Audit row should record step_up_failed/access_denied.
	require.NotNil(t, h.auditW)
	require.Len(t, h.auditW.entries, 1)
	require.Equal(t, "auth.step_up_failed", h.auditW.entries[0].FieldPath)
	require.Contains(t, *h.auditW.entries[0].NewValueRedacted, `"reason":"access_denied"`)
}

func TestProcessOAuthCallback_CopiesStepUpMarkerIntoPKCERecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	state := "test-state-xyz"
	stateMap := map[string]string{
		"provider":           "google",
		"client_callback":    "http://localhost:4200/callback",
		"step_up":            "true",
		"original_user_uuid": "uuid-original",
		"original_email":     "alice@example.com",
		"step_up_strength":   "strong",
	}
	stateJSON, err := json.Marshal(stateMap)
	require.NoError(t, err)

	ctx := context.Background()
	err = h.handlers.service.dbManager.Redis().Set(ctx, "oauth_state:"+state, string(stateJSON), 10*time.Minute)
	require.NoError(t, err)
	require.NoError(t, h.handlers.service.stateStore.StorePKCEChallenge(ctx, state,
		"challenge-abc", "S256", 10*time.Minute))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/callback?code=test-code-7&state="+state, nil)

	h.handlers.Callback(c)

	// Callback redirects to client_callback on success; status must be 302 (not error).
	require.Equal(t, http.StatusFound, w.Code, "callback should redirect; body=%s", w.Body.String())

	// The PKCE record at pkce:<code> must now carry step-up fields.
	pkceJSON, err := h.handlers.service.dbManager.Redis().Get(ctx, "pkce:test-code-7")
	require.NoError(t, err)
	var pkceMap map[string]string
	require.NoError(t, json.Unmarshal([]byte(pkceJSON), &pkceMap))
	require.Equal(t, "true", pkceMap["step_up"])
	require.Equal(t, "uuid-original", pkceMap["original_user_uuid"])
	require.Equal(t, "alice@example.com", pkceMap["original_email"])
	require.Equal(t, "strong", pkceMap["step_up_strength"])
	require.Equal(t, "challenge-abc", pkceMap["code_challenge"])
	require.Equal(t, "S256", pkceMap["code_challenge_method"])
}

func TestCallback_NonStepUpUpstreamError_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newStepUpTestHarness(t)
	defer h.cleanup()

	// No state stored; just an upstream error arriving on /oauth2/callback.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET",
		"/oauth2/callback?error=server_error&state=nonexistent-state",
		nil)

	h.handlers.Callback(c)

	require.Equal(t, http.StatusBadRequest, w.Code, "non-step-up upstream error should return 400; body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "server_error")

	// No audit row should be written for non-step-up errors.
	require.Empty(t, h.auditW.entries, "no step-up audit row for non-step-up flows")
}
