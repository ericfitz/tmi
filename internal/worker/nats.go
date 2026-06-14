package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config is the worker's NATS bootstrap configuration, read from env vars.
type Config struct {
	// NATSURL is the NATS server URL (env TMI_NATS_URL).
	NATSURL string
	// ComponentName is this worker's TMIComponent name (env TMI_COMPONENT_NAME).
	ComponentName string
}

// ConfigFromEnv builds a Config from the standard worker env vars.
func ConfigFromEnv() (Config, error) {
	url, err := MustEnv("TMI_NATS_URL")
	if err != nil {
		return Config{}, err
	}
	name, err := MustEnv("TMI_COMPONENT_NAME")
	if err != nil {
		return Config{}, err
	}
	return Config{NATSURL: url, ComponentName: name}, nil
}

// Conn bundles a NATS connection, a JetStream context, and the payload
// Object Store handle. It is the worker's single handle to the bus.
type Conn struct {
	nc   *nats.Conn
	js   jetstream.JetStream
	objs jetstream.ObjectStore
	cfg  Config
}

// Connect dials NATS, opens a JetStream context, and ensures the payload
// Object Store bucket exists.
//
// If TMI_NATS_CREDS is set in the environment, it is used as the path to a
// NATS credentials file, giving each component its own bus identity once the
// server enables authorization. Unset preserves the credential-less default.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	opts := []nats.Option{
		nats.Name("tmi-" + cfg.ComponentName),
		nats.MaxReconnects(-1),
	}
	if creds := os.Getenv("TMI_NATS_CREDS"); creds != "" {
		opts = append(opts, nats.UserCredentials(creds))
	}
	nc, err := nats.Connect(cfg.NATSURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("worker: nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("worker: jetstream context: %w", err)
	}
	objs, err := js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket: PayloadBucket,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("worker: object store: %w", err)
	}
	return &Conn{nc: nc, js: js, objs: objs, cfg: cfg}, nil
}

// JetStream returns the JetStream context for consumer/publish wiring.
func (c *Conn) JetStream() jetstream.JetStream { return c.js }

// Config returns the connection's config.
func (c *Conn) Config() Config { return c.cfg }

// Close closes the NATS connection. In-flight messages are not drained.
func (c *Conn) Close() { c.nc.Close() }

// PutPayload writes bytes to the Object Store under the given name and
// returns the object_ref to carry in an envelope.
func (c *Conn) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	if _, err := c.objs.PutBytes(ctx, name, data); err != nil {
		return "", fmt.Errorf("worker: put payload %s: %w", name, err)
	}
	return PayloadBucket + "/" + name, nil
}

// GetPayload reads a blob by the object_ref produced by PutPayload.
func (c *Conn) GetPayload(ctx context.Context, ref string) ([]byte, error) {
	name, ok := payloadName(ref)
	if !ok {
		return nil, fmt.Errorf("worker: malformed object_ref %q", ref)
	}
	data, err := c.objs.GetBytes(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("worker: get payload %s: %w", name, err)
	}
	return data, nil
}

// DeletePayload removes a blob by the object_ref produced by PutPayload.
// It is idempotent from the caller's perspective: deleting an absent blob
// is treated as success so result-consumer cleanup never blocks on a
// double-delivery.
func (c *Conn) DeletePayload(ctx context.Context, ref string) error {
	name, ok := payloadName(ref)
	if !ok {
		return fmt.Errorf("worker: malformed object_ref %q", ref)
	}
	if err := c.objs.Delete(ctx, name); err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			return nil
		}
		return fmt.Errorf("worker: delete payload %s: %w", name, err)
	}
	return nil
}

// payloadName strips the "<bucket>/" prefix from an object_ref.
func payloadName(ref string) (string, bool) {
	prefix := PayloadBucket + "/"
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		return "", false
	}
	return ref[len(prefix):], true
}

// PayloadRefForJob reports whether an object_ref names a blob that belongs to
// the given job. Blob names embed the job ID in one of the shipped patterns:
// the publisher's input blob ("job-<id>-source") or a stage-output blob whose
// leading path segment is the job ID ("<id>/extracted", "<id>/result").
// Envelope-supplied refs are worker-controlled, so consumers MUST check this
// before acting on a ref — honoring an arbitrary ref would let a forged
// envelope delete another job's blobs.
func PayloadRefForJob(ref, jobID string) bool {
	if jobID == "" {
		return false
	}
	name, ok := payloadName(ref)
	if !ok {
		return false
	}
	return name == "job-"+jobID+"-source" || strings.HasPrefix(name, jobID+"/")
}

// Publish publishes a pre-marshaled message to a JetStream subject (durable,
// stream-backed). For ephemeral signals such as heartbeats use PublishCore.
func (c *Conn) Publish(ctx context.Context, subject string, data []byte) error {
	if _, err := c.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("worker: publish %s: %w", subject, err)
	}
	return nil
}

// PublishCore publishes a message over core NATS (fire-and-forget, no
// JetStream stream or persistence). Use this for ephemeral signals such as
// heartbeats. Job messages that must be durable go through Publish.
func (c *Conn) PublishCore(subject string, data []byte) error {
	if err := c.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("worker: core publish %s: %w", subject, err)
	}
	return nil
}
