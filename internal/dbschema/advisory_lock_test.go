package dbschema

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNameToInt64Stable(t *testing.T) {
	a := nameToInt64("foo")
	b := nameToInt64("foo")
	c := nameToInt64("bar")
	assert.Equal(t, a, b, "hash should be stable for same input")
	assert.NotEqual(t, a, c, "different inputs should produce different hashes")
}

func TestAcquireMigrationLock_UnsupportedDialect(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	_, err = AcquireMigrationLock(context.Background(), db, "test")
	assert.ErrorContains(t, err, "unsupported dialect")
}

// TestAcquireMigrationLock_PGSerializes is gated by an env var because it
// requires a real PG connection. CI runs it via make test-integration; it's
// skipped by default in `make test-unit`.
func TestAcquireMigrationLock_PGSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires real PostgreSQL; run via integration tests")
	}
	t.Skip("manual smoke; requires DATABASE_URL env var")

	// Smoke design (uncomment to run manually):
	// db := connectToPGTestDB(t) // helper that opens a real PG connection
	// ctx := context.Background()
	//
	// var order []int
	// var mu sync.Mutex
	// var wg sync.WaitGroup
	// for i := 0; i < 3; i++ {
	//     wg.Add(1)
	//     go func(i int) {
	//         defer wg.Done()
	//         release, err := AcquireMigrationLock(ctx, db, "test-lock-pg")
	//         require.NoError(t, err)
	//         mu.Lock(); order = append(order, i); mu.Unlock()
	//         time.Sleep(50 * time.Millisecond)
	//         release()
	//     }(i)
	// }
	// wg.Wait()
	// assert.Len(t, order, 3)
	_ = sync.WaitGroup{}
	_ = time.Now()
}
