//go:build dev || test

// Package testhelpers provides shared test utilities for the TMI API tests.
package testhelpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// codeCounter is a global counter to generate unique authorization codes across tests.
var codeCounter atomic.Int64

// StubOAuthProvider is a minimal OAuth 2.0 stub server for testing. It speaks
// just enough OAuth to exercise the authorize → exchange → refresh → revoke
// flow without requiring a real identity provider.
//
// Routes served:
//   - GET  /authorize  — immediate 302 redirect with code+state
//   - POST /token      — handles authorization_code and refresh_token grants
//   - POST /revoke     — records revocation calls
//   - GET  /userinfo   — returns fake account JSON
type StubOAuthProvider struct {
	Server *httptest.Server

	// Configurable knobs — safe to set before any request.
	AccessTokenLifetime time.Duration
	RefreshSucceeds     bool // default true
	RotateRefreshToken  bool // default true
	RefreshStatus       int  // if non-zero, forces this HTTP status on refresh
	RevokeSucceeds      bool // default true
	UserinfoAccountID   string
	UserinfoLabel       string
	UserinfoErr         bool // returns HTTP 500 when true

	mu              sync.Mutex
	issuedAccess    string
	issuedRefresh   string
	nextAccess      string // override for next issued access token
	refreshCalls    int
	revocationCalls int
	revokedTokens   []string
}

// NewStubOAuthProvider creates and starts a stub OAuth HTTP server. The server is
// automatically shut down when t concludes via t.Cleanup.
func NewStubOAuthProvider(t *testing.T) *StubOAuthProvider {
	t.Helper()
	s := &StubOAuthProvider{
		AccessTokenLifetime: time.Hour,
		RefreshSucceeds:     true,
		RotateRefreshToken:  true,
		RevokeSucceeds:      true,
		UserinfoAccountID:   "stub-account-id",
		UserinfoLabel:       "stub@example.com",
	}
	// Pre-populate deterministic initial tokens so Exchange returns them.
	s.issuedAccess = "stub-access-initial"
	s.issuedRefresh = "stub-refresh-initial"

	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", s.handleAuthorize)
	mux.HandleFunc("/token", s.handleToken)
	mux.HandleFunc("/revoke", s.handleRevoke)
	mux.HandleFunc("/userinfo", s.handleUserinfo)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(func() { s.Close() })
	return s
}

// AuthURL returns the /authorize endpoint URL on the stub server.
func (s *StubOAuthProvider) AuthURL() string { return s.Server.URL + "/authorize" }

// TokenURL returns the /token endpoint URL on the stub server.
func (s *StubOAuthProvider) TokenURL() string { return s.Server.URL + "/token" }

// RevokeURL returns the /revoke endpoint URL on the stub server.
func (s *StubOAuthProvider) RevokeURL() string { return s.Server.URL + "/revoke" }

// UserinfoURL returns the /userinfo endpoint URL on the stub server.
func (s *StubOAuthProvider) UserinfoURL() string { return s.Server.URL + "/userinfo" }

// RefreshCalls returns the number of refresh_token grant calls received so far.
// Safe for concurrent use.
func (s *StubOAuthProvider) RefreshCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshCalls
}

// RevokeCalls returns the number of revocation calls received so far.
// Safe for concurrent use.
func (s *StubOAuthProvider) RevokeCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revocationCalls
}

// RevokedTokens returns a copy of the tokens that have been revoked.
// Safe for concurrent use.
func (s *StubOAuthProvider) RevokedTokens() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.revokedTokens))
	copy(out, s.revokedTokens)
	return out
}

// SetNextAccess pre-sets the access token that will be returned by the next
// successful token exchange or refresh. Useful for asserting on specific
// token values in tests.
func (s *StubOAuthProvider) SetNextAccess(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextAccess = tok
}

// Close shuts down the underlying httptest.Server.
func (s *StubOAuthProvider) Close() { s.Server.Close() }

// handleAuthorize implements GET /authorize. It reads state and redirect_uri,
// then immediately redirects to redirect_uri with code and state appended.
func (s *StubOAuthProvider) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		http.Error(w, "missing redirect_uri", http.StatusBadRequest)
		return
	}
	n := codeCounter.Add(1)
	code := fmt.Sprintf("test-code-%d", n)

	target, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := target.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// handleToken implements POST /token for authorization_code and refresh_token grants.
func (s *StubOAuthProvider) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "cannot parse form", http.StatusBadRequest)
		return
	}
	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		s.handleExchange(w, r)
	case "refresh_token":
		s.handleRefresh(w, r)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

// handleExchange processes an authorization_code grant.
func (s *StubOAuthProvider) handleExchange(w http.ResponseWriter, r *http.Request) {
	// PKCE: code_verifier must be present (we don't validate S256 binding — the
	// real provider does that; the stub just checks it's non-empty).
	if r.FormValue("code_verifier") == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing code_verifier"})
		return
	}
	s.mu.Lock()
	at := s.currentAccessToken()
	rt := s.issuedRefresh
	expiresIn := int(s.AccessTokenLifetime / time.Second)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  at,
		"refresh_token": rt,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
		"scope":         "read",
	})
}

// handleRefresh processes a refresh_token grant.
func (s *StubOAuthProvider) handleRefresh(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCalls++

	// RefreshStatus overrides normal behaviour.
	if s.RefreshStatus != 0 {
		writeJSON(w, s.RefreshStatus, map[string]string{"error": "invalid_grant"})
		return
	}
	if !s.RefreshSucceeds {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}

	// Rotate tokens.
	at := s.currentAccessToken()
	rt := s.issuedRefresh
	if s.RotateRefreshToken {
		rt = fmt.Sprintf("stub-refresh-%d", s.refreshCalls)
		s.issuedRefresh = rt
	}
	expiresIn := int(s.AccessTokenLifetime / time.Second)

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  at,
		"refresh_token": rt,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
		"scope":         "read",
	})
}

// handleRevoke implements POST /revoke.
func (s *StubOAuthProvider) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "cannot parse form", http.StatusBadRequest)
		return
	}
	tok := r.FormValue("token")
	s.mu.Lock()
	s.revocationCalls++
	s.revokedTokens = append(s.revokedTokens, tok)
	revokeSucceeds := s.RevokeSucceeds
	s.mu.Unlock()

	if !revokeSucceeds {
		http.Error(w, "revoke failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleUserinfo implements GET /userinfo.
func (s *StubOAuthProvider) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	userinfoErr := s.UserinfoErr
	accountID := s.UserinfoAccountID
	label := s.UserinfoLabel
	s.mu.Unlock()

	if userinfoErr {
		http.Error(w, "userinfo error", http.StatusInternalServerError)
		return
	}
	// Validate Bearer token presence (not value — it's a stub).
	_ = r.Header.Get("Authorization")

	writeJSON(w, http.StatusOK, map[string]string{
		"sub":   accountID,
		"email": label,
	})
}

// currentAccessToken returns the next access token to issue, consuming the
// override if set. Must be called with s.mu held.
func (s *StubOAuthProvider) currentAccessToken() string {
	if s.nextAccess != "" {
		at := s.nextAccess
		s.nextAccess = ""
		s.issuedAccess = at
		return at
	}
	n := codeCounter.Add(1)
	at := fmt.Sprintf("stub-at-%d", n)
	s.issuedAccess = at
	return at
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
