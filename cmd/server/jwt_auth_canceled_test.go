package main

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/assert"
)

// A canceled or expired request context while checking the revocation store is
// the client going away (or a stalled body read), not a dependency outage.
func TestRevocationAuthError_CanceledContextIsNot503(t *testing.T) {
	err := fmt.Errorf("failed to check token blacklist: %w", context.Canceled)
	got := revocationAuthError(nil, err)
	assert.Equal(t, http.StatusBadRequest, got.StatusCode)
	assert.Zero(t, got.RetryAfter)
}

// A deadline is the server's own timeout or a hung store: still an outage.
func TestRevocationAuthError_DeadlineExceededStays503(t *testing.T) {
	err := fmt.Errorf("failed to check token blacklist: %w", context.DeadlineExceeded)
	got := revocationAuthError(slogging.Get(), err)
	assert.Equal(t, http.StatusServiceUnavailable, got.StatusCode)
	assert.Positive(t, got.RetryAfter)
}

func TestRevocationAuthError_StoreOutageStill503(t *testing.T) {
	got := revocationAuthError(slogging.Get(), fmt.Errorf("failed to check token blacklist: %w", fmt.Errorf("redis: connection refused")))
	assert.Equal(t, http.StatusServiceUnavailable, got.StatusCode)
}
