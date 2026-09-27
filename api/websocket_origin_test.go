package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestOriginAllowed pins exact scheme+host matching for WebSocket origins;
// the previous prefix match admitted look-alike hosts.
func TestOriginAllowed(t *testing.T) {
	const host, tls = "api.tmi.dev", "tmi.internal"
	conf := []string{"https://www.tmi.dev", " https://app.example.com:8443/"}
	allowed := []string{
		"https://api.tmi.dev",
		"http://api.tmi.dev",
		"https://tmi.internal",
		"https://www.tmi.dev",
		"https://app.example.com:8443",
		"http://localhost:4200",
		"https://127.0.0.1:8080",
		"http://[::1]:3000",
	}
	for _, o := range allowed {
		assert.True(t, originAllowed(o, host, tls, conf), "must allow %q", o)
	}
	rejected := []string{
		"https://api.tmi.dev.evil.example",
		"https://localhost.evil.example",
		"https://127.0.0.1.evil.example",
		"https://www.tmi.dev.evil.example",
		"http://www.tmi.dev",      // scheme differs from configured entry
		"https://app.example.com", // port differs from configured entry
		"https://evil.example",
		"https://user@api.tmi.dev",
		"null",
		"file://api.tmi.dev",
	}
	for _, o := range rejected {
		assert.False(t, originAllowed(o, host, tls, conf), "must reject %q", o)
	}
}

// TestCheckWebSocketOrigin covers the request-level wrapper: the gin
// context carries dev mode and the CORS allowed origins, as the server's
// config middleware sets them.
func TestCheckWebSocketOrigin(t *testing.T) {
	t.Setenv("WEBSOCKET_ALLOWED_ORIGINS", "")
	newCtx := func(origin string) (*gin.Context, *http.Request) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		r := httptest.NewRequest("GET", "http://api.tmi.dev/ws/notifications", nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		c.Request = r
		c.Set("corsAllowedOrigins", []string{"https://www.tmi.dev"})
		return c, r
	}

	c, r := newCtx("")
	assert.True(t, checkWebSocketOrigin(c, r), "no Origin header")
	c, r = newCtx("https://www.tmi.dev")
	assert.True(t, checkWebSocketOrigin(c, r), "CORS-allowed SPA origin")
	c, r = newCtx("https://www.tmi.dev.evil.example")
	assert.False(t, checkWebSocketOrigin(c, r))
	c.Set("isDev", true)
	assert.True(t, checkWebSocketOrigin(c, r), "dev mode accepts any origin")
}
