//go:build dev || test

package auth

import (
	"github.com/gin-gonic/gin"
)

// registerTestProviderRoutes is called from RegisterRoutes when in dev/test builds
// The test provider uses the standard /auth/authorize/test route, not separate routes
func (h *Handlers) registerTestProviderRoutes(router *gin.Engine) {
	// No additional routes needed - test provider uses standard OAuth endpoints
}