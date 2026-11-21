package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpenAPIValidation_ThreatStatusCappedAtColumnSize guards CATS run
// 20261121T201842Z: threats.status is varchar(128) but the spec allowed 256
// characters, so a 129..256 char status reached the database and returned 500.
// The request validator must reject it with 400 before any handler runs.
func TestOpenAPIValidation_ThreatStatusCappedAtColumnSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)

	r := gin.New()
	r.Use(validator)
	r.POST("/threat_models/:threat_model_id/threats", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	tests := []struct {
		name   string
		status string
		want   int
	}{
		{"at column size", strings.Repeat("a", 128), http.StatusCreated},
		{"one over column size", strings.Repeat("a", 129), http.StatusBadRequest},
		{"spec-old maximum", strings.Repeat("a", 256), http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"name":"T","status":"` + tt.status + `"}`
			req := httptest.NewRequest(http.MethodPost, "/threat_models/"+testUUID1+"/threats", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code, w.Body.String())
		})
	}
}

// TestValidateSparseAuthorizationEntries_RejectsOverlongSubject guards the
// groups.group_name varchar(500) and groups.provider varchar(100) columns on
// the JSON Patch path, which bypasses the request-body schema limits.
func TestValidateSparseAuthorizationEntries_RejectsOverlongSubject(t *testing.T) {
	entry := func(provider, providerID string) []Authorization {
		return []Authorization{{
			Provider:      provider,
			ProviderId:    providerID,
			PrincipalType: AuthorizationPrincipalTypeGroup,
			Role:          AuthorizationRoleReader,
		}}
	}
	tests := []struct {
		name    string
		entries []Authorization
		wantErr bool
	}{
		{"group name at limit", entry("tmi", strings.Repeat("g", 500)), false},
		{"group name over limit", entry("tmi", strings.Repeat("g", 501)), true},
		{"provider at limit", entry(strings.Repeat("p", 100), "grp"), false},
		{"provider over limit", entry(strings.Repeat("p", 101), "grp"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSparseAuthorizationEntries(tt.entries)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			var reqErr *RequestError
			require.ErrorAs(t, err, &reqErr)
			assert.Equal(t, http.StatusBadRequest, reqErr.Status)
		})
	}
}
