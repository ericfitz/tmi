package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validatingSettingsService is the mock settings service with the real
// SettingsService validation in front of Set, so a rejected PUT produces the
// production error text.
type validatingSettingsService struct {
	*MockSettingsService
}

func (v validatingSettingsService) Set(ctx context.Context, setting *models.SystemSetting) error {
	svc := &SettingsService{}
	if err := svc.validateValue(setting); err != nil {
		return err
	}
	if err := svc.validateSettingSemantics(setting); err != nil {
		return err
	}
	return v.MockSettingsService.Set(ctx, setting)
}

// captureLogs redirects the global slogging logger to a temp dir and returns
// a function reading everything logged so far. The default logger is restored
// on cleanup.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, slogging.Initialize(slogging.Config{
		Level:  slogging.LogLevelDebug,
		IsDev:  true,
		LogDir: dir,
	}))
	t.Cleanup(func() {
		_ = slogging.Get().Close()
		_ = slogging.Initialize(slogging.Config{Level: slogging.LogLevelDebug, IsDev: true})
	})
	return func() string {
		b, err := os.ReadFile(filepath.Join(dir, "tmi.log"))
		require.NoError(t, err)
		return string(b)
	}
}

// A rejected PUT must not put the submitted value in the response body or the
// log. For a secret key that value is the plaintext secret.
func TestUpdateSystemSetting_RejectedValueIsNotEchoedOrLogged(t *testing.T) {
	const secret = "sk-live-SUPERSECRET-0123456789"
	for _, typ := range []string{"int", "bool", "float", "json"} {
		t.Run(typ, func(t *testing.T) {
			readLogs := captureLogs(t)
			server := &Server{
				settingsService: validatingSettingsService{NewMockSettingsService()},
				configProvider:  &MockConfigProvider{},
			}

			w := putSettingTyped(t, server, "timmy.llm_api_key", secret, typ)

			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), secret, "response body must not echo the submitted value")
			logs := readLogs()
			assert.Contains(t, logs, "timmy.llm_api_key", "the failure should still be logged by key")
			assert.False(t, strings.Contains(logs, secret), "log must not contain the submitted value")
		})
	}
}

// Every type-mismatch message in validateValue and the semantic validators
// names the problem without the offending value.
func TestSettingsValidationErrors_DoNotEmbedValue(t *testing.T) {
	const secret = "sk-live-SUPERSECRET-0123456789"
	s := &SettingsService{}
	for _, typ := range []models.DBVarchar{
		models.SystemSettingTypeInt, models.SystemSettingTypeBool,
		models.SystemSettingTypeFloat, models.SystemSettingTypeJSON,
	} {
		err := s.validateValue(&models.SystemSetting{
			SettingKey: "timmy.llm_api_key", SettingType: typ, Value: models.DBText(secret),
		})
		require.Error(t, err, string(typ))
		assert.NotContains(t, err.Error(), secret, string(typ))
	}
	for key := range settingSemanticValidators {
		err := ValidateSettingSemantics(key, secret, false)
		if err != nil {
			assert.NotContains(t, err.Error(), secret, key)
		}
	}
}

// The typed getters wrap strconv errors, which quote the offending input.
func TestParseCause_DropsInput(t *testing.T) {
	const secret = "sk-live-SUPERSECRET-0123456789"
	_, intErr := strconv.Atoi(secret)
	_, boolErr := strconv.ParseBool(secret)
	for _, err := range []error{intErr, boolErr} {
		require.Contains(t, err.Error(), secret, "precondition: strconv quotes its input")
		assert.NotContains(t, parseCause(err).Error(), secret)
	}
}
