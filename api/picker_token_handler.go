package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// PickerTokenConfig holds the Google Picker JS configuration values for a
// single OAuth provider. These are not secrets; they are served to the browser.
type PickerTokenConfig struct {
	DeveloperKey string
	AppID        string
}

// pickerTokenResponse is the JSON response for a successful picker-token request.
type pickerTokenResponse struct {
	AccessToken  string    `json:"access_token"` //nolint:gosec // G117 - short-lived token minted for browser picker
	ExpiresAt    time.Time `json:"expires_at"`
	DeveloperKey string    `json:"developer_key"`
	AppID        string    `json:"app_id"`
}

// PickerTokenHandler mints short-lived Google OAuth access tokens for
// browser-side Google Picker JS.
//
// The route (POST /me/picker_tokens/{provider_id}) is registered in Task 9.1.
// This handler only validates inputs and delegates to the shared refresh logic.
type PickerTokenHandler struct {
	tokens     ContentTokenRepository
	registry   *ContentOAuthProviderRegistry
	configs    map[string]PickerTokenConfig
	userLookup func(c *gin.Context) (string, bool)
}

// NewPickerTokenHandler creates a new PickerTokenHandler.
// configs maps provider IDs to their picker configuration values.
// userLookup extracts the authenticated user ID from the Gin context.
func NewPickerTokenHandler(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	configs map[string]PickerTokenConfig,
	userLookup func(c *gin.Context) (string, bool),
) *PickerTokenHandler {
	return &PickerTokenHandler{
		tokens:     tokens,
		registry:   registry,
		configs:    configs,
		userLookup: userLookup,
	}
}

// Handle processes POST /me/picker_tokens/{provider_id}.
//
// Validation order (mirrors task spec):
//  1. configs[providerID] exists AND has non-empty DeveloperKey AND AppID → else 422.
//  2. registry.Get(providerID) exists → else 422 (provider_not_registered).
//  3. User in context → else 401.
//  4. Token repo lookup → 404 if not linked.
//  5. Token status check → 401 if failed_refresh.
//  6. refreshIfNeeded → 401/503 on error.
//  7. 200 with access_token, expires_at, developer_key, app_id.
func (h *PickerTokenHandler) Handle(c *gin.Context) {
	log := slogging.Get().WithContext(c)
	providerID := c.Param("provider_id")

	// Step 1: Check picker configuration.
	cfg, hasCfg := h.configs[providerID]
	if !hasCfg || cfg.DeveloperKey == "" || cfg.AppID == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":        "picker_not_configured",
			"message":     "picker is not configured for this provider",
			"provider_id": providerID,
		})
		return
	}

	// Step 2: Check provider registry.
	_, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":        "provider_not_registered",
			"message":     "OAuth provider is not registered",
			"provider_id": providerID,
		})
		return
	}

	// Step 3: Authenticate the caller.
	userID, ok := h.userLookup(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    "unauthenticated",
			"message": "authentication required",
		})
		return
	}

	// Step 4: Look up the user's linked token.
	tok, err := h.tokens.GetByUserAndProvider(c.Request.Context(), userID, providerID)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":        "not_linked",
				"message":     "no linked token for this provider",
				"provider_id": providerID,
			})
			return
		}
		log.Error("picker_token: GetByUserAndProvider user=%s provider=%s: %v", userID, providerID, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Step 5: Short-circuit on permanently failed token.
	if tok.Status == ContentTokenStatusFailedRefresh {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    "token_refresh_failed",
			"message": "token is in a failed refresh state; please re-authorize",
		})
		return
	}

	// Step 6: Refresh if expired.
	accessToken, expiresAt, err := h.refreshIfNeeded(c, tok, providerID)
	if err != nil {
		if errors.Is(err, ErrAuthRequired) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    "token_refresh_failed",
				"message": "token refresh failed; please re-authorize",
			})
			return
		}
		if errors.Is(err, ErrTransient) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code":    "transient_failure",
				"message": "a transient error occurred; please retry",
			})
			return
		}
		log.Error("picker_token: refreshIfNeeded user=%s provider=%s: %v", userID, providerID, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Step 7: Return the picker token response.
	resp := pickerTokenResponse{
		AccessToken:  accessToken,
		DeveloperKey: cfg.DeveloperKey,
		AppID:        cfg.AppID,
	}
	if expiresAt != nil {
		resp.ExpiresAt = *expiresAt
	}

	log.Info("picker_token_minted user_id=%s provider_id=%s expires_at=%v", userID, providerID, resp.ExpiresAt)
	c.JSON(http.StatusOK, resp)
}

