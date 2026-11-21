package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var startupOnlyAPIKeys = []string{"auth.oauth_callback_url", "features.saml_enabled"}

// auth.oauth_callback_url and features.saml_enabled are startup-only (#1167):
// PUT must return 409 and must not reach the settings service.
func TestUpdateSystemSetting_StartupOnlyKeysReturn409(t *testing.T) {
	unsetSettingEnvVars(t)
	cfg, err := config.LoadSettingsSource("")
	require.NoError(t, err)

	for _, key := range startupOnlyAPIKeys {
		t.Run(key, func(t *testing.T) {
			ms := NewMockSettingsService()
			server := &Server{settingsService: ms, configProvider: NewConfigProviderAdapter(cfg)}
			w := putSetting(t, server, key, "x")
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			stored, _ := ms.Get(context.Background(), key)
			assert.Nil(t, stored)
		})
	}
}

// The public client config serves features.saml_enabled from the startup
// config value; a database row for the key is inert (#1167).
func TestGetClientConfig_SamlEnabledIsStartupConfig(t *testing.T) {
	cases := []struct {
		name     string
		cfgValue string
		rowValue string // "" = no row
		wantSaml bool
	}{
		{name: "config true, no row", cfgValue: "true", wantSaml: true},
		{name: "config true, row false is inert", cfgValue: "true", rowValue: "false", wantSaml: true},
		{name: "config false, row true is inert", cfgValue: "false", rowValue: "true", wantSaml: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ms := NewMockSettingsService()
			if tc.rowValue != "" {
				ms.AddSetting("features.saml_enabled", tc.rowValue, "bool")
			}
			server := &Server{
				settingsService: ms,
				configProvider: &MockConfigProvider{settings: []MigratableSetting{
					{Key: "features.saml_enabled", Value: tc.cfgValue, Type: "bool", Class: config.ClassificationFor("features.saml_enabled")},
				}},
			}
			r := gin.New()
			r.GET("/config", server.GetClientConfig)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/config", nil))
			require.Equal(t, http.StatusOK, w.Code)

			var cc ClientConfig
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cc))
			require.NotNil(t, cc.Features)
			require.NotNil(t, cc.Features.SamlEnabled)
			assert.Equal(t, tc.wantSaml, *cc.Features.SamlEnabled)
		})
	}
}

// With no config provider the flag keeps its code default (false).
func TestGetClientConfig_SamlEnabledNoProviderDefaultsFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ms := NewMockSettingsService()
	ms.AddSetting("features.saml_enabled", "true", "bool")
	server := &Server{settingsService: ms}
	r := gin.New()
	r.GET("/config", server.GetClientConfig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/config", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var cc ClientConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &cc))
	require.NotNil(t, cc.Features.SamlEnabled)
	assert.False(t, *cc.Features.SamlEnabled)
}

// A stale database row (seeded before #1167, or admin-set) for a startup-only
// key must not be reported by the admin reads: the server does not use it.
func TestAdminSettingsReads_StartupOnlyKeyIgnoresStaleRow(t *testing.T) {
	ms := NewMockSettingsService()
	addSettingWithOrigin(ms, "auth.oauth_callback_url", "http://stale.test/cb", "string", "admin")
	cfg := []MigratableSetting{{
		Key: "auth.oauth_callback_url", Value: "http://real.test/cb", Type: "string",
		Class: config.ClassificationFor("auth.oauth_callback_url"),
	}}
	r := adminSettingsRouter(t, ms, cfg)
	listed, single := readAdminSetting(t, r, "auth.oauth_callback_url")
	assert.Equal(t, "http://real.test/cb", listed["value"])
	assert.Equal(t, "http://real.test/cb", single["value"])
}

// A startup-only row with no config value to report (the callback URL is
// omitted when empty) must not be listed as a writable database setting, and a
// single read must be a 404, never a 500.
func TestAdminSettingsReads_StartupOnlyKeyWithoutConfigIsHidden(t *testing.T) {
	ms := NewMockSettingsService()
	addSettingWithOrigin(ms, "auth.oauth_callback_url", "http://stale.test/cb", "string", "admin")
	r := adminSettingsRouter(t, ms, nil)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var all []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &all))
	for _, s := range all {
		assert.NotEqual(t, "auth.oauth_callback_url", s["key"])
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings/auth.oauth_callback_url", nil))
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
