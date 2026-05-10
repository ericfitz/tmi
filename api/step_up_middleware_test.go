package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newGatedTestRouter(t *testing.T, table StepUpRouteTable, window time.Duration, authTime *time.Time) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Mimic the JWT-context shape: set userAuthTime before StepUpMiddleware runs.
	r.Use(func(c *gin.Context) {
		c.Set("userAuthTime", authTime)
		c.Next()
	})
	r.Use(StepUpMiddleware(window, table))
	r.PUT("/admin/settings/:key", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func tableWith(method, path string, required bool) StepUpRouteTable {
	tbl := StepUpRouteTable{required: map[stepUpRouteKey]bool{}}
	tbl.required[stepUpRouteKey{method: method, path: path}] = required
	return tbl
}

func TestStepUpMiddleware_FreshAuthTime_PassesThrough(t *testing.T) {
	authTime := time.Now().Add(-30 * time.Second)
	r := newGatedTestRouter(t, tableWith("PUT", "/admin/settings/{key}", true), 5*time.Minute, &authTime)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/admin/settings/foo", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", w.Code)
	}
}

func TestStepUpMiddleware_StaleAuthTime_Returns401WithChallenge(t *testing.T) {
	authTime := time.Now().Add(-10 * time.Minute)
	r := newGatedTestRouter(t, tableWith("PUT", "/admin/settings/{key}", true), 5*time.Minute, &authTime)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/admin/settings/foo", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
	wwwAuth := w.Header().Get("WWW-Authenticate")
	if !strings.Contains(wwwAuth, `error="insufficient_user_authentication"`) {
		t.Errorf("WWW-Authenticate: got %q, want contains insufficient_user_authentication", wwwAuth)
	}
	if !strings.Contains(wwwAuth, "max_age=300") {
		t.Errorf("WWW-Authenticate: got %q, want contains max_age=300", wwwAuth)
	}
}

func TestStepUpMiddleware_MissingAuthTime_Returns401(t *testing.T) {
	r := newGatedTestRouter(t, tableWith("PUT", "/admin/settings/{key}", true), 5*time.Minute, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/admin/settings/foo", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401 (missing auth_time should be treated as stale)", w.Code)
	}
}

func TestStepUpMiddleware_OptedOutRoute_PassesThroughEvenStale(t *testing.T) {
	authTime := time.Now().Add(-10 * time.Minute)
	r := newGatedTestRouter(t, tableWith("PUT", "/admin/settings/{key}", false), 5*time.Minute, &authTime)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/admin/settings/foo", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (opted-out route should not gate)", w.Code)
	}
}

func TestStepUpMiddleware_NonAdminRoute_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(StepUpMiddleware(5*time.Minute, StepUpRouteTable{required: map[stepUpRouteKey]bool{}}))
	r.PUT("/threat_models/:id", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/threat_models/foo", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200 (non-admin route should never gate)", w.Code)
	}
}
