//go:build !dev && !test

package auth

import (
	"github.com/gin-gonic/gin"
)

// registerTestProviderRoutes is a no-op in production builds
func (h *Handlers) registerTestProviderRoutes(router *gin.Engine) {
	// No test routes in production
}
