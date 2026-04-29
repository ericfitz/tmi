//go:build oracle

package dberrors

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClassifyOracleError_NonOraErr verifies that classifyOracleError returns
// nil when the input error does not contain a *godror.OraErr. This is the
// fall-through path that lets Classify drop down to the string fallback.
func TestClassifyOracleError_NonOraErr(t *testing.T) {
	plainErr := fmt.Errorf("not an Oracle error")
	got := classifyOracleError(plainErr)
	assert.Nil(t, got, "non-OraErr input should produce nil so caller can fall through")
}

// TestClassifyOracleError_NilError verifies that classifyOracleError handles
// a nil input gracefully (returns nil rather than panicking on errors.As).
func TestClassifyOracleError_NilError(t *testing.T) {
	got := classifyOracleError(nil)
	assert.Nil(t, got, "nil input should produce nil")
}

// TestClassifyOracleError_WrappedNonOraErr verifies that classifyOracleError
// returns nil when an error is wrapped multiple times but never contains a
// *godror.OraErr in the chain. Confirms the errors.As traversal stops cleanly
// rather than mis-classifying unrelated wrapped errors.
func TestClassifyOracleError_WrappedNonOraErr(t *testing.T) {
	inner := errors.New("inner error")
	middle := fmt.Errorf("middle: %w", inner)
	outer := fmt.Errorf("outer: %w", middle)
	got := classifyOracleError(outer)
	assert.Nil(t, got, "wrapped non-OraErr chain should produce nil")
}

// Note on positive-path coverage:
//
// The godror.OraErr struct has unexported fields and no exported constructor,
// so a synthetic *godror.OraErr cannot be built in a unit test. The
// (err, code) -> sentinel mapping is exhaustively unit-tested via
// classifyOracleCode_test.go (TestClassifyOracleCode_*). The remaining
// behavior — that classifyOracleError correctly extracts oraErr.Code() and
// forwards it to classifyOracleCode via errors.As — is exercised end-to-end
// in the integration suite via `make test-integration-oci`, which provokes
// real ORA codes from the godror driver and asserts the resulting
// dberrors.Classify behavior.