// refreshIfNeeded returns the plaintext access token and expiry for the given
// ContentToken, refreshing it first when it is expired (or within 30s of
// expiry). The logic mirrors DelegatedSource.refresh in content_source_delegated.go.
//
// Error returns:
//   - ErrAuthRequired: permanent OAuth failure (token revoked/invalid) or no
//     refresh token; caller should ask user to re-authorize.
//   - ErrTransient: transient network/5xx failure; caller may retry.
//   - Other: unexpected repository or provider error.
func (h *PickerTokenHandler) refreshIfNeeded(c *gin.Context, tok *ContentToken, providerID string) (string, *time.Time, error) {
	log := slogging.Get().WithContext(c)

	if !pickerTokenExpired(tok) {
		return tok.AccessToken, tok.ExpiresAt, nil
	}

	log.Debug("picker_token: token expired, refreshing provider=%s", providerID)

	provider, ok := h.registry.Get(providerID)
	if !ok {
		// Registry check already passed in Handle; this is a safety net.
		log.Warn("picker_token: provider vanished from registry provider=%s", providerID)
		return "", nil, ErrAuthRequired
	}

	var permanentFailure bool
	var transientFailure bool

	updated, err := h.tokens.RefreshWithLock(c.Request.Context(), tok.ID, func(current *ContentToken) (*ContentToken, error) {
		// Re-check expiry inside the lock: another goroutine may have already
		// refreshed the token between our initial check and acquiring the lock.
		if !pickerTokenExpired(current) {
			log.Debug("picker_token: token already refreshed by peer, skipping provider call provider=%s", providerID)
			return current, nil
		}

		// No refresh token — flip to failed_refresh and commit.
		if current.RefreshToken == "" {
			log.Warn("picker_token: no refresh token available, marking failed provider=%s", providerID)
			current.Status = ContentTokenStatusFailedRefresh
			current.LastError = "no refresh token available"
			permanentFailure = true
			return current, nil
		}

		resp, refreshErr := provider.Refresh(c.Request.Context(), current.RefreshToken)
		if refreshErr != nil {
			if IsContentOAuthPermanentFailure(refreshErr) {
				log.Warn("picker_token: permanent refresh failure, marking failed provider=%s err=%s", providerID, refreshErr)
				current.Status = ContentTokenStatusFailedRefresh
				current.LastError = refreshErr.Error()
				permanentFailure = true
				return current, nil
			}
			log.Warn("picker_token: transient refresh failure provider=%s err=%s", providerID, refreshErr)
			transientFailure = true
			return nil, fmt.Errorf("picker_token refresh transient: %w", refreshErr)
		}

		// Success: update token fields.
		now := time.Now()
		current.AccessToken = resp.AccessToken
		if resp.RefreshToken != "" {
			current.RefreshToken = resp.RefreshToken
		}
		if resp.Scope != "" {
			current.Scopes = resp.Scope
		}
		current.ExpiresAt = resp.ExpiresAt()
		current.LastRefreshAt = &now
		current.LastError = ""
		current.Status = ContentTokenStatusActive
		return current, nil
	})

	if permanentFailure {
		return "", nil, ErrAuthRequired
	}
	if transientFailure {
		return "", nil, ErrTransient
	}
	if err != nil {
		return "", nil, err
	}
	return updated.AccessToken, updated.ExpiresAt, nil
}

// pickerTokenExpired returns true when the token has an expiry time that is
// within 30 seconds of the current time. Mirrors DelegatedSource.expired.
func pickerTokenExpired(t *ContentToken) bool {
	if t.ExpiresAt == nil {
		return false
	}
	const skew = 30 * time.Second
	return time.Now().Add(skew).After(*t.ExpiresAt)
}
