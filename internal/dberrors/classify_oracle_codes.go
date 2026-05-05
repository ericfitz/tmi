package dberrors

import (
	"sync/atomic"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ora01555Counter tracks the cumulative number of ORA-01555 (snapshot too
// old) occurrences observed by classifyOracleCode. Exposed via
// SnapshotTooOldCount() so operators can confirm the baseline rate before
// deciding whether to reclassify ORA-01555 as transient (issue #312). Also
// emitted as a WARN log per occurrence so the rate is visible in
// observability without scraping a metrics endpoint.
var ora01555Counter atomic.Uint64

// SnapshotTooOldCount returns the cumulative number of ORA-01555 occurrences
// observed since process start. Intended for operator inspection and tests.
func SnapshotTooOldCount() uint64 {
	return ora01555Counter.Load()
}

// classifyOracleCode maps an ORA- numeric code to a typed sentinel.
// Lives outside the //go:build oracle file so it can be unit-tested
// without depending on godror (which requires CGO + Oracle Instant Client).
//
// Returns nil for codes that intentionally defer to data — e.g., ORA-01555
// (snapshot too old), where the right answer (transient retry vs. unfixable
// undo exhaustion) depends on the caller's transaction shape and on the
// observed occurrence rate. ORA-01555 occurrences are counted in
// ora01555Counter and emit a WARN log so operators can decide whether the
// rate justifies reclassification.
func classifyOracleCode(err error, code int) error {
	switch code {
	// Unique constraint violated
	case 1: // ORA-00001
		return Wrap(err, ErrDuplicate)

	// Foreign key violations
	case 2291, 2292: // ORA-02291 (parent key not found), ORA-02292 (child record found)
		return Wrap(err, ErrForeignKey)

	// Constraint violations
	case 12899: // ORA-12899 (value too large for column)
		return Wrap(err, ErrConstraint)
	case 1400: // ORA-01400 (cannot insert NULL into ...) — PG-23502 analogue
		return Wrap(err, ErrConstraint)
	case 2290: // ORA-02290 (check constraint violated) — PG-23514 analogue
		return Wrap(err, ErrConstraint)

	// Serialization / deadlock / lock contention
	case 8177: // ORA-08177 (can't serialize access)
		return Wrap(err, ErrTransient)
	case 60: // ORA-00060 (deadlock detected)
		return Wrap(err, ErrTransient)
	case 54: // ORA-00054 (resource busy and acquire with NOWAIT specified or timeout expired)
		return Wrap(err, ErrTransient)

	// Connection errors
	case 3113, 3114: // ORA-03113/03114 (end-of-file on communication channel / not connected)
		return Wrap(err, ErrTransient)
	case 3135: // ORA-03135 (connection lost contact)
		return Wrap(err, ErrTransient)
	case 12170: // ORA-12170 (connect timeout)
		return Wrap(err, ErrTransient)
	case 12537: // ORA-12537 (TNS: connection closed) — companion to 12541/12543, common on ADB maintenance
		return Wrap(err, ErrTransient)
	case 12541, 12543: // ORA-12541/12543 (no listener / destination host unreachable)
		return Wrap(err, ErrTransient)

	// Package state discarded — retry is safe regardless of whether the same session
	// is reused: the failing session has already invalidated its stale cursor by the
	// time the retry begins; a different pool session was never affected. Common on
	// ADB plan-change/upgrade events.
	case 4068: // ORA-04068 (existing state of package has been discarded)
		return Wrap(err, ErrTransient)

	// Permission / credential errors
	case 1017: // ORA-01017 (invalid username/password)
		return Wrap(err, ErrPermission)
	case 1031: // ORA-01031 (insufficient privileges)
		return Wrap(err, ErrPermission)
	case 1045: // ORA-01045 (user lacks CREATE SESSION privilege; logon denied)
		return Wrap(err, ErrPermission)
	case 28001: // ORA-28001 (the password has expired) — fires if ADB credential rotates and wallet wasn't refreshed
		return Wrap(err, ErrPermission)

	// User-requested cancellation (often query timeout)
	case 1013: // ORA-01013 (user requested cancel of current operation)
		return Wrap(err, ErrContextDone)

	// Application-defined errors (range -20000 to -20999).
	// ORA-20001 is the code raised by the audit_entries / version_snapshots
	// append-only triggers (T19, #356). Classify it as ErrAppendOnlyViolation
	// so callers can distinguish a T19 trip from a generic constraint
	// violation; the error chain still satisfies errors.Is(err, ErrConstraint)
	// for any caller that only cares about the broader category.
	case 20001:
		return Wrap(err, ErrAppendOnlyViolation)

	// Additional ADB transient conditions
	case 18: // ORA-00018 (maximum number of sessions exceeded) — ADB tier-cap exhaustion
		return Wrap(err, ErrTransient)
	case 20: // ORA-00020 (maximum number of processes exceeded)
		return Wrap(err, ErrTransient)
	case 3156: // ORA-03156 (RPC connection timed out)
		return Wrap(err, ErrTransient)
	case 12519: // ORA-12519 (TNS:no appropriate service handler found) — ADB shape-resize transient
		return Wrap(err, ErrTransient)
	case 12520: // ORA-12520 (TNS:listener could not find available handler) — ADB autoscale
		return Wrap(err, ErrTransient)
	case 25408: // ORA-25408 (can not safely replay call) — Application Continuity / replay-driver
		return Wrap(err, ErrTransient)
	}

	// ORA-01555 (snapshot too old) — defer to data. We instrument the
	// occurrence rate (counter + WARN log) so operators can decide whether
	// to reclassify as transient based on observed traffic. The classic
	// long-running-query-against-shrinking-undo case will not benefit from
	// a single-statement retry; the list/pagination retry-against-fresher-SCN
	// case would. Returning nil keeps callers free to surface the error
	// distinctly while we collect data. See issue #312.
	if code == 1555 {
		count := ora01555Counter.Add(1)
		slogging.Get().Warn("ORA-01555 snapshot too old observed (cumulative count: %d): %v", count, err)
	}

	return nil
}
