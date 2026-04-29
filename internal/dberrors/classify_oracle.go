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
	return classifyOracleCode(err, oraErr.Code())
}
