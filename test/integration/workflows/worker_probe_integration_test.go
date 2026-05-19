package workflows

// TestWorkerProbe_ContractEndToEnd_Integration builds the worker-probe binary,
// runs it against a real NATS server, publishes a job envelope, and asserts
// the probe echoes back a probeResult proving all three contract legs:
//
//   - bootstrap_ok       — the probe could load its env-driven bootstrap config
//   - stamped_config_seen — the envelope contained a valid StampedConfig
//   - secret_resolved    — the probe resolved the mounted embedding-api-key secret
//
// Gate: set TMI_RUN_NATS_TESTS=1 with a NATS server available.
// URL:  TMI_TEST_NATS_URL (defaults to nats://127.0.0.1:4222 if unset).
//
// This test is skipped cleanly when NATS is absent; it never FAILs due to
// infrastructure absence.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
)

// probeJobEnvelope is the subset of the worker-probe job envelope we marshal
// to publish the job. It must match the jobEnvelope struct in
// cmd/worker-probe/main.go exactly.
type probeJobEnvelope struct {
	JobID  string              `json:"job_id"`
	Config probeStampedConfig  `json:"config"`
}

// probeStampedConfig mirrors internal/config.StampedConfig as a local type
// so this module does not need to import the main module's config package
// (which would pull in heavy monolith dependencies). The JSON encoding is
// identical.
type probeStampedConfig struct {
	Embedding probeEmbeddingProfile `json:"embedding"`
}

// probeEmbeddingProfile mirrors internal/config.EmbeddingProfile.
type probeEmbeddingProfile struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	Dimension int    `json:"dimension"`
}

// probeResult mirrors the probeResult struct in cmd/worker-probe/main.go.
type probeResult struct {
	JobID             string `json:"job_id"`
	BootstrapOK       bool   `json:"bootstrap_ok"`
	StampedConfigSeen bool   `json:"stamped_config_seen"`
	SecretResolved    bool   `json:"secret_resolved"`
}

