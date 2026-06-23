package db

import (
	"database/sql"
	"fmt"
	"time"
)

// serializableTxOptions is the default isolation requested for every
// transaction that does not explicitly opt down. Starting from SERIALIZABLE
// (issue #451) makes the safe behavior the default on both PostgreSQL (SSI)
// and Oracle ADB (snapshot isolation); weaker levels are an explicit,
// per-site decision (#449).
// SEM@e52f9bdea940a3032c58dec83ce3a82fd6b305b7: return Serializable transaction options as the safe default isolation level (pure)
func serializableTxOptions() *sql.TxOptions {
	return &sql.TxOptions{Isolation: sql.LevelSerializable}
}

// resolveTxOptions selects the *sql.TxOptions to forward into BeginTx /
// gorm.Transaction.
//
//   - No options, a single nil entry, or an explicit sql.LevelDefault ->
//     default to SERIALIZABLE. Default is collapsed (not forwarded) because on
//     Oracle godror issues no ALTER SESSION for it, so the transaction would
//     silently inherit whatever isolation the pooled physical connection last
//     ran — nondeterministic across the pool (oracle-db-admin review of #448,
//     Note 1). Collapsing it makes "I don't care" mean our safe default.
//   - An explicit portable level (ReadCommitted, Serializable) is forwarded
//     unchanged; godror materializes both as an explicit ALTER SESSION, so
//     they are deterministic on a reused connection.
//   - Any other level is rejected: godror (Oracle) errors on
//     RepeatableRead/Snapshot/etc., so requesting one is a programming error
//     that must surface here rather than at BeginTx on Oracle only. Keeping
//     the allow-list portable also avoids silently diverging PG vs. Oracle
//     behavior.
//
// SEM@e52f9bdea940a3032c58dec83ce3a82fd6b305b7: validate and normalize transaction isolation options, upgrading Default to Serializable (pure)
func resolveTxOptions(opts []*sql.TxOptions) (*sql.TxOptions, error) {
	if len(opts) == 0 || opts[0] == nil {
		return serializableTxOptions(), nil
	}

	o := opts[0]
	switch o.Isolation {
	case sql.LevelDefault:
		// Upgrade Default -> Serializable, preserving any ReadOnly intent.
		return &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: o.ReadOnly}, nil
	case sql.LevelReadCommitted, sql.LevelSerializable:
		return o, nil
	default:
		return nil, fmt.Errorf("db: non-portable isolation level %s requested; "+
			"only ReadCommitted or Serializable are allowed (godror rejects the rest; "+
			"Default is upgraded to Serializable)",
			o.Isolation)
	}
}

// jitteredBackoff returns the delay to wait before retry attempt `attempt`
// (attempt >= 1). It uses equal jitter over the capped exponential window:
//
//	window = min(BaseDelay * 2^(attempt-1), MaxDelay)
//	delay  = window/2 + rand[0, window/2]   ->  in [window/2, window]
//
// The window/2 floor preserves exponential growth while the jitter
// decorrelates retries so contending writers don't re-collide in lockstep
// after a serialization failure. randN must behave like rand.Int63n (returns
// [0, n) and panics on n <= 0); it is injected so tests can pin the extremes.
// SEM@e52f9bdea940a3032c58dec83ce3a82fd6b305b7: compute an equal-jitter exponential backoff delay for a retry attempt (pure)
func jitteredBackoff(cfg RetryConfig, attempt int, randN func(int64) int64) time.Duration {
	// #nosec G115 - attempt is always >= 1 here, so the shift is in range.
	window := cfg.BaseDelay * time.Duration(int64(1)<<uint(attempt-1))
	if window <= 0 || window > cfg.MaxDelay {
		window = cfg.MaxDelay
	}

	half := window / 2
	// randN(n) returns [0, n); +1 makes the jitter span [0, window-half]
	// inclusive so the ceiling reaches the full window.
	jitter := time.Duration(randN(int64(window-half) + 1))
	return half + jitter
}
