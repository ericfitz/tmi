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

type fakeACL struct {
	added   []string
	removed []string
}

func (f *fakeACL) AddPassword(_ context.Context, pw string) error {
	f.added = append(f.added, pw)
	return nil
}
func (f *fakeACL) RemovePasswordHash(_ context.Context, h string) error {
	f.removed = append(f.removed, h)
	return nil
}

func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

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
type echoHook struct{ prefix string }

func (echoHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h echoHook) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		return errors.New(h.prefix + " invalid modifier " + cmd.Args()[len(cmd.Args())-1].(string))
	}
}
func (echoHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

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
