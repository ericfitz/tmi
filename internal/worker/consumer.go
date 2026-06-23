package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// JobError is a typed, terminal failure a handler returns when a job can
// never succeed (malformed input, unsupported format, timeout). The
// consumer terminates such a message rather than redelivering it.
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: typed terminal job failure signalling whether redelivery can help (pure)
type JobError struct {
	// ReasonCode is a pkg/extract Reason* constant (or a status string).
	ReasonCode string
	// Detail is optional human-readable context.
	Detail string
	// Terminal is true when redelivery cannot help.
	Terminal bool
}

// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: format a JobError as a human-readable string (pure)
func (e *JobError) Error() string {
	return fmt.Sprintf("job error: reason=%s detail=%q terminal=%v", e.ReasonCode, e.Detail, e.Terminal)
}

// outcome is the consumer's per-message decision.
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: enumerated ack/nak/term disposition for a consumed JetStream message (pure)
type outcome int

const (
	// OutcomeAck marks the message processed successfully.
	OutcomeAck outcome = iota
	// OutcomeNak requests redelivery (transient failure).
	OutcomeNak
	// OutcomeTerm permanently drops the message (terminal failure).
	OutcomeTerm
)

// outcomeFor maps a handler's returned error to a consumer outcome.
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: map a handler error to the appropriate ack, nak, or term outcome (pure)
func outcomeFor(err error) outcome {
	if err == nil {
		return OutcomeAck
	}
	var je *JobError
	if errors.As(err, &je) && je.Terminal {
		return OutcomeTerm
	}
	return OutcomeNak
}

// JobHandler processes one decoded job. Returning nil acks the message;
// returning a terminal *JobError terminates it; any other error naks it for
// redelivery. The handler is responsible for publishing the result envelope
// for terminal failures BEFORE returning the *JobError — the consumer only
// decides ack/nak/term, it does not publish results.
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: callback contract for processing a decoded job and signalling ack/nak/term via error (pure)
type JobHandler func(ctx context.Context, job jobenvelope.Job) error

// ConsumerConfig configures the durable consumer.
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: parameters for binding a durable JetStream consumer to a stream and subject (pure)
type ConsumerConfig struct {
	// StreamName is the JetStream stream the consumer binds. The controller
	// renders one stream per component (TMI_<NAME>); RunConsumer creates the
	// stream if it does not yet exist.
	StreamName string
	// Durable is the durable consumer name (stable across restarts).
	Durable string
	// FilterSubject is the subject filter (e.g. "jobs.extract.>").
	FilterSubject string
	// AckWait is the redelivery timeout — also the JetStream-side backstop
	// for a worker that dies mid-job.
	AckWait time.Duration
	// MaxDeliver caps redeliveries before JetStream dead-letters.
	MaxDeliver int
}

// idempotency tracks job_ids already completed this process lifetime so a
// redelivered message is acked without reprocessing. A worker restart loses
// the set; the result-blob-exists check in the handler is the durable guard.
//
// NOT goroutine-safe: it relies on jetstream.Consume delivering messages
// serially from a single goroutine (verified for nats.go v1.36.0). If the
// consumer ever moves to concurrent/worker-pool delivery, this map must be
// guarded with a sync.Mutex.
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: in-process set of completed job IDs used to skip redelivered messages (mutates shared state)
type idempotency struct {
	seen map[string]struct{}
}

// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: build an empty in-process idempotency tracker (pure)
func newIdempotency() *idempotency { return &idempotency{seen: map[string]struct{}{}} }

// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: report whether a job ID has already been processed this lifetime (pure)
func (i *idempotency) done(id string) bool {
	_, ok := i.seen[id]
	return ok
}

// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: record a job ID as completed in the idempotency tracker (mutates shared state)
func (i *idempotency) mark(id string) { i.seen[id] = struct{}{} }

// ensureStream returns the named stream, creating it (WorkQueue/File) bound
// to filterSubject if it does not yet exist. The controller normally renders
// the stream; this fallback keeps a worker self-sufficient (and lets the
// process-mode integration tests run without the controller).
// SEM@3b4afc57df700de14d06ec4e93a7038dcf52b9d2: fetch or create a JetStream work-queue stream bound to the given subject
func ensureStream(ctx context.Context, js jetstream.JetStream, name, filterSubject string) (jetstream.Stream, error) {
	s, err := js.Stream(ctx, name)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil, fmt.Errorf("worker: lookup stream %s: %w", name, err)
	}
	s, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  []string{filterSubject},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("worker: create stream %s: %w", name, err)
	}
	return s, nil
}

// RunConsumer creates the durable consumer and dispatches messages to the
// handler until ctx is cancelled. It blocks; run it on the main goroutine.
// SEM@e69b1723153a31aa74eb58c885a3ca54a9cbb016: bind a durable JetStream consumer and dispatch jobs to the handler until context is cancelled
func RunConsumer(ctx context.Context, conn *Conn, cfg ConsumerConfig, handle JobHandler) error {
	logger := slogging.Get()

	stream, err := ensureStream(ctx, conn.JetStream(), cfg.StreamName, cfg.FilterSubject)
	if err != nil {
		return err
	}
	// Bind the durable consumer the controller pre-created (it must already
	// exist for KEDA to have scaled this worker up from zero). Only create it
	// as a fallback when absent — e.g. the process-mode integration tests run
	// without a controller. Binding (rather than CreateOrUpdate) avoids a
	// filter-subject conflict: the controller-created consumer has no filter
	// (the per-component stream is single-purpose), whereas the fallback below
	// sets one for the controller-less path.
	cons, err := stream.Consumer(ctx, cfg.Durable)
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		cons, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Durable:       cfg.Durable,
			FilterSubject: cfg.FilterSubject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       cfg.AckWait,
			MaxDeliver:    cfg.MaxDeliver,
		})
	}
	if err != nil {
		return fmt.Errorf("worker: bind/create consumer %s: %w", cfg.Durable, err)
	}
	idem := newIdempotency()

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var job jobenvelope.Job
		if err := json.Unmarshal(msg.Data(), &job); err != nil {
			logger.Error("worker consumer: undecodable message on %s: %v", msg.Subject(), err)
			_ = msg.Term() // a message we cannot decode can never succeed
			return
		}
		if err := jobenvelope.Validate(job); err != nil {
			logger.Error("worker consumer: invalid envelope job=%s: %v", job.JobID, err)
			_ = msg.Term()
			return
		}
		if idem.done(job.JobID) {
			logger.Debug("worker consumer: job %s already processed, acking redelivery", job.JobID)
			_ = msg.Ack()
			return
		}
		err := handle(ctx, job)
		switch outcomeFor(err) {
		case OutcomeAck:
			idem.mark(job.JobID)
			_ = msg.Ack()
		case OutcomeTerm:
			idem.mark(job.JobID)
			logger.Warn("worker consumer: terminal failure job=%s: %v", job.JobID, err)
			_ = msg.Term()
		default:
			logger.Warn("worker consumer: transient failure job=%s, will redeliver: %v", job.JobID, err)
			_ = msg.Nak()
		}
	})
	if err != nil {
		return fmt.Errorf("worker: consume: %w", err)
	}
	defer cc.Stop()

	<-ctx.Done()
	logger.Info("worker consumer: shutting down")
	return nil
}
