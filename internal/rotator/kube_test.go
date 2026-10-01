package rotator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// SEM@3b682947: test that the Kubernetes secret store round-trips decoded data
func TestKubeSecretStore_RoundTripDecodesData(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-secrets", Namespace: "tmi-platform", ResourceVersion: "1",
			Annotations: map[string]string{"tmi.dev/rotated-at.x": "2026-01-01T00:00:00Z"}},
		Data: map[string][]byte{"A": []byte("one")},
	})
	st := NewKubeSecretStore(cs, "tmi-platform")
	s, err := st.Get(context.Background(), "tmi-secrets")
	require.NoError(t, err)
	require.Equal(t, "one", s.Data["A"])
	require.Equal(t, "2026-01-01T00:00:00Z", s.Annotations["tmi.dev/rotated-at.x"])
	s.Data["B"] = "two"
	delete(s.Data, "A")
	require.NoError(t, st.Update(context.Background(), s))
	got, _ := cs.CoreV1().Secrets("tmi-platform").Get(context.Background(), "tmi-secrets", metav1.GetOptions{})
	require.Equal(t, []byte("two"), got.Data["B"])
	_, hasA := got.Data["A"]
	require.False(t, hasA)
}

// SEM@3b682947: test that Kubernetes update conflicts map to the conflict error
func TestKubeSecretStore_ConflictMapsToErrConflict(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tmi-secrets", Namespace: "tmi-platform", ResourceVersion: "1"}})
	cs.PrependReactor("update", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "secrets"}, "tmi-secrets", errors.New("stale"))
	})
	st := NewKubeSecretStore(cs, "tmi-platform")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	err := st.Update(context.Background(), s)
	require.ErrorIs(t, err, ErrConflict)
}

// SEM@3b682947: test that a stale resource version yields the conflict error
func TestKubeSecretStore_StaleResourceVersionIsErrConflict(t *testing.T) {
	cs := fake.NewClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tmi-secrets", Namespace: "tmi-platform", ResourceVersion: "2"}})
	st := NewKubeSecretStore(cs, "tmi-platform")
	err := st.Update(context.Background(), &Secret{Name: "tmi-secrets", ResourceVersion: "1"})
	require.ErrorIs(t, err, ErrConflict)
}

// SEM@3b682947: test that the rollout waiter waits for generation then availability
func TestKubeRolloutWaiter_WaitsForGenerationThenAvailability(t *testing.T) {
	one := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-server", Namespace: "tmi-platform", Generation: 3},
		Spec:       appsv1.DeploymentSpec{Replicas: &one},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1},
	}
	cs := fake.NewClientset(dep)
	w := NewKubeRolloutWaiter(cs, "tmi-platform")
	w.Poll = 5 * time.Millisecond
	gen, err := w.Generation(context.Background(), "tmi-server")
	require.NoError(t, err)
	require.EqualValues(t, 3, gen)

	// Generation has not moved: times out.
	err = w.WaitRolled(context.Background(), "tmi-server", 3, 30*time.Millisecond)
	require.Error(t, err)

	// Reloader bumped the template (generation 4); status catches up after a moment.
	dep.Generation = 4
	dep.Status.ObservedGeneration = 3
	dep.Status.AvailableReplicas = 0
	_, _ = cs.AppsV1().Deployments("tmi-platform").Update(context.Background(), dep, metav1.UpdateOptions{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		upd := dep.DeepCopy()
		upd.Status.ObservedGeneration = 4
		upd.Status.AvailableReplicas = 1
		upd.Status.UpdatedReplicas = 1
		_, _ = cs.AppsV1().Deployments("tmi-platform").UpdateStatus(context.Background(), upd, metav1.UpdateOptions{})
	}()
	require.NoError(t, w.WaitRolled(context.Background(), "tmi-server", 3, 2*time.Second))
}
