package db

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const leakyPassword = "S3cr3t-Hunter2-PW"

type fakeOraErr struct{ msg string }

func (e fakeOraErr) Error() string { return e.msg }
func (fakeOraErr) Code() int       { return 1017 }

func initTestLogger(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, slogging.Initialize(slogging.Config{Level: slogging.LogLevelDebug, LogDir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}))
	return filepath.Join(dir, "tmi.log")
}

func TestFailDB_DoesNotLeakDriverText(t *testing.T) {
	logPath := initTestLogger(t)
	cases := map[string]struct {
		err   error
		class string
	}{
		"pg":    {&pgconn.PgError{Code: "28P01", Message: "bad password " + leakyPassword}, "sqlstate=28P01"},
		"ora":   {fakeOraErr{"ORA-01017 user/" + leakyPassword + "@host"}, "ORA-01017"},
		"other": {errors.New("dial postgres://u:" + leakyPassword + "@h/db"), "*errors.errorString"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := failDB(slogging.Get(), "Failed to ping database", tc.err)
			assert.NotContains(t, got.Error(), leakyPassword)
			assert.Contains(t, got.Error(), tc.class)
			assert.ErrorIs(t, got, tc.err, "original error must stay reachable")
		})
	}
	b, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.NotContains(t, string(b), leakyPassword)
	assert.Contains(t, string(b), "Failed to ping database")
}

func TestNewGormDB_ConnectFailureDoesNotLeakPassword(t *testing.T) {
	logPath := initTestLogger(t)
	_, err := NewGormDB(GormConfig{
		Type: DatabaseTypePostgres, Host: "127.0.0.1", Port: "1",
		User: "u", Password: leakyPassword, Database: "d", SSLMode: "disable",
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), leakyPassword)
	b, rerr := os.ReadFile(logPath)
	require.NoError(t, rerr)
	assert.NotContains(t, string(b), leakyPassword)
}
