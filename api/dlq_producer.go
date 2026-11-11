package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// maxDeliverAdvisory is the subset of a JetStream MAX_DELIVERIES consumer
// advisory the DLQ producer needs. Parsed from a local struct rather than a
// nats-server type to avoid a server dependency. The advisory carries the
// source stream + sequence but NOT the original payload, which is why the
// producer recovers it via GetMsg.
type maxDeliverAdvisory struct {
	Stream     string `json:"stream"`
	Consumer   string `json:"consumer"`
	StreamSeq  uint64 `json:"stream_seq"`
	Deliveries uint64 `json:"deliveries"`
}

// parseMaxDeliverAdvisory decodes a MAX_DELIVERIES advisory payload.
func parseMaxDeliverAdvisory(data []byte) (maxDeliverAdvisory, error) {
	var adv maxDeliverAdvisory
	if err := json.Unmarshal(data, &adv); err != nil {
		return maxDeliverAdvisory{}, err
	}
	return adv, nil
}

// isSelfReferentialStream reports whether advisories for the given source
// stream must be ignored to avoid dead-letter loops and wasted work: the
// result stream, the DLQ stream, and the advisory-capture stream are all
// consumed by the monolith itself and must never be dead-lettered.
func isSelfReferentialStream(stream string) bool {
	return stream == worker.ResultStream ||
		stream == worker.DLQStream ||
		stream == worker.DLQAdvisoryStream
}

// decodeJobForDLQ decodes recovered source bytes as a Job envelope and returns
// it only when it is a valid job. This scopes dead-lettering to job streams
// without hardcoding component names: a Result envelope or any non-job message
// fails jobenvelope.Validate and is skipped.
func decodeJobForDLQ(data []byte) (jobenvelope.Job, bool) {
	var job jobenvelope.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return jobenvelope.Job{}, false
	}
	if err := jobenvelope.Validate(job); err != nil {
		return jobenvelope.Job{}, false
	}
	return job, true
}

// DLQProducer turns JetStream MAX_DELIVERIES advisories into dead-letter
// messages. For each advisory on a per-component job stream it recovers the
// original Job envelope by sequence, republishes it to jobs.dlq, and deletes
// the source message (reclaiming the WorkQueue slot). It is the only durable
// path by which a worker that crashed mid-job (and thus never published a
// result) reaches a clean terminal state.
type DLQProducer struct {
	conn   *worker.Conn
	cancel context.CancelFunc
}

// NewDLQProducer constructs a DLQProducer bound to the monolith's NATS conn.
func NewDLQProducer(conn *worker.Conn) *DLQProducer {
	return &DLQProducer{conn: conn}
}

// ensureStreams creates (or updates) the DLQ stream and the advisory-capture
// stream. Idempotent: safe to call on every startup.
func (p *DLQProducer) ensureStreams(ctx context.Context) error {
	js := p.conn.JetStream()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.DLQStream,
		Subjects:  []string{worker.SubjectDLQ},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		return fmt.Errorf("dlq-producer: ensure %s stream: %w", worker.DLQStream, err)
	}
	// NOTE: capturing $JS.EVENT.ADVISORY.* into a user stream works in
	// single-account NATS (TMI's dev/default topology). In a multi-account
	// (operator/JWT) NATS deployment, the system account must export these
	// advisory subjects and the application account must import them, or this
	// stream captures nothing. This is a production NATS operator precondition.
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.DLQAdvisoryStream,
		Subjects:  []string{worker.SubjectMaxDeliverAdvisory},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    24 * time.Hour,
	}); err != nil {
		return fmt.Errorf("dlq-producer: ensure %s stream: %w", worker.DLQAdvisoryStream, err)
	}
	return nil
}

