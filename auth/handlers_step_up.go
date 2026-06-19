// Package auth — /oauth2/step_up handler (#397).
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// StepUp is the GET /oauth2/step_up handler. See
// docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md.
//
// This is the strong-provider path only. The weak-provider short-circuit
// (rotate-in-place) is implemented in Task 6.
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: handle step-up authentication request, routing to weak or strong re-auth path
func (h *Handlers) StepUp(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// 1. Extract JWT (header > cookie priority, same as JWTMiddleware).
	tokenStr, ok := h.readStepUpJWT(c)
	if !ok {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_token",
			"error_description": "Missing or invalid access token",
		})
		return
	}

	claims, err := h.service.ValidateToken(tokenStr)
	if err != nil {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_token",
			"error_description": "Token validation failed",
		})
		return
	}

	actor := StepUpActor{
		Email:          claims.Email,
		Provider:       claims.IdentityProvider,
		ProviderUserID: claims.Subject,
		DisplayName:    claims.Name,
	}

	// 2. Client-credentials rejection.
	if strings.HasPrefix(claims.Subject, "sa:") {
		_ = h.stepUpAud().LogRejected(c.Request.Context(), actor, "unsupported_grant_type",
			map[string]string{"subject_prefix": "sa"})
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_grant_type",
			"error_description": "Step-up does not apply to client credentials grants",
		})
		return
	}

	// 3. Provider lookup.
	providerID := claims.IdentityProvider
	provider, err := h.getProviderWithContext(c.Request.Context(), providerID)
	if err != nil {
		_ = h.stepUpAud().LogRejected(c.Request.Context(), actor, "invalid_provider",
			map[string]string{"provider": providerID})
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_provider",
			"error_description": fmt.Sprintf("Provider %q is not configured or is disabled", providerID),
		})
		return
	}

	// 4. Validate query params.
	clientCallback := c.Query("client_callback")
	if clientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback parameter is required",
		})
		return
	}
	allow := NewClientCallbackAllowList(h.clientCallbackAllowList(c.Request.Context()))
	if !allow.Allowed(clientCallback) {
		logger.Warn("Rejected /oauth2/step_up: client_callback %q not in allowlist", clientCallback)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback is not in the allowlist",
		})
		return
	}

	codeChallenge := c.Query("code_challenge")
	if codeChallenge == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "code_challenge parameter is required",
		})
		return
	}
	if err := ValidateCodeChallengeFormat(codeChallenge); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": fmt.Sprintf("Invalid code_challenge format: %v", err),
		})
		return
	}
	codeChallengeMethod := c.Query("code_challenge_method")
	if codeChallengeMethod == "" {
		codeChallengeMethod = pkceMethodS256
	}
	if codeChallengeMethod != pkceMethodS256 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Only S256 code_challenge_method is supported",
		})
		return
	}

	if rt := c.Query("response_type"); rt != "" && rt != oauthResponseTypeCode {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_response_type",
			"error_description": "Only response_type=code is supported",
		})
		return
	}
	if sc := c.Query("scope"); sc != "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_scope",
			"error_description": "scope is not accepted on /oauth2/step_up",
		})
		return
	}

	// 5. Classify strength.
	cfg, err := h.providerConfig(providerID)
	if err != nil {
		// Should not happen — getProvider succeeded above.
		_ = h.stepUpAud().LogRejected(c.Request.Context(), actor, "invalid_provider",
			map[string]string{"provider": providerID})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	strength := ClassifyStepUpStrength(cfg)

	// 6. Weak path — rotate-in-place short-circuit.
	if strength == StepUpWeak {
		h.stepUpWeakShortCircuit(c, actor)
		return
	}

	// 7. Strong path — store state and redirect upstream.
	h.stepUpStrongRedirect(c, provider, cfg, actor, clientCallback, codeChallenge, codeChallengeMethod)
}

