package db

import (
	"errors"
	"testing"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// connClosingDialector stands in for the Oracle dialector: it owns a
// pre-built pool and exposes closeConn, which closeFailedConnection must call.
type connClosingDialector struct {
	gorm.Dialector
	closed int
	err    error
}

func (d *connClosingDialector) closeConn() error {
	d.closed++
	return d.err
}

func TestCloseFailedConnection_ClosesGormPool(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{PrepareStmt: true})
	require.NoError(t, err)
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Ping())

	closeFailedConnection(gdb, gdb.Dialector, slogging.Get())

	assert.ErrorContains(t, sqlDB.Ping(), "sql: database is closed")
}

func TestCloseFailedConnection_ClosesDialectorConnWhenGormNeverOpened(t *testing.T) {
	d := &connClosingDialector{}

	closeFailedConnection(nil, d, slogging.Get())

	assert.Equal(t, 1, d.closed)
}

func TestCloseFailedConnection_ClosesBothHandlesAndSwallowsCloseErrors(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	d := &connClosingDialector{err: errors.New("already closed")}

	assert.NotPanics(t, func() { closeFailedConnection(gdb, d, slogging.Get()) })

	assert.Equal(t, 1, d.closed)
	assert.ErrorContains(t, sqlDB.Ping(), "sql: database is closed")
}
