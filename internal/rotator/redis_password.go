package rotator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	// #nosec G101 -- Secret data key name, not a credential
	RedisPasswordKey  = "TMI_REDIS_PASSWORD"
	AnnRetire         = "tmi.dev/rotation-retire."
	redisRotationName = "redis-password"
	redisPhaseSwapped = "swapped"
)

// RedisACL manages the passwords accepted for the default Redis user.
// Returned errors must never contain a password (Redis echoes ACL modifiers).
type RedisACL interface {
	AddPassword(ctx context.Context, pw string) error
	SetOnlyPassword(ctx context.Context, pw string) error
}

// GoRedisACL implements RedisACL with ACL SETUSER.
type GoRedisACL struct{ client *redis.Client }

func NewGoRedisACL(client *redis.Client) *GoRedisACL { return &GoRedisACL{client: client} }

func (a *GoRedisACL) AddPassword(ctx context.Context, pw string) error {
	return aclError(a.client.Do(ctx, "ACL", "SETUSER", "default", ">"+pw).Err())
}

// SetOnlyPassword makes pw the default user's only password in one atomic
// ACL SETUSER (resetpass clears the password list; flags and permissions stay).
// Idempotent: unlike "!hash", it does not fail when an old password is already gone.
func (a *GoRedisACL) SetOnlyPassword(ctx context.Context, pw string) error {
	return aclError(a.client.Do(ctx, "ACL", "SETUSER", "default", "resetpass", ">"+pw).Err())
}

func aclError(err error) error {
	if err == nil {
		return nil
	}
	for _, class := range []string{"WRONGPASS", "NOPERM", "NOAUTH"} {
		if strings.HasPrefix(err.Error(), class) {
			return errors.New("redis ACL SETUSER default failed: " + class)
		}
	}
	return errors.New("redis ACL SETUSER default failed")
}

// RedisPasswordFromSecret returns a go-redis credentials func that reads the
// current password from the Secret on every (re)connect, so a Redis restart
// mid-rotation reconnects with whatever the Secret holds now. Redis accepts
// the Secret's password in every phase: OLD before the swap, then OLD+NEW
// (and finally only NEW) once the Secret holds NEW.
func RedisPasswordFromSecret(store SecretStore, secretName string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		s, err := store.Get(ctx, secretName)
		if err != nil {
			return "", err
		}
		return s.Data[RedisPasswordKey], nil
	}
}

// RedisPasswordRotation rotates TMI_REDIS_PASSWORD without a Redis restart.
type RedisPasswordRotation struct{ acl RedisACL }

func NewRedisPasswordRotation(acl RedisACL) *RedisPasswordRotation {
	return &RedisPasswordRotation{acl: acl}
}

func (r *RedisPasswordRotation) Name() string { return redisRotationName }

func (r *RedisPasswordRotation) Run(ctx context.Context, env *Env) error {
	logger := slogging.Get()
	s, err := env.Secrets.Get(ctx, env.SecretName)
	if err != nil {
		return err
	}
	phase := s.Annotations[AnnPhase+r.Name()]

	if phase == "" {
		old := s.Data[RedisPasswordKey]
		if old == "" {
			return fmt.Errorf("%s has no %s to rotate", env.SecretName, RedisPasswordKey)
		}
		next, err := NewPassword()
		if err != nil {
			return err
		}
		if err := r.acl.AddPassword(ctx, next); err != nil {
			return fmt.Errorf("add new Redis password: %w", err)
		}
		logger.Info("Redis accepts the new password; swapping the Secret")
		if err := env.Transition(ctx, r.Name(), "", redisPhaseSwapped, func(s *Secret) {
			s.Data[RedisPasswordKey] = next
		}); err != nil {
			return err
		}
		if s, err = env.Secrets.Get(ctx, env.SecretName); err != nil {
			return err
		}
		phase = redisPhaseSwapped
	}

	if phase != redisPhaseSwapped {
		return fmt.Errorf("unknown %s phase %q", r.Name(), phase)
	}
	// Idempotent: a Redis restart drops the in-memory ACL entry for NEW.
	if err := r.acl.AddPassword(ctx, s.Data[RedisPasswordKey]); err != nil {
		return fmt.Errorf("re-add new Redis password: %w", err)
	}
	if err := env.WaitServerRolled(ctx, s, r.Name()); err != nil {
		return fmt.Errorf("waiting for %s to pick up the new Redis password: %w", env.ServerDeployment, err)
	}
	// Any later rollout satisfies the wait, including one from a peer that has
	// since swapped to another password: resetting Redis to our NEW then would
	// lock the server out. Re-check the Secret still holds our phase and NEW.
	// ponytail: a peer swap in the ms between this re-read and the ACL write
	// still races; closing it needs a Secret-level lease around the retire.
	cur, err := env.Secrets.Get(ctx, env.SecretName)
	if err != nil {
		return err
	}
	if cur.Annotations[AnnPhase+r.Name()] != redisPhaseSwapped || cur.Data[RedisPasswordKey] != s.Data[RedisPasswordKey] {
		return fmt.Errorf("%s changed while waiting for the rollout: %w (another rotator run is active?)", env.SecretName, ErrConflict)
	}
	// Retire by keeping only NEW: succeeds whether OLD is still set, already
	// gone (Redis restart, peer run), or joined by an orphan from a lost race.
	if err := r.acl.SetOnlyPassword(ctx, s.Data[RedisPasswordKey]); err != nil {
		return fmt.Errorf("retire old Redis password: %w", err)
	}
	logger.Info("Old Redis password retired")
	// AnnRetire is no longer written; the delete cleans up legacy Secrets.
	return env.Transition(ctx, r.Name(), redisPhaseSwapped, "", func(s *Secret) {
		delete(s.Annotations, AnnRetire+r.Name())
	})
}
