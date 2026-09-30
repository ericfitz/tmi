package rotator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: fake ACL manager recording added and removed passwords for tests
type fakeACL struct {
	added   []string
	removed []string
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: record an added password in the fake ACL
func (f *fakeACL) AddPassword(_ context.Context, pw string) error {
	f.added = append(f.added, pw)
	return nil
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: record a removed password hash in the fake ACL
func (f *fakeACL) RemovePasswordHash(_ context.Context, h string) error {
	f.removed = append(f.removed, h)
	return nil
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: compute the SHA-256 hex digest of a string for tests (pure)
func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that a Redis password rotation completes a full cycle
func TestRedisPasswordRotation_FullCycle(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{"TMI_REDIS_PASSWORD": "old"}, Annotations: map[string]string{}})
	acl := &fakeACL{}
	r := NewRedisPasswordRotation(acl)
	require.NoError(t, r.Run(context.Background(), env))

	s, _ := st.Get(context.Background(), "tmi-secrets")
	newPw := s.Data["TMI_REDIS_PASSWORD"]
	require.NotEqual(t, "old", newPw)
	require.Len(t, newPw, 32)
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
	require.NotEmpty(t, s.Annotations[AnnRotatedAt+"redis-password"])
	_, retireLeft := s.Annotations[AnnRetire+"redis-password"]
	require.False(t, retireLeft)
	require.Equal(t, []string{newPw, newPw}, acl.added, "added before the swap and re-added on resume")
	require.Equal(t, []string{sha("old")}, acl.removed)
	require.Equal(t, 1, st.DataWrites, "exactly one server roll per rotation")
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that a resumed rotation re-adds the new password
func TestRedisPasswordRotation_ResumeReAddsNewPassword(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "0"}})
	st.DataWrites = 1 // the swap write already happened and the server rolled
	acl := &fakeACL{}
	require.NoError(t, NewRedisPasswordRotation(acl).Run(context.Background(), env))
	require.Equal(t, []string{"new"}, acl.added)
	require.Equal(t, []string{sha("old")}, acl.removed)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that a rollout timeout keeps the rotation phase for resume
func TestRedisPasswordRotation_RolloutTimeoutKeepsPhase(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "5"}})
	// DataWrites (0) <= since (5): the fake waiter reports "did not roll".
	acl := &fakeACL{}
	err := NewRedisPasswordRotation(acl).Run(context.Background(), env)
	require.Error(t, err)
	require.Empty(t, acl.removed, "old password must not be retired before the rollout")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "swapped", s.Annotations[AnnPhase+"redis-password"])
}

// echoHook makes every command fail the way Redis does: echoing the ACL modifier.
// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: Redis hook echoing a fixed prefix and command argument in errors for tests
type echoHook struct{ prefix string }

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: pass through the dial hook unchanged (pure)
func (echoHook) DialHook(next redis.DialHook) redis.DialHook { return next }

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: fail every Redis command with an error echoing its last argument
func (h echoHook) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		return errors.New(h.prefix + " invalid modifier " + cmd.Args()[len(cmd.Args())-1].(string))
	}
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: pass through the pipeline hook unchanged (pure)
func (echoHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that Redis ACL errors never contain the password
func TestGoRedisACL_ErrorsNeverContainPassword(t *testing.T) {
	const pw = "s3cret-pw-value"
	for _, prefix := range []string{"ERR", "WRONGPASS", "NOPERM"} {
		c := redis.NewClient(&redis.Options{Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("no dial")
		}})
		c.AddHook(echoHook{prefix})
		acl := NewGoRedisACL(c)
		for _, err := range []error{acl.AddPassword(context.Background(), pw), acl.RemovePasswordHash(context.Background(), pw)} {
			require.Error(t, err)
			require.NotContains(t, err.Error(), pw)
		}
		require.Contains(t, acl.AddPassword(context.Background(), pw).Error(), "ACL SETUSER default failed")
	}
}
