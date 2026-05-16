package controller

import (
	"strings"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/nats-io/nats.go"
)

// streamNameFor returns the JetStream stream name for a component.
// JetStream stream names cannot contain dots or spaces, so the component
// name is upcased and sanitized. Deterministic across reconciles.
func streamNameFor(c *platformv1alpha1.TMIComponent) string {
	return "TMI_" + sanitizeName(c.Name)
}

// consumerNameFor returns the durable JetStream consumer name for a component.
func consumerNameFor(c *platformv1alpha1.TMIComponent) string {
	return sanitizeName(c.Name) + "_CONSUMER"
}

// sanitizeName upcases s and replaces JetStream-illegal characters (".", "-",
// " ") with "_". NOTE: this is not injective — names differing only in those
// characters (e.g. "tmi-extractor" vs "tmi.extractor") collide. In practice
// each component is a distinct TMIComponent CR with a unique K8s name, so a
// collision would require two deliberately near-identical names.
func sanitizeName(s string) string {
	up := strings.ToUpper(s)
	return strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(up)
}

// StreamConfigFor returns the JetStream stream configuration that binds all
// of a component's job subjects.
func StreamConfigFor(c *platformv1alpha1.TMIComponent) *nats.StreamConfig {
	return &nats.StreamConfig{
		Name:      streamNameFor(c),
		Subjects:  append([]string(nil), c.Spec.JobSubjects...),
		Retention: nats.WorkQueuePolicy, // each job delivered to exactly one worker
		Storage:   nats.FileStorage,
	}
}
