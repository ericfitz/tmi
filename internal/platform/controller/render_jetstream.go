package controller

import (
	"strings"
	"time"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/nats-io/nats.go"
)

const (
	// defaultConsumerAckWait is the durable consumer's redelivery window when a
	// component does not set TMI_JOB_ACK_WAIT in its spec.config. It is also the
	// JetStream-side backstop for a worker that dies mid-job.
	defaultConsumerAckWait = 90 * time.Second

	// defaultConsumerMaxDeliver caps redeliveries before JetStream dead-letters
	// a message. It mirrors the value the worker binaries pass to RunConsumer.
	defaultConsumerMaxDeliver = 3
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

// ConsumerConfigFor returns the durable consumer the controller pre-creates on
// a component's stream. The name MUST equal consumerNameFor(c) because that is
// exactly the consumer the KEDA ScaledObject (render_scaledobject.go) watches
// for queue depth — KEDA cannot scale a worker from zero unless this consumer
// already exists under that name. The worker (cmd/extractor, cmd/chunkembed)
// binds this same durable rather than creating its own.
//
// The consumer carries NO FilterSubject: each per-component stream is
// single-purpose, so the durable consumes every subject the stream binds. This
// keeps the config identical regardless of how many jobSubjects a component
// declares and avoids a filter-subject conflict when the worker binds it.
func ConsumerConfigFor(c *platformv1alpha1.TMIComponent) *nats.ConsumerConfig {
	ackWait := defaultConsumerAckWait
	if v, ok := c.Spec.Config["TMI_JOB_ACK_WAIT"]; ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ackWait = d
		}
	}
	return &nats.ConsumerConfig{
		Durable:    consumerNameFor(c),
		AckPolicy:  nats.AckExplicitPolicy,
		AckWait:    ackWait,
		MaxDeliver: defaultConsumerMaxDeliver,
	}
}
