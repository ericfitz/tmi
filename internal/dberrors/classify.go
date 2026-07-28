package dberrors

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Classify wraps a raw database error with the appropriate typed sentinel.
// It checks in order: context errors, GORM errors, driver-specific errors
// (PostgreSQL pgconn.PgError, Oracle godror.OraErr), then falls back to
// string matching for errors that don't carry typed driver info.
// SEM@bacedc2fda0d7e7c4267e5fef6abc1a24bafed1a: convert a raw database error to a typed sentinel, including no-rows cases (pure)
func Classify(err error) error {
	if err == nil {
		return nil
	}

	// Context cancellation
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return Wrap(err, ErrContextDone)
	}

	// Already classified — don't double-wrap
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrConstraint) ||
		errors.Is(err, ErrTransient) || errors.Is(err, ErrPermission) ||
		errors.Is(err, ErrContextDone) || errors.Is(err, ErrUndefinedObject) {
		return err
	}

	// GORM-specific: record not found
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Wrap(err, ErrNotFound)
	}

	// database/sql: no rows returned. This is a DISTINCT value from
	// gorm.ErrRecordNotFound — errors.Is does not bridge them — and it is
	// what callers see when they finish a query with a raw *sql.Row/*sql.Rows
	// Scan (e.g. db.Raw(...).Row().Scan(...), or any GORM finisher that
	// falls back to the stdlib driver's zero-row signal) rather than a GORM
	// model query. Classify it centrally so every call site gets the same
	// 404-shaped sentinel instead of falling through unclassified to a
	// bare, policy-violating 500.
	if errors.Is(err, sql.ErrNoRows) {
		return Wrap(err, ErrNotFound)
	}

	// Driver-specific classification (PostgreSQL, Oracle)
	if classified := classifyPgError(err); classified != nil {
		return classified
	}
	if classified := classifyOracleError(err); classified != nil {
		return classified
	}

	// String-matching fallback for errors without typed driver info
	return classifyByString(err)
}

// classifyByString is the fallback classifier for errors that don't carry
// typed driver information (e.g., raw net.OpError, TLS errors).
// This should handle a minimal set of patterns — driver-specific checks
// cover the vast majority of cases.
// SEM@6a279d3dfc40bdd9ee0faa2abb1456f6dc5e003b: classify an untyped database error by matching known error message patterns (pure)
func classifyByString(err error) error {
	errStr := strings.ToLower(err.Error())

	// Connection/transient errors
	transientPatterns := []string{
		"driver: bad connection",
		"connection refused",
		"connection reset by peer",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"no connection available",
		"connection timed out",
		"unexpected eof",
		"server closed",
		"ssl connection has been closed",
		"connection is shut down",
		"invalid connection",
		"connection unexpectedly closed",
	}
	for _, pattern := range transientPatterns {
		if strings.Contains(errStr, pattern) {
			return Wrap(err, ErrTransient)
		}
	}

	// Permission errors
	permissionPatterns := []string{
		"permission denied",
		"insufficient privilege",
	}
	for _, pattern := range permissionPatterns {
		if strings.Contains(errStr, pattern) {
			return Wrap(err, ErrPermission)
		}
	}

	// Constraint patterns BEFORE the bare "not found" check below. Order is
	// load-bearing: Oracle phrases a foreign-key violation as
	// "ORA-02291: integrity constraint (...) violated - parent key not found",
	// which contains both "constraint" and "not found". With "not found"
	// tested first, an FK violation classified as ErrNotFound and surfaced as
	// a 404 instead of a constraint error (#598). Only reachable when the
	// `oracle` build tag is absent — classifyOracleError is a no-op then and
	// ORA errors fall through to here — but the protection was a build flag
	// rather than code, which is what this restores.
	if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "unique constraint") {
		return Wrap(err, ErrDuplicate)
	}
	// "parent key not found" is ORA-02291; "child record found" is ORA-02292
	// (delete blocked by a dependent row). Both are foreign-key violations.
	if strings.Contains(errStr, "foreign key") ||
		strings.Contains(errStr, "parent key not found") ||
		strings.Contains(errStr, "child record found") {
		return Wrap(err, ErrForeignKey)
	}
	if strings.Contains(errStr, "constraint") || strings.Contains(errStr, "violates") {
		return Wrap(err, ErrConstraint)
	}

	// Not found (from RowsAffected == 0 checks that return error strings)
	if strings.Contains(errStr, "not found") {
		return Wrap(err, ErrNotFound)
	}

	// Unclassified — return as-is
	return err
}
