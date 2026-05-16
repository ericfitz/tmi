package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestReconcile_CreatesChildObjects(t *testing.T) {
	// Locate the control-plane binaries fetched by setup-envtest.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		assets, _ := filepath.Abs("../../../bin/k8s")
		entries, err := os.ReadDir(assets)
		if err != nil || len(entries) == 0 {
			t.Skip("envtest assets not found; run: setup-envtest use 1.30.0 --bin-dir ./bin/k8s")
		}
		if err := os.Setenv("KUBEBUILDER_ASSETS", filepath.Join(assets, entries[0].Name())); err != nil {
			t.Fatalf("set KUBEBUILDER_ASSETS: %v", err)
		}
	}

	crdPath, _ := filepath.Abs("../../../config/crd/bases")
	kedaCRDPath, _ := filepath.Abs("../../../config/crd/external")
	env := &envtest.Environment{CRDDirectoryPaths: []string{crdPath, kedaCRDPath}}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() { _ = env.Stop() }()

	if err := platformv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}
	k8s, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx := context.Background()
	comp := &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-extractor", Namespace: "default"},
		Spec: platformv1alpha1.TMIComponentSpec{
			Image:       "tmi/extractor:dev",
			JobSubjects: []string{"jobs.extract.ooxml"},
			InputMode:   platformv1alpha1.InputContentRef,
			Egress:      platformv1alpha1.EgressNone,
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
			Scaling: platformv1alpha1.ScalingSpec{MinReplicas: 0, MaxReplicas: 10, QueueDepthTarget: 5},
		},
	}
	if err := k8s.Create(ctx, comp); err != nil {
		t.Fatalf("create TMIComponent: %v", err)
	}

	r := &TMIComponentReconciler{Client: k8s, Scheme: scheme.Scheme}
	key := types.NamespacedName{Name: "tmi-extractor", Namespace: "default"}
	if _, err := r.ReconcileComponent(ctx, key); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var dep appsv1.Deployment
	if err := waitGet(ctx, k8s, key, &dep); err != nil {
		t.Fatalf("expected child Deployment: %v", err)
	}
	var np networkingv1.NetworkPolicy
	if err := waitGet(ctx, k8s, key, &np); err != nil {
		t.Fatalf("expected child NetworkPolicy: %v", err)
	}

	// The child ScaledObject must exist (KEDA CRD, read as unstructured).
	so := &unstructured.Unstructured{}
	so.SetAPIVersion("keda.sh/v1alpha1")
	so.SetKind("ScaledObject")
	if err := waitGet(ctx, k8s, key, so); err != nil {
		t.Fatalf("expected child ScaledObject: %v", err)
	}

	// Reconcile a second time: the reconciler MUST be idempotent. This is
	// the regression guard for the Create-then-Update resourceVersion bug —
	// the second pass exercises the AlreadyExists -> Update path.
	if _, err := r.ReconcileComponent(ctx, key); err != nil {
		t.Fatalf("second reconcile must succeed (idempotency): %v", err)
	}
	// Children must still exist after the second reconcile.
	if err := waitGet(ctx, k8s, key, &appsv1.Deployment{}); err != nil {
		t.Fatalf("Deployment missing after second reconcile: %v", err)
	}
}

func waitGet(ctx context.Context, c client.Client, key types.NamespacedName, obj client.Object) error {
	var lastErr error
	for i := 0; i < 20; i++ {
		if lastErr = c.Get(ctx, key, obj); lastErr == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}
