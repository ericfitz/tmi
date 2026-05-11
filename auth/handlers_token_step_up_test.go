package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// These tests exercise the step-up identity-match branch in the /oauth2/token
// handler (#397). Rather than threading a stub Provider through the full
// handler, we test the two extracted helpers (`stepUpIdentityMatchAndRotate` and
// `stepUpAuditComplete`) directly. The Token handler invokes them only when
// the PKCE record carries step_up="true"; their behavior is self-contained.
//
// See plan: docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md Task 8.

func TestStepUpIdentityCheck_Match_BlacklistsAndReturnsTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t, withAuditWriter(auditW))
	defer h.cleanup()

	user := User{
		InternalUUID:   "uuid-original",
		Provider:       "google",
		ProviderUserID: "uid-alice",
		Email:          "alice@example.com",
		Name:           "Alice",
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/oauth2/token", strings.NewReader(""))
	c.Request.AddCookie(&http.Cookie{Name: RefreshTokenCookieName, Value: "old-refresh-token-xyz"})

	ok := h.handlers.stepUpIdentityMatchAndRotate(
		c, context.Background(), true, user, "google",
		"alice@example.com", // attempted (re-auth) email
		"uuid-original",     // original UUID — matches
		"alice@example.com", // original email
	)
	require.True(t, ok, "identity-match should return true")
	require.Equal(t, http.StatusOK, w.Code, "no response should have been written; default 200")
	require.Empty(t, auditW.entries, "no audit row on match (completion audit happens later)")
}

// When isStepUp is false the helper must be a strict no-op: no audit row, no
// blacklist, and proceed=true regardless of identity comparison.
func TestStepUpIdentityCheck_NotStepUp_IsNoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t, withAuditWriter(auditW))
	defer h.cleanup()

	user := User{InternalUUID: "uuid-anything"}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/oauth2/token", strings.NewReader(""))

	ok := h.handlers.stepUpIdentityMatchAndRotate(c, context.Background(), false, user, "google", "", "", "")
	require.True(t, ok)
	require.Empty(t, auditW.entries)
}

func TestStepUpIdentityCheck_Mismatch_Returns400AndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t, withAuditWriter(auditW))
	defer h.cleanup()

	// Re-authed user has a DIFFERENT InternalUUID than the original.
	user := User{
		InternalUUID:   "uuid-eve",
		Provider:       "google",
		ProviderUserID: "uid-eve",
		Email:          "eve@example.com",
		Name:           "Eve",
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/oauth2/token", strings.NewReader(""))

	ok := h.handlers.stepUpIdentityMatchAndRotate(
		c, context.Background(), true, user, "google",
		"eve@example.com",     // attempted (re-authed) email
		"uuid-alice-original", // original UUID — different
		"alice@example.com",   // original email
	)
	require.False(t, ok, "identity mismatch must return false")
	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "identity_mismatch")

	require.Len(t, auditW.entries, 1, "exactly one step_up_failed row should be written")
	require.Equal(t, "auth.step_up_failed", auditW.entries[0].FieldPath)
	require.NotNil(t, auditW.entries[0].NewValueRedacted)
	// attempted_email must be redacted (sha256_prefix envelope), not verbatim.
	require.Contains(t, *auditW.entries[0].NewValueRedacted, `sha256_prefix`)
	require.NotContains(t, *auditW.entries[0].NewValueRedacted, "eve@example.com")
	// Actor identity (original user) must be present.
	require.Equal(t, "alice@example.com", auditW.entries[0].ActorEmail)
	require.Equal(t, "google", auditW.entries[0].ActorProvider)
}

func TestStepUpAuditComplete_Strong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t, withAuditWriter(auditW))
	defer h.cleanup()

	user := User{
		InternalUUID:   "uuid-alice",
		Provider:       "google",
		ProviderUserID: "uid-alice",
		Email:          "alice@example.com",
		Name:           "Alice",
	}
	h.handlers.stepUpAuditComplete(context.Background(), true, user, "google", "strong")

	require.Len(t, auditW.entries, 1)
	require.Equal(t, "auth.step_up_complete", auditW.entries[0].FieldPath)
	require.NotNil(t, auditW.entries[0].NewValueRedacted)
	require.Contains(t, *auditW.entries[0].NewValueRedacted, `"strength":"strong"`)
	require.Contains(t, *auditW.entries[0].NewValueRedacted, `"mode":"round_trip"`)
	require.Equal(t, "alice@example.com", auditW.entries[0].ActorEmail)
	require.Equal(t, "google", auditW.entries[0].ActorProvider)
	require.Equal(t, "uid-alice", auditW.entries[0].ActorProviderID)
	require.Equal(t, "Alice", auditW.entries[0].ActorDisplayName)
}

func TestStepUpAuditComplete_Weak(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t, withAuditWriter(auditW))
	defer h.cleanup()

	user := User{
		InternalUUID:   "uuid-bob",
		Provider:       "github",
		ProviderUserID: "uid-bob",
		Email:          "bob@example.com",
		Name:           "Bob",
	}
	h.handlers.stepUpAuditComplete(context.Background(), true, user, "github", "weak")

	require.Len(t, auditW.entries, 1)
	require.Equal(t, "auth.step_up_complete", auditW.entries[0].FieldPath)
	require.NotNil(t, auditW.entries[0].NewValueRedacted)
	require.Contains(t, *auditW.entries[0].NewValueRedacted, `"strength":"weak"`)
	require.Contains(t, *auditW.entries[0].NewValueRedacted, `"mode":"round_trip"`)
}

// When isStepUp is false the completion helper must not write any audit row
// — this guards against accidentally double-auditing every successful token
// mint after the Token handler always-calls the helper.
func TestStepUpAuditComplete_NotStepUp_IsNoOp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auditW := &memorySystemAuditWriter{}
	h := newStepUpTestHarness(t, withAuditWriter(auditW))
	defer h.cleanup()

	user := User{Email: "alice@example.com", Provider: "google", ProviderUserID: "uid-alice"}
	h.handlers.stepUpAuditComplete(context.Background(), false, user, "google", "strong")
	require.Empty(t, auditW.entries)
}
