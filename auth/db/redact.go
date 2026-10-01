package db

import (
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/jackc/pgx/v5/pgconn"
)

// redactedError carries a fixed, credential-free message while keeping the
// original driver error reachable via Unwrap (errors.Is/As still work).
// Error() deliberately never includes the driver text (#1005).
type redactedError struct {
	msg string
	err error
}

// SEM@0000000: return the credential-free message of a redacted connection error (pure)
func (e *redactedError) Error() string { return e.msg }

// SEM@0000000: expose the original driver error for errors.Is/As (pure)
func (e *redactedError) Unwrap() error { return e.err }

// safeErrClass returns a log-safe class for a driver error: SQLSTATE for
// pgx, ORA-NNNNN for godror (anything with Code() int), else the Go type name.
// SEM@0000000: derive a credential-free error class from a database driver error (pure)
func safeErrClass(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "sqlstate=" + pgErr.Code
	}
	var oraErr interface{ Code() int }
	if errors.As(err, &oraErr) {
		return fmt.Sprintf("ORA-%05d", oraErr.Code())
	}
	return fmt.Sprintf("%T", err)
}

// failDB logs a fixed message plus the safe error class and returns an error
// whose text is equally credential-free, so callers that log it don't re-leak.
// SEM@0000000: log a driver failure with a fixed message and safe class, returning a redacted error
func failDB(log *slogging.Logger, msg string, err error) error {
	class := safeErrClass(err)
	log.Error("%s (error_class=%s)", msg, class)
	return &redactedError{msg: fmt.Sprintf("%s (error_class=%s)", msg, class), err: err}
}
