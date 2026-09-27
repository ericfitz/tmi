package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// feedbackBodyLimit bounds feedback request bodies. The largest schema-valid
// body is ~2.03 MB (a 2,000,000-char ASCII data-URL screenshot plus small
// fields); 2.5 MiB leaves headroom while rejecting multi-MB garbage before any
// middleware spends seconds parsing and regex-validating it (#959).
const feedbackBodyLimit int64 = 5 << 19 // 2.5 MiB

// bodySizeLimits maps "METHOD route" (gin FullPath) to a maximum body size.
// Only routes that document 413 belong here.
var bodySizeLimits = map[string]int64{
	"POST /threat_models/:threat_model_id/feedback": feedbackBodyLimit,
	"POST /usability_feedback":                      feedbackBodyLimit,
}

// BodySizeLimitMiddleware enforces bodySizeLimits before any middleware reads
// the body: a declared Content-Length over the limit is rejected with 413
// without reading; otherwise the body is read with a hard cap into memory
// (413 when exceeded, 400 on any other read failure) and replaced with the
// buffered copy, so later middleware never sees a half-read body.
// SEM@c84c2daa103a3a28a16ed5ae37e2deba75ba12ac: enforce per-route request body size limits with 413 before body parsing (pure)
func BodySizeLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, ok := bodySizeLimits[c.Request.Method+" "+c.FullPath()]
		if !ok || c.Request.Body == nil {
			c.Next()
			return
		}
		if c.Request.ContentLength > limit {
			rejectBodyTooLarge(c)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
		_ = c.Request.Body.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				rejectBodyTooLarge(c)
				return
			}
			slogging.Get().WithContext(c).Warn("Failed to read request body: %v", err)
			HandleRequestError(c, InvalidInputError("Request body could not be read"))
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Next()
	}
}

// rejectBodyTooLarge writes the documented 413 and stops the chain.
// SEM@c84c2daa103a3a28a16ed5ae37e2deba75ba12ac: reject a request whose body exceeds its route limit with 413 (pure)
func rejectBodyTooLarge(c *gin.Context) {
	HandleRequestError(c, PayloadTooLargeError("request body exceeds the maximum size for this endpoint"))
	c.Abort()
}
