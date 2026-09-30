package rotator

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// MemorySecretStore is an in-memory SecretStore for tests. It bumps
// resourceVersion on every Update and counts data writes so a FakeRolloutWaiter
// can imitate Reloader (which rolls on data changes only).
// SEM@<sha>: in-memory SecretStore with resourceVersion conflicts and data-write counting (mutates shared state)
type MemorySecretStore struct {
	mu         sync.Mutex
	secrets    map[string]*Secret
	version    int
	DataWrites int
}

// NewMemorySecretStore builds a store seeded with the given Secrets.
// SEM@<sha>: build a MemorySecretStore seeded with the given Secrets (pure)
func NewMemorySecretStore(seed ...*Secret) *MemorySecretStore {
	st := &MemorySecretStore{secrets: map[string]*Secret{}}
	for _, s := range seed {
		c := s.Clone()
		c.ResourceVersion = "0"
		st.secrets[c.Name] = c
	}
	return st
}

// Get returns a copy of the named Secret.
// SEM@<sha>: return a copy of the named Secret or a not-found error
func (m *MemorySecretStore) Get(_ context.Context, name string) (*Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.secrets[name]
	if !ok {
		return nil, fmt.Errorf("secret %q not found", name)
	}
	return s.Clone(), nil
}

// Update stores s if its resourceVersion is current.
// SEM@<sha>: store the Secret if its resourceVersion is current, else ErrConflict
func (m *MemorySecretStore) Update(_ context.Context, s *Secret) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.secrets[s.Name]
	if !ok {
		return fmt.Errorf("secret %q not found", s.Name)
	}
	if cur.ResourceVersion != s.ResourceVersion {
		return ErrConflict
	}
	if !equalMaps(cur.Data, s.Data) {
		m.DataWrites++
	}
	m.version++
	c := s.Clone()
	c.ResourceVersion = strconv.Itoa(m.version)
	m.secrets[s.Name] = c
	return nil
}

// SEM@<sha>: compare two string maps for equality (pure)
func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// FakeRolloutWaiter reports a generation equal to the store's data-write count,
// so every data write "rolls" the server immediately.
// SEM@<sha>: RolloutWaiter fake whose generation tracks MemorySecretStore data writes
type FakeRolloutWaiter struct {
	store      *MemorySecretStore
	deployment string
	Waits      int
}

// NewFakeRolloutWaiter binds a fake waiter to a MemorySecretStore.
// SEM@<sha>: build a FakeRolloutWaiter bound to a MemorySecretStore (pure)
func NewFakeRolloutWaiter(store *MemorySecretStore, deployment string) *FakeRolloutWaiter {
	return &FakeRolloutWaiter{store: store, deployment: deployment}
}

// Generation reports the fake Deployment generation.
// SEM@<sha>: report the fake Deployment generation (data writes so far)
func (f *FakeRolloutWaiter) Generation(_ context.Context, deployment string) (int64, error) {
	if deployment != f.deployment {
		return 0, fmt.Errorf("unknown deployment %q", deployment)
	}
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	return int64(f.store.DataWrites), nil
}

// WaitRolled succeeds when a data write happened after since.
// SEM@<sha>: succeed when a data write happened after since, else time out
func (f *FakeRolloutWaiter) WaitRolled(ctx context.Context, deployment string, since int64, _ time.Duration) error {
	f.Waits++
	gen, err := f.Generation(ctx, deployment)
	if err != nil {
		return err
	}
	if gen <= since {
		return fmt.Errorf("rollout of %s did not start (generation %d <= %d)", deployment, gen, since)
	}
	return nil
}
