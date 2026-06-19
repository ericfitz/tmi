package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/nats-io/nats.go"
)

// StreamProvisioner ensures the JetStream stream and durable consumer a
// component needs exist before any worker pod runs. It is the missing half of
// reconciliation: rendering the KEDA ScaledObject is pointless unless the
// stream and consumer it watches already exist, because KEDA scales a worker
// from zero by reading that consumer's pending-message depth.
//
// The reconciler holds this as an interface so envtest unit tests (which have
// no NATS) can leave it nil and skip provisioning.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: interface for idempotently ensuring a JetStream stream and consumer exist for a component (pure)
type StreamProvisioner interface {
	EnsureStreamAndConsumer(ctx context.Context, c *platformv1alpha1.TMIComponent) error
}

// NATSProvisioner is the live StreamProvisioner backed by a NATS JetStream
// connection. In the current out-of-cluster e2e flow the controller reaches
// NATS through the host port-forward (nats://127.0.0.1:4222); once the
// controller ships as an in-cluster Deployment it will use the in-cluster
// service DNS (nats://nats.tmi-platform.svc:4222). Either way the URL is
// supplied via the TMI_NATS_URL env var.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: live StreamProvisioner backed by a NATS JetStream connection (pure)
type NATSProvisioner struct {
	nc *nats.Conn
	js nats.JetStreamContext
}

// NewNATSProvisioner dials NATS and opens a JetStream context. It uses
// RetryOnFailedConnect so the controller can start before the port-forward (or
// the NATS pod) is reachable: JetStream calls then fail until the connection
// establishes, which surfaces as a reconcile error and a requeue.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: connect to NATS with retry and return a provisioner with a JetStream context
func NewNATSProvisioner(url string) (*NATSProvisioner, error) {
	nc, err := nats.Connect(url,
		nats.Name("tmi-component-controller"),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("controller: nats connect %s: %w", url, err)
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("controller: jetstream context: %w", err)
	}
	return &NATSProvisioner{nc: nc, js: js}, nil
}

// Close closes the underlying NATS connection.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: close the underlying NATS connection (mutates shared state)
func (p *NATSProvisioner) Close() {
	if p.nc != nil {
		p.nc.Close()
	}
}

// EnsureStreamAndConsumer creates the component's stream and durable consumer
// if they do not already exist. It is idempotent: an existing stream/consumer
// is left untouched (the config is stable across reconciles), so it is safe to
// call on every reconcile. Creation is gated on a NotFound lookup rather than
// blind AddStream/AddConsumer so a transient connectivity error is returned as
// an error (triggering requeue) instead of being mistaken for "already exists".
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: idempotently create a JetStream stream and durable consumer for a component if absent
func (p *NATSProvisioner) EnsureStreamAndConsumer(ctx context.Context, c *platformv1alpha1.TMIComponent) error {
	streamCfg := StreamConfigFor(c)
	if _, err := p.js.StreamInfo(streamCfg.Name, nats.Context(ctx)); err != nil {
		if !errors.Is(err, nats.ErrStreamNotFound) {
			return fmt.Errorf("controller: stream info %s: %w", streamCfg.Name, err)
		}
		if _, err := p.js.AddStream(streamCfg, nats.Context(ctx)); err != nil {
			return fmt.Errorf("controller: add stream %s: %w", streamCfg.Name, err)
		}
	}

	consCfg := ConsumerConfigFor(c)
	if _, err := p.js.ConsumerInfo(streamCfg.Name, consCfg.Durable, nats.Context(ctx)); err != nil {
		if !errors.Is(err, nats.ErrConsumerNotFound) {
			return fmt.Errorf("controller: consumer info %s/%s: %w", streamCfg.Name, consCfg.Durable, err)
		}
		if _, err := p.js.AddConsumer(streamCfg.Name, consCfg, nats.Context(ctx)); err != nil {
			return fmt.Errorf("controller: add consumer %s/%s: %w", streamCfg.Name, consCfg.Durable, err)
		}
	}
	return nil
}
