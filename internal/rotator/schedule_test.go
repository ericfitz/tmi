package rotator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseRotateEvery(t *testing.T) {
	d, err := ParseRotateEvery("90d")
	require.NoError(t, err)
	require.Equal(t, 90*24*time.Hour, d)
	d, err = ParseRotateEvery("36h")
	require.NoError(t, err)
	require.Equal(t, 36*time.Hour, d)
	_, err = ParseRotateEvery("3 months")
	require.Error(t, err)
	_, err = ParseRotateEvery("0d")
	require.Error(t, err)
}

func TestIsDue(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s := &Secret{Annotations: map[string]string{}}
	require.True(t, IsDue(s, "x", now), "never rotated is due")
	s.Annotations[AnnRotatedAt+"x"] = now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)
	require.False(t, IsDue(s, "x", now), "10 days old, default 90d")
	s.Annotations[AnnEvery+"x"] = "7d"
	require.True(t, IsDue(s, "x", now))
	s.Annotations[AnnEvery+"x"] = "3 months" // unparsable: warn and use the default
	require.False(t, IsDue(s, "x", now))
	s.Annotations[AnnRotatedAt+"x"] = "garbage"
	require.True(t, IsDue(s, "x", now), "unparsable rotated-at is due")
}
