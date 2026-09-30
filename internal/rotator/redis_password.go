package rotator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// SEM@<sha>: add or retire passwords on the Redis default user
type RedisACL interface {
	AddPassword(ctx context.Context, pw string) error
	RemovePasswordHash(ctx context.Context, sha256hex string) error
}

// GoRedisACL implements RedisACL with ACL SETUSER.
// SEM@<sha>: RedisACL over a go-redis client using ACL SETUSER
type GoRedisACL struct{ client *redis.Client }

// SEM@<sha>: wrap a go-redis client as a RedisACL (pure)
func NewGoRedisACL(client *redis.Client) *GoRedisACL { return &GoRedisACL{client: client} }

// SEM@<sha>: add a password to the default user (ACL SETUSER default >pw); idempotent
func (a *GoRedisACL) AddPassword(ctx context.Context, pw string) error {
	return aclError(a.client.Do(ctx, "ACL", "SETUSER", "default", ">"+pw).Err())
}

// SEM@<sha>: remove a password from the default user by its SHA-256 (ACL SETUSER default !hash); idempotent
func (a *GoRedisACL) RemovePasswordHash(ctx context.Context, sha256hex string) error {
	return aclError(a.client.Do(ctx, "ACL", "SETUSER", "default", "!"+sha256hex).Err())
}

// SEM@<sha>: replace a Redis ACL error with a fixed message, keeping only a known error class (pure)
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
// SEM@<sha>: phased rotation of the Redis password: add new, swap Secret, roll server, retire old
type RedisPasswordRotation struct{ acl RedisACL }

// SEM@<sha>: build a RedisPasswordRotation over a RedisACL (pure)
func NewRedisPasswordRotation(acl RedisACL) *RedisPasswordRotation {
	return &RedisPasswordRotation{acl: acl}
}

// SEM@<sha>: return the rotation name used in annotations and ROTATE (pure)
func (r *RedisPasswordRotation) Name() string { return redisRotationName }

// SEM@<sha>: advance the Redis password rotation from its recorded phase to completion
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
			s.Annotations[AnnRetire+r.Name()] = sha256Hex(old)
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
	if retire := s.Annotations[AnnRetire+r.Name()]; retire != "" {
		if err := r.acl.RemovePasswordHash(ctx, retire); err != nil {
			return fmt.Errorf("retire old Redis password: %w", err)
		}
		logger.Info("Old Redis password retired")
	}
	return env.Transition(ctx, r.Name(), redisPhaseSwapped, "", func(s *Secret) {
		delete(s.Annotations, AnnRetire+r.Name())
	})
}

// SEM@<sha>: hex SHA-256 of a string (pure)
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
