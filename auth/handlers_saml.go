package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// GetSAMLMetadata returns SAML service provider metadata
func (h *Handlers) GetSAMLMetadata(c *gin.Context, providerID string) {
	logger := slogging.Get()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "SAML authentication is not enabled",
		})
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "SAML manager not initialized",
		})
		return
	}

	// Lazy-initialize DB-sourced provider if needed
	if err := h.ensureSAMLProvider(samlManager, providerID); err != nil {
		logger.Warn("failed to ensure SAML provider %q: %v", providerID, err)
	}

	// Get provider
	provider, err := samlManager.GetProvider(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("SAML provider not found: %v", err),
		})
		return
	}

	// Generate metadata
	metadata, err := provider.GenerateMetadata()
	if err != nil {
		logger.Error("Failed to generate SAML metadata: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate metadata",
		})
		return
	}

	// Return metadata as XML
	c.Header("Content-Type", "application/samlmetadata+xml")
	c.Data(http.StatusOK, "application/samlmetadata+xml", []byte(metadata))
}

// InitiateSAMLLogin starts SAML authentication flow
func (h *Handlers) InitiateSAMLLogin(c *gin.Context, providerID string, clientCallback *string) {
	logger := slogging.Get()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "SAML authentication is not enabled",
		})
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "SAML manager not initialized",
		})
		return
	}

	// Lazy-initialize DB-sourced provider if needed
	if err := h.ensureSAMLProvider(samlManager, providerID); err != nil {
		logger.Warn("failed to ensure SAML provider %q: %v", providerID, err)
	}

	// Get provider
	provider, err := samlManager.GetProvider(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("SAML provider not found: %v", err),
		})
		return
	}

	// Initiate SAML authentication
	authURL, relayState, err := provider.InitiateLogin(clientCallback)
	if err != nil {
		logger.Error("Failed to initiate SAML login: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to initiate SAML authentication",
		})
		return
	}

	// Store state for CSRF protection
	if err := h.service.stateStore.StoreState(c.Request.Context(), relayState, providerID, 10*time.Minute); err != nil {
		logger.Error("Failed to store SAML relay state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to initiate SAML authentication",
		})
		return
	}

	// Store client callback URL if provided
	if clientCallback != nil && *clientCallback != "" {
		if err := h.service.stateStore.StoreCallbackURL(c.Request.Context(), relayState, *clientCallback, 10*time.Minute); err != nil {
			logger.Error("Failed to store SAML callback URL: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to initiate SAML authentication",
			})
			return
		}
		logger.Info("Stored SAML callback URL for relay state: %s -> %s", relayState, *clientCallback)
	}

	// Redirect to IdP
	c.Redirect(http.StatusFound, authURL)
}

// redirectWithError attempts to redirect to client callback URL with error, or returns JSON error if no callback
// For SAML: uses relayState to retrieve callback URL from state store
func (h *Handlers) redirectWithError(c *gin.Context, ctx context.Context, relayState string, statusCode int, errorMsg string) {
	logger := slogging.Get()

	// Try to get callback URL - even if state validation failed, we might have stored it
	callbackURL, _ := h.service.stateStore.GetCallbackURL(ctx, relayState)

	if callbackURL != "" {
		// Redirect to client with error in fragment
		redirectURL, err := url.Parse(callbackURL)
		if err != nil {
			logger.Error("Invalid callback URL during error redirect: %v", err)
			c.JSON(statusCode, gin.H{
				"error": errorMsg,
			})
			return
		}

		// Add error to fragment using OAuth 2.0 error format
		fragment := fmt.Sprintf("error=saml_error&error_description=%s", url.QueryEscape(errorMsg))
		redirectURL.Fragment = fragment

		c.Redirect(http.StatusFound, redirectURL.String())
		return
	}

	// No callback URL, return JSON error
	c.JSON(statusCode, gin.H{
		"error": errorMsg,
	})
}

// redirectWithErrorOAuth redirects to client callback URL with error for OAuth flows
func (h *Handlers) redirectWithErrorOAuth(c *gin.Context, callbackURL string, statusCode int, errorMsg string) {
	logger := slogging.Get()

	if callbackURL == "" {
		// No callback URL, return JSON error
		c.JSON(statusCode, gin.H{
			"error": errorMsg,
		})
		return
	}

	// Redirect to client with error in fragment
	redirectURL, err := url.Parse(callbackURL)
	if err != nil {
		logger.Error("Invalid callback URL during error redirect: %v", err)
		c.JSON(statusCode, gin.H{
			"error": errorMsg,
		})
		return
	}

	// Add error to fragment using OAuth 2.0 error format
	fragment := fmt.Sprintf("error=oauth_error&error_description=%s", url.QueryEscape(errorMsg))
	redirectURL.Fragment = fragment

	c.Redirect(http.StatusFound, redirectURL.String())
}

