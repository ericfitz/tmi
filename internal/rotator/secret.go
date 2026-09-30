// Package rotator rotates the high-value secrets in tmi-secrets one idempotent
// phase at a time, recording progress in Secret annotations (#965).
package rotator

import (
	"context"
	"errors"
	"time"
)

// Secret is a snapshot of one Kubernetes Secret with decoded data.
// SEM@<sha>: snapshot of a Kubernetes Secret's data, annotations and resourceVersion (pure)
type Secret struct {
	Name            string
	Data            map[string]string
	Annotations     map[string]string
	ResourceVersion string
}

// ErrConflict is returned by SecretStore.Update when the Secret changed after it was read.
var ErrConflict = errors.New("secret changed since it was read")

// SecretStore reads and writes whole Secrets with optimistic concurrency.
// SEM@<sha>: read/update a Kubernetes Secret with resourceVersion conflict detection
type SecretStore interface {
	Get(ctx context.Context, name string) (*Secret, error)
	// Update replaces data and annotations; returns ErrConflict when s.ResourceVersion is stale.
	Update(ctx context.Context, s *Secret) error
}

// RolloutWaiter observes Deployment rollouts triggered by Secret changes (Reloader).
// SEM@<sha>: observe a Deployment's generation and wait for a rollout past it
type RolloutWaiter interface {
	Generation(ctx context.Context, deployment string) (int64, error)
	// WaitRolled returns once the Deployment's generation exceeds since and its rollout is complete.
	WaitRolled(ctx context.Context, deployment string, since int64, timeout time.Duration) error
}

// Clone returns a deep copy so a store can hand out independent snapshots.
// SEM@<sha>: deep-copy a Secret snapshot (pure)
func (s *Secret) Clone() *Secret {
	c := &Secret{Name: s.Name, ResourceVersion: s.ResourceVersion, Data: map[string]string{}, Annotations: map[string]string{}}
	for k, v := range s.Data {
		c.Data[k] = v
	}
	for k, v := range s.Annotations {
		c.Annotations[k] = v
	}
	return c
}
