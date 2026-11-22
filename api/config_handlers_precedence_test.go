package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adminSettingsRouter serves the two admin settings read endpoints as an
// administrator, backed by the given settings service and config provider.
func adminSettingsRouter(t *testing.T, settings *MockSettingsService, cfg []MigratableSetting) *gin.Engine {
	t.Helper()
	original := GlobalGroupMemberRepository
	t.Cleanup(func() { restoreConfigStores(original) })
	GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: true}

	gin.SetMode(gin.TestMode)
	server := &Server{settingsService: settings, configProvider: &MockConfigProvider{settings: cfg}}
	userUUID := uuid.New()
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "admin@example.com")
		c.Set("userInternalUUID", userUUID.String())
		c.Set("userProvider", "test")
		c.Next()
	})
	r.GET("/admin/settings", server.ListSystemSettings)
	r.GET("/admin/settings/:key", func(c *gin.Context) { server.GetSystemSetting(c, c.Param("key")) })
	return r
}

// readAdminSetting returns key as reported by GET /admin/settings and by
// GET /admin/settings/{key}.
func readAdminSetting(t *testing.T, r *gin.Engine, key string) (listed, single map[string]any) {
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
	require.NotNil(t, listed, "%s missing from GET /admin/settings", key)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings/"+key, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &single))
	return listed, single
}

func addSettingWithOrigin(m *MockSettingsService, key, value, settingType, origin string) {
	m.AddSetting(key, value, settingType)
	if origin != "" {
		m.settings[key].Origin = models.NullableDBVarchar{String: origin, Valid: true}
	}
}

// The admin read endpoints must report the value the server actually uses,
// by the same precedence as SettingsService.GetResolvedString (#1161).
func TestAdminSettingsReads_FollowRuntimePrecedence(t *testing.T) {
	const key = "websocket.inactivity_timeout_seconds" // operational, admin-visible
	cases := []struct {
		name        string
		cfgExplicit bool
		rowValue    string
		rowOrigin   string // "" leaves Origin NULL (pre-#794 row: not explicit)
		wantValue   string
		wantSource  string
		wantRO      bool // key is Static, so PUT is refused and read_only is true whatever the source
	}{
		{"default config, seeded row", false, "100", models.SystemSettingOriginSeeded, "100", "database", true},
		{"default config, NULL-origin row", false, "100", "", "100", "database", true},
		{"default config, admin-set row", false, "100", models.SystemSettingOriginExplicit, "100", "database", true},
		{"explicit config, seeded row", true, "100", models.SystemSettingOriginSeeded, "200", "config", true},
		{"explicit config, admin-set row", true, "100", models.SystemSettingOriginExplicit, "100", "database", true},
		{"default config, empty row", false, "", models.SystemSettingOriginExplicit, "200", "config", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := NewMockSettingsService()
			addSettingWithOrigin(settings, key, tc.rowValue, "int", tc.rowOrigin)
			r := adminSettingsRouter(t, settings, []MigratableSetting{
				{Key: key, Value: "200", Type: "int", Source: "config", Explicit: tc.cfgExplicit},
			})

			listed, single := readAdminSetting(t, r, key)
			for name, got := range map[string]map[string]any{"list": listed, "get": single} {
				assert.Equal(t, tc.wantValue, got["value"], "%s value", name)
				assert.Equal(t, tc.wantSource, got["source"], "%s source", name)
				assert.Equal(t, tc.wantRO, got["read_only"], "%s read_only", name)
			}
		})
	}
}

// A secret whose database value wins must still be masked on both endpoints.
func TestAdminSettingsReads_DatabaseWinnerSecretIsMasked(t *testing.T) {
	const key = "timmy.llm_api_key" // operational, admin-visible, secret
	settings := NewMockSettingsService()
	addSettingWithOrigin(settings, key, "sk-live-value", "string", models.SystemSettingOriginExplicit)
	r := adminSettingsRouter(t, settings, []MigratableSetting{
		{Key: key, Value: "", Type: "string", Source: "config", Secret: true},
	})

	listed, single := readAdminSetting(t, r, key)
	for name, got := range map[string]map[string]any{"list": listed, "get": single} {
		assert.Equal(t, secretMaskConfigured, got["value"], "%s must mask the stored secret", name)
		assert.Equal(t, "database", got["source"], name)
	}
}

// The single GET now reads the database for config-supplied keys too, so a
// transient fault (retried, then still failing) must surface as the documented
// 503, not a 500.
func TestGetSystemSetting_TransientDBErrorIs503(t *testing.T) {
	const key = "websocket.inactivity_timeout_seconds"
	settings := NewMockSettingsService()
	settings.getErr = dberrors.Wrap(errors.New("ORA-03113: end-of-file on communication channel"), dberrors.ErrTransient)
	r := adminSettingsRouter(t, settings, []MigratableSetting{
		{Key: key, Value: "200", Type: "int", Source: "config", Explicit: true},
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/settings/"+key, nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
