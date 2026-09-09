package main

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/auth"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenBlacklistChecker_CheckCredentialRevoked pins #862: a service-account
// token is rejected once its client credential is revoked, and a checker
// without a blacklist store admits everything (no Redis configured).
func TestTokenBlacklistChecker_CheckCredentialRevoked(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()

	ctx := context.Background()
	tb := auth.NewTokenBlacklist(rdb, nil)
	checker := NewTokenBlacklistChecker(tb)

	assert.NoError(t, checker.CheckCredentialRevoked(ctx, "cred-1"))

	require.NoError(t, tb.RevokeCredential(ctx, "cred-1", 0))
	err = checker.CheckCredentialRevoked(ctx, "cred-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoked")
	assert.Equal(t, 401, revocationAuthError(nil, err).StatusCode)

	assert.NoError(t, checker.CheckCredentialRevoked(ctx, "cred-2"))
	assert.NoError(t, NewTokenBlacklistChecker(nil).CheckCredentialRevoked(ctx, "cred-1"))
}
