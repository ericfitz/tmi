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
	for name, cause := range map[string]error{
		"canceled": context.Canceled,
		"deadline": context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			err := fmt.Errorf("failed to check token blacklist: %w", cause)
			got := revocationAuthError(nil, err)
			assert.Equal(t, http.StatusBadRequest, got.StatusCode)
			assert.Zero(t, got.RetryAfter)
		})
	}
}

func TestRevocationAuthError_StoreOutageStill503(t *testing.T) {
	got := revocationAuthError(slogging.Get(), fmt.Errorf("failed to check token blacklist: %w", fmt.Errorf("redis: connection refused")))
	assert.Equal(t, http.StatusServiceUnavailable, got.StatusCode)
}