// stepUpWeakShortCircuit handles step-up for providers that ignore prompt=login
// (currently github). Instead of a useless upstream round-trip, this rotates
// the user's tokens in-place and writes a step_up_complete row marked
// strength=weak, mode=short_circuit. See design spec §3.5.
// SEM@d5fb16fc8487e59524ab63468836f050fd972731: complete a weak step-up by rotating tokens in place without upstream redirect (mutates shared state)
func (h *Handlers) stepUpWeakShortCircuit(c *gin.Context, actor StepUpActor) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	user, err := h.service.GetUserByProviderID(ctx, actor.Provider, actor.ProviderUserID)
	if err != nil {
		logger.Error("step-up weak: user lookup failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Blacklist the previous refresh token if the cookie is present. Construct
	// the TokenBlacklist on-demand — same pattern as handlers_revocation.go.
	if oldRefresh, cerr := c.Cookie(RefreshTokenCookieName); cerr == nil && oldRefresh != "" {
		if h.service != nil && h.service.dbManager != nil && h.service.dbManager.Redis() != nil {
			blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient(), h.service.GetKeyManager())
			if bErr := blacklist.BlacklistToken(ctx, oldRefresh); bErr != nil {
				logger.Warn("step-up weak: failed to blacklist old refresh: %v", bErr)
			}
		}
	} else {
		logger.Debug("step-up weak: no refresh cookie present; nothing to blacklist")
	}

	tokenPair, err := h.service.GenerateTokensWithUserInfo(ctx, user, nil)
	if err != nil {
		logger.Error("step-up weak: token mint failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	if h.cookieOpts.Enabled {
		SetTokenCookies(c, tokenPair, h.cookieOpts)
	}

	_ = h.stepUpAud().LogComplete(ctx, actor, StepUpWeak, actor.Provider, "short_circuit")

	c.JSON(http.StatusOK, gin.H{
		"result":    "step_up_weak_complete",
		"provider":  actor.Provider,
		"auth_time": time.Now().Unix(),
		"message":   "Provider does not support guaranteed fresh re-auth; tokens rotated and step-up window reset. Audit log records this as a weak step-up.",
	})
}

// readStepUpJWT extracts the JWT using the same Bearer-then-cookie priority as
// the JWTMiddleware in cmd/server/jwt_auth.go (Priority 1: Authorization;
// Priority 2: HttpOnly cookie). We do not reuse that function because it lives
// in package main; the priority logic is small enough to inline.
// SEM@e3abca5a18ebb1c482126bc626ffda566518f79f: extract the bearer JWT from the Authorization header or access-token cookie (pure)
func (h *Handlers) readStepUpJWT(c *gin.Context) (string, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			return parts[1], true
		}
		return "", false
	}
	if tok := ExtractAccessTokenFromCookie(c); tok != "" {
		return tok, true
	}
	return "", false
}

// providerConfig returns the OAuthProviderConfig for the given providerID.
// Used by step-up to classify strength. It must resolve from the same source
// as getProviderWithContext — the DB-backed registry when wired, falling back
// to the YAML snapshot otherwise — so a provider that getProviderWithContext
// accepted is not then falsely rejected here (which previously surfaced as a
// 500 on /oauth2/step_up, e.g. for the runtime-registered "tmi" dev provider).
// SEM@5b2abe3e40f300aa145684c82ce8b25bdb93ec2e: fetch the OAuth provider config by provider ID from registry or static config (pure)
func (h *Handlers) providerConfig(providerID string) (OAuthProviderConfig, error) {
	if h.registry != nil {
		if cfg, ok := h.registry.GetOAuthProvider(providerID); ok {
			return cfg, nil
		}
		return OAuthProviderConfig{}, fmt.Errorf("provider %q not found", providerID)
	}
	if cfg, ok := h.config.OAuth.Providers[providerID]; ok {
		return cfg, nil
	}
	return OAuthProviderConfig{}, fmt.Errorf("provider %q not found", providerID)
}

