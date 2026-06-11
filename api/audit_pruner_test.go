package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
)

func TestPruneFailureMessage_AppendOnlyViolation(t *testing.T) {
	err := fmt.Errorf("failed to prune audit entries: %w",
		dberrors.Wrap(errors.New("ERROR: audit history is append-only (SQLSTATE P0001)"), dberrors.ErrAppendOnlyViolation))
	msg := pruneFailureMessage("audit entries", err)
	assert.Contains(t, msg, "append-only trigger")
	assert.Contains(t, msg, "restart the server")
}

func TestPruneFailureMessage_GenericError(t *testing.T) {
	msg := pruneFailureMessage("version snapshots", errors.New("connection refused"))
	assert.Contains(t, msg, "failed to prune version snapshots")
	assert.NotContains(t, msg, "append-only trigger")
}
