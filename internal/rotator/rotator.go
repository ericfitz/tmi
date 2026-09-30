package rotator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Env is everything a Rotation needs from the cluster.
// SEM@<sha>: cluster handles and settings shared by every rotation (pure)
type Env struct {
	Secrets          SecretStore
	Rollouts         RolloutWaiter
	SecretName       string        // "tmi-secrets"
	ServerDeployment string        // "tmi-server"
	RolloutTimeout   time.Duration // 10m
	Now              func() time.Time
}

// Rotation is one secret's phased, resumable rotation.
// SEM@<sha>: phased, idempotent rotation of one named secret
type Rotation interface {
	Name() string
	// Run resumes from the phase recorded on the Secret and returns when the
	// rotation is complete or parked (waiting on a later run). Idempotent.
	Run(ctx context.Context, env *Env) error
}

// Run evaluates every rotation: forced, in progress (phase annotation set), or
// due. Each rotation runs even if an earlier one failed; the first error is returned.
// SEM@<sha>: run every forced, in-progress or due rotation and report the first failure
func Run(ctx context.Context, env *Env, rotations []Rotation, force string) error {
	logger := slogging.Get()
	var firstErr error
	for _, r := range rotations {
		s, err := env.Secrets.Get(ctx, env.SecretName)
		if err != nil {
			return fmt.Errorf("read %s: %w", env.SecretName, err)
		}
		phase := s.Annotations[AnnPhase+r.Name()]
		now := env.Now()
		logger.InfoCtx(ctx, "rotation status",
			slog.String("secret", r.Name()),
			slog.Int("age_days", AgeDays(s, r.Name(), now)),
			slog.String("phase", phase))
		switch {
		case force == r.Name():
			logger.Info("Rotation forced secret=%s", r.Name())
		case phase != "":
			logger.Info("Rotation resuming secret=%s phase=%s", r.Name(), phase)
		case IsDue(s, r.Name(), now):
			logger.Info("Rotation due secret=%s", r.Name())
		default:
			continue
		}
		if err := r.Run(ctx, env); err != nil {
			logger.Error("Rotation failed secret=%s error=%v", r.Name(), err)
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", r.Name(), err)
			}
			continue
		}
		logger.Info("Rotation step complete secret=%s", r.Name())
	}
	return firstErr
}

// Transition reads the Secret, applies mutate, records the phase (or, for
// nextPhase == "", clears it and stamps rotated-at) together with the server
// Deployment's generation before the write, and writes everything in one Update.
// A conflict means another run touched the Secret: fail, do not retry.
// SEM@<sha>: apply a rotation phase change to the Secret atomically with its bookkeeping annotations
func (e *Env) Transition(ctx context.Context, name, nextPhase string, mutate func(s *Secret)) error {
	s, err := e.Secrets.Get(ctx, e.SecretName)
	if err != nil {
		return err
	}
	gen, err := e.Rollouts.Generation(ctx, e.ServerDeployment)
	if err != nil {
		return fmt.Errorf("read %s generation: %w", e.ServerDeployment, err)
	}
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations[AnnGeneration+name] = strconv.FormatInt(gen, 10)
	if nextPhase == "" {
		delete(s.Annotations, AnnPhase+name)
		s.Annotations[AnnRotatedAt+name] = e.Now().UTC().Format(time.RFC3339)
	} else {
		s.Annotations[AnnPhase+name] = nextPhase
	}
	// mutate runs last so a completing write may override rotated-at
	// (the settings-key drop keeps the promotion date as the rotation date).
	if mutate != nil {
		mutate(s)
	}
	if err := e.Secrets.Update(ctx, s); err != nil {
		if errors.Is(err, ErrConflict) {
			return fmt.Errorf("%s phase %q: %w (another rotator run is active?)", name, nextPhase, err)
		}
		return err
	}
	slogging.Get().Info("Rotation phase recorded secret=%s phase=%q", name, nextPhase)
	return nil
}

// WaitServerRolled waits for the server rollout caused by the last Transition of name.
// SEM@<sha>: wait for the server Deployment to roll past the generation recorded for a rotation
func (e *Env) WaitServerRolled(ctx context.Context, s *Secret, name string) error {
	since, err := strconv.ParseInt(s.Annotations[AnnGeneration+name], 10, 64)
	if err != nil {
		return fmt.Errorf("missing rotation-generation annotation for %s", name)
	}
	return e.Rollouts.WaitRolled(ctx, e.ServerDeployment, since, e.RolloutTimeout)
}
