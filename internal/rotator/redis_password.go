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
// SEM@3b682947: add a password to, or reset to one password on, the Redis default user
// SEM@3b682947: define Redis ACL operations for adding a password and keeping only one
type RedisACL interface {
	AddPassword(ctx context.Context, pw string) error
	SetOnlyPassword(ctx context.Context, pw string) error
}

// GoRedisACL implements RedisACL with ACL SETUSER.
// SEM@3b682947: RedisACL over a go-redis client using ACL SETUSER
// SEM@3b682947: Redis ACL manager backed by a go-redis client
type GoRedisACL struct{ client *redis.Client }

// SEM@3b682947: wrap a go-redis client as a RedisACL (pure)
// SEM@3b682947: build a go-redis ACL manager
func NewGoRedisACL(client *redis.Client) *GoRedisACL { return &GoRedisACL{client: client} }

// SEM@3b682947: add a password to the default user (ACL SETUSER default >pw); idempotent
// SEM@3b682947: add a password to the Redis default user (writes Redis)
func (a *GoRedisACL) AddPassword(ctx context.Context, pw string) error {
	return aclError(a.client.Do(ctx, "ACL", "SETUSER", "default", ">"+pw).Err())
}

// SetOnlyPassword makes pw the default user's only password in one atomic
// ACL SETUSER (resetpass clears the password list; flags and permissions stay).
// Idempotent: unlike "!hash", it does not fail when an old password is already gone.
// SEM@3b682947: replace all default-user passwords with one (ACL SETUSER default resetpass >pw); idempotent
// SEM@3b682947: set the sole password of the Redis default user (writes Redis)
func (a *GoRedisACL) SetOnlyPassword(ctx context.Context, pw string) error {
	return aclError(a.client.Do(ctx, "ACL", "SETUSER", "default", "resetpass", ">"+pw).Err())
}

// SEM@3b682947: replace a Redis ACL error with a fixed message, keeping only a known error class (pure)
// SEM@3b682947: wrap a Redis ACL error in a fixed message that never leaks the password (pure)
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

// RedisPasswordRotation rotates TMI_REDIS_PASSWORD without a Redis restart.
// SEM@3b682947: phased rotation of the Redis password: add new, swap Secret, roll server, retire old
// SEM@3b682947: rotation of the Redis password
type RedisPasswordRotation struct{ acl RedisACL }

// SEM@3b682947: build a RedisPasswordRotation over a RedisACL (pure)
// SEM@3b682947: build a Redis password rotation
func NewRedisPasswordRotation(acl RedisACL) *RedisPasswordRotation {
	return &RedisPasswordRotation{acl: acl}
}

// SEM@3b682947: return the rotation name used in annotations and ROTATE (pure)
// SEM@3b682947: return the rotation's name (pure)
func (r *RedisPasswordRotation) Name() string { return redisRotationName }

// SEM@3b682947: advance the Redis password rotation from its recorded phase to completion
// SEM@3b682947: run the phased, resumable Redis password rotation (writes secret, writes Redis)
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
