//go:build dev || test

package testhelpers

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStubOAuthProvider_AuthorizeRedirects(t *testing.T) {
	stub := NewStubOAuthProvider(t)

	redirectURI := "http://localhost:9999/callback"
	state := "random-state-value"

	// Issue GET /authorize — httptest.Server follows the redirect automatically
	// unless we use a non-redirecting client.
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse // do not follow redirects
		},
	}

	req, err := http.NewRequest(http.MethodGet,
		stub.AuthURL()+"?state="+url.QueryEscape(state)+"&redirect_uri="+url.QueryEscape(redirectURI),
		nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusFound, resp.StatusCode)

	location := resp.Header.Get("Location")
	require.NotEmpty(t, location)

	parsed, err := url.Parse(location)
	require.NoError(t, err)

	q := parsed.Query()
	assert.Equal(t, state, q.Get("state"), "state must be echoed back")
	code := q.Get("code")
	assert.True(t, strings.HasPrefix(code, "test-code-"), "code must have expected prefix, got: %s", code)

	// Verify redirect target matches the configured redirect_uri.
	assert.Equal(t, "localhost:9999", parsed.Host)
	assert.Equal(t, "/callback", parsed.Path)
}

func TestStubOAuthProvider_ExchangeReturnsTokens(t *testing.T) {
	stub := NewStubOAuthProvider(t)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "test-code-1")
	form.Set("redirect_uri", "http://localhost/cb")
	form.Set("client_id", "test-client")
	form.Set("client_secret", "test-secret")
	form.Set("code_verifier", "some-pkce-verifier")

	resp, err := http.PostForm(stub.TokenURL(), form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), `"access_token"`)
	assert.Contains(t, string(body), `"refresh_token"`)
	assert.Contains(t, string(body), `"token_type"`)
	assert.Contains(t, string(body), `"expires_in"`)
}

func TestStubOAuthProvider_ExchangeRejectsMissingPKCE(t *testing.T) {
	stub := NewStubOAuthProvider(t)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "test-code-1")
	form.Set("client_id", "test-client")
	// deliberately no code_verifier

	resp, err := http.PostForm(stub.TokenURL(), form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestStubOAuthProvider_RefreshRotatesTokens(t *testing.T) {
	stub := NewStubOAuthProvider(t)
	stub.RotateRefreshToken = true

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "stub-refresh-initial")
	form.Set("client_id", "test-client")

	resp, err := http.PostForm(stub.TokenURL(), form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, stub.RefreshCalls(), "refresh call counter must be 1")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// With token rotation the returned refresh token should differ from initial.
	assert.Contains(t, string(body), `"refresh_token"`)
	// The rotated refresh token includes the call counter.
	assert.Contains(t, string(body), "stub-refresh-1")
}

func TestStubOAuthProvider_RefreshForced400(t *testing.T) {
	stub := NewStubOAuthProvider(t)
	stub.RefreshStatus = http.StatusBadRequest

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "some-refresh-token")
	form.Set("client_id", "test-client")

	resp, err := http.PostForm(stub.TokenURL(), form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, 1, stub.RefreshCalls(), "refresh call counter must still be incremented")
}

func TestStubOAuthProvider_RevokeRecorded(t *testing.T) {
	stub := NewStubOAuthProvider(t)

	tokenToRevoke := "my-access-token-abc"
	form := url.Values{}
	form.Set("token", tokenToRevoke)
	form.Set("client_id", "test-client")

	resp, err := http.PostForm(stub.RevokeURL(), form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, stub.RevokeCalls(), "revoke call counter must be 1")

	revokedTokens := stub.RevokedTokens()
	require.Len(t, revokedTokens, 1)
	assert.Equal(t, tokenToRevoke, revokedTokens[0])
}

func TestStubOAuthProvider_UserinfoReturnsAccountInfo(t *testing.T) {
	stub := NewStubOAuthProvider(t)
	stub.UserinfoAccountID = "user-999"
	stub.UserinfoLabel = "user999@example.com"

	req, err := http.NewRequest(http.MethodGet, stub.UserinfoURL(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer some-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "user-999")
	assert.Contains(t, string(body), "user999@example.com")
}

func TestStubOAuthProvider_UserinfoErr(t *testing.T) {
	stub := NewStubOAuthProvider(t)
	stub.UserinfoErr = true

	req, err := http.NewRequest(http.MethodGet, stub.UserinfoURL(), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
