package dberrors

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

// TestClassifyPgError_StringTruncation verifies that PostgreSQL's
// SQLSTATE 22001 (string_data_right_truncation) maps to ErrConstraint,
// matching ORA-12899 behavior so cross-DB length-overflow reporting is
// consistent.
func TestClassifyPgError_StringTruncation(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "22001",
		Message: "value too long for type character varying(10)",
	}
	got := classifyPgError(pgErr)
	assert.True(t, errors.Is(got, ErrConstraint), "22001 should map to ErrConstraint")
}

// TestClassifyPgError_NumericOutOfRange verifies that PostgreSQL's
// SQLSTATE 22003 (numeric_value_out_of_range) maps to ErrConstraint.
func TestClassifyPgError_NumericOutOfRange(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "22003",
		Message: "smallint out of range",
	}
	got := classifyPgError(pgErr)
	assert.True(t, errors.Is(got, ErrConstraint), "22003 should map to ErrConstraint")
}

// TestClassifyPgError_UniqueViolation verifies the existing 23505 path is
// unaffected by the new Class 22 handling.
func TestClassifyPgError_UniqueViolation(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:    "23505",
		Message: "duplicate key value violates unique constraint",
	}
	got := classifyPgError(pgErr)
	assert.True(t, errors.Is(got, ErrDuplicate), "23505 should map to ErrDuplicate")
	assert.True(t, errors.Is(got, ErrConstraint), "ErrDuplicate wraps ErrConstraint")
}
