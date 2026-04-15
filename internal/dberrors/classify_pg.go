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

	return nil
}
