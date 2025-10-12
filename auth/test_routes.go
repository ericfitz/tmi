//go:build dev || test

package auth

import (
	"github.com/gin-gonic/gin"
)

// registerTestProviderRoutes is a placeholder for test provider route registration
// The test provider uses the standard OAuth endpoints via OpenAPI: /oauth2/authorize?idp=test
// This function is kept for backward compatibility but does nothing
func (h *Handlers) registerTestProviderRoutes(router *gin.Engine) {
	// No additional routes needed - test provider uses standard OAuth endpoints
}
