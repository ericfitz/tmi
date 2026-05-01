package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToken_ClientCredentials_LockoutReturns429 pins the end-to-end T15
// behavior: when the per-client_id failure counter crosses the lock
// threshold, /oauth2/token returns 429 with a numeric Retry-After header
// instead of 401, BEFORE the secret is checked. If a future refactor moves
// the bcrypt comparison ahead of the lockout check, an attacker regains
// the timing channel and this test fails.
func TestToken_ClientCredentials_LockoutReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	// Pre-populate the lockout counter to a hard-lock count.
	lockout := NewOAuthTokenLockout(client)
	for i := 0; i < 50; i++ {
		_, err := lockout.RecordFailure(context.Background(), "client:tmi_cc_locked")
		require.NoError(t, err)
	}

	h := &Handlers{}
	h.SetTokenLockout(lockout)
	router.POST("/oauth2/token", h.Token)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", "tmi_cc_locked")
	form.Set("client_secret", "any-value")

	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "too_many_requests")

	retryAfter := w.Header().Get("Retry-After")
	require.NotEmpty(t, retryAfter, "Retry-After header must be present")
	secs, err := strconv.Atoi(retryAfter)
	require.NoError(t, err, "Retry-After must be an integer (seconds)")
	assert.Greater(t, secs, 0, "Retry-After must be positive")
}

// TestToken_ClientCredentials_BelowThresholdNotLocked is the unit-level
// guard that a sub-threshold counter does not trip the lockout response.
// The full handler integration would require a real auth service; this
// asserts the same invariant at the lockout-check call site that the
// handler reads.
func TestToken_ClientCredentials_BelowThresholdNotLocked(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	lockout := NewOAuthTokenLockout(client)
	for i := 0; i < 4; i++ {
		_, err := lockout.RecordFailure(context.Background(), "client:newbie")
		require.NoError(t, err)
	}
	d := lockout.Check(context.Background(), "client:newbie")
	assert.False(t, d.Locked, "4 failures must not lock; threshold is 5")
	assert.Equal(t, int64(4), d.Count)
}
