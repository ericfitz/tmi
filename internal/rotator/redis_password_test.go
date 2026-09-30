package rotator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"sort"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// fakeACL models the default user's password list the way Redis does:
// ">pw" adds (no-op if present), "resetpass >pw" replaces the whole list.
// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: fake Redis default-user password list recording ACL calls for tests
type fakeACL struct {
	passwords map[string]bool
	added     []string
	setOnly   []string
	// beforeSetOnly, if set, runs once before the first SetOnlyPassword (interleaves a peer run).
	beforeSetOnly func()
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: add a password to the fake default user and record it
func (f *fakeACL) AddPassword(_ context.Context, pw string) error {
	if f.passwords == nil {
		f.passwords = map[string]bool{}
	}
	f.passwords[pw] = true
	f.added = append(f.added, pw)
	return nil
}

// SEM@451b8e1bd71b581392bf388c61e8d06507f13080: replace the fake default user's passwords with one and record it
func (f *fakeACL) SetOnlyPassword(_ context.Context, pw string) error {
	if hook := f.beforeSetOnly; hook != nil {
		f.beforeSetOnly = nil
		hook()
	}
	f.passwords = map[string]bool{pw: true}
	f.setOnly = append(f.setOnly, pw)
	return nil
}

// SEM@451b8e1bd71b581392bf388c61e8d06507f13080: list the fake default user's passwords, sorted (pure)
func (f *fakeACL) list() []string {
	out := make([]string, 0, len(f.passwords))
	for pw := range f.passwords {
		out = append(out, pw)
	}
	sort.Strings(out)
	return out
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: compute the SHA-256 hex digest of a string for tests (pure)
func sha(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that a Redis password rotation completes a full cycle
func TestRedisPasswordRotation_FullCycle(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{"TMI_REDIS_PASSWORD": "old"}, Annotations: map[string]string{}})
	acl := &fakeACL{passwords: map[string]bool{"old": true}}
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
	require.Equal(t, []string{newPw}, acl.setOnly)
	require.Equal(t, []string{newPw}, acl.list(), "only the new password remains")
	require.Equal(t, 1, st.DataWrites, "exactly one server roll per rotation")
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that a resumed rotation re-adds the new password
func TestRedisPasswordRotation_ResumeReAddsNewPassword(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "0"}})
	st.DataWrites = 1 // the swap write already happened and the server rolled
	acl := &fakeACL{passwords: map[string]bool{"old": true, "new": true}}
	require.NoError(t, NewRedisPasswordRotation(acl).Run(context.Background(), env))
	require.Equal(t, []string{"new"}, acl.added)
	require.Equal(t, []string{"new"}, acl.list())
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
}

// SEM@29cc34458a8a4bb805834efb797ee212ac2f2792: test that a rollout timeout keeps the rotation phase for resume
func TestRedisPasswordRotation_RolloutTimeoutKeepsPhase(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "5"}})
	// DataWrites (0) <= since (5): the fake waiter reports "did not roll".
	acl := &fakeACL{passwords: map[string]bool{"old": true, "new": true}}
	err := NewRedisPasswordRotation(acl).Run(context.Background(), env)
	require.Error(t, err)
	require.Empty(t, acl.setOnly, "old password must not be retired before the rollout")
	require.Equal(t, []string{"new", "old"}, acl.list())
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "swapped", s.Annotations[AnnPhase+"redis-password"])
}

// swappedSecret is a Secret already swapped to "new", with the server rolled.
// SEM@451b8e1bd71b581392bf388c61e8d06507f13080: build a tmi-secrets fixture in phase swapped with the server rolled (pure)
func swappedSecret() *Secret {
	return &Secret{Name: "tmi-secrets",
		Data:        map[string]string{"TMI_REDIS_PASSWORD": "new"},
		Annotations: map[string]string{AnnPhase + "redis-password": "swapped", AnnRetire + "redis-password": sha("old"), AnnGeneration + "redis-password": "0"}}
}

// A Redis restart after the swap leaves only NEW (requirepass); OLD is gone.
// SEM@451b8e1bd71b581392bf388c61e8d06507f13080: test that a resumed rotation completes when the old password is already gone
func TestRedisPasswordRotation_ResumeAfterRedisRestart(t *testing.T) {
	env, st := testEnv(swappedSecret())
	st.DataWrites = 1
	acl := &fakeACL{passwords: map[string]bool{"new": true}}
	require.NoError(t, NewRedisPasswordRotation(acl).Run(context.Background(), env))
	require.Equal(t, []string{"new"}, acl.list())
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
}

// An orphan from a run that lost the compare-and-swap is dropped by the retire.
// SEM@451b8e1bd71b581392bf388c61e8d06507f13080: test that retiring drops an orphaned password from a lost race
func TestRedisPasswordRotation_RetireDropsOrphanedPassword(t *testing.T) {
	env, st := testEnv(swappedSecret())
	st.DataWrites = 1
	acl := &fakeACL{passwords: map[string]bool{"old": true, "new": true, "orphan": true}}
	require.NoError(t, NewRedisPasswordRotation(acl).Run(context.Background(), env))
	require.Equal(t, []string{"new"}, acl.list())
}

// Two runs resume the same swapped phase; the peer completes (and retires)
// just before this run's retire. The retire must not fail; the run then loses
// the phase compare-and-swap, and Redis ends with exactly NEW.
// SEM@451b8e1bd71b581392bf388c61e8d06507f13080: test that a concurrent run's retire after the peer already retired succeeds
func TestRedisPasswordRotation_ConcurrentRetireIsIdempotent(t *testing.T) {
	env, st := testEnv(swappedSecret())
	st.DataWrites = 1
	acl := &fakeACL{passwords: map[string]bool{"old": true, "new": true}}
	var peerErr error
	acl.beforeSetOnly = func() { peerErr = NewRedisPasswordRotation(acl).Run(context.Background(), env) }

	err := NewRedisPasswordRotation(acl).Run(context.Background(), env)
	require.NoError(t, peerErr, "peer run completes")
	require.ErrorIs(t, err, ErrConflict, "the later run fails only on the phase CAS, not on the retire")
	require.NotContains(t, err.Error(), "retire old Redis password")
	require.Equal(t, []string{"new", "new"}, acl.setOnly, "both retires succeeded")
	require.Equal(t, []string{"new"}, acl.list())
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
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
		for _, err := range []error{acl.AddPassword(context.Background(), pw), acl.SetOnlyPassword(context.Background(), pw)} {
			require.Error(t, err)
			require.NotContains(t, err.Error(), pw)
		}
		require.Contains(t, acl.AddPassword(context.Background(), pw).Error(), "ACL SETUSER default failed")
	}
}
