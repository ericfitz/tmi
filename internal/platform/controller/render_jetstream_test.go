package controller

import (
	"testing"
	"time"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/nats-io/nats.go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func jsComp(name string, subjects ...string) *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tmi-platform"},
		Spec:       platformv1alpha1.TMIComponentSpec{JobSubjects: subjects},
	}
}

func TestStreamNameFor_IsDeterministicAndUppercase(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml")
	// JetStream stream names cannot contain dots; must be stable across reconciles.
	got := streamNameFor(c)
	if got != streamNameFor(c) {
		t.Fatal("streamNameFor must be deterministic")
	}
	if got == "" {
		t.Fatal("streamNameFor must not be empty")
	}
	for _, r := range got {
		if r == '.' || r == ' ' {
			t.Fatalf("stream name %q contains an illegal character", got)
		}
	}
}

func TestConsumerNameFor_IsDeterministic(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml")
	got := consumerNameFor(c)
	if got != consumerNameFor(c) {
		t.Fatal("consumerNameFor must be deterministic")
	}
	if got == "" {
		t.Fatal("consumerNameFor must not be empty")
	}
}

func TestStreamConfigFor_BindsAllJobSubjects(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml", "jobs.extract.pdf")
	cfg := StreamConfigFor(c)
	if len(cfg.Subjects) != 2 {
		t.Fatalf("stream config must bind all job subjects, got %d", len(cfg.Subjects))
	}
}

func TestStreamConfigFor_UsesWorkQueueAndFileStorage(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml")
	cfg := StreamConfigFor(c)
	if cfg.Retention != nats.WorkQueuePolicy {
		t.Fatal("stream must use WorkQueuePolicy retention")
	}
	if cfg.Storage != nats.FileStorage {
		t.Fatal("stream must use FileStorage")
	}
}

// scaledObjectConsumerName digs the KEDA trigger's "consumer" metadata out of
// the unstructured ScaledObject so the test below can compare it against the
// controller- and worker-side names.
func scaledObjectConsumerName(t *testing.T, c *platformv1alpha1.TMIComponent) string {
	t.Helper()
	so := RenderScaledObject(c)
	spec, ok := so.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("ScaledObject spec missing")
	}
	triggers, ok := spec["triggers"].([]interface{})
	if !ok || len(triggers) == 0 {
		t.Fatal("ScaledObject triggers missing")
	}
	trig, ok := triggers[0].(map[string]interface{})
	if !ok {
		t.Fatal("ScaledObject trigger[0] malformed")
	}
	meta, ok := trig["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("ScaledObject trigger metadata missing")
	}
	name, _ := meta["consumer"].(string)
	return name
}

// TestConsumerName_AgreesAcrossKEDAControllerAndWorker is the regression guard
// for #444. The bug was a three-way name disagreement: the KEDA ScaledObject
// watched "TMI_EXTRACTOR_CONSUMER", while the worker created a durable named
// "tmi-extractor", so KEDA could never observe queue depth and never scaled the
// worker from zero. The controller (ConsumerConfigFor), the KEDA ScaledObject
// (RenderScaledObject), and the worker (worker.ConsumerNameFor, used by
// cmd/extractor and cmd/chunkembed) MUST all derive the SAME consumer name.
func TestConsumerName_AgreesAcrossKEDAControllerAndWorker(t *testing.T) {
	for _, name := range []string{"tmi-extractor", "tmi-chunk-embed"} {
		c := jsComp(name, "jobs.extract.plaintext")

		keda := scaledObjectConsumerName(t, c)
		provisioned := ConsumerConfigFor(c).Durable
		workerSide := worker.ConsumerNameFor(name)

		if keda == "" {
			t.Fatalf("%s: KEDA ScaledObject has no consumer name", name)
		}
		if keda != provisioned {
			t.Errorf("%s: KEDA consumer %q != controller-provisioned consumer %q", name, keda, provisioned)
		}
		if keda != workerSide {
			t.Errorf("%s: KEDA consumer %q != worker.ConsumerNameFor %q", name, keda, workerSide)
		}
	}
}

func TestConsumerConfigFor_DefaultsAndAckWaitOverride(t *testing.T) {
	// No TMI_JOB_ACK_WAIT -> default.
	c := jsComp("tmi-extractor", "jobs.extract.plaintext")
	cfg := ConsumerConfigFor(c)
	if cfg.AckWait != defaultConsumerAckWait {
		t.Errorf("default AckWait = %v, want %v", cfg.AckWait, defaultConsumerAckWait)
	}
	if cfg.AckPolicy != nats.AckExplicitPolicy {
		t.Error("consumer must use AckExplicitPolicy so JetStream redelivers on worker death")
	}
	if cfg.MaxDeliver != defaultConsumerMaxDeliver {
		t.Errorf("MaxDeliver = %d, want %d", cfg.MaxDeliver, defaultConsumerMaxDeliver)
	}

	// spec.config TMI_JOB_ACK_WAIT overrides the default.
	c.Spec.Config = map[string]string{"TMI_JOB_ACK_WAIT": "120s"}
	if got := ConsumerConfigFor(c).AckWait; got != 120*time.Second {
		t.Errorf("override AckWait = %v, want 120s", got)
	}

	// A malformed value falls back to the default rather than erroring.
	c.Spec.Config = map[string]string{"TMI_JOB_ACK_WAIT": "not-a-duration"}
	if got := ConsumerConfigFor(c).AckWait; got != defaultConsumerAckWait {
		t.Errorf("malformed AckWait = %v, want default %v", got, defaultConsumerAckWait)
	}
}
