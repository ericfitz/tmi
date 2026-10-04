package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/internal/rotator"
)

// SEM@3b682947: verify rotator options load defaults and environment overrides
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

// SEM@3b682947: verify Redis authentication errors are recognized
func TestIsRedisAuthError(t *testing.T) {
	require.True(t, isRedisAuthError(errString("WRONGPASS invalid username-password pair")))
	require.True(t, isRedisAuthError(errString("NOAUTH Authentication required")))
	require.False(t, isRedisAuthError(errString("connection refused")))
	require.False(t, isRedisAuthError(nil))
}

// SEM@3b682947: string-backed error type for rotator tests
type errString string

// SEM@3b682947: return the error message string (pure)
func (e errString) Error() string { return string(e) }

// SEM@b949412f: verify the escrow ARN loads from the environment and is optional
func TestLoadOptions_SettingsEscrowARN(t *testing.T) {
	env := map[string]string{"TMI_ROTATOR_SETTINGS_ESCROW_SECRET_ARN": "arn:aws:secretsmanager:us-east-1:1:secret:x"}
	o, err := loadOptions(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.Equal(t, "arn:aws:secretsmanager:us-east-1:1:secret:x", o.SettingsEscrowARN)

	o, err = loadOptions(func(string) string { return "" })
	require.NoError(t, err)
	require.Empty(t, o.SettingsEscrowARN)
}

// SEM@b949412f: verify a malformed or regionless escrow ARN is rejected at load
func TestLoadOptions_RejectsMalformedEscrowARN(t *testing.T) {
	env := map[string]string{"TMI_ROTATOR_SETTINGS_ESCROW_SECRET_ARN": "arn:aws:s3:::bucket"}
	_, err := loadOptions(func(k string) string { return env[k] })
	require.ErrorContains(t, err, "TMI_ROTATOR_SETTINGS_ESCROW_SECRET_ARN")
}

// SEM@b949412f: verify run exits 2 on a malformed escrow ARN before touching the cluster
func TestRun_MalformedEscrowARNExits2(t *testing.T) {
	t.Setenv("TMI_ROTATOR_SETTINGS_ESCROW_SECRET_ARN", "arn:aws:s3:::bucket")
	require.Equal(t, 2, run())
}

// SEM@b949412f: verify an empty escrow ARN yields the no-op escrow
func TestNewSettingsEscrow_EmptyARNIsNoop(t *testing.T) {
	e, err := newSettingsEscrow(context.Background(), "")
	require.NoError(t, err)
	require.IsType(t, rotator.NoopEscrow{}, e)
}
