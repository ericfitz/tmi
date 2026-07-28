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
// SEM@379ec443351a5e4c305ce61c48bfaab5ed1a5f97: return the cumulative count of ORA-01555 snapshot-too-old errors observed since process start (pure)
func SnapshotTooOldCount() uint64 {
	return ora01555Counter.Load()
}

// oracleCodeSentinels maps an ORA- numeric code to the sentinel it classifies
// as. A table rather than a switch because the switch outgrew the cyclomatic
// complexity budget; every arm was a bare `return Wrap(err, X)`, so the mapping
// is pure data and reads better as such. Codes absent from this table fall
// through to classifyOracleCode's ORA-01555 instrumentation and then to nil,
// which means "defer to the string fallback in classify.go".
var oracleCodeSentinels = map[int]error{
	// Unique constraint violated
	1: ErrDuplicate, // ORA-00001

	// Foreign key violations
	2291: ErrForeignKey, // parent key not found
	2292: ErrForeignKey, // child record found

	// Undefined schema object (PG 42P01 analogue). ORA-02289 fires when a
	// referenced sequence does not exist (e.g. NEXTVAL on a dropped sequence);
	// ORA-00942 when a table or view does not exist. Classified distinctly so
	// callers that can repair the object (e.g. reinstall the alias sequence)
	// can do so instead of returning 500. Not transient — a bare retry fails
	// identically until the object is recreated.
	2289: ErrUndefinedObject, // sequence does not exist
	942:  ErrUndefinedObject, // table or view does not exist

	// Constraint violations
	12899: ErrConstraint, // value too large for column
	1400:  ErrConstraint, // cannot insert NULL into ... — PG-23502 analogue
	// ORA-01407 (cannot update ... to NULL) is the UPDATE-path sibling of
	// ORA-01400, which Oracle raises only on INSERT. Reachable because Oracle
	// binds '' as NULL: emptying a NOT NULL string column succeeds on
	// PostgreSQL and raises this on Oracle. Without this entry the string
	// fallback in classify.go matches none of its patterns, the error stays
	// unclassified, and StoreErrorToRequestError degrades it to a 500 on
	// Oracle only — a Zero-500 violation invisible in PG testing.
	1407: ErrConstraint,
	2290: ErrConstraint, // check constraint violated — PG-23514 analogue
	// ORA-01438 (value larger than specified precision allowed for this column)
	// is the Oracle half of the pair classify_pg.go documents when it maps PG
	// 22003 (numeric_value_out_of_range) to ErrConstraint. Reachable the same
	// way ORA-01407 was: Threat.Score is decimal(3,1) -> NUMBER(3,1) on Oracle,
	// and a JSON Patch `replace /score 999` is unchecked because map-based
	// Updates runs Threat.BeforeSave against an empty struct. PG returns 400
	// via 22003; without this Oracle fell through to a 500. ORA-01401 is the
	// pre-11g spelling of ORA-12899, folded in for completeness.
	1438: ErrConstraint,
	1401: ErrConstraint,

	// Serialization / deadlock / lock contention
	8177: ErrTransient, // can't serialize access
	60:   ErrTransient, // deadlock detected
	54:   ErrTransient, // resource busy and acquire with NOWAIT specified or timeout expired

	// Connection errors
	3113:  ErrTransient, // end-of-file on communication channel
	3114:  ErrTransient, // not connected
	3135:  ErrTransient, // connection lost contact
	12170: ErrTransient, // connect timeout
	12537: ErrTransient, // TNS: connection closed — companion to 12541/12543, common on ADB maintenance
	12541: ErrTransient, // no listener
	12543: ErrTransient, // destination host unreachable

	// Package state discarded — retry is safe regardless of whether the same
	// session is reused: the failing session has already invalidated its stale
	// cursor by the time the retry begins; a different pool session was never
	// affected. Common on ADB plan-change/upgrade events.
	4068: ErrTransient, // existing state of package has been discarded

	// Permission / credential errors
	1017:  ErrPermission, // invalid username/password
	1031:  ErrPermission, // insufficient privileges
	1045:  ErrPermission, // user lacks CREATE SESSION privilege; logon denied
	28001: ErrPermission, // the password has expired — fires if ADB credential rotates and wallet wasn't refreshed

	// User-requested cancellation (often query timeout)
	1013: ErrContextDone, // user requested cancel of current operation

	// Application-defined errors (range -20000 to -20999).
	// ORA-20001 is the code raised by the audit_entries / version_snapshots
	// append-only triggers (T19, #356). Classified as ErrAppendOnlyViolation so
	// callers can distinguish a T19 trip from a generic constraint violation;
	// the error chain still satisfies errors.Is(err, ErrConstraint) for any
	// caller that only cares about the broader category.
	20001: ErrAppendOnlyViolation,

	// Additional ADB transient conditions
	18:    ErrTransient, // maximum number of sessions exceeded — ADB tier-cap exhaustion
	20:    ErrTransient, // maximum number of processes exceeded
	3156:  ErrTransient, // RPC connection timed out
	12519: ErrTransient, // TNS:no appropriate service handler found — ADB shape-resize transient
	12520: ErrTransient, // TNS:listener could not find available handler — ADB autoscale
	25408: ErrTransient, // can not safely replay call — Application Continuity / replay-driver
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
// SEM@178dbd0418cfb7e057d4297c7a88c5879cb64c7f: map an Oracle ORA- error code to a typed sentinel error; instrument ORA-01555 with counter and warn log (pure)
func classifyOracleCode(err error, code int) error {
	if sentinel, ok := oracleCodeSentinels[code]; ok {
		return Wrap(err, sentinel)
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
