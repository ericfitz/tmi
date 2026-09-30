package rotator

import (
	"bytes"
	"context"
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/crypto"
)

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: in-memory settings store test double for key rotation
type fakeSettings struct {
	rows      map[int]int64 // rows per key id
	reencrypt int
	lastKr    Keyring
}

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: re-encrypt fake settings rows under the current key (test double)
func (f *fakeSettings) ReEncrypt(_ context.Context, kr Keyring) (int, error) {
	f.reencrypt++
	f.lastKr = kr
	var moved int64
	for id, n := range f.rows {
		if id != kr.CurrentID {
			moved += n
			delete(f.rows, id)
		}
	}
	f.rows[kr.CurrentID] += moved
	return int(moved), nil
}

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: count fake settings rows under a key id (test double)
func (f *fakeSettings) CountWithID(_ context.Context, _ Keyring, id int) (int64, error) {
	return f.rows[id], nil
}

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: build a settings-key secret fixture for tests
func settingsSecret() *Secret {
	return &Secret{Name: "tmi-secrets", Data: map[string]string{
		"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "0000000000000000000000000000000000000000000000000000000000000001",
		"TMI_DATABASE_URL":                   "postgres://x",
	}, Annotations: map[string]string{}}
}

// SEM@f5ed9122de949614f44d9fb1970d38d90f2d9081: test that StageThenPromoteThenReencrypt
func TestSettingsKeyRotation_StageThenPromoteThenReencrypt(t *testing.T) {
	env, st := testEnv(settingsSecret())
	fs := &fakeSettings{rows: map[int]int64{1: 40}}
	r := NewSettingsKeyRotation(fs, 8*24*time.Hour)
	require.NoError(t, r.Run(context.Background(), env))

	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
	kr, err := KeyringFromSecret(s)
	require.NoError(t, err)
	require.Equal(t, 2, kr.CurrentID)
	require.Equal(t, 1, kr.PreviousID)
	require.Equal(t, 64, len(kr.CurrentKeyHex))
	require.True(t, kr.PreviousKeyHex == "0000000000000000000000000000000000000000000000000000000000000001", "previous key is the old key")
	require.True(t, fs.lastKr.CurrentKeyHex == kr.CurrentKeyHex && fs.lastKr.CurrentKeyHex != "0000000000000000000000000000000000000000000000000000000000000001", "re-encrypt ran under the new key")
	require.Equal(t, 2, st.DataWrites, "stage and promote each roll the server once")
	require.Equal(t, 1, fs.reencrypt)
	require.EqualValues(t, 40, fs.rows[2])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnRotatedAt+"settings-key"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnPromotedAt+"settings-key"])
}

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: test that DropWaitsForGraceAndZeroRows
func TestSettingsKeyRotation_DropWaitsForGraceAndZeroRows(t *testing.T) {
	env, st := testEnv(settingsSecret())
	fs := &fakeSettings{rows: map[int]int64{1: 3}}
	r := NewSettingsKeyRotation(fs, 8*24*time.Hour)
	require.NoError(t, r.Run(context.Background(), env)) // -> reencrypted
	writes := st.DataWrites

	// Same day: parked.
	require.NoError(t, r.Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
	require.Equal(t, writes, st.DataWrites)

	// Grace elapsed but a row under the old id reappeared (restored backup): re-encrypt, still parked.
	env.Now = func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) }
	fs.rows[1] = 1
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
	require.Zero(t, fs.rows[1])

	// Next run: drop.
	require.NoError(t, r.Run(context.Background(), env))
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"settings-key"])
	_, hasPrev := s.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"]
	require.False(t, hasPrev)
	_, hasPrevID := s.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"]
	require.False(t, hasPrevID)
	require.Equal(t, writes+1, st.DataWrites)
}

