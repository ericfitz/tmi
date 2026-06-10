package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingDriver is a minimal database/sql driver that captures the
// driver.TxOptions handed to BeginTx, letting us assert — hermetically, with
// no real database — that WithRetryableTransaction forwards the resolved
// isolation level. database/sql translates sql.TxOptions{Isolation: X} into
// driver.TxOptions{Isolation: driver.IsolationLevel(X)}, so the captured
// value is directly comparable to the sql.IsolationLevel constants.
type recordingDriver struct {
	lastOpts   driver.TxOptions
	beginCalls int
}

func (d *recordingDriver) Open(string) (driver.Conn, error) { return &recordingConn{d: d}, nil }

type recordingConn struct{ d *recordingDriver }

func (c *recordingConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (c *recordingConn) Close() error                        { return nil }
func (c *recordingConn) Begin() (driver.Tx, error)           { return recordingTx{}, nil }

func (c *recordingConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.d.lastOpts = opts
	c.d.beginCalls++
	return recordingTx{}, nil
}

type recordingTx struct{}

func (recordingTx) Commit() error   { return nil }
func (recordingTx) Rollback() error { return nil }

// openRecordingDB registers a uniquely-named recording driver (sql.Register
// panics on duplicate names) and returns an open *sql.DB plus the driver so
// the test can inspect the captured options.
func openRecordingDB(t *testing.T) (*sql.DB, *recordingDriver) {
	t.Helper()
	drv := &recordingDriver{}
	name := "recording-" + t.Name()
	sql.Register(name, drv)
	db, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, drv
}

func TestWithRetryableTransaction_DefaultsToSerializable(t *testing.T) {
	db, drv := openRecordingDB(t)
	cfg := DefaultRetryConfig()

	err := WithRetryableTransaction(context.Background(), db, cfg, func(*sql.Tx) error { return nil })

	require.NoError(t, err)
	assert.Equal(t, 1, drv.beginCalls)
	assert.Equal(t, driver.IsolationLevel(sql.LevelSerializable), drv.lastOpts.Isolation)
}

func TestWithRetryableTransaction_ForwardsExplicitLevel(t *testing.T) {
	db, drv := openRecordingDB(t)
	cfg := DefaultRetryConfig()

	err := WithRetryableTransaction(context.Background(), db, cfg, func(*sql.Tx) error { return nil },
		&sql.TxOptions{Isolation: sql.LevelReadCommitted})

	require.NoError(t, err)
	assert.Equal(t, driver.IsolationLevel(sql.LevelReadCommitted), drv.lastOpts.Isolation)
}

func TestWithRetryableTransaction_RejectsNonPortableLevelWithoutBeginning(t *testing.T) {
	db, drv := openRecordingDB(t)
	cfg := DefaultRetryConfig()

	err := WithRetryableTransaction(context.Background(), db, cfg, func(*sql.Tx) error { return nil },
		&sql.TxOptions{Isolation: sql.LevelRepeatableRead})

	require.Error(t, err)
	assert.Contains(t, err.Error(), sql.LevelRepeatableRead.String())
	assert.Equal(t, 0, drv.beginCalls, "must reject before opening a transaction")
}
