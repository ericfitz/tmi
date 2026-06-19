package api

import (
	"context"
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// TimmyConfigReader is the read surface the middleware needs. *TimmyConfigProvider
// satisfies it; tests inject a stub.
// SEM@97d90c492e6b6921c50b9c6e84de6ad5ece1dbb2: interface for reading the current Timmy configuration per request (pure)
type TimmyConfigReader interface {
	Current(ctx context.Context) config.TimmyConfig
}

// TimmyEnabledMiddleware checks Timmy configuration per request and gates access
// to Timmy endpoints. When Timmy is disabled, all /chat/sessions and
// /admin/timmy/ paths return 404. When enabled but not fully configured, those
// paths return 503. All other paths pass through unaffected.
// SEM@97d90c492e6b6921c50b9c6e84de6ad5ece1dbb2: gate Timmy endpoints with 404/503 when Timmy is disabled or misconfigured
func TimmyEnabledMiddleware(reader TimmyConfigReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		isTimmyPath := strings.Contains(path, "/chat/sessions") || strings.HasPrefix(path, "/admin/timmy")
		if !isTimmyPath {
			c.Next()
			return
		}

		cfg := reader.Current(c.Request.Context())
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