// Start ensures the streams exist, creates a durable consumer on the
// advisory-capture stream, and begins processing advisories in the background.
// It returns after the consumer is created. Call Stop to release resources.
func (p *DLQProducer) Start(ctx context.Context) error {
	logger := slogging.Get()
	ctx, cancel := context.WithCancel(ctx)

	if err := p.ensureStreams(ctx); err != nil {
		cancel()
		return err
	}

	js := p.conn.JetStream()
	advStream, err := js.Stream(ctx, worker.DLQAdvisoryStream)
	if err != nil {
		cancel()
		return fmt.Errorf("dlq-producer: lookup advisory stream: %w", err)
	}
	cons, err := advStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "monolith-dlq-producer",
		FilterSubject: worker.SubjectMaxDeliverAdvisory,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    5,
	})
	if err != nil {
		cancel()
		return fmt.Errorf("dlq-producer: create advisory consumer: %w", err)
	}

	cc, err := cons.Consume(p.makeCallback(ctx))
	if err != nil {
		cancel()
		return fmt.Errorf("dlq-producer: consume advisory stream: %w", err)
	}
	p.cancel = cancel
	go func() {
		<-ctx.Done()
		cc.Stop()
		logger.Info("dlq-producer: shut down")
	}()
	logger.Info("dlq-producer: subscribed to %s", worker.SubjectMaxDeliverAdvisory)
	return nil
}

// Stop cancels the producer's context. Safe to call when Start was never run.
func (p *DLQProducer) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// makeCallback returns the advisory handler. It must never panic — a panic
// here would crash the monolith — so it is guarded.
func (p *DLQProducer) makeCallback(ctx context.Context) func(jetstream.Msg) {
	logger := slogging.Get()
	js := p.conn.JetStream()

	return func(msg jetstream.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("dlq-producer: panic on %s: %v — terminating", msg.Subject(), r)
				_ = msg.Term()
			}
		}()

		adv, err := parseMaxDeliverAdvisory(msg.Data())
		if err != nil {
			logger.Error("dlq-producer: undecodable advisory on %s: %v — terminating", msg.Subject(), err)
			_ = msg.Term()
			return
		}

		// Never dead-letter the result or DLQ streams themselves (loop guard).
		if isSelfReferentialStream(adv.Stream) {
			_ = msg.Ack()
			return
		}

		// Recover the original message by sequence from its source stream.
		srcStream, err := js.Stream(ctx, adv.Stream)
		if err != nil {
			logger.Warn("dlq-producer: source stream %s lookup failed: %v — nak", adv.Stream, err)
			_ = msg.Nak()
			return
		}
		raw, err := srcStream.GetMsg(ctx, adv.StreamSeq)
		if err != nil {
			// Already deleted by a prior advisory delivery — idempotent ack.
			if errors.Is(err, jetstream.ErrMsgNotFound) {
				_ = msg.Ack()
				return
			}
			logger.Warn("dlq-producer: GetMsg seq=%d on %s failed: %v — nak", adv.StreamSeq, adv.Stream, err)
			_ = msg.Nak()
			return
		}

		// Only dead-letter valid Job envelopes (skips stray Results, etc.).
		if _, ok := decodeJobForDLQ(raw.Data); !ok {
			logger.Warn("dlq-producer: seq=%d on %s is not a valid job — acking advisory without dead-lettering", adv.StreamSeq, adv.Stream)
			_ = msg.Ack()
			return
		}

		// Publish-then-delete: publish to jobs.dlq first so nothing is lost on
		// publish failure; then delete the source to reclaim the WorkQueue slot.
		if _, err := js.Publish(ctx, worker.SubjectDLQ, raw.Data); err != nil {
			logger.Warn("dlq-producer: publish to %s failed: %v — nak", worker.SubjectDLQ, err)
			_ = msg.Nak()
			return
		}
		if err := srcStream.DeleteMsg(ctx, adv.StreamSeq); err != nil && !errors.Is(err, jetstream.ErrMsgNotFound) {
			// The DLQ message is already published; a failed source delete only
			// leaves a dead slot, not a correctness bug. Log and ack.
			logger.Warn("dlq-producer: DeleteMsg seq=%d on %s failed: %v", adv.StreamSeq, adv.Stream, err)
		}
		logger.Info("dlq-producer: dead-lettered seq=%d from %s to %s", adv.StreamSeq, adv.Stream, worker.SubjectDLQ)
		_ = msg.Ack()
	}
}