// workerProbeNATSURL returns the NATS endpoint for integration tests. Mirrors
// natsURL in internal/worker/nats_test.go and hbTestNATSURL in
// internal/worker/heartbeat_test.go: CI sets TMI_TEST_NATS_URL; locally
// defaults to localhost.
func workerProbeNATSURL() string {
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

// TestWorkerProbe_ContractEndToEnd_Integration is the end-to-end contract test
// for the worker-probe binary. It is gated by TMI_RUN_NATS_TESTS (consistent
// with internal/worker/*_test.go) and skips cleanly when NATS is absent.
func TestWorkerProbe_ContractEndToEnd_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}

	natsURL := workerProbeNATSURL()

	// ── Step 1: verify NATS is reachable ─────────────────────────────────────
	// Attempt a quick connection; skip (never fail) if the server is not up.
	pingConn, err := nats.Connect(natsURL, nats.Timeout(3*time.Second))
	if err != nil {
		t.Skipf("NATS not reachable at %s (skip, not fail): %v", natsURL, err)
	}
	pingConn.Close()

	// ── Step 2: write mounted-secret file ─────────────────────────────────────
	tmpDir := t.TempDir()
	secretPath := filepath.Join(tmpDir, "embedding-key")
	secretContent := "integration-test-key\n"
	if err := os.WriteFile(secretPath, []byte(secretContent), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	// ── Step 3: build the worker-probe binary ─────────────────────────────────
	probeBin := filepath.Join(tmpDir, "worker-probe")
	buildCmd := exec.Command(
		"go", "build",
		"-o", probeBin,
		"github.com/ericfitz/tmi/cmd/worker-probe",
	)
	// Run go build from the project root (two levels up from test/integration).
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	buildCmd.Dir = projectRoot
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build worker-probe: %v\n%s", err, buildOut)
	}

	// ── Step 4: start the probe process ──────────────────────────────────────
	const jobID = "probe-job-1"

	probeCtx, probeCancel := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(probeCancel)

	probeEnv := append(
		os.Environ(),
		"TMI_WORKER_NATS_URL="+natsURL,
		"TMI_WORKER_HEARTBEAT_SUBJECT=workers.heartbeat.probe",
		"TMI_WORKER_SECRET_MOUNT_EMBEDDING_API_KEY="+secretPath,
	)

	probeCmd := exec.CommandContext(probeCtx, probeBin)
	probeCmd.Env = probeEnv
	// Pipe probe stdout/stderr to the test log so failures are debuggable.
	probeCmd.Stdout = &testWriter{t: t, prefix: "[probe stdout] "}
	probeCmd.Stderr = &testWriter{t: t, prefix: "[probe stderr] "}

	if err := probeCmd.Start(); err != nil {
		t.Fatalf("start worker-probe: %v", err)
	}
	t.Cleanup(func() {
		// Best-effort: kill the probe so it never leaks.
		if probeCmd.Process != nil {
			_ = probeCmd.Process.Kill()
		}
		_ = probeCmd.Wait()
	})

	// ── Step 5: connect to NATS as the "monolith side" ───────────────────────
	nc, err := nats.Connect(natsURL, nats.Name("tmi-test-monolith"))
	if err != nil {
		t.Fatalf("NATS connect (monolith side): %v", err)
	}
	t.Cleanup(nc.Close)

	// Subscribe to the result subject BEFORE publishing the job so we cannot
	// miss the reply. Buffer=1 is sufficient; the probe publishes exactly once.
	resultSubject := "jobs.result." + jobID
	resultCh := make(chan *nats.Msg, 1)
	sub, err := nc.ChanSubscribe(resultSubject, resultCh)
	if err != nil {
		t.Fatalf("subscribe to %s: %v", resultSubject, err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	// Flush so the subscription interest is registered on the server before
	// the probe subscribes and before we publish. This mirrors the pattern in
	// internal/worker/heartbeat_test.go (sub.Flush after SubscribeSync).
	if err := nc.Flush(); err != nil {
		t.Fatalf("flush subscription: %v", err)
	}
	// Brief pause so the probe's subscribe() on "jobs.probe" is live.
	// 200 ms is the standard pragmatic allowance used in this codebase.
	time.Sleep(200 * time.Millisecond)

	// ── Step 6: publish job envelope ─────────────────────────────────────────
	// The StampedConfig MUST pass Validate(): non-empty Model and Endpoint,
	// positive Dimension. If invalid, the probe sets stamped_config_seen=false.
	env := probeJobEnvelope{
		JobID: jobID,
		Config: probeStampedConfig{
			Embedding: probeEmbeddingProfile{
				Model:     "text-embedding-3-large",
				Endpoint:  "https://e",
				Dimension: 3072,
			},
		},
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal job envelope: %v", err)
	}
	if err := nc.Publish("jobs.probe", envBytes); err != nil {
		t.Fatalf("publish job envelope: %v", err)
	}

	// ── Step 7: wait for and assert the probe result ──────────────────────────
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer waitCancel()

	select {
	case msg := <-resultCh:
		var result probeResult
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			t.Fatalf("unmarshal probe result: %v\nraw: %s", err, msg.Data)
		}
		t.Logf("probe result: %+v", result)
		if !result.BootstrapOK {
			t.Error("bootstrap_ok is false — probe failed to load bootstrap config")
		}
		if !result.StampedConfigSeen {
			t.Error("stamped_config_seen is false — probe did not receive a valid StampedConfig")
		}
		if !result.SecretResolved {
			t.Error("secret_resolved is false — probe could not resolve embedding-api-key secret")
		}
	case <-waitCtx.Done():
		t.Fatal("timed out waiting for probe result on " + resultSubject)
	}
}

// testWriter implements io.Writer and forwards lines to t.Log so probe output
// appears in verbose test output and failed-test logs.
type testWriter struct {
	t      *testing.T
	prefix string
}

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Log(w.prefix + string(p))
	return len(p), nil
}
