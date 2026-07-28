package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// These tests pin the two properties that keep the reconciler from hot-looping
// against the API server. Both were violated in production: on the AWS cluster
// the tmi-chunk-embed and tmi-extractor Deployments reached
// metadata.generation ~2.45 million in six days — a measured ~4.3 spec writes
// per second, each one re-triggering the Owns(&appsv1.Deployment{}) watch and
// so the next reconcile.
//
// Unit-level with a fake client on purpose: the sibling envtest suite skips
// entirely unless control-plane binaries were fetched, and a regression guard
// that silently skips is not a guard.

func int32Ptr(i int32) *int32 { return &i }

// baseDeployment is a minimal stand-in for RenderDeployment's output: note it
// deliberately leaves Spec.Replicas nil, exactly as RenderDeployment does.
func baseDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-chunk-embed", Namespace: "tmi-platform"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"tmi.dev/component": "tmi-chunk-embed"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"tmi.dev/component": "tmi-chunk-embed"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "worker", Image: "tmi-chunk-embed:v1"}}},
			},
		},
	}
}

// The autoscaler owns spec.replicas. RenderDeployment never sets it, so a
// blind Update sends nil and the API server defaults it back to 1 — undoing a
// KEDA scale-to-zero on every reconcile. apply() must carry the live value
// forward instead.
func TestApply_PreservesLiveReplicas(t *testing.T) {
	live := baseDeployment()
	live.Spec.Replicas = int32Ptr(0) // KEDA scaled this to zero (no queue depth)

	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(live).Build()
	r := &TMIComponentReconciler{Client: cl, Scheme: scheme.Scheme}

	rendered := baseDeployment() // Replicas nil, as rendered
	if err := r.apply(context.Background(), rendered); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var got appsv1.Deployment
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(live), &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.Replicas == nil {
		t.Fatal("spec.replicas is nil after apply; the live value was dropped")
	}
	if *got.Spec.Replicas != 0 {
		t.Fatalf("spec.replicas = %d, want 0 — apply() clobbered the autoscaler's value", *got.Spec.Replicas)
	}
}

// Even with replicas preserved, an unconditional Update rewrites the object
// and re-fires the watch. A reconcile that changes nothing must not write.
func TestApply_SkipsWriteWhenUnchanged(t *testing.T) {
	live := baseDeployment()
	live.Spec.Replicas = int32Ptr(2)

	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(live).Build()
	r := &TMIComponentReconciler{Client: cl, Scheme: scheme.Scheme}

	var before appsv1.Deployment
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(live), &before); err != nil {
		t.Fatalf("get before: %v", err)
	}

	// Reconcile twice with identical rendered output.
	for i := 0; i < 2; i++ {
		if err := r.apply(context.Background(), baseDeployment()); err != nil {
			t.Fatalf("apply %d: %v", i, err)
		}
	}

	var after appsv1.Deployment
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(live), &after); err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.ResourceVersion != before.ResourceVersion {
		t.Fatalf("resourceVersion changed %s -> %s: apply() wrote despite no change, which re-fires the watch and hot-loops",
			before.ResourceVersion, after.ResourceVersion)
	}
}

// A genuine change must still be written, or the controller stops converging.
func TestApply_WritesWhenChanged(t *testing.T) {
	live := baseDeployment()
	live.Spec.Replicas = int32Ptr(1)

	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(live).Build()
	r := &TMIComponentReconciler{Client: cl, Scheme: scheme.Scheme}

	rendered := baseDeployment()
	rendered.Spec.Template.Spec.Containers[0].Image = "tmi-chunk-embed:v2"
	if err := r.apply(context.Background(), rendered); err != nil {
		t.Fatalf("apply: %v", err)
	}

	var got appsv1.Deployment
	if err := cl.Get(context.Background(), client.ObjectKeyFromObject(live), &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if img := got.Spec.Template.Spec.Containers[0].Image; img != "tmi-chunk-embed:v2" {
		t.Fatalf("image = %q, want the updated tag — a real change was not applied", img)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 1 {
		t.Fatal("spec.replicas should still be the live value after a legitimate update")
	}
}

// apply() is generic over client.Object; non-Deployment children have no
// autoscaler and must keep working unchanged.
func TestApply_CreatesWhenAbsent(t *testing.T) {
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
	r := &TMIComponentReconciler{Client: cl, Scheme: scheme.Scheme}

	if err := r.apply(context.Background(), baseDeployment()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	var got appsv1.Deployment
	if err := cl.Get(context.Background(), client.ObjectKey{Name: "tmi-chunk-embed", Namespace: "tmi-platform"}, &got); err != nil {
		t.Fatalf("object was not created: %v", err)
	}
}
