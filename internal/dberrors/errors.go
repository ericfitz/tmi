// Package dberrors provides typed database error classification.
// Repositories use Classify() to wrap raw driver errors with sentinel types.
// Services check IsFatal() and decide retry policy.
// Handlers use errors.Is() for HTTP status mapping.
package dberrors

import (
	"errors"
	"fmt"
)

// Top-level error categories
var (
	ErrNotFound    = errors.New("not found")
	ErrConstraint  = errors.New("constraint violation")
	ErrTransient   = errors.New("transient database error")
	ErrPermission  = errors.New("permission denied")
	ErrContextDone = errors.New("context cancelled")

	// ErrUndefinedObject fires when a referenced schema object (table, view, or
	// sequence) does not exist — PostgreSQL SQLSTATE 42P01, Oracle ORA-02289
	// (sequence does not exist) / ORA-00942 (table or view does not exist). It
	// indicates schema drift (an object dropped out from under a running
	// server), not a user-input error, and is NOT transient: a bare retry
	// without repairing the object will fail identically. Callers that can
	// recreate the object (e.g. reinstall a missing sequence) may use this to
	// trigger a repair-and-retry; otherwise it should surface as a 500.
	ErrUndefinedObject = errors.New("undefined database object")
)

// Constraint sub-categories (wrap ErrConstraint so errors.Is works for both)
var (
	ErrDuplicate  = fmt.Errorf("duplicate: %w", ErrConstraint)
	ErrForeignKey = fmt.Errorf("foreign key: %w", ErrConstraint)

	// ErrAppendOnlyViolation fires when the audit_entries / version_snapshots
	// append-only triggers (T19, #356) reject an UPDATE or DELETE. Distinct
	// from a generic constraint violation because it indicates a code-correctness
	// bug (or operator error / hostile action), not a user input error — callers
	// should never expect a retry to fix this.
	ErrAppendOnlyViolation = fmt.Errorf("append-only violation: %w", ErrConstraint)
)

// IsRetryable returns true if the error represents a transient condition
// that may succeed on retry (connection errors, serialization failures, deadlocks).
func IsRetryable(err error) bool {
	return errors.Is(err, ErrTransient)
}

// IsFatal returns true if the error indicates the server is fundamentally broken
// and should shut down (permission denied, invalid credentials).
func IsFatal(err error) bool {
	return errors.Is(err, ErrPermission)
}

// Wrap wraps a raw error with a typed sentinel, preserving the original error chain.
// Example: Wrap(rawErr, ErrDuplicate) returns an error where:
//   - errors.Is(result, ErrDuplicate) == true
//   - errors.Is(result, ErrConstraint) == true (because ErrDuplicate wraps ErrConstraint)
//   - errors.Unwrap(result) chain reaches rawErr
func Wrap(err error, sentinel error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}
