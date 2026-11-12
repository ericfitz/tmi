package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// #1155: GORM's clock must truncate to microseconds, the precision PostgreSQL
// and Oracle store, so a write response equals a later read.
func TestNowFunc_TruncatesToMicroseconds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		now := NowFunc()
		assert.Zero(t, now.Nanosecond()%1000, "NowFunc must truncate to microseconds")
		assert.Equal(t, time.UTC, now.Location())
	}
	assert.WithinDuration(t, time.Now(), NowFunc(), time.Second)
}
