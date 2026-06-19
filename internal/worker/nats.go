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
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: NATS bootstrap configuration holding server URL and component name (pure)
type Config struct {
	// NATSURL is the NATS server URL (env TMI_NATS_URL).
	NATSURL string
	// ComponentName is this worker's TMIComponent name (env TMI_COMPONENT_NAME).
	ComponentName string
}

// ConfigFromEnv builds a Config from the standard worker env vars.
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: build NATS worker config from required environment variables (pure)
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
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: worker handle bundling a NATS connection, JetStream context, and payload object store (pure)
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
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: connect to NATS, open JetStream context, and ensure payload object store bucket exists
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
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: return the JetStream context for consumer and publish wiring (pure)
func (c *Conn) JetStream() jetstream.JetStream { return c.js }

// Config returns the connection's config.
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: return the connection's NATS bootstrap config (pure)
func (c *Conn) Config() Config { return c.cfg }

// Close closes the NATS connection. In-flight messages are not drained.
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: close the NATS connection without draining in-flight messages
func (c *Conn) Close() { c.nc.Close() }

// PutPayload writes bytes to the Object Store under the given name and
// returns the object_ref to carry in an envelope.
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: store a blob in the payload object store and return its object ref
func (c *Conn) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	if _, err := c.objs.PutBytes(ctx, name, data); err != nil {
		return "", fmt.Errorf("worker: put payload %s: %w", name, err)
	}
	return PayloadBucket + "/" + name, nil
}

// GetPayload reads a blob by the object_ref produced by PutPayload.
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: fetch a blob from the payload object store by object ref
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
// SEM@b4d93cd08868f7468a93b099aca5217f7b263d56: delete a payload blob by object ref; idempotent if already absent
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
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: strip the bucket prefix from an object ref, returning the bare blob name (pure)
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
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: validate that an object ref belongs to the given job ID, rejecting forged refs (pure)
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
// SEM@9d9b68ad12f48fd7a991a43f29615cea731c76ca: publish a message to a durable JetStream subject
func (c *Conn) Publish(ctx context.Context, subject string, data []byte) error {
	if _, err := c.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("worker: publish %s: %w", subject, err)
	}
	return nil
}

// PublishCore publishes a message over core NATS (fire-and-forget, no
// JetStream stream or persistence). Use this for ephemeral signals such as
// heartbeats. Job messages that must be durable go through Publish.
// SEM@71c1d8554ecca870da2bafa898b79d1c29d43ebf: publish a fire-and-forget message over core NATS without persistence
func (c *Conn) PublishCore(subject string, data []byte) error {
	if err := c.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("worker: core publish %s: %w", subject, err)
	}
	return nil
}
