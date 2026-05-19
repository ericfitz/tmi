// Command worker-probe is a stub TMI worker. It exists to prove the worker
// bootstrap + job-envelope + secret-mount contract end to end before #347's
// real workers depend on it. It is a test fixture, not production code.
//
// NATS approach: this binary uses the plain github.com/nats-io/nats.go client
// directly rather than internal/worker.Connect or internal/worker.RunConsumer
// for two reasons:
//
//  1. internal/worker.Connect unconditionally creates a JetStream Object Store
//     bucket (TMI_PAYLOADS), which requires JetStream to be enabled on the
//     server. For a one-shot probe that receives a single message this extra
//     dependency is inappropriate.
//
//  2. internal/worker.RunConsumer is a long-running durable JetStream consumer
//     loop; there is no internal/worker primitive for "receive exactly one
//     message with a timeout." The probe requires exactly that pattern.
//
// The probe does use worker.SubjectResultPrefix (a read-only constant) to
// derive the result subject — no new exported helpers were added to
// internal/worker.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	nats "github.com/nats-io/nats.go"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/config/bootstrap"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
)

// jobEnvelope is the subset of the #347 job envelope the probe needs: the
// stamped config block. #347 owns the full schema.
type jobEnvelope struct {
	JobID  string               `json:"job_id"`
	Config config.StampedConfig `json:"config"`
}

// probeResult is what the probe echoes back, proving each contract leg.
type probeResult struct {
	JobID             string `json:"job_id"`
	BootstrapOK       bool   `json:"bootstrap_ok"`
	StampedConfigSeen bool   `json:"stamped_config_seen"`
	SecretResolved    bool   `json:"secret_resolved"`
}

// probeSubject is the NATS subject the probe listens on for its single job.
const probeSubject = "jobs.probe"

// receiveTimeout is how long the probe waits for a message before giving up.
const receiveTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		slogging.Get().Error("worker-probe: %v", err)
		os.Exit(1)
	}
}

// run is the real entry point. Separating it from main allows defers to
// execute before os.Exit is called by main.
func run() error {
	logger := slogging.Get()

	// Step 1: bootstrap
	wb, err := bootstrap.LoadWorker()
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
	}
	logger.Info("worker-probe: bootstrap succeeded nats_url=%s", wb.NATSURL)

	// Step 2: connect to NATS via plain nats.go (see package doc for rationale)
	nc, err := nats.Connect(wb.NATSURL, nats.Name("tmi-worker-probe"))
	if err != nil {
		return fmt.Errorf("NATS connect failed: %w", err)
	}
	defer nc.Close()
	logger.Info("worker-probe: NATS connected")

	// Step 3: publish heartbeat if a subject is configured
	if wb.HeartbeatSubject != "" {
		if err := nc.Publish(wb.HeartbeatSubject, []byte("worker-probe alive")); err != nil {
			logger.Warn("worker-probe: heartbeat publish failed (non-fatal): %v", err)
		} else {
			logger.Info("worker-probe: heartbeat published subject=%s", wb.HeartbeatSubject)
		}
	} else {
		logger.Info("worker-probe: no heartbeat subject configured, skipping")
	}

	// Step 4: receive exactly one message on jobs.probe with a 30s timeout
	ctx, cancel := context.WithTimeout(context.Background(), receiveTimeout)
	defer cancel()

	msgCh := make(chan *nats.Msg, 1)
	sub, err := nc.Subscribe(probeSubject, func(m *nats.Msg) {
		select {
		case msgCh <- m:
		default:
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe failed subject=%s: %w", probeSubject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	logger.Info("worker-probe: waiting for job message subject=%s timeout=%s", probeSubject, receiveTimeout)

	var msg *nats.Msg
	select {
	case msg = <-msgCh:
	case <-ctx.Done():
		return fmt.Errorf("timed out waiting for message on %s", probeSubject)
	}

	// Step 5: deserialize job envelope
	var env jobEnvelope
	if err := json.Unmarshal(msg.Data, &env); err != nil {
		return fmt.Errorf("failed to decode job envelope: %w", err)
	}
	logger.Info("worker-probe: received job job_id=%s", env.JobID)

	// Step 6: build probe result
	result := probeResult{
		JobID:             env.JobID,
		BootstrapOK:       true,
		StampedConfigSeen: env.Config.Embedding.Model != "",
	}

	// Step 7: resolve secret
	secret, err := wb.ReadSecret("embedding-api-key")
	if err != nil {
		logger.Warn("worker-probe: could not resolve secret embedding-api-key (non-fatal): %v", err)
	} else {
		result.SecretResolved = secret != ""
		logger.Info("worker-probe: secret embedding-api-key resolved")
	}

	// Step 8: marshal result and publish to jobs.result.<job_id>
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal probe result: %w", err)
	}
	resultSubject := worker.SubjectResultPrefix + env.JobID
	if err := nc.Publish(resultSubject, resultBytes); err != nil {
		return fmt.Errorf("failed to publish result subject=%s: %w", resultSubject, err)
	}

	// Step 9: log success and exit cleanly
	logger.Info("worker-probe: result published subject=%s bootstrap_ok=%v stamped_config_seen=%v secret_resolved=%v",
		resultSubject, result.BootstrapOK, result.StampedConfigSeen, result.SecretResolved)
	return nil
}
