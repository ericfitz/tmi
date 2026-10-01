package rotator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// SEM@0000000: test that the Redis password func tracks the Secret through every rotation phase
func TestRedisPasswordFromSecret_TracksSecretAcrossPhases(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{RedisPasswordKey: "old"}, Annotations: map[string]string{}})
	ctx := context.Background()
	pwf := RedisPasswordFromSecret(st, "tmi-secrets")

	got, err := pwf(ctx)
	require.NoError(t, err)
	require.Equal(t, "old", got, "idle: current password")

	// swapped: the Secret holds NEW (Redis accepts both) and a reconnect must use NEW.
	require.NoError(t, env.Transition(ctx, "redis-password", "", "swapped", func(s *Secret) { s.Data[RedisPasswordKey] = "new" }))
	got, _ = pwf(ctx)
	require.Equal(t, "new", got, "swapped: new password")

	// completed: phase cleared, still NEW.
	require.NoError(t, env.Transition(ctx, "redis-password", "swapped", "", nil))
	got, _ = pwf(ctx)
	require.Equal(t, "new", got, "after retire")
}

// SEM@0000000: test that the Redis password func fails when the Secret cannot be read
func TestRedisPasswordFromSecret_MissingSecret(t *testing.T) {
	_, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	_, err := RedisPasswordFromSecret(st, "nope")(context.Background())
	require.Error(t, err)
}
