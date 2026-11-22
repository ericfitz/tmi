package api

import (
	"bytes"
	"io"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RequestBodyReadMiddleware reads the whole request body into memory before
// any other body-inspecting middleware or the authenticator runs, and replaces
// it with the buffered copy.
//
// A body that cannot be read to completion (the client declared more bytes
// than it sent, stalled until the server read timeout, or disconnected) is a
// client error: it is answered with the documented 400 instead of being
// swallowed by the later middleware that read the body. Swallowing it let the
// request proceed with a connection whose context was already canceled, which
// the authenticator then reported as a 503 (CATS InvalidContentLengthHeaders).
// Buffering here also means every later reader sees a complete body.
//
// Requests without a body, and GET protocol upgrades (WebSocket), pass through
// untouched.
func RequestBodyReadMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody ||
			c.Request.ContentLength == 0 ||
			(c.Request.Method == http.MethodGet && c.GetHeader("Upgrade") != "") {
			c.Next()
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		_ = c.Request.Body.Close()
		if err != nil {
			slogging.Get().WithContext(c).Warn("Request body could not be read to completion: %v", err)
			// Protocol routes use the RFC 6749 error enum, which has no invalid_input.
			reqErr := InvalidInputError("Request body is incomplete or could not be read")
			reqErr.Code = unicodeErrorCode(c)
			HandleRequestError(c, reqErr)
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Next()
	}
}
