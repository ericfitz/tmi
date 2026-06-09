package controller

import (
	"strings"
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func unstructuredNestedString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	return unstructured.NestedString(obj, fields...)
}

func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	return unstructured.NestedSlice(obj, fields...)
}

func unstructuredNestedInt64(obj map[string]interface{}, fields ...string) (int64, bool, error) {
	return unstructured.NestedInt64(obj, fields...)
}

func scaledComp() *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-extractor", Namespace: "tmi-platform"},
		Spec: platformv1alpha1.TMIComponentSpec{
			JobSubjects: []string{"jobs.extract.ooxml"},
			Scaling: platformv1alpha1.ScalingSpec{
				MinReplicas: 0, MaxReplicas: 10, QueueDepthTarget: 5,
			},
		},
	}
}

func TestRenderScaledObject_TargetsTheDeployment(t *testing.T) {
	so := RenderScaledObject(scaledComp())
	if so.GetName() != "tmi-extractor" || so.GetNamespace() != "tmi-platform" {
		t.Fatalf("unexpected name/namespace: %s/%s", so.GetNamespace(), so.GetName())
	}
	if so.GetAPIVersion() != "keda.sh/v1alpha1" || so.GetKind() != "ScaledObject" {
		t.Fatalf("unexpected GVK: %s %s", so.GetAPIVersion(), so.GetKind())
	}
	ref, _, _ := unstructuredNestedString(so.Object, "spec", "scaleTargetRef", "name")
	if ref != "tmi-extractor" {
		t.Fatalf("scaleTargetRef.name = %q, want tmi-extractor", ref)
	}
}

func TestRenderScaledObject_UsesNatsJetStreamTrigger(t *testing.T) {
	so := RenderScaledObject(scaledComp())
	triggers, found, _ := unstructuredNestedSlice(so.Object, "spec", "triggers")
	if !found || len(triggers) != 1 {
		t.Fatalf("expected exactly one trigger, found=%v len=%d", found, len(triggers))
	}
	trig := triggers[0].(map[string]interface{})
	if trig["type"] != "nats-jetstream" {
		t.Fatalf("trigger type = %v, want nats-jetstream", trig["type"])
	}
}

func TestRenderScaledObject_TriggerMetadataValues(t *testing.T) {
	so := RenderScaledObject(scaledComp())
	minReplicas, _, _ := unstructuredNestedInt64(so.Object, "spec", "minReplicaCount")
	maxReplicas, _, _ := unstructuredNestedInt64(so.Object, "spec", "maxReplicaCount")
	if minReplicas != 0 || maxReplicas != 10 {
		t.Fatalf("replica counts = %d/%d, want 0/10", minReplicas, maxReplicas)
	}
	triggers, _, _ := unstructuredNestedSlice(so.Object, "spec", "triggers")
	meta := triggers[0].(map[string]interface{})["metadata"].(map[string]interface{})
	if meta["lagThreshold"] != "5" {
		t.Fatalf("lagThreshold = %v, want \"5\"", meta["lagThreshold"])
	}
	if meta["stream"] != "TMI_TMI_EXTRACTOR" {
		t.Fatalf("stream = %v, want TMI_TMI_EXTRACTOR", meta["stream"])
	}
	// The monitoring endpoint must be a bare host:port. KEDA's nats-jetstream
	// scaler prepends the scheme itself; a scheme here yields a doubled
	// "http://http://..." URL that never resolves, leaving the queue-depth
	// metric unavailable and autoscaling silently broken.
	endpoint, _ := meta["natsServerMonitoringEndpoint"].(string)
	if strings.Contains(endpoint, "://") {
		t.Fatalf("natsServerMonitoringEndpoint = %q, must not include a scheme (KEDA adds it)", endpoint)
	}
	if endpoint != "nats.tmi-platform.svc:8222" {
		t.Fatalf("natsServerMonitoringEndpoint = %q, want nats.tmi-platform.svc:8222", endpoint)
	}
}
