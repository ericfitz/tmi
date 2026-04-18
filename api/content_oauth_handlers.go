package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ContentOAuthHandlers holds the dependencies for the /me/content_tokens/* and
// /oauth2/content_callback endpoints.
type ContentOAuthHandlers struct {
	Cfg           config.ContentOAuthConfig
	Registry      *ContentOAuthProviderRegistry
	StateStore    *ContentOAuthStateStore
	Tokens        ContentTokenRepository
	CallbackAllow *ClientCallbackAllowList
	// UserLookup extracts the caller's internal user UUID from the Gin context.
	// It returns ("", false) when no authenticated user is present.
	// This indirection keeps the handler independent of the specific auth
	// middleware implementation and makes it easy to stub in tests.
	UserLookup func(c *gin.Context) (userID string, ok bool)
}

// authorizeRequest is the JSON body for POST /me/content_tokens/{provider_id}/authorize.
type authorizeRequest struct {
	ClientCallback string `json:"client_callback"`
}

// authorizeResponse is the JSON response for a successful authorize request.
type authorizeResponse struct {
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// contentTokenInfo is the read-only view of a content token returned to callers.
// It deliberately omits access_token and refresh_token.
type contentTokenInfo struct {
	ProviderID           string     `json:"provider_id"`
	ProviderAccountID    string     `json:"provider_account_id,omitempty"`
	ProviderAccountLabel string     `json:"provider_account_label,omitempty"`
	Scopes               []string   `json:"scopes"`
	Status               string     `json:"status"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt        *time.Time `json:"last_refresh_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

// toContentTokenInfo converts a ContentToken domain value to the read-only API view.
func toContentTokenInfo(t ContentToken) contentTokenInfo {
	scopes := strings.Fields(t.Scopes)
	if scopes == nil {
		scopes = []string{}
	}
	return contentTokenInfo{
		ProviderID:           t.ProviderID,
		ProviderAccountID:    t.ProviderAccountID,
		ProviderAccountLabel: t.ProviderAccountLabel,
		Scopes:               scopes,
		Status:               t.Status,
		ExpiresAt:            t.ExpiresAt,
		LastRefreshAt:        t.LastRefreshAt,
		CreatedAt:            t.CreatedAt,
	}
}

// List handles GET /me/content_tokens.
// Returns 200 with {"content_tokens": [ContentTokenInfo]} or 401 if not authenticated.
func (h *ContentOAuthHandlers) List(c *gin.Context) {
	userID, ok := h.UserLookup(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	toks, err := h.Tokens.ListByUser(c.Request.Context(), userID)
	if err != nil {
		slogging.Get().WithContext(c).Error("list content tokens: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	out := make([]contentTokenInfo, 0, len(toks))
	for _, t := range toks {
		out = append(out, toContentTokenInfo(t))
	}
	c.JSON(http.StatusOK, gin.H{"content_tokens": out})
}

// Authorize handles POST /me/content_tokens/:provider_id/authorize.
// Validates the provider and client_callback, generates PKCE, stores state in Redis,
// and returns {authorization_url, expires_at}.
func (h *ContentOAuthHandlers) Authorize(c *gin.Context) {
	userID, ok := h.UserLookup(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	providerID := c.Param("provider_id")
	provider, ok := h.Registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":       "content_token_provider_not_configured",
			"provider_id": providerID,
		})
		return
	}

	var req authorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ClientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_callback_required"})
		return
	}
	if !h.CallbackAllow.Allowed(req.ClientCallback) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "client_callback_not_allowed"})
		return
	}

	verifier, err := NewPKCEVerifier()
	if err != nil {
		slogging.Get().WithContext(c).Error("generate PKCE verifier: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	ttl := 10 * time.Minute
	payload := ContentOAuthStatePayload{
		UserID:           userID,
		ProviderID:       providerID,
		ClientCallback:   req.ClientCallback,
		PKCECodeVerifier: verifier,
		CreatedAt:        time.Now(),
	}
	nonce, err := h.StateStore.Put(c.Request.Context(), payload, ttl)
	if err != nil {
		slogging.Get().WithContext(c).Error("put content oauth state: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	challenge := PKCES256Challenge(verifier)
	authURL := provider.AuthorizationURL(nonce, challenge, h.Cfg.CallbackURL)

	c.JSON(http.StatusOK, authorizeResponse{
		AuthorizationURL: authURL,
		ExpiresAt:        time.Now().Add(ttl),
	})
}

// Delete handles DELETE /me/content_tokens/:provider_id.
// Deletes the token and attempts provider-side revocation (best-effort).
// Returns 204 whether or not the row existed (idempotent).
func (h *ContentOAuthHandlers) Delete(c *gin.Context) {
	userID, ok := h.UserLookup(c)
	if !ok {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	providerID := c.Param("provider_id")
	tok, err := h.Tokens.DeleteByUserAndProvider(c.Request.Context(), userID, providerID)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			c.Status(http.StatusNoContent)
			return
		}
		slogging.Get().WithContext(c).Error("delete content token: %v", err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Best-effort provider-side revocation; failures are logged but do not affect the response.
	_ = h.revokeAtProvider(c, providerID, tok.AccessToken) // error already logged inside revokeAtProvider

	c.Status(http.StatusNoContent)
}

// Callback handles GET /oauth2/content_callback.
// This is a public endpoint (no auth middleware).
// It completes the OAuth authorization code flow, stores the resulting token,
// and redirects to the client_callback URL with status=success or status=error.
func (h *ContentOAuthHandlers) Callback(c *gin.Context) {
	ctx := c.Request.Context()
	logger := slogging.Get().WithContext(c)

	nonce := c.Query("state")
	if nonce == "" {
		renderCallbackError(c, "missing_state")
		return
	}

	state, err := h.StateStore.Consume(ctx, nonce)
	if err != nil {
		logger.Warn("content oauth callback: invalid/expired state: %v", err)
		renderCallbackError(c, "invalid_state")
		return
	}

	// Provider reported an error (e.g. user denied access).
	if perr := c.Query("error"); perr != "" {
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", perr)
		return
	}

	code := c.Query("code")
	if code == "" {
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "missing_code")
		return
	}

	provider, ok := h.Registry.Get(state.ProviderID)
	if !ok {
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "provider_not_configured")
		return
	}

	tok, err := provider.ExchangeCode(ctx, code, state.PKCECodeVerifier, h.Cfg.CallbackURL)
	if err != nil {
		logger.Error("content oauth exchange code provider=%s: %v", state.ProviderID, err)
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "token_exchange_failed")
		return
	}

	// Best-effort: if we can't fetch account info, we still store the token.
	accountID, label, _ := provider.FetchAccountInfo(ctx, tok.AccessToken)

	stored := &ContentToken{
		UserID:               state.UserID,
		ProviderID:           state.ProviderID,
		AccessToken:          tok.AccessToken,
		RefreshToken:         tok.RefreshToken,
		Scopes:               tok.Scope,
		ExpiresAt:            tok.ExpiresAt(),
		Status:               ContentTokenStatusActive,
		ProviderAccountID:    accountID,
		ProviderAccountLabel: label,
	}
	if err := h.Tokens.Upsert(ctx, stored); err != nil {
		logger.Error("content oauth token upsert provider=%s: %v", state.ProviderID, err)
		redirectClientCallback(c, state.ClientCallback, state.ProviderID, "error", "persist_failed")
		return
	}

	redirectClientCallback(c, state.ClientCallback, state.ProviderID, "success", "")
}

// renderCallbackError writes a minimal HTML error page with 400 status.
// This is used when we cannot redirect to a client_callback URL (e.g., missing or expired state).
func renderCallbackError(c *gin.Context, code string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusBadRequest,
		"<!doctype html><title>Content link error</title><p>An error occurred completing the content-provider link: %s.</p>",
		code)
}

// redirectClientCallback builds a redirect URL by appending status/error/provider_id query params
// to cb and issues a 302 redirect. It correctly handles cb URLs that already contain query params.
func redirectClientCallback(c *gin.Context, cb, providerID, status, errCode string) {
	q := url.Values{}
	q.Set("status", status)
	q.Set("provider_id", providerID)
	if errCode != "" {
		q.Set("error", errCode)
	}
	sep := "?"
	if strings.Contains(cb, "?") {
		sep = "&"
	}
	c.Redirect(http.StatusFound, cb+sep+q.Encode())
}
