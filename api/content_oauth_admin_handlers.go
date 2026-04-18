package api

import (
	"errors"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// revokeAtProvider attempts to revoke accessToken at the named provider.
// It is best-effort: failures are logged at Warn level and the error is
// returned so callers can decide whether to surface it.
func (h *ContentOAuthHandlers) revokeAtProvider(c *gin.Context, providerID, accessToken string) error {
	provider, ok := h.Registry.Get(providerID)
	if !ok {
		return nil // provider not configured locally; nothing to revoke
	}
	if err := provider.Revoke(c.Request.Context(), accessToken); err != nil {
		slogging.Get().WithContext(c).Warn("content oauth provider revoke failed provider=%s: %v", providerID, err)
		return err
	}
	return nil
}

// AdminList handles GET /admin/users/:user_id/content_tokens.
// Returns 200 with {"content_tokens": [...]} for the path user.
// Admin-role enforcement is applied by middleware at route registration time.
func (h *ContentOAuthHandlers) AdminList(c *gin.Context) {
	userID := c.Param("user_id")

	toks, err := h.Tokens.ListByUser(c.Request.Context(), userID)
	if err != nil {
		slogging.Get().WithContext(c).Error("admin list content tokens user=%s: %v", userID, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	out := make([]contentTokenInfo, 0, len(toks))
	for _, t := range toks {
		out = append(out, toContentTokenInfo(t))
	}
	c.JSON(http.StatusOK, gin.H{"content_tokens": out})
}

// AdminDelete handles DELETE /admin/users/:user_id/content_tokens/:provider_id.
// Deletes the token and attempts provider-side revocation (best-effort).
// Returns 204 whether or not the row existed (idempotent).
// Admin-role enforcement is applied by middleware at route registration time.
func (h *ContentOAuthHandlers) AdminDelete(c *gin.Context) {
	userID := c.Param("user_id")
	providerID := c.Param("provider_id")

	tok, err := h.Tokens.DeleteByUserAndProvider(c.Request.Context(), userID, providerID)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			c.Status(http.StatusNoContent)
			return
		}
		slogging.Get().WithContext(c).Error("admin delete content token user=%s provider=%s: %v", userID, providerID, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// Best-effort provider-side revocation; failures do not affect the response.
	_ = h.revokeAtProvider(c, providerID, tok.AccessToken) // error already logged inside revokeAtProvider

	c.Status(http.StatusNoContent)
}
