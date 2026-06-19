//go:build oracle

package dberrors

import (
	"errors"

	"github.com/godror/godror"
)

// classifyOracleError extracts a godror.OraErr and classifies by ORA- code.
// Returns nil if the error doesn't contain an OraErr.
// SEM@a804aeb2d0a73b7033982c725952db9d89d71453: map an Oracle ORA- error code to a canonical db error (pure)
func classifyOracleError(err error) error {
	var oraErr *godror.OraErr
	if !errors.As(err, &oraErr) {
		return nil
	}
	return classifyOracleCode(err, oraErr.Code())
}
