package rotator

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// KubeSecretStore is the SecretStore for a real cluster.
// SEM@<sha>: SecretStore over the Kubernetes API with resourceVersion-checked updates
type KubeSecretStore struct {
	cs        kubernetes.Interface
	namespace string
}

// NewKubeSecretStore builds a KubeSecretStore for one namespace.
// SEM@<sha>: build a KubeSecretStore for one namespace (pure)
func NewKubeSecretStore(cs kubernetes.Interface, namespace string) *KubeSecretStore {
	return &KubeSecretStore{cs: cs, namespace: namespace}
}

// Get fetches a Secret and decodes its data into strings.
// SEM@<sha>: fetch a Secret and decode its data into strings
func (k *KubeSecretStore) Get(ctx context.Context, name string) (*Secret, error) {
	obj, err := k.cs.CoreV1().Secrets(k.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get secret %s/%s: %w", k.namespace, name, err)
	}
	s := &Secret{Name: name, ResourceVersion: obj.ResourceVersion, Data: map[string]string{}, Annotations: map[string]string{}}
	for key, v := range obj.Data {
		s.Data[key] = string(v)
	}
	for key, v := range obj.Annotations {
		s.Annotations[key] = v
	}
	return s, nil
}

// Update writes data and annotations back; a stale resourceVersion or a 409 is ErrConflict.
// SEM@<sha>: write a Secret's data and annotations back, mapping a 409 to ErrConflict
func (k *KubeSecretStore) Update(ctx context.Context, s *Secret) error {
	obj, err := k.cs.CoreV1().Secrets(k.namespace).Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get secret %s/%s: %w", k.namespace, s.Name, err)
	}
	if obj.ResourceVersion != s.ResourceVersion {
		return fmt.Errorf("secret %s/%s: %w", k.namespace, s.Name, ErrConflict)
	}
	obj.Data = map[string][]byte{}
	for key, v := range s.Data {
		obj.Data[key] = []byte(v)
	}
	obj.StringData = nil
	obj.Annotations = map[string]string{}
	for key, v := range s.Annotations {
		obj.Annotations[key] = v
	}
	if _, err := k.cs.CoreV1().Secrets(k.namespace).Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("secret %s/%s: %w", k.namespace, s.Name, ErrConflict)
		}
		return fmt.Errorf("update secret %s/%s: %w", k.namespace, s.Name, err)
	}
	return nil
}

// KubeRolloutWaiter polls a Deployment until Reloader's rollout completes.
// SEM@<sha>: RolloutWaiter that polls Deployment generation and replica status
type KubeRolloutWaiter struct {
	cs        kubernetes.Interface
	namespace string
	Poll      time.Duration
}

// NewKubeRolloutWaiter builds a waiter polling every 5s.
// SEM@<sha>: build a KubeRolloutWaiter polling every 5s (pure)
func NewKubeRolloutWaiter(cs kubernetes.Interface, namespace string) *KubeRolloutWaiter {
	return &KubeRolloutWaiter{cs: cs, namespace: namespace, Poll: 5 * time.Second}
}

// Generation reads a Deployment's metadata.generation.
// SEM@<sha>: read a Deployment's metadata.generation
func (w *KubeRolloutWaiter) Generation(ctx context.Context, deployment string) (int64, error) {
	d, err := w.cs.AppsV1().Deployments(w.namespace).Get(ctx, deployment, metav1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("get deployment %s/%s: %w", w.namespace, deployment, err)
	}
	return d.Generation, nil
}

// WaitRolled first waits for the generation to pass since (Reloader has patched
// the pod template), then for the rollout to complete. A Recreate Deployment
// dips to zero available replicas in between; that is progress, not failure.
// SEM@<sha>: block until a Deployment rolled past a generation and is fully available, or time out
func (w *KubeRolloutWaiter) WaitRolled(ctx context.Context, deployment string, since int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		d, err := w.cs.AppsV1().Deployments(w.namespace).Get(ctx, deployment, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get deployment %s/%s: %w", w.namespace, deployment, err)
		}
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		rolled := d.Generation > since &&
			d.Status.ObservedGeneration >= d.Generation &&
			d.Status.UpdatedReplicas == want &&
			d.Status.AvailableReplicas == want &&
			d.Status.Replicas == want
		if rolled {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("deployment %s did not finish rolling within %s (generation %d, since %d, available %d/%d)",
				deployment, timeout, d.Generation, since, d.Status.AvailableReplicas, want)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(w.Poll):
		}
	}
}
