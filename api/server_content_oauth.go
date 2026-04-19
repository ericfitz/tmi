package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Delegation wrappers that satisfy the generated ServerInterface for the
// delegated content provider endpoints. Each method forwards to the
// corresponding method on the attached *ContentOAuthHandlers; when the
// handler bundle is not wired (no encryption key configured or Redis
// unavailable) the delegation returns 503 to signal the subsystem is not
// available rather than 404.

// contentOAuthUnavailable writes a 503 response when the content OAuth
// subsystem is not wired.
func contentOAuthUnavailable(c *gin.Context) {
	c.Header("Retry-After", "60")
	c.JSON(http.StatusServiceUnavailable, Error{
		Error:            "service_unavailable",
		ErrorDescription: "Delegated content provider subsystem is not configured on this server.",
	})
}

// setProviderIDParam mutates the Gin context's path parameters so the
// underlying handler (which reads via c.Param) observes the provider id even
// when it arrived through the generated typed parameter. This avoids
// duplicating the path-param extraction logic in the handler.
func setProviderIDParam(c *gin.Context, providerID string) {
	// Gin's Params is a slice; rewrite the existing entry if present,
	// otherwise append.
	for i, p := range c.Params {
		if p.Key == "provider_id" {
			c.Params[i].Value = providerID
			return
		}
	}
	c.Params = append(c.Params, gin.Param{Key: "provider_id", Value: providerID})
}

// setUserIDParam is the admin-endpoint analogue of setProviderIDParam.
func setUserIDParam(c *gin.Context, userID string) {
	for i, p := range c.Params {
		if p.Key == "user_id" {
			c.Params[i].Value = userID
			return
		}
	}
	c.Params = append(c.Params, gin.Param{Key: "user_id", Value: userID})
}

// ListMyContentTokens implements ServerInterface.ListMyContentTokens.
func (s *Server) ListMyContentTokens(c *gin.Context) {
	if s.contentOAuth == nil {
		contentOAuthUnavailable(c)
		return
	}
	s.contentOAuth.List(c)
}

// AuthorizeContentToken implements ServerInterface.AuthorizeContentToken.
func (s *Server) AuthorizeContentToken(c *gin.Context, providerId string) {
	if s.contentOAuth == nil {
		contentOAuthUnavailable(c)
		return
	}
	setProviderIDParam(c, providerId)
	s.contentOAuth.Authorize(c)
}

// DeleteMyContentToken implements ServerInterface.DeleteMyContentToken.
func (s *Server) DeleteMyContentToken(c *gin.Context, providerId string) {
	if s.contentOAuth == nil {
		contentOAuthUnavailable(c)
		return
	}
	setProviderIDParam(c, providerId)
	s.contentOAuth.Delete(c)
}

// ContentOAuthCallback implements ServerInterface.ContentOAuthCallback.
// The generated typed params are unused here because the underlying handler
// reads the query string directly via c.Query.
func (s *Server) ContentOAuthCallback(c *gin.Context, _ ContentOAuthCallbackParams) {
	if s.contentOAuth == nil {
		contentOAuthUnavailable(c)
		return
	}
	s.contentOAuth.Callback(c)
}

// AdminListUserContentTokens implements ServerInterface.AdminListUserContentTokens.
func (s *Server) AdminListUserContentTokens(c *gin.Context, internalUuid openapi_types.UUID) {
	if s.contentOAuth == nil {
		contentOAuthUnavailable(c)
		return
	}
	setUserIDParam(c, internalUuid.String())
	s.contentOAuth.AdminList(c)
}

// AdminDeleteUserContentToken implements ServerInterface.AdminDeleteUserContentToken.
func (s *Server) AdminDeleteUserContentToken(c *gin.Context, internalUuid openapi_types.UUID, providerId string) {
	if s.contentOAuth == nil {
		contentOAuthUnavailable(c)
		return
	}
	setUserIDParam(c, internalUuid.String())
	setProviderIDParam(c, providerId)
	s.contentOAuth.AdminDelete(c)
}
