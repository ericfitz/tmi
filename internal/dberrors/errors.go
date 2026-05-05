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
