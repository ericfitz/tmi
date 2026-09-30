//go:build oracle

package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSettingsReEncryptAllOracleIntegration is the live-Oracle check for the
// batched, resumable ReEncryptAll (#965 oracle-db-admin follow-up): LIKE /
// NOT LIKE on the CLOB value column, Find + FOR UPDATE (no ORA-02014), a NOT IN
// list of a few hundred skipped keys, and UpdateColumn leaving modified_at
// alone. Rows are namespaced zz965.reenc.* and removed on cleanup. Values are
// never logged.
//
// ReEncryptAll has no key filter, so it would rewrite every row in the shared
// system_settings table under the test key. The test therefore skips unless
// the table holds only its own rows. The test-only schema must also have no
// live TMI server attached: the guard counts once, so a concurrent writer
// could race it.
// SEM@5740a75fafc8da46a061901361ed61990a6c8916: verify batched re-encryption SQL shapes and row locking against Oracle ADB (writes DB)
func TestSettingsReEncryptAllOracleIntegration(t *testing.T) {
	gormDB := openAuditAppendOnlyOracleDB(t)
	ctx := context.Background()
	const ns = "zz965.reenc."

	if !gormDB.Migrator().HasTable(&models.SystemSetting{}) {
		require.NoError(t, gormDB.AutoMigrate(&models.SystemSetting{}))
	}
	var foreign int64
	require.NoError(t, gormDB.Model(&models.SystemSetting{}).
		Where("setting_key NOT LIKE ?", ns+"%").Count(&foreign).Error)
	if foreign > 0 {
		t.Skipf("system_settings holds %d non-test rows; ReEncryptAll would rewrite them", foreign)
	}
	cleanup := func() {
		gormDB.Where("setting_key LIKE ?", ns+"%").Delete(&models.SystemSetting{})
	}
	cleanup()
	t.Cleanup(cleanup)

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	enc, err := crypto.NewSettingsEncryptorFromKeys(key, nil, 1)
	require.NoError(t, err)
	svc := NewSettingsService(gormDB, nil)
	svc.SetEncryptor(enc)

	seed := func(k, v string) {
		require.NoError(t, gormDB.Create(&models.SystemSetting{
			SettingKey: models.DBVarchar(k), Value: models.DBText(v),
			SettingType: models.SystemSettingTypeString,
		}).Error)
	}

	// Good plaintext rows (re-encryptable) and undecryptable ones (ENC under an
	// unknown key id) to force a large NOT IN skip list, kept under the 900 cap.
	const good, bad = 5, 300
	for i := 0; i < good; i++ {
		seed(fmt.Sprintf("%sgood.%03d", ns, i), "plain")
	}
	for i := 0; i < bad; i++ {
		seed(fmt.Sprintf("%sbad.%03d", ns, i), "ENC:v1:99:AAAA")
	}
	var before models.SystemSetting
	require.NoError(t, gormDB.First(&before, "setting_key = ?", ns+"good.000").Error)

	n, errs, err := svc.ReEncryptAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, good, n)
	assert.Len(t, errs, bad)

	cnt, err := svc.CountValuesWithContextID(ctx, 1)
	require.NoError(t, err)
	assert.EqualValues(t, good, cnt)

	var after models.SystemSetting
	require.NoError(t, gormDB.First(&after, "setting_key = ?", ns+"good.000").Error)
	assert.NotEqual(t, string(before.Value), string(after.Value))
	assert.True(t, before.ModifiedAt.Equal(after.ModifiedAt), "UpdateColumn must not move modified_at")

	// Resumability: a second pass has nothing left to re-encrypt.
	n, _, err = svc.ReEncryptAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
