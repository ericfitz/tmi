package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timmyStaticKeys are the timmy.* keys that must stay Static (PUT returns 409)
// because no live runtime reader honors a database value (#1163):
//   - llm_max_tokens: TimmyConfigProvider.Current never reads it.
//   - query_decomposition_enabled, rerank_top_k, max_memory_mb,
//     inactivity_timeout_seconds: read only while building the runtime
//     (cmd/server/main.go timmyBuilder), and absent from
//     TimmyConfigProvider.WiringHash, so a change does not trigger a rebuild.
//   - embedding_cleanup_interval_minutes, embedding_idle_days_active,
//     embedding_idle_days_closed, dump_extracted_text_to_note: read once at
//     startup from the boot config (cmd/server/main.go).
var timmyStaticKeys = []string{
	"timmy.llm_max_tokens",
	"timmy.query_decomposition_enabled",
	"timmy.rerank_top_k",
	"timmy.max_memory_mb",
	"timmy.inactivity_timeout_seconds",
	"timmy.embedding_cleanup_interval_minutes",
	"timmy.embedding_idle_days_active",
	"timmy.embedding_idle_days_closed",
	"timmy.dump_extracted_text_to_note",
}

// Every timmy.* def not in timmyStaticKeys is read live (wiring hash or
// per-request live config) and must be Hot; TimmyConfigProvider reads exactly
// those keys.
func TestTimmySettingMutabilityMatchesRuntimeReaders(t *testing.T) {
	static := map[string]bool{}
	for _, k := range timmyStaticKeys {
		static[k] = true
	}
	seen := 0
	for _, d := range config.AllSettingDefs() {
		if !strings.HasPrefix(d.Key, "timmy.") {
			continue
		}
		seen++
		want := config.MutabilityHot
		if static[d.Key] {
			want = config.MutabilityStatic
		}
		assert.Equal(t, want, d.Class.Mutability, "%s mutability", d.Key)
	}
	assert.Equal(t, 37, seen, "timmy.* def count changed; classify the new key")
}

// A newly writable timmy secret must be stored encrypted by PUT and masked by
// GET (and by the PUT response).
func TestPutTimmySecretKeys_StoredEncryptedAndMasked(t *testing.T) {
	unsetSettingEnvVars(t)
	cfg, err := config.LoadSettingsSource("")
	require.NoError(t, err)

	keyBytes := make([]byte, 32)
	_, err = rand.Read(keyBytes)
	require.NoError(t, err)
	enc, err := crypto.NewSettingsEncryptorFromKeys(keyBytes, nil, 1)
	require.NoError(t, err)

	for _, key := range []string{
		"timmy.llm_api_key",
		"timmy.text_embedding_api_key",
		"timmy.code_embedding_api_key",
		"timmy.rerank_api_key",
	} {
		t.Run(key, func(t *testing.T) {
			const secret = "sk-test-plaintext-value"
			gormDB := setupSettingsTestDB(t)
			svc := NewSettingsService(gormDB, nil)
			svc.SetEncryptor(enc)
			server := &Server{settingsService: svc, configProvider: NewConfigProviderAdapter(cfg)}

			w := putSetting(t, server, key, secret)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), secret, "PUT response must not echo the secret")

			var row models.SystemSetting
			require.NoError(t, gormDB.Where("setting_key = ?", key).First(&row).Error)
			assert.NotEqual(t, secret, string(row.Value))
			assert.True(t, crypto.IsEncrypted(string(row.Value)), "stored value must be encrypted")
			plain, err := enc.Decrypt(string(row.Value))
			require.NoError(t, err)
			assert.Equal(t, secret, plain)

			// The runtime reader still sees the plaintext.
			got, err := svc.GetString(context.Background(), key)
			require.NoError(t, err)
			assert.Equal(t, secret, got)

			gin.SetMode(gin.TestMode)
			original := GlobalGroupMemberRepository
			t.Cleanup(func() { restoreConfigStores(original) })
			GlobalGroupMemberRepository = &mockGroupMemberStoreForAdmin{isAdminResult: true}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("userEmail", "admin@example.com")
				c.Set("userInternalUUID", uuid.New().String())
				c.Set("userProvider", "test")
				c.Next()
			})
			r.GET("/admin/settings/:key", func(c *gin.Context) { server.GetSystemSetting(c, c.Param("key")) })
			gw := httptest.NewRecorder()
			r.ServeHTTP(gw, httptest.NewRequest(http.MethodGet, "/admin/settings/"+key, nil))
			require.Equal(t, http.StatusOK, gw.Code, gw.Body.String())
			assert.NotContains(t, gw.Body.String(), secret)
			var body map[string]any
			require.NoError(t, json.Unmarshal(gw.Body.Bytes(), &body))
			assert.Equal(t, secretMaskConfigured, body["value"])
		})
	}
}
