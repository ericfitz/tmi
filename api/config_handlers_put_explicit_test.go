package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unsetSettingEnvVars unsets every SettingDef env var for the duration of the
// test, so a value exported in a developer or CI shell cannot make a key
// Explicit and change the real-provider results. t.Setenv registers the
// restore; os.Unsetenv then removes the variable.
func unsetSettingEnvVars(t *testing.T) {
	t.Helper()
	for _, d := range config.AllSettingDefs() {
		if d.EnvVar == "" {
			continue
		}
		t.Setenv(d.EnvVar, "")
		require.NoError(t, os.Unsetenv(d.EnvVar))
	}
}

// TestUpdateSystemSetting_ExplicitnessGuard pins #1160: for a key the config
// layer knows, PUT /admin/settings/{key} is allowed only when the setting is
// Hot, Operational and not explicitly set by the config file or environment.
// Static, bootstrap, explicit and provider-subtree keys keep 409; keys unknown
// to config are unaffected.
func TestUpdateSystemSetting_ExplicitnessGuard(t *testing.T) {
	hot := config.ConfigClass{Category: config.CategoryOperational, Mutability: config.MutabilityHot}
	static := config.ConfigClass{Category: config.CategoryOperational, Mutability: config.MutabilityStatic}
	bootstrap := config.ConfigClass{Category: config.CategoryBootstrap, Mutability: config.MutabilityStatic}

	tests := []struct {
		name       string
		settings   []MigratableSetting
		key        string
		wantStatus int
	}{
		{
			name:       "non-explicit hot is writable",
			settings:   []MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "config", Class: hot}},
			key:        "timmy.enabled",
			wantStatus: http.StatusOK,
		},
		{
			name:       "non-explicit static is rejected",
			settings:   []MigratableSetting{{Key: "websocket.inactivity_timeout_seconds", Value: "60", Type: "int", Source: "config", Class: static}},
			key:        "websocket.inactivity_timeout_seconds",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "explicit hot via config file is rejected",
			settings:   []MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "config", Explicit: true, Class: hot}},
			key:        "timmy.enabled",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "explicit hot via environment is rejected",
			settings:   []MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "environment", Explicit: true, Class: hot}},
			key:        "timmy.enabled",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "bootstrap is rejected",
			settings:   []MigratableSetting{{Key: "server.port", Value: "8080", Type: "string", Source: "config", Class: bootstrap}},
			key:        "server.port",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "oauth provider subtree key is rejected even when hot and not explicit",
			settings:   []MigratableSetting{{Key: "auth.oauth.providers.google.client_secret", Value: "x", Type: "string", Source: "config", Secret: true, Class: hot}},
			key:        "auth.oauth.providers.google.client_secret",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "saml provider subtree key is rejected",
			settings:   []MigratableSetting{{Key: "auth.saml.providers.corp.enabled", Value: "true", Type: "bool", Source: "config", Class: hot}},
			key:        "auth.saml.providers.corp.enabled",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "content oauth provider subtree key is rejected",
			settings:   []MigratableSetting{{Key: "content_oauth.providers.box.client_id", Value: "x", Type: "string", Source: "config", Class: hot}},
			key:        "content_oauth.providers.box.client_id",
			wantStatus: http.StatusConflict,
		},
		{
			name:       "key unknown to config is writable",
			settings:   []MigratableSetting{{Key: "timmy.enabled", Value: "false", Type: "bool", Source: "config", Explicit: true, Class: hot}},
			key:        "custom.admin_key",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalAdminStore := GlobalGroupMemberRepository
			defer restoreConfigStores(originalAdminStore)
			GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: true}

			gin.SetMode(gin.TestMode)
			r := gin.New()
			mockSettings := NewMockSettingsService()
			server := &Server{
				settingsService: mockSettings,
				configProvider:  &MockConfigProvider{settings: tt.settings},
			}
			userUUID := uuid.New()
			r.Use(func(c *gin.Context) {
				c.Set("userEmail", "test@example.com")
				c.Set("userInternalUUID", userUUID.String())
				c.Set("userProvider", "test")
				c.Next()
			})
			r.PUT("/admin/settings/:key", func(c *gin.Context) {
				server.UpdateSystemSetting(c, c.Param("key"))
			})

			req, _ := http.NewRequest("PUT", "/admin/settings/"+tt.key, strings.NewReader(`{"value": "x", "type": "string"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code, w.Body.String())

			stored, err := mockSettings.Get(context.Background(), tt.key)
			require.NoError(t, err)
			if tt.wantStatus == http.StatusOK {
				require.NotNil(t, stored, "accepted write must reach the settings service")
				assert.Equal(t, "x", string(stored.Value))
			} else {
				assert.Nil(t, stored, "rejected write must not reach the settings service")
			}
		})
	}
}

func putSetting(t *testing.T, server *Server, key, value string) *httptest.ResponseRecorder {
	t.Helper()
	originalAdminStore := GlobalGroupMemberRepository
	t.Cleanup(func() { restoreConfigStores(originalAdminStore) })
	GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: true}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "test@example.com")
		c.Set("userInternalUUID", uuid.New().String())
		c.Set("userProvider", "test")
		c.Next()
	})
	r.PUT("/admin/settings/:key", func(c *gin.Context) {
		server.UpdateSystemSetting(c, c.Param("key"))
	})
	req, _ := http.NewRequest("PUT", "/admin/settings/"+key, strings.NewReader(`{"value": "`+value+`", "type": "string"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestUpdateSystemSetting_RealConfigProvider drives the guard with the real
// provider over a real default Config, so the Class values are the ones
// production emits (registry-projected, without per-def Static overrides).
func TestUpdateSystemSetting_RealConfigProvider(t *testing.T) {
	unsetSettingEnvVars(t)
	cfg, err := config.LoadSettingsSource("")
	require.NoError(t, err)

	for _, key := range []string{
		"websocket.inactivity_timeout_seconds",
		"webhooks.allow_http_targets",
		"ssrf.webhook.allowlist", // omitted from GetMigratableSettings when empty
		"content_extractors.pptx_slides",
		"auth.jwt.expiration_seconds",
	} {
		t.Run("static "+key+" is rejected", func(t *testing.T) {
			ms := NewMockSettingsService()
			server := &Server{settingsService: ms, configProvider: NewConfigProviderAdapter(cfg)}
			w := putSetting(t, server, key, "1")
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			stored, _ := ms.Get(context.Background(), key)
			assert.Nil(t, stored)
		})
	}

	for _, key := range []string{"timmy.enabled", "ui.default_theme"} {
		t.Run("hot "+key+" is writable", func(t *testing.T) {
			ms := NewMockSettingsService()
			server := &Server{settingsService: ms, configProvider: NewConfigProviderAdapter(cfg)}
			w := putSetting(t, server, key, "x")
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			stored, err := ms.Get(context.Background(), key)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, "x", string(stored.Value))
		})
	}
}

// TestRuntimeWritableConfigSettings_ExactSet pins the set of config-known keys
// the PUT guard lets through when nothing is explicit. A change to a def's
// Category or Mutability, or a new Hot operational def, must update this list
// deliberately.
func TestRuntimeWritableConfigSettings_ExactSet(t *testing.T) {
	want := []string{
		"auth.everyone_is_a_reviewer",
		"auth.oauth.client_callback_allowlist",
		"auth.oauth_callback_url",
		"extraction.async_enabled",
		"features.webhooks_enabled",
		"features.websocket_enabled",
		"operator.contact",
		"operator.jurisdiction",
		"operator.name",
		"timmy.enabled",
		"ui.default_theme",
		"upload.max_file_size_mb",
		"websocket.max_participants",
	}

	unsetSettingEnvVars(t)
	cfg, err := config.LoadSettingsSource("")
	require.NoError(t, err)
	provider := NewConfigProviderAdapter(cfg)

	var got []string
	for _, d := range config.AllSettingDefs() {
		if blocked, _ := configWriteBlock(provider, d.Key); !blocked {
			got = append(got, d.Key)
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got, "writable config-known key set changed")
}
