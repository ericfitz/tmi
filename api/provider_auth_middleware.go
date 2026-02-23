package api

import (
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// SameProviderMiddleware ensures the authenticated user is from the same provider as specified in the path
func SameProviderMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Get IdP from path parameter (could be "idp" or "provider")
		idp := c.Param("idp")
		if idp == "" {
			idp = c.Param("provider")
		}

		if idp == "" {
			logger.Error("SameProviderMiddleware: No idp/provider parameter found in path")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Provider parameter not found in request path",
			})
			c.Abort()
			return
		}

		// Get user's IdP from JWT claims (set by JWT middleware)
		userIdP := c.GetString("userProvider")
		if userIdP == "" {
			logger.Error("SameProviderMiddleware: No userProvider found in context")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Authentication required",
			})
			c.Abort()
			return
		}

		// Check if user's provider matches the requested provider
		if userIdP != idp {
			logger.Warn("SameProviderMiddleware: Provider mismatch - user_idp=%s, requested_idp=%s", userIdP, idp)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "provider_mismatch",
				Message: "You can only access resources for your own provider",
			})
			c.Abort()
			return
		}

		logger.Debug("SameProviderMiddleware: Provider match confirmed - idp=%s", idp)
		c.Next()
	}
}

// SAMLProviderOnlyMiddleware ensures the provider is a SAML provider (not OAuth)
func SAMLProviderOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Get IdP from path parameter
		idp := c.Param("idp")
		if idp == "" {
			idp = c.Param("provider")
		}

		if idp == "" {
			logger.Error("SAMLProviderOnlyMiddleware: No idp/provider parameter found in path")
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Provider parameter not found in request path",
			})
			c.Abort()
			return
		}

		// Check if provider is a SAML provider (starts with "saml_")
		if !strings.HasPrefix(idp, "saml_") {
			logger.Warn("SAMLProviderOnlyMiddleware: Non-SAML provider requested - idp=%s", idp)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_provider_type",
				Message: "This endpoint only supports SAML providers (provider must start with 'saml_')",
			})
			c.Abort()
			return
		}

		logger.Debug("SAMLProviderOnlyMiddleware: SAML provider confirmed - idp=%s", idp)
		c.Next()
	}
}
