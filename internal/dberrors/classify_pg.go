package dberrors

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// classifyPgError extracts a pgconn.PgError and classifies by SQLSTATE code.
// Returns nil if the error doesn't contain a PgError.
func classifyPgError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}

	code := pgErr.Code

	// Class 23 — Integrity Constraint Violation
	if strings.HasPrefix(code, "23") {
		switch code {
		case "23505": // unique_violation
			return Wrap(err, ErrDuplicate)
		case "23503": // foreign_key_violation
			return Wrap(err, ErrForeignKey)
		default: // 23000 (integrity_constraint_violation), 23502 (not_null), 23514 (check), etc.
			return Wrap(err, ErrConstraint)
		}
	}

	// Class 22 — Data Exception (length / numeric overflow). Mapped to
	// ErrConstraint -> HTTP 400 to match Oracle's ORA-12899 behavior. This is
	// correct when an end-user-supplied string overflows a column. Server-
	// generated overflows (audit fields, derived identifiers) should be
	// treated as 500 by the caller — see issue #311 for the recommended
	// pattern of wrapping repository calls so input-bound writes opt into the
	// 400 mapping while server-bound writes default to 500. Adding parity
	// with ORA-12899 here so cross-DB behavior is consistent.
	switch code {
	case "22001": // string_data_right_truncation — analogue of ORA-12899
		return Wrap(err, ErrConstraint)
	case "22003": // numeric_value_out_of_range — closest analogue for numeric overflow
		return Wrap(err, ErrConstraint)
	}

	// Class 40 — Transaction Rollback
	switch code {
	case "40001": // serialization_failure
		return Wrap(err, ErrTransient)
	case "40P01": // deadlock_detected
		return Wrap(err, ErrTransient)
	}

	// Class 08 — Connection Exception
	if strings.HasPrefix(code, "08") {
		return Wrap(err, ErrTransient)
	}

	// Class 57 — Operator Intervention
	switch code {
	case "57P01": // admin_shutdown
		return Wrap(err, ErrTransient)
	case "57P03": // cannot_connect_now
		return Wrap(err, ErrTransient)
	}

	// Privilege errors
	switch code {
	case "42501": // insufficient_privilege
		return Wrap(err, ErrPermission)
	case "28P01": // invalid_password
		return Wrap(err, ErrPermission)
	case "28000": // invalid_authorization_specification
		return Wrap(err, ErrPermission)
	}

	// PG raises with SQLSTATE 'P0001' (raise_exception) when a PL/pgSQL
	// RAISE EXCEPTION fires without a custom ERRCODE. The audit-trail
	// append-only triggers (T19, #356) raise with this code; the message
	// always contains "append-only" so we use that as the distinguisher
	// from arbitrary user-defined raises in unrelated stored procedures.
	// The generic P0001 case still falls through to nil so callers can
	// decide how to handle it.
	if code == "P0001" && strings.Contains(pgErr.Message, "append-only") {
		return Wrap(err, ErrAppendOnlyViolation)
	}

	return nil
}
