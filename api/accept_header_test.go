package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func acceptTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(AcceptHeaderValidation())
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) }
	r.GET("/test", ok)
	r.GET("/saml/:provider/metadata", ok)
	r.POST("/threat_models/:threat_model_id/chat/sessions", ok)
	r.POST("/threat_models/:threat_model_id/chat/sessions/:session_id/messages", ok)
	return r
}

func doAccept(r *gin.Engine, method, path, accept string, set bool) int {
	req := httptest.NewRequest(method, path, nil)
	if set {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestAcceptHeaderStrictParsing(t *testing.T) {
	r := acceptTestRouter()
	tests := []struct {
		name   string
		accept string
		want   int
	}{
		{"json", "application/json", 200},
		{"json with charset", "application/json; charset=utf-8", 200},
		{"json with charset no space", "application/json;charset=utf-8", 200},
		{"json uppercase", "Application/JSON", 200},
		{"wildcard", "*/*", 200},
		{"wildcard with q", "*/*;q=0.8", 200},
		{"application wildcard", "application/*", 200},
		{"browser", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", 200},
		{"tmi-ux", "application/json, text/plain, */*", 200},
		{"curl", "*/*", 200},
		{"trailing semicolon", "application/json;", 200},
		{"empty parameter between", "application/json; ;q=0.5", 200},
		{"list with json", "text/html, application/json;q=0.9, */*;q=0.1", 200},
		{"list extra whitespace", "  text/html ,\tapplication/json  ;  q=0.5 ", 200},
		{"empty list elements tolerated", "text/html,,application/json", 200},
		{"q=1", "application/json;q=1", 200},
		{"q=1.000", "application/json;q=1.000", 200},
		{"q=0.123", "application/json;q=0.123", 200},
		{"uppercase Q", "application/json;Q=0.5", 200},
		{"quoted param with comma and semicolon", `application/json;foo="a,b;c"`, 200},
		{"empty header", "", 200},
		{"whitespace-only header", "   ", 200},

		{"q=0 json", "application/json;q=0", 406},
		{"q=0.000 json", "application/json;q=0.000", 406},
		{"q=0 wildcard", "*/*;q=0", 406},
		{"specific q=0 beats wildcard for json only", "application/json;q=0, application/*;q=0, text/event-stream;q=0, */*", 406},
		{"unsupported type", "text/xml", 406},
		{"event stream on JSON-only route", "text/event-stream", 406},
		{"text wildcard on JSON-only route", "text/*", 406},
		{"over 4KB header", "application/json, " + strings.Repeat("text/html, ", 400) + "*/*", 406},
		{"text/csv on ordinary route", "text/csv", 406},
		{"malformed q word", "*/*; q=invalid", 406},
		{"q above 1", "application/json;q=1.5", 406},
		{"q four decimals", "application/json;q=0.1234", 406},
		{"q=1.001", "application/json;q=1.001", 406},
		{"q negative", "application/json;q=-0.1", 406},
		{"q empty", "application/json;q=", 406},
		{"q=2", "*/*;q=2", 406},
		{"malformed entry among valid ones", "application/json, */*;q=bad", 406},
		{"no slash", "foo", 406},
		{"wildcard type with subtype", "*/json", 406},
		{"empty subtype", "application/", 406},
		{"empty type", "/json", 406},
		{"space in range", "application /json", 406},
		{"param without value", "application/json;charset", 406},
		{"unterminated quote", `application/json;foo="bar`, 406},
		{"junk after valid", "application/json junk", 406},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := doAccept(r, "GET", "/test", tc.accept, true)
			assert.Equal(t, tc.want, got, "Accept: %q", tc.accept)
		})
	}

	t.Run("absent header", func(t *testing.T) {
		assert.Equal(t, 200, doAccept(r, "GET", "/test", "", false))
	})
}

func TestAcceptHeaderSAMLMetadata(t *testing.T) {
	r := acceptTestRouter()

	t.Run("samlmetadata+xml reaches handler", func(t *testing.T) {
		assert.Equal(t, 200, doAccept(r, "GET", "/saml/p1/metadata", "application/samlmetadata+xml", true))
	})
	t.Run("samlmetadata+xml with q", func(t *testing.T) {
		assert.Equal(t, 200, doAccept(r, "GET", "/saml/p1/metadata", "application/samlmetadata+xml;q=0.9", true))
	})
	t.Run("samlmetadata+xml with q=0 is rejected", func(t *testing.T) {
		assert.Equal(t, 406, doAccept(r, "GET", "/saml/p1/metadata", "application/samlmetadata+xml;q=0", true))
	})
	t.Run("malformed header on saml route is 406", func(t *testing.T) {
		assert.Equal(t, 406, doAccept(r, "GET", "/saml/p1/metadata", "application/samlmetadata+xml, */json", true))
	})
	t.Run("unknown media type on routed saml path is 406", func(t *testing.T) {
		assert.Equal(t, 406, doAccept(r, "GET", "/saml/p1/metadata", "text/csv", true))
	})
	t.Run("samlmetadata+xml on other route is 406", func(t *testing.T) {
		assert.Equal(t, 406, doAccept(r, "GET", "/test", "application/samlmetadata+xml", true))
	})
	t.Run("unrouted method gets 405 not 406", func(t *testing.T) {
		for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
			assert.Equal(t, 405, doAccept(r, m, "/saml/p1/metadata", "text/xml", true), m)
		}
	})
	t.Run("unrouted path gets 404 not 406", func(t *testing.T) {
		assert.Equal(t, 404, doAccept(r, "GET", "/nope", "text/xml", true))
		assert.Equal(t, 404, doAccept(r, "GET", "/nope", "*/*; q=invalid", true))
	})
}

func TestAcceptHeaderEventStreamRoutes(t *testing.T) {
	r := acceptTestRouter()
	for _, path := range []string{
		"/threat_models/tm1/chat/sessions",
		"/threat_models/tm1/chat/sessions/s1/messages",
	} {
		assert.Equal(t, 200, doAccept(r, "POST", path, "text/event-stream", true), path)
		assert.Equal(t, 200, doAccept(r, "POST", path, "text/*", true), path)
		assert.Equal(t, 200, doAccept(r, "POST", path, "application/json", true), path)
		assert.Equal(t, 406, doAccept(r, "POST", path, "text/event-stream;q=0", true), path)
		assert.Equal(t, 406, doAccept(r, "POST", path, "text/csv", true), path)
	}
}

func TestAcceptHeaderSizeBoundary(t *testing.T) {
	r := acceptTestRouter()
	pad := func(n int) string { return "application/json;x=" + strings.Repeat("a", n-len("application/json;x=")) }
	assert.Equal(t, 200, doAccept(r, "GET", "/test", pad(4096), true))
	assert.Equal(t, 406, doAccept(r, "GET", "/test", pad(4097), true))
}
