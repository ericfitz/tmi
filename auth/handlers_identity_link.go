// Package auth — /me/identities/link/* handlers (#383).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// identityLinkPendingKey returns the Redis key for a pending identity link token.
func identityLinkPendingKey(token string) string {
	return fmt.Sprintf("identity_link_pending:%s", token)
}

// identityLinkPendingTTL is the TTL for pending identity link tokens.
const identityLinkPendingTTL = 5 * time.Minute

// identityLinkStateTTL is the TTL for identity link OAuth state.
const identityLinkStateTTL = 10 * time.Minute

// identityLinkPendingData holds the staged second-identity info before confirm.
type identityLinkPendingData struct {
	UserUUID       string `json:"user_uuid"`
	Provider       string `json:"provider"`
	ProviderUserID string `json:"provider_user_id"`
	Email          string `json:"email"`
	Name           string `json:"name"`
}

// generateLinkToken generates a 32-byte crypto-random token encoded as base64url.
// This produces 43 characters of high-entropy output suitable as a one-time token.
func generateLinkToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("failed to generate link token: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}

// StartIdentityLink handles POST /me/identities/link/start.
// It validates the request, builds OAuth state, stores it in Redis, and returns
// the authorization URL + state token for the client to use.
func (h *Handlers) StartIdentityLink(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// 1. Extract and validate the JWT to identify the current user.
	tokenStr, ok := h.readStepUpJWT(c) // reuse the same header/cookie extraction
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

	actor := IdentityLinkActor{
		Email:          claims.Email,
		Provider:       claims.IdentityProvider,
		ProviderUserID: claims.Subject,
		DisplayName:    claims.Name,
	}

	// 2. Reject service accounts.
	if strings.HasPrefix(claims.Subject, "sa:") {
		_ = h.identityLinkAud().LogRejected(c.Request.Context(), actor, "unsupported_grant_type",
			map[string]string{"subject_prefix": "sa"})
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "forbidden",
			"error_description": "Service accounts cannot link identities",
		})
		return
	}

	// 3. Look up the requesting user to get InternalUUID.
	ctx := c.Request.Context()
	user, err := h.service.GetUserByProviderID(ctx, claims.IdentityProvider, claims.Subject)
	if err != nil {
		logger.Error("StartIdentityLink: user lookup failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// 4. Validate provider.
	idp := c.Query("idp")
	if idp == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "idp parameter is required",
		})
		return
	}
	provider, err := h.getProviderWithContext(ctx, idp)
	if err != nil {
		_ = h.identityLinkAud().LogRejected(ctx, actor, "invalid_provider",
			map[string]string{"provider": idp})
		if strings.Contains(err.Error(), "not available in production") {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "Identity provider not available",
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_provider",
				"error_description": fmt.Sprintf("Provider %q is not configured or is disabled", idp),
			})
		}
		return
	}

	// 5. Validate client_callback.
	clientCallback := c.Query("client_callback")
	if clientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback parameter is required",
		})
		return
	}
	allow := NewClientCallbackAllowList(h.clientCallbackAllowList(ctx))
	if !allow.Allowed(clientCallback) {
		logger.Warn("StartIdentityLink: client_callback %q not in allowlist", clientCallback)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback is not in the allowlist",
		})
		return
	}

	// 6. Generate state and store in Redis with identity_link marker.
	// NOTE: PKCE is intentionally absent from this flow. PKCE (RFC 7636) protects
	// a public-client authorization-code exchange where the verifier proves the
	// same client that started the flow is the one redeeming the code. In the link
	// flow there is no public client exchanging a code — the server exchanges the
	// code confidentially in HandleIdentityLinkCallback. The binding mechanism is
	// the pending-token (delivered only to the allowlisted client_callback) plus
	// the UUID-matched step-up-fresh JWT required by ConfirmIdentityLink. PKCE
	// here added friction without adding security.
	state, err := generateRandomState()
	if err != nil {
		logger.Error("StartIdentityLink: state generation failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	stateKey := fmt.Sprintf("oauth_state:%s", state)
	stateData := map[string]string{
		"provider":        idp,
		"client_callback": clientCallback,
		"identity_link":   "true",
		"link_user_uuid":  user.InternalUUID,
	}
	stateJSON, err := json.Marshal(stateData)
	if err != nil {
		logger.Error("StartIdentityLink: state marshal failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if err := h.service.dbManager.Redis().Set(ctx, stateKey, string(stateJSON), identityLinkStateTTL); err != nil {
		logger.Error("StartIdentityLink: state store failed: %v", err)
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporarily_unavailable"})
		return
	}

	// 7. Build the authorization URL with prompt=select_account (and prompt=consent
	// for strong providers that honor it).
	cfg, err := h.providerConfig(idp)
	if err != nil {
		// Should not happen — getProviderWithContext succeeded above.
		logger.Error("StartIdentityLink: provider config lookup failed after provider resolved: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	authURL, err := BuildIdentityLinkAuthorizationURL(provider, cfg, state)
	if err != nil {
		logger.Error("StartIdentityLink: URL build failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	expiresAt := time.Now().UTC().Add(identityLinkStateTTL)
	c.JSON(http.StatusOK, gin.H{
		"link_state":        state,
		"authorization_url": authURL,
		"expires_at":        expiresAt.Format(time.RFC3339),
	})
}

// HandleIdentityLinkCallback is called from the shared Callback handler when
// stateData.IdentityLink is true. It performs a server-side code exchange to
// obtain the provider's user info (provider, sub, email, name) WITHOUT storing
// the IdP tokens. It stages a pending link record in Redis and redirects to the
// client_callback with link_pending={token}.
func (h *Handlers) HandleIdentityLinkCallback(c *gin.Context, code string, stateData *callbackStateData) error {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	// Ensure client_callback was set (required for the link flow).
	if stateData.ClientCallback == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "client_callback is required for identity link flow",
		})
		return fmt.Errorf("missing client_callback for identity link callback")
	}

	// Get the provider to exchange the code server-side.
	provider, err := h.getProviderWithContext(ctx, stateData.ProviderID)
	if err != nil {
		logger.Error("HandleIdentityLinkCallback: provider lookup failed: %v", err)
		actor := IdentityLinkActor{Provider: stateData.ProviderID, UserUUID: stateData.LinkUserUUID}
		_ = h.identityLinkAud().LogFailed(ctx, actor, "provider_unavailable", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("provider_unavailable"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("provider unavailable: %w", err)
	}

	// Server-side code exchange: get access token to fetch user info.
	// We DISCARD the access and refresh tokens after getting user info.
	tokenResponse, err := provider.ExchangeCode(ctx, code)
	if err != nil {
		logger.Error("HandleIdentityLinkCallback: code exchange failed: %v", err)
		actor := IdentityLinkActor{Provider: stateData.ProviderID, UserUUID: stateData.LinkUserUUID}
		_ = h.identityLinkAud().LogFailed(ctx, actor, "code_exchange_failed", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("code_exchange_failed"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("code exchange failed: %w", err)
	}

	// Fetch user info from the provider using the access token.
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		logger.Error("HandleIdentityLinkCallback: userinfo fetch failed: %v", err)
		actor := IdentityLinkActor{Provider: stateData.ProviderID, UserUUID: stateData.LinkUserUUID}
		_ = h.identityLinkAud().LogFailed(ctx, actor, "userinfo_fetch_failed", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("userinfo_fetch_failed"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("userinfo fetch failed: %w", err)
	}

	// Tokens are no longer needed — do not store them.
	// userInfo.ID is the provider_user_id (sub).
	providerUserID := userInfo.ID
	if providerUserID == "" {
		logger.Error("HandleIdentityLinkCallback: provider returned empty subject for provider=%s", stateData.ProviderID)
		actor := IdentityLinkActor{Provider: stateData.ProviderID, UserUUID: stateData.LinkUserUUID}
		_ = h.identityLinkAud().LogFailed(ctx, actor, "empty_subject", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("empty_subject"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("empty subject from provider")
	}

	// Boundary-validate IdP-supplied fields against column limits before staging.
	//
	// provider_user_id is an identity key: truncating it would silently map two
	// distinct identities to the same row, which is a security error. Reject it
	// and redirect with error=invalid_identity instead.
	const maxProviderUserIDLen = 500
	if len(providerUserID) > maxProviderUserIDLen {
		actor := IdentityLinkActor{Provider: stateData.ProviderID, UserUUID: stateData.LinkUserUUID}
		_ = h.identityLinkAud().LogFailed(ctx, actor, "identity_link_failed", map[string]string{
			"reason": "provider_user_id_too_long",
		})
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("invalid_identity"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("provider_user_id exceeds maximum length %d", maxProviderUserIDLen)
	}

	// email and name are display-cache fields: truncating is safe because they are
	// never used as identity keys. Column sizes are 320 (email) and 256 (name).
	const maxEmailLen = 320
	const maxNameLen = 256
	idpEmail := userInfo.Email
	if len(idpEmail) > maxEmailLen {
		logger.Warn("HandleIdentityLinkCallback: IdP email truncated from %d to %d chars for provider=%s",
			len(idpEmail), maxEmailLen, stateData.ProviderID)
		idpEmail = idpEmail[:maxEmailLen]
	}
	idpName := userInfo.Name
	if len(idpName) > maxNameLen {
		logger.Warn("HandleIdentityLinkCallback: IdP name truncated from %d to %d chars for provider=%s",
			len(idpName), maxNameLen, stateData.ProviderID)
		idpName = idpName[:maxNameLen]
	}

	actor := IdentityLinkActor{
		Provider:       stateData.ProviderID,
		ProviderUserID: providerUserID,
		UserUUID:       stateData.LinkUserUUID,
	}

	// Check for foreign binding: is this (provider, sub) already owned by a
	// different TMI user (either as primary or as a linked identity)?
	alreadyBound := h.isProviderSubAlreadyBound(ctx, stateData.ProviderID, providerUserID)

	if alreadyBound {
		_ = h.identityLinkAud().LogRejected(ctx, actor, "identity_already_bound", map[string]string{
			"provider": stateData.ProviderID,
			"sub":      redactSub(providerUserID),
		})
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("identity_already_bound"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("identity already bound")
	}

	// Generate a 32-byte crypto-random pending link token.
	linkToken, err := generateLinkToken()
	if err != nil {
		logger.Error("HandleIdentityLinkCallback: link token generation failed: %v", err)
		_ = h.identityLinkAud().LogFailed(ctx, actor, "token_generation_failed", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("server_error"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("link token generation failed: %w", err)
	}

	// Stage the pending link in Redis (5-minute TTL).
	pending := identityLinkPendingData{
		UserUUID:       stateData.LinkUserUUID,
		Provider:       stateData.ProviderID,
		ProviderUserID: providerUserID,
		Email:          idpEmail,
		Name:           idpName,
	}
	pendingJSON, err := json.Marshal(pending)
	if err != nil {
		logger.Error("HandleIdentityLinkCallback: pending marshal failed: %v", err)
		_ = h.identityLinkAud().LogFailed(ctx, actor, "staging_failed", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("server_error"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("pending marshal failed: %w", err)
	}
	pendingKey := identityLinkPendingKey(linkToken)
	if err := h.service.dbManager.Redis().Set(ctx, pendingKey, string(pendingJSON), identityLinkPendingTTL); err != nil {
		logger.Error("HandleIdentityLinkCallback: pending store failed: %v", err)
		_ = h.identityLinkAud().LogFailed(ctx, actor, "staging_failed", nil)
		redirectURL := fmt.Sprintf("%s?error=%s", stateData.ClientCallback, url.QueryEscape("server_error"))
		c.Redirect(http.StatusFound, redirectURL)
		return fmt.Errorf("pending store failed: %w", err)
	}

	logger.Debug("HandleIdentityLinkCallback: staged pending link token for user=%s provider=%s",
		stateData.LinkUserUUID, stateData.ProviderID)

	// Redirect to client_callback with link_pending=<token>.
	redirectURL := fmt.Sprintf("%s?link_pending=%s", stateData.ClientCallback, url.QueryEscape(linkToken))
	c.Redirect(http.StatusFound, redirectURL)
	return nil
}

// GetPendingIdentityLink handles GET /me/identities/link/pending/{link_id}.
// Returns the pending link details (both sides) if the token exists and belongs
// to the authenticated user. Returns 404 on any mismatch.
func (h *Handlers) GetPendingIdentityLink(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Extract and validate the JWT.
	tokenStr, ok := h.readStepUpJWT(c)
	if !ok {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}
	claims, err := h.service.ValidateToken(tokenStr)
	if err != nil {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}

	// Reject service accounts — consistent with the four sibling endpoints.
	if strings.HasPrefix(claims.Subject, "sa:") {
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "forbidden",
			"error_description": "Service accounts cannot link identities",
		})
		return
	}

	// Get link_id from path param.
	linkID := c.Param("link_id")
	if linkID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	ctx := c.Request.Context()

	// Retrieve pending data from Redis.
	pendingKey := identityLinkPendingKey(linkID)
	pendingJSON, err := h.service.dbManager.Redis().Get(ctx, pendingKey)
	if err != nil {
		// Missing or expired — return 404 with no distinguishable message.
		logger.Debug("GetPendingIdentityLink: key not found or expired: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	var pending identityLinkPendingData
	if err := json.Unmarshal([]byte(pendingJSON), &pending); err != nil {
		logger.Error("GetPendingIdentityLink: unmarshal failed: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	// UUID mismatch returns the same 404 — no distinguishable response.
	user, err := h.service.GetUserByProviderID(ctx, claims.IdentityProvider, claims.Subject)
	if err != nil {
		logger.Debug("GetPendingIdentityLink: user lookup failed: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if pending.UserUUID != user.InternalUUID {
		logger.Debug("GetPendingIdentityLink: UUID mismatch: pending=%s caller=%s", pending.UserUUID, user.InternalUUID)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	// Truncate provider_user_id to first 8 chars + ellipsis.
	truncatedSub := pending.ProviderUserID
	if len(truncatedSub) > 8 {
		truncatedSub = truncatedSub[:8] + "…"
	}

	c.JSON(http.StatusOK, gin.H{
		"pending": gin.H{
			"provider":         pending.Provider,
			"provider_user_id": truncatedSub,
			"email":            pending.Email,
			"name":             pending.Name,
		},
		"account": gin.H{
			"provider": claims.IdentityProvider,
			"email":    claims.Email,
		},
	})
}

// ConfirmIdentityLink handles POST /me/identities/link/confirm.
// Consumes the one-time pending link token and inserts the linked identity row.
// Returns 201 with the new LinkedIdentity on success.
func (h *Handlers) ConfirmIdentityLink(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Extract and validate the JWT.
	tokenStr, ok := h.readStepUpJWT(c)
	if !ok {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}
	claims, err := h.service.ValidateToken(tokenStr)
	if err != nil {
		c.Header("WWW-Authenticate", `Bearer error="invalid_token"`)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
		return
	}

	// Reject service accounts.
	if strings.HasPrefix(claims.Subject, "sa:") {
		c.JSON(http.StatusForbidden, gin.H{
			"error":             "forbidden",
			"error_description": "Service accounts cannot link identities",
		})
		return
	}

	// Parse request body.
	var body struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Missing required field: token",
		})
		return
	}

	ctx := c.Request.Context()

	// Retrieve pending data from Redis.
	pendingKey := identityLinkPendingKey(body.Token)
	pendingJSON, err := h.service.dbManager.Redis().Get(ctx, pendingKey)
	if err != nil {
		logger.Debug("ConfirmIdentityLink: pending key not found or expired: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	var pending identityLinkPendingData
	if err := json.Unmarshal([]byte(pendingJSON), &pending); err != nil {
		logger.Error("ConfirmIdentityLink: unmarshal failed: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	// Validate caller owns this pending link (UUID match).
	user, err := h.service.GetUserByProviderID(ctx, claims.IdentityProvider, claims.Subject)
	if err != nil {
		logger.Debug("ConfirmIdentityLink: user lookup failed: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if pending.UserUUID != user.InternalUUID {
		logger.Debug("ConfirmIdentityLink: UUID mismatch: pending=%s caller=%s", pending.UserUUID, user.InternalUUID)
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}

	// CRITICAL: Delete the Redis key FIRST before any other operation.
	// This ensures the token is one-time even if subsequent operations fail.
	_ = h.service.dbManager.Redis().Del(ctx, pendingKey)

	actor := IdentityLinkActor{
		Email:          claims.Email,
		Provider:       claims.IdentityProvider,
		ProviderUserID: claims.Subject,
		DisplayName:    claims.Name,
		UserUUID:       user.InternalUUID,
	}

	// Re-check primary identity (users table). This is a pre-flight guard; the
	// authoritative duplicate check inside CreateExclusive (below) covers the
	// linked_identities table in a serializable transaction.
	_, errUser := h.service.GetUserByProviderID(ctx, pending.Provider, pending.ProviderUserID)
	if errUser == nil {
		_ = h.identityLinkAud().LogRejected(ctx, actor, "identity_already_bound", map[string]string{
			"provider": pending.Provider,
			"sub":      redactSub(pending.ProviderUserID),
		})
		c.JSON(http.StatusConflict, gin.H{
			"error":             "conflict",
			"error_code":        "identity_already_bound",
			"error_description": "This identity is already linked to a TMI account",
		})
		return
	}

	// Insert the linked identity. CreateExclusive performs the linked_identities
	// re-check AND the insert inside a single serializable transaction, closing
	// the TOCTOU race that existed when the check and the insert were separate
	// statements. The unique index is the final backstop; CreateExclusive
	// surfaces both the read-caught and the constraint-caught paths as
	// dberrors.ErrDuplicate so the 409 branch below handles both.
	if h.identityLinkStore == nil {
		logger.Error("ConfirmIdentityLink: identityLinkStore not wired")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	created, err := h.identityLinkStore.CreateExclusive(ctx, LinkedIdentityInput{
		UserInternalUUID: user.InternalUUID,
		Provider:         pending.Provider,
		ProviderUserID:   pending.ProviderUserID,
		Email:            pending.Email,
		Name:             pending.Name,
	})
	if err != nil {
		if errors.Is(err, dberrors.ErrDuplicate) {
			_ = h.identityLinkAud().LogRejected(ctx, actor, "identity_already_bound", map[string]string{
				"provider": pending.Provider,
				"sub":      redactSub(pending.ProviderUserID),
			})
			c.JSON(http.StatusConflict, gin.H{
				"error":             "conflict",
				"error_code":        "identity_already_bound",
				"error_description": "This identity is already linked to a TMI account",
			})
			return
		}
		// Constraint or FK violations indicate invalid input (e.g. referential
		// integrity failure); surface as 400 rather than 500. The package auth
		// cannot import api's StoreErrorToRequestError, so we classify inline.
		if errors.Is(err, dberrors.ErrConstraint) || errors.Is(err, dberrors.ErrForeignKey) {
			logger.Warn("ConfirmIdentityLink: constraint error: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_input",
				"error_description": "Identity data violates a database constraint",
			})
			return
		}
		logger.Error("ConfirmIdentityLink: create failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Audit the successful link.
	_ = h.identityLinkAud().LogComplete(ctx, actor,
		user.Provider, user.ProviderUserID,
		string(created.Provider), string(created.ProviderUserID))

	// Truncate provider_user_id for the response.
	truncatedSub := string(created.ProviderUserID)
	if len(truncatedSub) > 8 {
		truncatedSub = truncatedSub[:8] + "…"
	}

	// Build response matching the LinkedIdentity OpenAPI schema.
	response := gin.H{
		"id":               string(created.ID),
		"provider":         string(created.Provider),
		"provider_user_id": truncatedSub,
		"linked_at":        created.LinkedAt.UTC().Format(time.RFC3339),
	}
	if created.Email != "" {
		response["email"] = string(created.Email)
	}
	if created.Name != "" {
		response["name"] = string(created.Name)
	}
	if created.LastUsedAt != nil {
		response["last_used_at"] = created.LastUsedAt.UTC().Format(time.RFC3339)
	}

	c.JSON(http.StatusCreated, response)
}

// isProviderSubAlreadyBound returns true if the (provider, providerUserID) pair
// is already owned by any TMI user — either as a primary identity in the users
// table or as a linked identity in the linked_identities table.
func (h *Handlers) isProviderSubAlreadyBound(ctx context.Context, provider, providerUserID string) bool {
	// Check the users (primary identities) table first.
	_, err := h.service.GetUserByProviderID(ctx, provider, providerUserID)
	if err == nil {
		return true
	}
	// Check the linked_identities table.
	if h.identityLinkStore != nil {
		_, errLink := h.identityLinkStore.GetByProviderSub(ctx, provider, providerUserID)
		if errLink == nil {
			return true
		}
	}
	return false
}
