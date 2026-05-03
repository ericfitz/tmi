package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeSpecMe represents the slice-4 (#367) annotations: every /me/* and the
// /me top-level operation are gated by `ownership: none`. The handler enforces
// subject-self by reading the JWT subject from context and scoping queries
// to that user — the middleware's job is only to pass authenticated callers
// through and to set the authzCovered flag so legacy resource middleware
// downstream can short-circuit.
const fakeSpecMe = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/me": {
      "get": {
        "operationId": "getCurrentUser",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      },
      "delete": {
        "operationId": "deleteCurrentUser",
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      }
    },
    "/me/client_credentials/{credential_id}": {
      "delete": {
        "operationId": "deleteClientCredential",
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      }
    },
    "/me/preferences": {
      "get": {
        "operationId": "getPreferences",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      },
      "put": {
        "operationId": "putPreferences",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      }
    },
    "/me/sessions": {
      "get": {
        "operationId": "listSessions",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      }
    }
  }
}`

func TestAuthzMiddleware_MeRoutes_PassThroughAndSetCovered(t *testing.T) {
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecMe))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Simulate JWT middleware having set userEmail.
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))

	var handlerSawCovered bool
	handler := func(c *gin.Context) {
		if v, ok := c.Get("authzCovered"); ok {
			b, _ := v.(bool)
			handlerSawCovered = b
		}
		c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path})
	}
	r.GET("/me", handler)
	r.DELETE("/me", handler)
	r.DELETE("/me/client_credentials/:credential_id", handler)
	r.GET("/me/preferences", handler)
	r.PUT("/me/preferences", handler)
	r.GET("/me/sessions", handler)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"get-me", http.MethodGet, "/me"},
		{"delete-me", http.MethodDelete, "/me"},
		{"delete-credential", http.MethodDelete, "/me/client_credentials/abc-123"},
		{"get-preferences", http.MethodGet, "/me/preferences"},
		{"put-preferences", http.MethodPut, "/me/preferences"},
		{"get-sessions", http.MethodGet, "/me/sessions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handlerSawCovered = false
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
			}
			if !handlerSawCovered {
				t.Error("authzCovered context flag was not propagated to the handler")
			}
		})
	}
}

// TestAuthzMiddleware_MeRoutes_NoOwnershipLookup asserts that /me/* routes do
// NOT trigger a parent-threat-model lookup. ThreatModelStore is set to nil
// here; if the middleware mistakenly tried to resolve a parent ACL, it would
// return 503. Confirming a 200 means the ownership=none short-circuit is
// taking effect for /me/* paths.
func TestAuthzMiddleware_MeRoutes_NoOwnershipLookup(t *testing.T) {
	original := ThreatModelStore
	defer func() { ThreatModelStore = original }()
	ThreatModelStore = nil

	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecMe))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.GET("/me/preferences", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/me/preferences", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; ThreatModelStore=nil should not 503 a /me/ route. body=%s",
			w.Code, w.Body.String())
	}
}
