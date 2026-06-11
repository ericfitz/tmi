package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditCursor_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 6, 11, 12, 34, 56, 789000000, time.UTC)
	enc := encodeAuditCursor(ts, "abc-123")
	dec, err := decodeAuditCursor(enc)
	require.NoError(t, err)
	assert.True(t, dec.CreatedAt.Equal(ts))
	assert.Equal(t, "abc-123", dec.ID)
}

func TestAuditCursor_Invalid(t *testing.T) {
	for _, bad := range []string{"", "!!!not-base64!!!", "aGVsbG8"} { // last decodes to "hello", not JSON
		_, err := decodeAuditCursor(bad)
		assert.Error(t, err, "input %q must be rejected", bad)
	}
}
