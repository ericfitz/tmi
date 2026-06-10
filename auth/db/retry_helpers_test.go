package db

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveTxOptions: when no caller-supplied options are present, the wrapper
// must default to SERIALIZABLE (issue #448 / #451 — start from the safe
// isolation level). A nil entry is treated the same as "unspecified".
func TestResolveTxOptions_DefaultsToSerializable(t *testing.T) {
	t.Run("no options", func(t *testing.T) {
		got, err := resolveTxOptions(nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, sql.LevelSerializable, got.Isolation)
	})

	t.Run("single nil option treated as unspecified", func(t *testing.T) {
		got, err := resolveTxOptions([]*sql.TxOptions{nil})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, sql.LevelSerializable, got.Isolation)
	})
}

// Callers may explicitly opt down to a weaker but portable level (long
// read-only / report paths, per #449). Only ReadCommitted and Serializable
// are forwarded unchanged: godror materializes both as an explicit
// ALTER SESSION, so they are deterministic across a pooled connection.
func TestResolveTxOptions_ForwardsPortableLevels(t *testing.T) {
	cases := []sql.IsolationLevel{
		sql.LevelReadCommitted,
		sql.LevelSerializable,
	}
	for _, lvl := range cases {
		lvl := lvl
		t.Run(lvl.String(), func(t *testing.T) {
			in := &sql.TxOptions{Isolation: lvl, ReadOnly: true}
			got, err := resolveTxOptions([]*sql.TxOptions{in})
			require.NoError(t, err)
			assert.Same(t, in, got, "portable options must be forwarded unchanged")
		})
	}
}

// sql.LevelDefault means "I don't care, use the DB default". On Oracle that is
// a footgun: godror issues no ALTER SESSION for it, so the transaction
// silently inherits whatever isolation the pooled physical connection last
// ran (oracle-db-admin review of #448, Note 1). We therefore collapse Default
// into our safe SERIALIZABLE default rather than forward it. This also means a
// zero-value &sql.TxOptions{} resolves to serializable instead of erroring.
func TestResolveTxOptions_DefaultLevelUpgradesToSerializable(t *testing.T) {
	got, err := resolveTxOptions([]*sql.TxOptions{{Isolation: sql.LevelDefault}})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, sql.LevelSerializable, got.Isolation)
}

// Upgrading Default -> Serializable must not discard a caller's ReadOnly
// intent (e.g. a bare &sql.TxOptions{ReadOnly: true}).
func TestResolveTxOptions_DefaultLevelPreservesReadOnly(t *testing.T) {
	got, err := resolveTxOptions([]*sql.TxOptions{{ReadOnly: true}})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, sql.LevelSerializable, got.Isolation)
	assert.True(t, got.ReadOnly, "ReadOnly intent must survive the Default->Serializable upgrade")
}

// godror rejects RepeatableRead/Snapshot/etc. with an error, so requesting a
// non-portable level is a programming error that must be caught before the
// transaction begins rather than blowing up at BeginTx on Oracle.
func TestResolveTxOptions_RejectsNonPortableLevels(t *testing.T) {
	cases := []sql.IsolationLevel{
		sql.LevelReadUncommitted,
		sql.LevelRepeatableRead,
		sql.LevelSnapshot,
		sql.LevelLinearizable,
	}
	for _, lvl := range cases {
		lvl := lvl
		t.Run(lvl.String(), func(t *testing.T) {
			got, err := resolveTxOptions([]*sql.TxOptions{{Isolation: lvl}})
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), lvl.String())
		})
	}
}

// jitteredBackoff must stay within [window/2, window] where window is the
// exponential delay capped at MaxDelay. A deterministic randN lets us pin the
// floor (randN -> 0) and ceiling (randN -> n-1) exactly.
func TestJitteredBackoff_Bounds(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 5, BaseDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second}

	t.Run("floor when randN returns 0", func(t *testing.T) {
		// attempt 1 -> window 100ms, half 50ms
		got := jitteredBackoff(cfg, 1, func(int64) int64 { return 0 })
		assert.Equal(t, 50*time.Millisecond, got)
	})

	t.Run("ceiling when randN returns n-1", func(t *testing.T) {
		got := jitteredBackoff(cfg, 1, func(n int64) int64 { return n - 1 })
		assert.Equal(t, 100*time.Millisecond, got)
	})

	t.Run("caps the exponential window at MaxDelay", func(t *testing.T) {
		// attempt 20 would be a huge exponential window; must clamp to MaxDelay
		got := jitteredBackoff(cfg, 20, func(int64) int64 { return 0 })
		assert.Equal(t, cfg.MaxDelay/2, got) // floor of the clamped window
	})
}

// Property: across attempts, with a real (seeded) distribution the value never
// escapes [half, window] and never exceeds MaxDelay.
func TestJitteredBackoff_StaysBounded(t *testing.T) {
	cfg := RetryConfig{MaxRetries: 10, BaseDelay: 10 * time.Millisecond, MaxDelay: 2 * time.Second}
	for attempt := 1; attempt < cfg.MaxRetries; attempt++ {
		window := cfg.BaseDelay * time.Duration(int64(1)<<uint(attempt-1))
		if window <= 0 || window > cfg.MaxDelay {
			window = cfg.MaxDelay
		}
		half := window / 2
		// sample both extremes of the injected RNG
		lo := jitteredBackoff(cfg, attempt, func(int64) int64 { return 0 })
		hi := jitteredBackoff(cfg, attempt, func(n int64) int64 { return n - 1 })
		assert.GreaterOrEqual(t, lo, half)
		assert.LessOrEqual(t, hi, window)
		assert.LessOrEqual(t, hi, cfg.MaxDelay)
	}
}
