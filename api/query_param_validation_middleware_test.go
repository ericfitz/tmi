package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryParameterControlCharMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func(path string) *gin.Engine {
		router := gin.New()
		router.Use(QueryParameterControlCharMiddleware())
		router.GET(path, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
		return router
	}

	// Routes that returned 500 in CATS run 20261121T201842Z because PostgreSQL
	// rejects 0x00 in the LIKE filter (SQLSTATE 22021).
	rejected := []struct{ name, route, target, code string }{
		{"teams name", "/teams", "/teams?name=a%00b", "invalid_input"},
		{"projects status", "/projects", "/projects?status=x%00", "invalid_input"},
		{"admin users email", "/admin/users", "/admin/users?email=a%00b", "invalid_input"},
		{"admin groups provider", "/admin/groups", "/admin/groups?provider=t%00mi", "invalid_input"},
		{"audit system field_path", "/admin/audit/system", "/admin/audit/system?field_path=x%00", "invalid_input"},
		{"audit threat_models actor_provider", "/admin/audit/threat_models", "/admin/audit/threat_models?actor_provider=t%00mi", "invalid_input"},
		{"saml users email", "/saml/providers/:idp/users", "/saml/providers/tmi/users?email=x%00", "invalid_request"},
		{"other C0 control (BEL)", "/teams", "/teams?name=a%07b", "invalid_input"},
		{"control char in later param", "/teams", "/teams?limit=5&name=ok&q=%01", "invalid_input"},
		{"control char in parameter name", "/teams", "/teams?na%00me=x", "invalid_input"},
		{"bypass: malformed escape then NUL", "/teams", "/teams?x=%zz&name=a%00b", "invalid_input"},
		{"bypass: semicolon then NUL", "/teams", "/teams?a=1;b=2&name=%00", "invalid_input"},
		{"bypass: NUL then malformed escape", "/teams", "/teams?name=a%00b&x=%zz", "invalid_input"},
		{"invalid UTF-8 value", "/teams", "/teams?name=%ff", "invalid_input"},
		{"invalid UTF-8 name", "/teams", "/teams?na%ffme=x", "invalid_input"},
		{"C1 control", "/teams", "/teams?name=%C2%80", "invalid_input"},
		{"control char in repeated value", "/teams", "/teams?name=ok&name=%00", "invalid_input"},
	}
	for _, tc := range rejected {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newRouter(tc.route).ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))

			assert.Equal(t, http.StatusBadRequest, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.code, body["error"])
			assert.NotContains(t, w.Body.String(), "%00")
		})
	}

	accepted := []struct{ name, target string }{
		{"no query", "/teams"},
		{"plain values", "/teams?name=abc&limit=5"},
		{"encoded tab and newline", "/teams?name=a%09b%0Ac"},
		{"non-ASCII text", "/teams?name=%C3%A9t%C3%A9"},
		{"plus-encoded space", "/teams?name=a+b"},
		{"clean malformed escape left to other layers", "/teams?name=%zz"},
		{"clean semicolon left to other layers", "/teams?a=1;b=2"},
	}
	for _, tc := range accepted {
		t.Run("accepts "+tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			newRouter("/teams").ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.target, nil))
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}

	t.Run("applies to non-GET methods", func(t *testing.T) {
		router := gin.New()
		router.Use(QueryParameterControlCharMiddleware())
		router.DELETE("/teams", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/teams?name=a%00", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
