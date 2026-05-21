package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type stubTimmyConfigReader struct{ cfg config.TimmyConfig }

func (s stubTimmyConfigReader) Current(_ context.Context) config.TimmyConfig { return s.cfg }

func runTimmyMiddleware(t *testing.T, cfg config.TimmyConfig) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TimmyEnabledMiddleware(stubTimmyConfigReader{cfg: cfg}))
	r.POST("/threat_models/x/chat/sessions", func(c *gin.Context) { c.Status(http.StatusOK) })
	req, _ := http.NewRequest("POST", "/threat_models/x/chat/sessions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestTimmyMiddleware_Disabled404(t *testing.T) {
	assert.Equal(t, http.StatusNotFound, runTimmyMiddleware(t, config.TimmyConfig{Enabled: false}))
}

func TestTimmyMiddleware_EnabledUnconfigured503(t *testing.T) {
	assert.Equal(t, http.StatusServiceUnavailable, runTimmyMiddleware(t, config.TimmyConfig{Enabled: true}))
}

func TestTimmyMiddleware_EnabledConfiguredPasses(t *testing.T) {
	cfg := config.TimmyConfig{
		Enabled: true, LLMProvider: "openai", LLMModel: "gpt-5.5",
		TextEmbeddingProvider: "openai", TextEmbeddingModel: "text-embedding-3-large",
	}
	assert.Equal(t, http.StatusOK, runTimmyMiddleware(t, cfg))
}

func TestTimmyMiddleware_NonTimmyPathPasses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TimmyEnabledMiddleware(stubTimmyConfigReader{cfg: config.TimmyConfig{Enabled: false}}))
	r.GET("/threat_models", func(c *gin.Context) { c.Status(http.StatusOK) })
	req, _ := http.NewRequest("GET", "/threat_models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
