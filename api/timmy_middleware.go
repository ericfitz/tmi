package api

import (
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// TimmyEnabledMiddleware checks Timmy configuration and gates access to Timmy endpoints.
// When Timmy is disabled, all /chat/ and /admin/timmy/ paths return 404.
// When Timmy is enabled but not fully configured, those paths return 503.
// All other paths pass through unaffected.
func TimmyEnabledMiddleware(cfg config.TimmyConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Only intercept Timmy-related paths
		isTimmyPath := strings.Contains(path, "/chat/sessions") || strings.HasPrefix(path, "/admin/timmy")
		if !isTimmyPath {
			c.Next()
			return
		}

		if !cfg.Enabled {
			HandleRequestError(c, NotFoundError("Timmy AI assistant is not enabled"))
			c.Abort()
			return
		}

		if !cfg.IsConfigured() {
			HandleRequestError(c, ServiceUnavailableError("Timmy is enabled but LLM/embedding providers are not configured"))
			c.Abort()
			return
		}

		c.Next()
	}
}