// SEM@f5ed9122de949614f44d9fb1970d38d90f2d9081: test that ResumeFromStaged
func TestSettingsKeyRotation_ResumeFromStaged(t *testing.T) {
	sec := settingsSecret()
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"] = "0000000000000000000000000000000000000000000000000000000000000002"
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"] = "2"
	sec.Annotations[AnnPhase+"settings-key"] = "staged"
	sec.Annotations[AnnGeneration+"settings-key"] = "0"
	env, st := testEnv(sec)
	st.DataWrites = 1
	fs := &fakeSettings{rows: map[int]int64{1: 2}}
	require.NoError(t, NewSettingsKeyRotation(fs, time.Hour).Run(context.Background(), env))
	s, _ := st.Get(context.Background(), "tmi-secrets")
	kr, _ := KeyringFromSecret(s)
	require.Equal(t, 2, kr.CurrentID)
	require.True(t, kr.CurrentKeyHex == "0000000000000000000000000000000000000000000000000000000000000002", "staged key promoted")
	require.Equal(t, 1, kr.PreviousID)
	require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"])
}

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: test that Validation
func TestKeyringFromSecret_Validation(t *testing.T) {
	_, err := KeyringFromSecret(&Secret{Data: map[string]string{}})
	require.Error(t, err, "no key")
	_, err = KeyringFromSecret(&Secret{Data: map[string]string{"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "zz"}})
	require.Error(t, err, "not 64 hex chars")
	kr, err := KeyringFromSecret(&Secret{Data: map[string]string{"TMI_SECRET_SETTINGS_ENCRYPTION_KEY": "0000000000000000000000000000000000000000000000000000000000000001"}})
	require.NoError(t, err)
	require.Equal(t, 1, kr.CurrentID, "missing context id defaults to 1")
	require.Zero(t, kr.PreviousID)
}

// SEM@f9701c26907815153aebfeed71c2e2cbc5cd1622: test that UnreadableRowDoesNotFail
func TestGormSettingsStore_UnreadableRowDoesNotFail(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormlogger.Discard, DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, gormDB.AutoMigrate(&models.SystemSetting{}))

	k1, k2, k3 := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32)
	seed := func(key string, k []byte, id int) {
		enc, err := crypto.NewSettingsEncryptorFromKeyring(k, id, nil, 0)
		require.NoError(t, err)
		v, err := enc.Encrypt("x")
		require.NoError(t, err)
		require.NoError(t, gormDB.Create(&models.SystemSetting{SettingKey: models.DBVarchar(key), Value: models.DBText(v), SettingType: models.SystemSettingTypeString, ModifiedAt: time.Now()}).Error)
	}
	seed("good", k1, 1)
	seed("bad", k3, 9) // no key in the ring opens it

	kr := Keyring{CurrentKeyHex: hex.EncodeToString(k2), CurrentID: 2, PreviousKeyHex: hex.EncodeToString(k1), PreviousID: 1}
	store := NewGormSettingsStore(gormDB, nil)
	n, err := store.ReEncrypt(context.Background(), kr)
	require.NoError(t, err, "an unreadable row must not fail the rotation")
	require.Equal(t, 1, n)
	left, err := store.CountWithID(context.Background(), kr, 1)
	require.NoError(t, err)
	require.Zero(t, left)
}

// SEM@5492df59c8facc3d077821c77a8dd7bed4281289: test that StagedWithoutPairRefusesToPromote
func TestSettingsKeyRotation_StagedWithoutPairRefusesToPromote(t *testing.T) {
	sec := settingsSecret()
	sec.Annotations[AnnPhase+"settings-key"] = "staged"
	sec.Annotations[AnnGeneration+"settings-key"] = "0"
	env, st := testEnv(sec)
	st.DataWrites = 1 // the stage write already happened, so the rollout wait passes and the guard is reached
	before, _ := st.Get(context.Background(), "tmi-secrets")
	dataBefore := before.Clone().Data
	err := NewSettingsKeyRotation(&fakeSettings{rows: map[int]int64{}}, time.Hour).Run(context.Background(), env)
	require.ErrorContains(t, err, "refusing to promote")
	after, _ := st.Get(context.Background(), "tmi-secrets")
	require.True(t, reflect.DeepEqual(dataBefore, after.Data), "secret data must be unchanged")
	require.Equal(t, 1, st.DataWrites)
}
