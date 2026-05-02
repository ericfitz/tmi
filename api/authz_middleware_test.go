package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// newAuthzTestRouter builds a Gin engine with a fixed test AuthzTable
// (loaded from fakeSpecJSON in authz_table_test.go) and the AuthzMiddleware
// installed. Test handlers respond 200 with the path so we can assert
// pass-through. JWT setup is simulated by setting context keys directly.
func newAuthzTestRouter(t *testing.T) (*gin.Engine, *AuthzTable) {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecJSON))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Test-only context shim: the production JWT middleware sets these keys.
	// isAdmin is always explicitly set (true or false) when userEmail is
	// present so that the RequireAdministrator short-circuit can distinguish
	// "authenticated non-admin" (isAdmin=false) from "unauthenticated"
	// (isAdmin absent).
	r.Use(func(c *gin.Context) {
		if email := c.GetHeader("X-Test-User-Email"); email != "" {
			c.Set("userEmail", email)
			// Explicitly set isAdmin so RequireAdministrator's test hook fires.
			c.Set("isAdmin", c.GetHeader("X-Test-Is-Admin") == "true")
		}
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))

	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path}) }
	r.GET("/health", ok)
	r.GET("/admin/users", ok)
	r.GET("/admin/users/:id", ok)
	r.DELETE("/admin/users/:id", ok)
	r.GET("/legacy/path", ok)

	return r, tbl
}

func doRequest(t *testing.T, r *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthzMiddleware_PublicEndpoint_AllowsAnonymous(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/health", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminEndpoint_RejectsAnonymous(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/admin/users", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminEndpoint_RejectsNonAdmin(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/admin/users", map[string]string{
		"X-Test-User-Email": "alice@example.com",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminEndpoint_AllowsAdmin(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/admin/users", map[string]string{
		"X-Test-User-Email": "charlie@example.com",
		"X-Test-Is-Admin":   "true",
	})
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminParameterized_AllowsAdmin(t *testing.T) {
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "DELETE", "/admin/users/abc-123", map[string]string{
		"X-Test-User-Email": "charlie@example.com",
		"X-Test-Is-Admin":   "true",
	})
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_LegacyPath_PassesThrough(t *testing.T) {
	// /legacy/path has no x-tmi-authz in fakeSpecJSON. Middleware must
	// pass through so existing per-resource middleware can take over.
	r, _ := newAuthzTestRouter(t)
	w := doRequest(t, r, "GET", "/legacy/path", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// fakeSpecUnsupportedRole exercises the default arm of checkAuthzRoles:
// security_reviewer is a recognized role name in the schema but slice 1
// (this PR) doesn't yet implement it. The middleware must skip-and-continue,
// then deny with 403 if no supported role satisfies the gate.
const fakeSpecUnsupportedRole = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/sec_only": {
      "get": {
        "operationId": "secOnly",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["security_reviewer"]}
      }
    },
    "/admin_or_sec": {
      "get": {
        "operationId": "adminOrSec",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none", "roles": ["admin", "security_reviewer"]}
      }
    }
  }
}`

func TestAuthzMiddleware_UnsupportedRoleOnly_Denies403(t *testing.T) {
	// A rule whose entire role list is unsupported must deny with 403,
	// not 500. (Pre-fix bug: returned 500.)
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecUnsupportedRole))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("isAdmin", false)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.GET("/sec_only", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := doRequest(t, r, "GET", "/sec_only", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminOrUnsupportedRole_AllowsAdmin(t *testing.T) {
	// A rule with [admin, security_reviewer] must allow an admin even though
	// security_reviewer is unsupported. (Pre-fix bug: 500'd if security_reviewer
	// appeared first in iteration.)
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecUnsupportedRole))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "charlie@example.com")
		c.Set("isAdmin", true)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.GET("/admin_or_sec", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := doRequest(t, r, "GET", "/admin_or_sec", nil)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_AdminOrUnsupportedRole_RejectsNonAdmin(t *testing.T) {
	// A rule with [admin, security_reviewer] must deny a non-admin with 403
	// (security_reviewer is unsupported and skipped; admin then evaluates
	// and denies via RequireAdministrator).
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecUnsupportedRole))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("isAdmin", false)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.GET("/admin_or_sec", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := doRequest(t, r, "GET", "/admin_or_sec", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_SetsAuthzCoveredFlag(t *testing.T) {
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecJSON))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "charlie@example.com")
		c.Set("isAdmin", true)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	var observedCovered bool
	r.GET("/admin/users", func(c *gin.Context) {
		v, _ := c.Get("authzCovered")
		observedCovered, _ = v.(bool)
		c.Status(http.StatusOK)
	})

	w := doRequest(t, r, "GET", "/admin/users", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !observedCovered {
		t.Error("authzCovered context flag was not set after middleware")
	}
}
