package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The read_only flag the admin reads report must equal "PUT would be refused",
// the decision configWriteBlock makes, so a UI that disables editing on the
// flag agrees with the server.
func TestAdminSettingsReads_ReadOnlyMatchesPutOutcome(t *testing.T) {
	cases := []struct {
		name   string
		key    string
		cfg    []MigratableSetting
		row    string // "" = no row
		origin string
	}{
		{"hot, no config explicit, no row", "timmy.enabled",
			[]MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "config"}}, "", ""},
		{"hot, explicit file config, no row", "timmy.enabled",
			[]MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "config", Explicit: true}}, "", ""},
		{"hot, explicit env config, no row", "timmy.enabled",
			[]MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "environment", Explicit: true}}, "", ""},
		{"hot, explicit env config, explicit row", "timmy.enabled",
			[]MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "environment", Explicit: true}}, "true", models.SystemSettingOriginExplicit},
		{"static, seeded row", "websocket.inactivity_timeout_seconds",
			[]MigratableSetting{{Key: "websocket.inactivity_timeout_seconds", Value: "200", Type: "int", Source: "config"}}, "100", models.SystemSettingOriginSeeded},
		{"static, no row", "websocket.inactivity_timeout_seconds",
			[]MigratableSetting{{Key: "websocket.inactivity_timeout_seconds", Value: "200", Type: "int", Source: "config"}}, "", ""},
		{"bootstrap", "auth.oauth_callback_url",
			[]MigratableSetting{{Key: "auth.oauth_callback_url", Value: "https://x/cb", Type: "string", Source: "config"}}, "", ""},
		{"provider subtree", "auth.oauth.providers.google.client_id",
			[]MigratableSetting{{Key: "auth.oauth.providers.google.client_id", Value: "x", Type: "string", Source: "config"}}, "", ""},
		{"custom key (row only)", "custom.admin_key", nil, "v", models.SystemSettingOriginExplicit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := NewMockSettingsService()
			if tc.row != "" {
				addSettingWithOrigin(settings, tc.key, tc.row, "string", tc.origin)
				if len(tc.cfg) > 0 {
					settings.settings[tc.key].SettingType = models.DBVarchar(tc.cfg[0].Type)
				}
			}
			r := adminSettingsRouter(t, settings, tc.cfg)
			server := &Server{settingsService: settings, configProvider: &MockConfigProvider{settings: tc.cfg}}
			r.PUT("/admin/settings/:key", func(c *gin.Context) { server.UpdateSystemSetting(c, c.Param("key")) })

			// PUT outcome on a fresh copy of the state.
			put := httptest.NewRecorder()
			body := `{"value":"x","type":"string"}`
			if len(tc.cfg) > 0 {
				body = `{"value":"` + putValueFor(tc.cfg[0].Type) + `","type":"` + tc.cfg[0].Type + `"}`
			}
			req := httptest.NewRequest(http.MethodPut, "/admin/settings/"+tc.key, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			// Reads first (PUT mutates the mock), PUT last.
			listed, single := readAdminSettingOrNil(t, r, tc.key)
			r.ServeHTTP(put, req)
			blocked := put.Code == http.StatusConflict
			require.Contains(t, []int{http.StatusOK, http.StatusConflict}, put.Code, put.Body.String())

			for name, got := range map[string]map[string]any{"list": listed, "get": single} {
				require.NotNil(t, got, "%s: key not visible", name)
				assert.Equal(t, blocked, got["read_only"], "%s read_only must equal (PUT refused)", name)
			}
		})
	}
}

func putValueFor(typ string) string {
	switch typ {
	case "bool":
		return "true"
	case "int":
		return "5"
	}
	return "x"
}

// readAdminSettingOrNil is readAdminSetting without the require on presence.
func readAdminSettingOrNil(t *testing.T, r *gin.Engine, key string) (listed, single map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var all []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &all))
	for _, s := range all {
		if s["key"] == key {
			listed = s
		}
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings/"+key, nil))
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &single))
	}
	return listed, single
}