// ProcessSAMLResponse handles SAML assertion consumer service
func (h *Handlers) ProcessSAMLResponse(c *gin.Context, providerID string, samlResponse string, relayState string) {
	logger := slogging.Get()
	ctx := c.Request.Context()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		h.redirectWithError(c, ctx, relayState, http.StatusNotFound, "SAML authentication is not enabled")
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		h.redirectWithError(c, ctx, relayState, http.StatusInternalServerError, "SAML manager not initialized")
		return
	}

	// Verify state for CSRF protection
	if relayState != "" {
		storedProviderID, err := h.service.stateStore.ValidateState(ctx, relayState)
		if err != nil {
			logger.Error("Invalid SAML relay state: %v", err)
			h.redirectWithError(c, ctx, relayState, http.StatusBadRequest, "Invalid or expired state")
			return
		}
		// Use the provider ID from the state if not specified
		if providerID == "" || providerID == "default" {
			providerID = storedProviderID
		}
	}

	// Lazy-initialize DB-sourced provider if needed (e.g., after server restart)
	if err := h.ensureSAMLProvider(samlManager, providerID); err != nil {
		logger.Warn("failed to ensure SAML provider %q: %v", providerID, err)
	}

	// Process SAML response
	_, tokenPair, err := samlManager.ProcessSAMLResponse(ctx, providerID, samlResponse, relayState)
	if err != nil {
		logger.Error("Failed to process SAML response: %v", err)
		h.redirectWithError(c, ctx, relayState, http.StatusUnauthorized, fmt.Sprintf("Authentication failed: %v", err))
		return
	}

	// Check if there's a client callback URL
	callbackURL, _ := h.service.stateStore.GetCallbackURL(ctx, relayState)
	if callbackURL != "" {
		// Redirect to client with tokens in fragment (implicit flow style)
		redirectURL, err := url.Parse(callbackURL)
		if err != nil {
			logger.Error("Invalid callback URL: %v", err)
			h.redirectWithError(c, ctx, relayState, http.StatusInternalServerError, "Invalid callback URL")
			return
		}

		// Add tokens to fragment
		fragment := fmt.Sprintf("access_token=%s&refresh_token=%s&token_type=%s&expires_in=%d",
			tokenPair.AccessToken,
			tokenPair.RefreshToken,
			tokenPair.TokenType,
			tokenPair.ExpiresIn,
		)
		redirectURL.Fragment = fragment

		// Set HttpOnly session cookies before redirect
		if h.cookieOpts.Enabled {
			SetTokenCookies(c, *tokenPair, h.cookieOpts)
		}

		c.Redirect(http.StatusFound, redirectURL.String())
		return
	}

	// Set HttpOnly session cookies
	if h.cookieOpts.Enabled {
		SetTokenCookies(c, *tokenPair, h.cookieOpts)
	}

	// Return tokens as JSON
	c.JSON(http.StatusOK, tokenPair)
}

// ProcessSAMLLogout handles SAML single logout
func (h *Handlers) ProcessSAMLLogout(c *gin.Context, providerID string, samlRequest string) {
	logger := slogging.Get()

	// Check if SAML is enabled
	if !h.config.SAML.Enabled {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "SAML authentication is not enabled",
		})
		return
	}

	// Get SAML manager
	samlManager := h.service.GetSAMLManager()
	if samlManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "SAML manager not initialized",
		})
		return
	}

	// Lazy-initialize DB-sourced provider if needed
	if err := h.ensureSAMLProvider(samlManager, providerID); err != nil {
		logger.Warn("failed to ensure SAML provider %q: %v", providerID, err)
	}

	// Get provider
	provider, err := samlManager.GetProvider(providerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("SAML provider not found: %v", err),
		})
		return
	}

	// Process and validate logout request (includes signature verification)
	logoutReq, err := provider.ProcessLogoutRequest(samlRequest)
	if err != nil {
		logger.Error("Failed to process SAML logout: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid logout request",
		})
		return
	}

	// Invalidate user sessions based on the NameID from the logout request
	ctx := c.Request.Context()
	if logoutReq.NameID != nil {
		nameID := logoutReq.NameID.Value
		logger.Info("Processing SAML logout for NameID: %s", nameID)

		// Try to find the user by email (assuming NameID is email)
		user, err := h.service.GetUserByEmail(ctx, nameID)
		if err == nil {
			// Invalidate all sessions for this user using InternalUUID
			if err := h.service.InvalidateUserSessions(ctx, user.InternalUUID); err != nil {
				logger.Warn("Failed to invalidate sessions during SAML logout: %v", err)
				// Log but don't fail the logout
			}
		} else {
			logger.Warn("User not found for SAML logout NameID: %s", nameID)
		}
	}

	// Create logout response
	logoutResponse, err := provider.MakeLogoutResponse(logoutReq.ID, "urn:oasis:names:tc:SAML:2.0:status:Success")
	if err != nil {
		logger.Error("Failed to create SAML logout response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to create logout response",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "Logout successful",
		"logout_response": logoutResponse,
	})
}
