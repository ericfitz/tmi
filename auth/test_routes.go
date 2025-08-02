//go:build dev || test

package auth

import (
	"github.com/gin-gonic/gin"
)

// RegisterTestRoutes registers the test OAuth provider routes
// This is only available in dev and test builds
func (h *Handlers) RegisterTestRoutes(router *gin.Engine) {
	testAuth := router.Group("/auth/test")
	{
		testAuth.GET("/authorize", gin.WrapF(HandleTestAuthorize))
		testAuth.POST("/token", gin.WrapF(HandleTestToken))
	}
}

// registerTestProviderRoutes is called from RegisterRoutes when in dev/test builds
func (h *Handlers) registerTestProviderRoutes(router *gin.Engine) {
	h.RegisterTestRoutes(router)
}