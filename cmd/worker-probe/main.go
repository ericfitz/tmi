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
// The probe does use worker.ResultSubject (an existing helper) to derive the
// result subject — no new exported helpers were added to internal/worker.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
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
// SEM@7af169a39c0d9b658b5d5b8cc6f1425a0eca358e: carrier for a worker job's ID and stamped embedding config (pure)
type jobEnvelope struct {
	JobID  string               `json:"job_id"`
	Config config.StampedConfig `json:"config"`
}

// probeResult is what the probe echoes back, proving each contract leg.
// SEM@7af169a39c0d9b658b5d5b8cc6f1425a0eca358e: result struct echoing bootstrap, config, and secret contract outcomes for a probe job (pure)
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

// embedStubAddr is the listen address for the --embed-stub HTTP server.
const embedStubAddr = ":8443"

// embedStubVectorLen is the fixed embedding dimension the stub returns. It does
// not need to match TMI_EMBEDDING_MODEL — the chunk-embed worker stores the
// vector verbatim — but a realistic length keeps the canned response sane.
const embedStubVectorLen = 1536

// SEM@c96b2f1a4f2875aa62728488157edf756d9d4578: dispatch the worker-probe as an embed stub or a NATS probe run
func main() {
	embedStub := flag.Bool("embed-stub", false,
		"run as an in-cluster stub OpenAI-compatible embedding server on "+embedStubAddr+" (e2e fixture)")
	flag.Parse()

	if *embedStub {
		if err := runEmbedStub(); err != nil {
			slogging.Get().Error("worker-probe: embed-stub: %v", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slogging.Get().Error("worker-probe: %v", err)
		os.Exit(1)
	}
}

// runEmbedStub serves a canned OpenAI-shaped /v1/embeddings response forever so
// an in-cluster chunk-embed worker (egress: allowlist) can reach a real
// embedding endpoint in the e2e tier. It is a test fixture, not production code.
// SEM@da3b3c52d378ce3b2e2eb2010bada47c51dbd37f: serve a canned OpenAI-compatible embedding response for in-cluster e2e fixture use
func runEmbedStub() error {
	logger := slogging.Get()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// The OpenAI embeddings API returns one embedding per input element,
		// index-aligned. langchaingo's EmbedDocuments batches all chunks into
		// one request and requires len(data) == len(input); returning a single
		// embedding for a multi-input request makes the client error. Decode
		// the request's "input" (string or []string) and emit a matching count.
		n := 1
		var req struct {
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && len(req.Input) > 0 {
			var arr []string
			if json.Unmarshal(req.Input, &arr) == nil && len(arr) > 0 {
				n = len(arr)
			}
		}
		data := make([]any, n)
		for i := range data {
			data[i] = map[string]any{
				"object":    "embedding",
				"index":     i,
				"embedding": make([]float64, embedStubVectorLen),
			}
		}
		resp := map[string]any{
			"object": "list",
			"model":  "stub",
			"data":   data,
			"usage": map[string]any{
				"prompt_tokens": 0,
				"total_tokens":  0,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			logger.Warn("worker-probe: embed-stub: encode response failed: %v", err)
		}
	})

	srv := &http.Server{
		Addr:              embedStubAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	logger.Info("worker-probe: embed-stub listening addr=%s vector_len=%d", embedStubAddr, embedStubVectorLen)
	return srv.ListenAndServe()
}

// run is the real entry point. Separating it from main allows defers to
// execute before os.Exit is called by main.
// SEM@0aba7f3799aed0f98991b3f45a64df18fbb029bd: bootstrap, connect to NATS, receive one probe job, and publish the probe result
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

	// Step 6: build probe result. StampedConfigSeen is true only when the
	// envelope carried a genuinely-valid stamped config (non-empty model and
	// endpoint, positive dimension) — not merely a non-empty model string.
	result := probeResult{
		JobID:             env.JobID,
		BootstrapOK:       true,
		StampedConfigSeen: env.Config.Validate() == nil,
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
	resultSubject := worker.ResultSubject(env.JobID)
	if err := nc.Publish(resultSubject, resultBytes); err != nil {
		return fmt.Errorf("failed to publish result subject=%s: %w", resultSubject, err)
	}

	// Step 9: log success and exit cleanly
	logger.Info("worker-probe: result published subject=%s bootstrap_ok=%v stamped_config_seen=%v secret_resolved=%v",
		resultSubject, result.BootstrapOK, result.StampedConfigSeen, result.SecretResolved)
	return nil
}
