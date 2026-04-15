package dberrors

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// Classify wraps a raw database error with the appropriate typed sentinel.
// It checks in order: context errors, GORM errors, driver-specific errors
// (PostgreSQL pgconn.PgError, Oracle godror.OraErr), then falls back to
// string matching for errors that don't carry typed driver info.
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
		errors.Is(err, ErrContextDone) {
		return err
	}

	// GORM-specific: record not found
	if errors.Is(err, gorm.ErrRecordNotFound) {
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

	// Not found (from RowsAffected == 0 checks that return error strings)
	if strings.Contains(errStr, "not found") {
		return Wrap(err, ErrNotFound)
	}

	// Constraint patterns (fallback for non-typed driver errors)
	if strings.Contains(errStr, "duplicate") || strings.Contains(errStr, "unique constraint") {
		return Wrap(err, ErrDuplicate)
	}
	if strings.Contains(errStr, "foreign key") {
		return Wrap(err, ErrForeignKey)
	}
	if strings.Contains(errStr, "constraint") || strings.Contains(errStr, "violates") {
		return Wrap(err, ErrConstraint)
	}

	// Unclassified — return as-is
	return err
}