// stepUpStrongRedirect implements the strong-provider path: store state, store
// PKCE, build the upstream URL with prompt=login&max_age=0, and redirect.
// SEM@5d36fbba264b6e4f105d4eb316e4f509c58d7300: store step-up state and PKCE challenge, then redirect the user to the upstream provider (mutates shared state)
func (h *Handlers) stepUpStrongRedirect(c *gin.Context, provider Provider, cfg OAuthProviderConfig, actor StepUpActor, clientCallback, codeChallenge, codeChallengeMethod string) {
	logger := slogging.Get().WithContext(c)

	state := c.Query("state")
	if state == "" {
		var err error
		state, err = generateRandomState()
		if err != nil {
			logger.Error("Failed to generate state for step-up: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
	}

	ctx := c.Request.Context()

	// Look up the original user's internal_uuid for the identity-match check at
	// token-mint time. The User.InternalUUID is the authoritative TMI identity.
	user, err := h.service.GetUserByProviderID(ctx, actor.Provider, actor.ProviderUserID)
	if err != nil {
		logger.Error("step-up: user lookup failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	stateKey := fmt.Sprintf("oauth_state:%s", state)
	stateData := map[string]string{
		"provider":           actor.Provider,
		"client_callback":    clientCallback,
		"step_up":            "true",
		"original_user_uuid": user.InternalUUID,
		"original_email":     user.Email,
		"step_up_strength":   StepUpStrong.String(),
	}
	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		logger.Error("step-up: state marshal failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if err := h.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), 10*time.Minute); err != nil {
		logger.Error("step-up: state store failed: %v", err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporarily_unavailable"})
		return
	}
	if err := h.service.stateStore.StorePKCEChallenge(ctx, state, codeChallenge, codeChallengeMethod, 10*time.Minute); err != nil {
		logger.Error("step-up: PKCE store failed: %v", err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporarily_unavailable"})
		return
	}

	authURL, err := BuildStepUpAuthorizationURL(provider, cfg, state)
	if err != nil {
		logger.Error("step-up: URL build failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	// Content negotiation (#455): XHR/fetch callers that send
	// Accept: application/json receive the upstream authorize URL in a JSON
	// envelope instead of a 302. A cross-origin fetch cannot read the Location
	// of an auto-followed 302 (CORS) and `redirect: 'manual'` yields an opaque
	// response, so a browser SPA needs the URL in the body to perform a
	// deterministic top-level navigation. Browser top-level navigations
	// (Accept: text/html,...) still receive the 302 — state and PKCE were
	// already stored above, so both transports drive the identical upstream flow.
	if clientPrefersJSON(c) {
		logger.Debug("step-up strong JSON: provider=%s state=%s", actor.Provider, state)
		c.JSON(http.StatusOK, gin.H{
			"result":       "step_up_redirect",
			"redirect_url": authURL,
		})
		return
	}

	logger.Debug("step-up strong redirect: provider=%s state=%s", actor.Provider, state)
	c.Redirect(http.StatusFound, authURL)
}

// clientPrefersJSON reports whether the caller explicitly listed
// application/json in its Accept header. A bare wildcard (*/*) does NOT count:
// browser top-level navigations send Accept: text/html,...,*/* and must keep
// receiving the 302 redirect, while an XHR/fetch step-up call sends an explicit
// Accept: application/json and opts into the JSON envelope.
// SEM@5d36fbba264b6e4f105d4eb316e4f509c58d7300: detect whether the request Accept header prefers JSON over HTML (pure)
func clientPrefersJSON(c *gin.Context) bool {
	for _, part := range strings.Split(c.GetHeader("Accept"), ",") {
		media := strings.TrimSpace(part)
		if i := strings.IndexByte(media, ';'); i >= 0 {
			media = strings.TrimSpace(media[:i])
		}
		if strings.EqualFold(media, "application/json") {
			return true
		}
	}
	return false
}
