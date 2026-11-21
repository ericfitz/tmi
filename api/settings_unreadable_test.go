package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/crypto"
)

// A system_settings row that cannot be decrypted (for example after a key
// rotation without the previous key) must never turn an admin read into a 500
// or break the resolver (#1169). The row is treated as unusable, exactly like
// a missing row.

const unreadableTestKey = "websocket.inactivity_timeout_seconds" // operational, admin-visible

func testEncryptor(t *testing.T, fill byte) *crypto.SettingsEncryptor {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = fill
	}
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	require.NoError(t, err)
	return enc
}

// seedUnreadableRow writes a row encrypted under one key and returns a
// service holding a different key, so reading the row fails to decrypt.
func seedUnreadableRow(t *testing.T, key, plaintext string) (*SettingsService, *gorm.DB) {
	t.Helper()
	db := setupSettingsTestDB(t)
	ciphertext, err := testEncryptor(t, 1).Encrypt(plaintext)
	require.NoError(t, err)
	row := models.SystemSetting{
		SettingKey:  models.DBVarchar(key),
		Value:       models.DBText(ciphertext),
		SettingType: models.SystemSettingTypeString,
		Origin:      models.NullableDBVarchar{String: models.SystemSettingOriginExplicit, Valid: true},
	}
	require.NoError(t, db.Create(&row).Error)
	s := NewSettingsService(db, nil)
	s.SetEncryptor(testEncryptor(t, 2))
	return s, db
}

func TestGetResolvedString_UnreadableRowFallsBackToConfig(t *testing.T) {
	s, _ := seedUnreadableRow(t, unreadableTestKey, "999")
	s.SetConfigProvider(&stubConfigProvider{settings: []MigratableSetting{
		operationalSetting(unreadableTestKey, "300", "config", true),
	}})

	got, exists, err := s.GetResolvedString(context.Background(), unreadableTestKey)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "300", got)
}

func TestGetResolvedString_UnreadableRowNoConfigIsAbsent(t *testing.T) {
	s, _ := seedUnreadableRow(t, unreadableTestKey, "999")

	got, exists, err := s.GetResolvedString(context.Background(), unreadableTestKey)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, got)
}

func TestSettingsService_List_UnreadableRowIsFlaggedNotFatal(t *testing.T) {
	s, db := seedUnreadableRow(t, unreadableTestKey, "999")
	// A readable row (written under the service's own key) next to it.
	ct, err := testEncryptor(t, 2).Encrypt("hello")
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.SystemSetting{
		SettingKey:  "ui.default_theme",
		Value:       models.DBText(ct),
		SettingType: models.SystemSettingTypeString,
	}).Error)

	rows, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byKey := map[string]models.SystemSetting{}
	for _, r := range rows {
		byKey[string(r.SettingKey)] = r
	}
	assert.True(t, byKey[unreadableTestKey].Unreadable)
	assert.Empty(t, string(byKey[unreadableTestKey].Value), "ciphertext must not be exposed")
	assert.False(t, byKey["ui.default_theme"].Unreadable)
	assert.Equal(t, "hello", string(byKey["ui.default_theme"].Value))
}

func unreadableRouter(t *testing.T, s *SettingsService, cfg []MigratableSetting) *gin.Engine {
	t.Helper()
	original := GlobalGroupMemberRepository
	t.Cleanup(func() { restoreConfigStores(original) })
	GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: true}

	gin.SetMode(gin.TestMode)
	server := &Server{settingsService: s, configProvider: &MockConfigProvider{settings: cfg}}
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
	r.DELETE("/admin/settings/:key", func(c *gin.Context) { server.DeleteSystemSetting(c, c.Param("key")) })
	return r
}

func doJSON(t *testing.T, r *gin.Engine, method, path string) (int, map[string]any, []map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	var one map[string]any
	var many []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &one)
	_ = json.Unmarshal(w.Body.Bytes(), &many)
	return w.Code, one, many
}

func TestListSystemSettings_UnreadableRowDoesNotFail(t *testing.T) {
	s, _ := seedUnreadableRow(t, unreadableTestKey, "999")
	r := unreadableRouter(t, s, nil)

	code, _, all := doJSON(t, r, http.MethodGet, "/admin/settings")
	require.Equal(t, http.StatusOK, code)
	var found map[string]any
	for _, e := range all {
		if e["key"] == unreadableTestKey {
			found = e
		}
	}
	require.NotNil(t, found, "unreadable row must still be listed")
	assert.Equal(t, "<unreadable>", found["value"])
	assert.Equal(t, "database", found["source"])
}

func TestListSystemSettings_UnreadableRowYieldsToConfig(t *testing.T) {
	s, _ := seedUnreadableRow(t, unreadableTestKey, "999")
	r := unreadableRouter(t, s, []MigratableSetting{
		operationalSetting(unreadableTestKey, "300", "config", true),
	})

	code, _, all := doJSON(t, r, http.MethodGet, "/admin/settings")
	require.Equal(t, http.StatusOK, code)
	for _, e := range all {
		if e["key"] == unreadableTestKey {
			assert.Equal(t, "300", e["value"])
			assert.Equal(t, "config", e["source"])
			return
		}
	}
	t.Fatal("key missing from list")
}

func TestGetSystemSetting_UnreadableRowUsesConfigValue(t *testing.T) {
	s, _ := seedUnreadableRow(t, unreadableTestKey, "999")
	r := unreadableRouter(t, s, []MigratableSetting{
		operationalSetting(unreadableTestKey, "300", "config", true),
	})

	code, one, _ := doJSON(t, r, http.MethodGet, "/admin/settings/"+unreadableTestKey)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "300", one["value"])
	assert.Equal(t, "config", one["source"])
}

func TestGetSystemSetting_UnreadableRowNoConfigIsMasked(t *testing.T) {
	s, _ := seedUnreadableRow(t, unreadableTestKey, "999")
	r := unreadableRouter(t, s, nil)

	code, one, _ := doJSON(t, r, http.MethodGet, "/admin/settings/"+unreadableTestKey)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "<unreadable>", one["value"])
	assert.Equal(t, "database", one["source"])
}

// An operator must be able to delete the unreadable row to repair it.
func TestDeleteSystemSetting_UnreadableRowCanBeDeleted(t *testing.T) {
	s, db := seedUnreadableRow(t, unreadableTestKey, "999")
	r := unreadableRouter(t, s, nil)

	code, _, _ := doJSON(t, r, http.MethodDelete, "/admin/settings/"+unreadableTestKey)
	require.Equal(t, http.StatusNoContent, code)
	var n int64
	require.NoError(t, db.Model(&models.SystemSetting{}).Count(&n).Error)
	assert.Zero(t, n)
}
