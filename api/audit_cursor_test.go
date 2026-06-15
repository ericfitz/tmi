package api

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditCursor_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 6, 11, 12, 34, 56, 789000000, time.UTC)
	enc := encodeAuditCursor(ts, "abc-123", dirForward)
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

func TestEncodeDecodeCursor_Direction(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	enc := encodeAuditCursor(now, "id-1", dirBackward)
	c, err := decodeAuditCursor(enc)
	require.NoError(t, err)
	require.Equal(t, dirBackward, c.Dir)
	require.Equal(t, "id-1", c.ID)
	require.True(t, now.Equal(c.CreatedAt))
}

func TestDecodeCursor_RejectsBadDirection(t *testing.T) {
	raw := `{"t":"2026-01-01T00:00:00Z","i":"x","d":"q"}`
	enc := base64.RawURLEncoding.EncodeToString([]byte(raw))
	_, err := decodeAuditCursor(enc)
	require.Error(t, err)
}

func TestDecodeCursor_EmptyDirectionIsForward(t *testing.T) {
	enc := encodeAuditCursor(time.Now().UTC(), "id-2", dirForward)
	c, err := decodeAuditCursor(enc)
	require.NoError(t, err)
	require.Equal(t, dirForward, c.Dir)
}
