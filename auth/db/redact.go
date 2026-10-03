package db

import (
	"errors"
	"fmt"
	"regexp"

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

// dpiCode matches the leading Oracle client-library code (e.g. DPI-1047).
var dpiCode = regexp.MustCompile(`^DPI-\d+`)

// safeErrClass returns a log-safe class for a driver error: SQLSTATE for
// pgx, ORA-NNNNN for godror (anything with Code() int), DPI-NNNN for client
// library errors (godror reports Code() 0 for those), else the Go type name.
// classifyByString in internal/dberrors cannot see driver text through the
// redacted error; wrap startup connects in retries only via typed checks.
// SEM@0000000: derive a credential-free error class from a database driver error (pure)
func safeErrClass(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "sqlstate=" + pgErr.Code
	}
	var oraErr interface{ Code() int }
	if errors.As(err, &oraErr) {
		if oraErr.Code() != 0 {
			return fmt.Sprintf("ORA-%05d", oraErr.Code())
		}
		var msgErr interface{ Message() string }
		if errors.As(err, &msgErr) {
			if dpi := dpiCode.FindString(msgErr.Message()); dpi != "" {
				return dpi
			}
		}
	}
	return fmt.Sprintf("%T", err)
}

// permanentConnectClasses are connect-error classes a retry cannot fix:
// bad or locked credentials, unknown TNS alias, wallet/certificate failures,
// a missing Oracle client, a missing Postgres database. Each Oracle retry is
// another failed login, which can lock the ADB account (#972 Oracle review).
var permanentConnectClasses = map[string]bool{
	"ORA-01017": true, "ORA-28000": true, "ORA-28001": true, "ORA-12154": true,
	"ORA-28759": true, "ORA-29024": true, "DPI-1047": true,
	"sqlstate=28P01": true, "sqlstate=28000": true, "sqlstate=3D000": true,
}

// IsPermanentConnectError reports whether a connect error can never succeed on retry.
// SEM@82c42d73: classify a database connect error as non-retryable (pure)
func IsPermanentConnectError(err error) bool {
	return err != nil && permanentConnectClasses[safeErrClass(err)]
}

// failDB logs a fixed message plus the safe error class and returns an error
// whose text is equally credential-free, so callers that log it don't re-leak.
// SEM@0000000: log a driver failure with a fixed message and safe class, returning a redacted error
func failDB(log *slogging.Logger, msg string, err error) error {
	class := safeErrClass(err)
	log.Error("%s (error_class=%s)", msg, class)
	return &redactedError{msg: fmt.Sprintf("%s (error_class=%s)", msg, class), err: err}
}
