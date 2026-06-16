//go:build oracle

package api

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openOracleDBForTimezone opens a direct GORM connection to the Oracle ADB
// backend used by `make test-integration-oci`, with the connection pool sized so
// the test can force several physical sessions open at once. Reuses
// authdb.ParseDatabaseURL + authdb.NewGormDB so the godror DSN (including the
// issue #459 onInit/timezone params) is built exactly as in production. Skips
// when TMI_DATABASE_URL is unset.
func openOracleDBForTimezone(t *testing.T, maxConns int) *authdb.GormDB {
	t.Helper()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TMI_DATABASE_URL not set; run under `make test-integration-oci` with scripts/oci-env.sh sourced")
	}

	cfg, err := authdb.ParseDatabaseURL(dbURL)
	require.NoError(t, err, "parse TMI_DATABASE_URL")
	require.Equal(t, authdb.DatabaseTypeOracle, cfg.Type,
		"this test requires an oracle:// TMI_DATABASE_URL (got %q)", cfg.Type)

	if cfg.OracleWalletLocation == "" {
		if w := os.Getenv("TMI_ORACLE_WALLET_LOCATION"); w != "" {
			cfg.OracleWalletLocation = w
		} else if w := os.Getenv("TNS_ADMIN"); w != "" {
			cfg.OracleWalletLocation = w
		}
	}

	// Size the pool so the concurrent phase below forces multiple distinct
	// physical sessions instead of serializing on one connection.
	cfg.MaxOpenConns = maxConns
	cfg.MaxIdleConns = maxConns

	gormDB, err := authdb.NewGormDB(*cfg)
	require.NoError(t, err, "open Oracle ADB connection")
	t.Cleanup(func() { _ = gormDB.Close() })
	return gormDB
}

// TestOracleSessionTimezonePooledConnectionsOracleIntegration verifies the issue
// #459 fix: every pooled Oracle session — not just the single init connection —
// has its SESSIONTIMEZONE pinned to UTC via the godror onInit DSN parameter.
//
// It opens N dedicated *sql.Conn handles concurrently and holds them all open at
// the same time (a barrier prevents any connection returning to the pool until
// every goroutine has acquired one). This guarantees the pool materializes
// several physical sessions, each of which must independently report UTC.
func TestOracleSessionTimezonePooledConnectionsOracleIntegration(t *testing.T) {
	const n = 5

	gormDB := openOracleDBForTimezone(t, n)

	sqlDB, err := gormDB.DB().DB()
	require.NoError(t, err, "get underlying *sql.DB")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var (
		wg        sync.WaitGroup
		acquired  sync.WaitGroup // counts goroutines that have pinned a connection
		release   = make(chan struct{})
		resultsMu sync.Mutex
		results   = make([]string, 0, n)
		errs      = make([]error, 0)
	)
	acquired.Add(n)
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			conn, err := sqlDB.Conn(ctx)
			if err != nil {
				resultsMu.Lock()
				errs = append(errs, err)
				resultsMu.Unlock()
				acquired.Done()
				return
			}
			defer func() { _ = conn.Close() }()

			var tz string
			qErr := conn.QueryRowContext(ctx, "SELECT SESSIONTIMEZONE FROM DUAL").Scan(&tz)

			// Signal this connection is pinned, then wait so all N sessions are
			// simultaneously checked out before any returns to the pool.
			acquired.Done()
			<-release

			resultsMu.Lock()
			if qErr != nil {
				errs = append(errs, qErr)
			} else {
				results = append(results, tz)
			}
			resultsMu.Unlock()
		}()
	}

	acquired.Wait() // all N physical sessions now open concurrently
	close(release)  // let them record results and return to the pool
	wg.Wait()

	require.Empty(t, errs, "querying SESSIONTIMEZONE across pooled connections")
	require.Len(t, results, n, "expected one SESSIONTIMEZONE result per pooled connection")
	for i, tz := range results {
		assert.Equal(t, "+00:00", tz,
			"pooled connection %d should report UTC SESSIONTIMEZONE, got %q", i, tz)
	}

	// Cross-check that a normal pooled GORM query (not a pinned *sql.Conn) is UTC.
	var tz string
	require.NoError(t, gormDB.DB().Raw("SELECT SESSIONTIMEZONE FROM DUAL").Scan(&tz).Error)
	assert.Equal(t, "+00:00", tz, "GORM pooled query should report UTC SESSIONTIMEZONE")
}
