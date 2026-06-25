package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterStaticFiles_ServesEmbeddedAssets pins the fix for #498: static
// assets are served from the binary's embedded FS, not a relative ./static
// directory that does not exist in the production container image. Every route
// below previously 404'd in any containerized deployment.
func TestRegisterStaticFiles_ServesEmbeddedAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerStaticFiles(r)

	paths := []string{
		// Provider sign-in icons referenced by GET /oauth2/providers (#498).
		"/static/provider-logos/signin/github.svg",
		"/static/provider-logos/signin/google.svg",
		"/static/provider-logos/signin/microsoft.svg",
		"/static/provider-logos/signin/tmi.svg",
		"/static/provider-logos/signin/oauth.svg",
		// Individually-registered top-level static files.
		"/favicon.ico",
		"/favicon.svg",
		"/robots.txt",
		"/TMI-Logo.svg",
	}

	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusOK, w.Code, "expected 200 serving %s, body=%q", p, w.Body.String())
		assert.NotZerof(t, w.Body.Len(), "expected non-empty body serving %s", p)
	}
}

// TestRegisterStaticFiles_MissingAssetReturns404 confirms the static handler
// still 404s for assets that genuinely do not exist (no accidental catch-all).
func TestRegisterStaticFiles_MissingAssetReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerStaticFiles(r)

	req := httptest.NewRequest(http.MethodGet, "/static/provider-logos/signin/does-not-exist.svg", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}
