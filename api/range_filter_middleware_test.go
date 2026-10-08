package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRangeFilterRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RangeFilterValidationMiddleware())
	ok := func(c *gin.Context) { c.String(http.StatusOK, "ok") }
	for route := range rangeFilterRoutes {
		r.GET(route, ok)
		r.POST(route, ok)
	}
	r.GET("/unrelated", ok)
	return r
}

func doRange(r *gin.Engine, method, path string, q url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path+"?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestRangeFilterValidationMiddleware_Pairs covers, for every configured pair,
// inverted, equal, valid, one-bound, and unparsable inputs.
func TestRangeFilterValidationMiddleware_Pairs(t *testing.T) {
	r := newRangeFilterRouter()
	const early, late = "2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"
	concrete := map[string]string{
		"/threat_models/:threat_model_id/threats":     "/threat_models/abc/threats",
		"/threat_models/:threat_model_id/audit_trail": "/threat_models/abc/audit_trail",
	}

	for route, pairs := range rangeFilterRoutes {
		path := route
		if p, ok := concrete[route]; ok {
			path = p
		}
		for _, p := range pairs {
			lo, hi, mid := early, late, "2026-01-15T00:00:00Z"
			if p.Kind == rangeKindNumber {
				lo, hi, mid = "2.5", "7.5", "5"
			}
			t.Run(route+" "+p.Lower+"/"+p.Upper, func(t *testing.T) {
				// inverted
				w := doRange(r, http.MethodGet, path, url.Values{p.Lower: {hi}, p.Upper: {lo}})
				require.Equal(t, http.StatusBadRequest, w.Code)
				var body map[string]any
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, "invalid_input", body["error"])
				desc, _ := body["error_description"].(string)
				assert.Contains(t, desc, p.Lower)
				assert.Contains(t, desc, p.Upper)

				// equal
				w = doRange(r, http.MethodGet, path, url.Values{p.Lower: {mid}, p.Upper: {mid}})
				if p.LowerExclusive || p.UpperExclusive {
					assert.Equal(t, http.StatusBadRequest, w.Code, "equal with an exclusive bound is empty")
				} else {
					assert.Equal(t, http.StatusOK, w.Code, "equal inclusive bounds is a single-value range")
				}

				// valid
				assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, path, url.Values{p.Lower: {lo}, p.Upper: {hi}}).Code)

				// one bound only
				assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, path, url.Values{p.Lower: {hi}}).Code)
				assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, path, url.Values{p.Upper: {lo}}).Code)

				// unparsable or empty value passes through to existing validation
				assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, path, url.Values{p.Lower: {"garbage"}, p.Upper: {lo}}).Code)
				assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, path, url.Values{p.Lower: {hi}, p.Upper: {"garbage"}}).Code)
				assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, path, url.Values{p.Lower: {""}, p.Upper: {lo}}).Code)
			})
		}
	}
}

func TestRangeFilterValidationMiddleware_Instants(t *testing.T) {
	r := newRangeFilterRouter()
	// Same instant in different offsets: equal, valid for inclusive /admin/users.
	q := url.Values{"created_after": {"2026-01-01T01:00:00+01:00"}, "created_before": {"2026-01-01T00:00:00Z"}}
	assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, "/admin/users", q).Code)
	// Later wall-clock reading but earlier instant: compared as instants.
	q = url.Values{"created_after": {"2026-01-01T05:00:00+09:00"}, "created_before": {"2026-01-01T00:00:00Z"}}
	assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, "/admin/users", q).Code, "05:00+09:00 is 20:00Z the day before: valid")
	q = url.Values{"created_after": {"2026-01-01T09:00:00+09:00"}, "created_before": {"2025-12-31T23:00:00Z"}}
	assert.Equal(t, http.StatusBadRequest, doRange(r, http.MethodGet, "/admin/users", q).Code, "00:00Z after 23:00Z the day before: inverted")
	// Sub-second precision matters.
	q = url.Values{"created_after": {"2026-01-01T00:00:00.000000002Z"}, "created_before": {"2026-01-01T00:00:00.000000001Z"}}
	assert.Equal(t, http.StatusBadRequest, doRange(r, http.MethodGet, "/admin/users", q).Code)
}

func TestRangeFilterValidationMiddleware_IgnoresUnrelated(t *testing.T) {
	r := newRangeFilterRouter()
	inverted := url.Values{"created_after": {"2026-02-01T00:00:00Z"}, "created_before": {"2026-01-01T00:00:00Z"}}
	assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, "/unrelated", inverted).Code, "unlisted route")
	assert.Equal(t, http.StatusOK, doRange(r, http.MethodPost, "/admin/users", inverted).Code, "non-GET method")
	// A pair not configured for the route is ignored (usability_feedback has no modified pair).
	q := url.Values{"modified_after": {"2026-02-01T00:00:00Z"}, "modified_before": {"2026-01-01T00:00:00Z"}}
	assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, "/usability_feedback", q).Code)
}

func TestRangeFilterValidationMiddleware_ExclusivityPerRoute(t *testing.T) {
	r := newRangeFilterRouter()
	same := url.Values{"created_after": {"2026-01-01T00:00:00Z"}, "created_before": {"2026-01-01T00:00:00Z"}}
	assert.Equal(t, http.StatusBadRequest, doRange(r, http.MethodGet, "/threat_models/x/threats", same).Code, "threats: exclusive/exclusive")
	assert.Equal(t, http.StatusBadRequest, doRange(r, http.MethodGet, "/usability_feedback", same).Code, "feedback: before is exclusive")
	assert.Equal(t, http.StatusOK, doRange(r, http.MethodGet, "/threat_models", same).Code, "threat models: inclusive")
}
