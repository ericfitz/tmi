package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadOptions_DefaultsAndOverrides(t *testing.T) {
	o, err := loadOptions(func(string) string { return "" })
	require.NoError(t, err)
	require.Equal(t, "tmi-platform", o.Namespace)
	require.Equal(t, "tmi-secrets", o.SecretName)
	require.Equal(t, "tmi-server", o.ServerDeployment)
	require.Equal(t, 10*time.Minute, o.RolloutTimeout)
	require.Equal(t, 192*time.Hour, o.SettingsPreviousGrace)
	require.Equal(t, "redis", o.RedisHost)
	require.Equal(t, "6379", o.RedisPort)

	env := map[string]string{"ROTATE": "redis-password", "TMI_ROTATOR_ROLLOUT_TIMEOUT": "30s", "TMI_REDIS_DB": "1"}
	o, err = loadOptions(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, "redis-password", o.Force)
	require.Equal(t, 30*time.Second, o.RolloutTimeout)
	require.Equal(t, 1, o.RedisDB)

	_, err = loadOptions(func(k string) string { return map[string]string{"TMI_ROTATOR_ROLLOUT_TIMEOUT": "soon"}[k] })
	require.Error(t, err)
}

func TestIsRedisAuthError(t *testing.T) {
	require.True(t, isRedisAuthError(errString("WRONGPASS invalid username-password pair")))
	require.True(t, isRedisAuthError(errString("NOAUTH Authentication required")))
	require.False(t, isRedisAuthError(errString("connection refused")))
	require.False(t, isRedisAuthError(nil))
}

type errString string

func (e errString) Error() string { return string(e) }
