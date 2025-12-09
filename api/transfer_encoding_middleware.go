package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// TransferEncodingValidationMiddleware rejects requests with Transfer-Encoding header
// Transfer-Encoding (especially chunked) is not supported by this API
// Returns 400 Bad Request instead of 501 Not Implemented for better HTTP semantics
func TransferEncodingValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)

		// Check for Transfer-Encoding header
		te := c.GetHeader("Transfer-Encoding")
		if te != "" {
			logger.Warn("Request rejected: unsupported Transfer-Encoding header: %s", te)
			c.JSON(http.StatusBadRequest, Error{
				Error:            "unsupported_encoding",
				ErrorDescription: "Transfer-Encoding header is not supported. Please use standard Content-Length encoding.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
