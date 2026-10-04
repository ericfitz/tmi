//go:build oracle

package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SEM@0000000: verify closing the Oracle dialector's pre-built pool without a database
func TestOracleDialectorCloseConn_ClosesPrebuiltPool(t *testing.T) {
	d, _ := getOracleDialector(GormConfig{
		User:                "tmi",
		Password:            "not-a-real-password",
		OracleConnectString: "localhost:1521/FREEPDB1",
	})
	od, ok := d.(oracleAdditiveDialector)
	require.True(t, ok, "getOracleDialector returns oracleAdditiveDialector")
	require.NotNil(t, od.Conn)

	require.NoError(t, od.closeConn())

	assert.ErrorContains(t, od.Conn.Ping(), "sql: database is closed")
	assert.NoError(t, od.closeConn(), "a second close is harmless")
}
