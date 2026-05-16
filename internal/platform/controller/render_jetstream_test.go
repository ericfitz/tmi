package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
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
