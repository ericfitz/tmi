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

// fakeDPIErr mimics a godror client-library error: Code() 0, DPI-NNNN message.
type fakeDPIErr struct{ msg string }

func (e fakeDPIErr) Error() string   { return e.msg }
func (fakeDPIErr) Code() int         { return 0 }
func (e fakeDPIErr) Message() string { return e.msg }

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
		"dpi":   {fakeDPIErr{"DPI-1047: Cannot locate client for user/" + leakyPassword}, "DPI-1047"},
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

// fakeOraCodeErr is an Oracle error with a configurable ORA code.
type fakeOraCodeErr struct{ code int }

func (e fakeOraCodeErr) Error() string { return "ora" }
func (e fakeOraCodeErr) Code() int     { return e.code }

func TestIsPermanentConnectError(t *testing.T) {
	wrap := func(err error) error { return failDB(slogging.Get(), "connect failed", err) }
	for _, code := range []int{1017, 28000, 28001, 12154, 28759, 29024, 28040, 1045, 1005} {
		assert.True(t, IsPermanentConnectError(wrap(fakeOraCodeErr{code})), "ORA-%05d", code)
	}
	for _, code := range []int{12514, 12541, 12170, 12537, 3113, 3114} {
		assert.False(t, IsPermanentConnectError(wrap(fakeOraCodeErr{code})), "ORA-%05d is transient", code)
	}
	assert.True(t, IsPermanentConnectError(wrap(fakeDPIErr{msg: "DPI-1047: Cannot locate a 64-bit Oracle Client library"})))
	assert.True(t, IsPermanentConnectError(wrap(fakeDPIErr{msg: "DPI-1072: the Oracle Client library version is unsupported"})))
	assert.False(t, IsPermanentConnectError(wrap(fakeDPIErr{msg: "DPI-1080: connection was closed"})))
	for _, code := range []string{"28P01", "28000", "3D000"} {
		assert.True(t, IsPermanentConnectError(wrap(&pgconn.PgError{Code: code})), "sqlstate %s", code)
	}
	assert.False(t, IsPermanentConnectError(wrap(&pgconn.PgError{Code: "57P03"})), "cannot_connect_now is transient")
	assert.False(t, IsPermanentConnectError(errors.New("dial tcp: connection refused")))
	assert.False(t, IsPermanentConnectError(nil))
}
