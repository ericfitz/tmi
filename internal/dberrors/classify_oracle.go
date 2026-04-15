//go:build oracle

package dberrors

import (
	"errors"

	"github.com/godror/godror"
)

// classifyOracleError extracts a godror.OraErr and classifies by ORA- code.
// Returns nil if the error doesn't contain an OraErr.
func classifyOracleError(err error) error {
	var oraErr *godror.OraErr
	if !errors.As(err, &oraErr) {
		return nil
	}

	code := oraErr.Code()

	switch code {
	// Unique constraint violated
	case 1: // ORA-00001
		return Wrap(err, ErrDuplicate)

	// Foreign key violations
	case 2291, 2292: // ORA-02291 (parent key not found), ORA-02292 (child record found)
		return Wrap(err, ErrForeignKey)

	// Serialization / deadlock
	case 8177: // ORA-08177 (can't serialize access)
		return Wrap(err, ErrTransient)
	case 60: // ORA-00060 (deadlock detected)
		return Wrap(err, ErrTransient)

	// Connection errors
	case 3113, 3114: // ORA-03113/03114 (end-of-file on communication channel / not connected)
		return Wrap(err, ErrTransient)
	case 3135: // ORA-03135 (connection lost contact)
		return Wrap(err, ErrTransient)
	case 12170: // ORA-12170 (connect timeout)
		return Wrap(err, ErrTransient)
	case 12541, 12543: // ORA-12541/12543 (no listener / destination host unreachable)
		return Wrap(err, ErrTransient)

	// Permission / credential errors
	case 1017: // ORA-01017 (invalid username/password)
		return Wrap(err, ErrPermission)
	case 1031: // ORA-01031 (insufficient privileges)
		return Wrap(err, ErrPermission)
	}

	return nil
}
