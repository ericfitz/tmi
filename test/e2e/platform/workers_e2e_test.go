//go:build e2e

package platform_e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// embedStubImage is the image tag for the in-cluster stub embedding server,
// built from cmd/worker-probe (--embed-stub) and loaded into kind by
// deployEmbedStub. Kept distinct from the worker images so it never collides.
const embedStubImage = "tmi-worker-probe:e2e"

// repoRoot walks up from the test's working directory until it finds go.mod,
// returning the module root so the e2e test can build and containerize the
// worker-probe binary.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod above %s", dir)
		}
		dir = parent
	}
}

// deployEmbedStub builds the worker-probe binary for the kind node platform,
// packages it into a minimal static image, loads it into kind, and deploys an
// in-cluster stub embedding server (Pod + Service "embed-stub" on :8443). It
// returns a cleanup function. The chunk-embed worker reaches this stub at
// http://embed-stub.tmi-platform.svc:8443/v1 under its egress:allowlist policy.
func deployEmbedStub(t *testing.T) func() {
	t.Helper()
	root := repoRoot(t)

	// 1. Build the worker-probe binary for the kind node (linux, host arch).
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "worker-probe")
	build := exec.Command("go", "build", "-trimpath", "-o", binPath, "./cmd/worker-probe")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build worker-probe for embed-stub: %v\n%s", err, out)
	}

	// 2. Package the static binary into a minimal image (no shell needed).
	dockerfile := "FROM cgr.dev/chainguard/static:latest\n" +
		"COPY worker-probe /worker-probe\n" +
		"ENTRYPOINT [\"/worker-probe\"]\n"
	dbuild := exec.Command("docker", "build", "-t", embedStubImage, "-f", "-", binDir)
	dbuild.Stdin = strings.NewReader(dockerfile)
	if out, err := dbuild.CombinedOutput(); err != nil {
		t.Fatalf("docker build embed-stub image: %v\n%s", err, out)
	}
	load := exec.Command("kind", "load", "docker-image", embedStubImage, "--name", "tmi-platform")
	if out, err := load.CombinedOutput(); err != nil {
		t.Fatalf("kind load embed-stub image: %v\n%s", err, out)
	}

	// 3. Deploy the stub Pod + Service. The pod runs worker-probe --embed-stub,
	//    serving an OpenAI-shaped /v1/embeddings response on :8443 forever.
	manifest := `
apiVersion: v1
kind: Pod
metadata:
  name: embed-stub
  namespace: tmi-platform
  labels:
    tmi.dev/role: embed-stub
spec:
  restartPolicy: Never
  containers:
    - name: stub
      image: ` + embedStubImage + `
      imagePullPolicy: Never
      args: ["--embed-stub"]
      ports:
        - containerPort: 8443
---
apiVersion: v1
kind: Service
metadata:
  name: embed-stub
  namespace: tmi-platform
spec:
  selector:
    tmi.dev/role: embed-stub
  ports:
    - port: 8443
      targetPort: 8443
`
	applyStdin(t, manifest)
	kubectl(t, "-n", "tmi-platform", "wait", "--for=condition=Ready", "pod/embed-stub", "--timeout=90s")

	// 4. Point chunk-embed at the in-cluster stub and scope its allowlist to the
	//    stub pods only (drop openInternet; the target is in-cluster).
	crOverride := `
apiVersion: tmi.dev/v1alpha1
kind: TMIComponent
metadata:
  name: tmi-chunk-embed
  namespace: tmi-platform
spec:
  image: tmi-chunk-embed:dev
  jobSubjects:
    - jobs.chunkembed.>
  inputMode: content-ref
  egress: allowlist
  allowlist:
    clusterPeers:
      - podSelector: { tmi.dev/role: embed-stub }
        ports: [8443]
  config:
    TMI_COMPONENT_NAME: tmi-chunk-embed
    TMI_NATS_URL: nats://nats.tmi-platform.svc:4222
    TMI_EMBEDDING_MODEL: text-embedding-3-small
    TMI_EMBEDDING_BASE_URL: http://embed-stub.tmi-platform.svc:8443/v1
    TMI_JOB_ACK_WAIT: 120s
  secretRefs:
    - name: TMI_EMBEDDING_API_KEY
      secretName: tmi-embedding
      secretKey: api-key
  resources:
    requests: { cpu: 250m, memory: 256Mi }
    limits: { cpu: 1000m, memory: 512Mi }
  scaling:
    minReplicas: 0
    maxReplicas: 10
    queueDepthTarget: 5
`
	applyStdin(t, crOverride)

	return func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", "tmi-platform", "delete", "pod", "embed-stub", "--grace-period=0", "--force", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete embed-stub pod: %v\n%s", err, out)
		}
		cmd = exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", "tmi-platform", "delete", "service", "embed-stub", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete embed-stub service: %v\n%s", err, out)
		}
		// Restore the shipped chunk-embed CR so later tiers see the default.
		cmd = exec.Command("kubectl", "--context", "kind-tmi-platform",
			"apply", "-f", filepath.Join(root, "deployments/k8s/platform/components/tmi-chunk-embed.yml"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: restore tmi-chunk-embed CR: %v\n%s", err, out)
		}
	}
}

// TestWorkersE2E_PlaintextJob assumes `make e2e-platform-up`, the
// component-controller, and both TMIComponent CRs are already deployed (the
// Makefile target test-e2e-workers wires that). It connects to the
// in-cluster NATS through a port-forward on localhost:4222, puts a plaintext
// payload, publishes an extract job, and asserts a completed result
// envelope lands. KEDA scales tmi-extractor and tmi-chunk-embed from zero on
// queue depth, so the timeout is generous to allow cold start.
func TestWorkersE2E_PlaintextJob(t *testing.T) {
	// #444 fix: the pipeline failed to deliver the extract job because KEDA
	// could never scale the extractor from zero. KEDA watches a durable
	// consumer named TMI_EXTRACTOR_CONSUMER (consumerNameFor), but the worker
	// created one named "tmi-extractor", and nothing pre-created the consumer
	// at all — so KEDA observed no queue depth and never started a worker. The
	// fix pre-creates the stream + durable consumer in the controller
	// (ConsumerConfigFor), aligns the worker durable to ConsumerNameFor, and
	// has the worker bind that consumer. This test requires the controller to
	// run with TMI_NATS_URL set (the make target / harness wires it) so the
	// stream and consumer exist before KEDA evaluates queue depth.
	const natsURL = "nats://127.0.0.1:4222"

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Stand up an in-cluster stub embedding endpoint and point chunk-embed at it
	// (scoped via egress:allowlist clusterPeers). With a reachable embedder the
	// chunk-embed stage can complete, so the pipeline reaches a completed result.
	cleanupStub := deployEmbedStub(t)
	defer cleanupStub()

	conn, err := worker.Connect(ctx, worker.Config{NATSURL: natsURL, ComponentName: "e2e"})
	if err != nil {
		t.Fatalf("connect to in-cluster NATS (is the port-forward up?): %v", err)
	}
	defer conn.Close()

	jobID := "e2e-job-1"
	srcRef, err := conn.PutPayload(ctx, jobID+"/source", []byte("end to end plaintext"))
	if err != nil {
		t.Fatalf("put source payload: %v", err)
	}

	results := subscribeResult(ctx, t, conn, jobID)

	dl := time.Now().Add(90 * time.Second)
	job := jobenvelope.Job{
		JobID:       jobID,
		ContentType: "text/plain",
		Limits:      jobenvelope.Limits{WallClock: jobenvelope.Duration(10 * time.Second)},
		Deadline:    &dl,
		Input:       jobenvelope.Input{ObjectRef: srcRef, ByteSize: 20},
	}
	jb, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := conn.Publish(ctx, worker.SubjectExtractPrefix+"plaintext", jb); err != nil {
		t.Fatalf("publish extract job: %v", err)
	}

	select {
	case res := <-results:
		// With an in-cluster stub embedding endpoint reachable under the
		// chunk-embed worker's egress:allowlist policy, the full pipeline
		// (extract -> chunkembed -> embed -> result) must reach a completed
		// result. (Issue #443: egress:allowlist now enforces real L3 egress to
		// the declared clusterPeer target, so the embed step succeeds.)
		if res.Status != jobenvelope.StatusCompleted {
			t.Fatalf("expected completed result, got status=%q reason=%q", res.Status, res.ReasonCode)
		}
		t.Logf("e2e result: status=%s reason=%s", res.Status, res.ReasonCode)
	case <-ctx.Done():
		t.Fatal("timed out waiting for the worker pipeline result envelope")
	}

	// Sanity: confirm KEDA scaled the extractor up at some point.
	out, err := exec.CommandContext(ctx, "kubectl", "--context", "kind-tmi-platform",
		"-n", "tmi-platform", "get", "pods", "-l", "app=tmi-extractor", "--no-headers").Output()
	if err != nil {
		t.Logf("pod check skipped: %v", err)
	} else {
		t.Logf("tmi-extractor pods: %q", string(out))
	}
}

// subscribeResult creates a durable JetStream consumer on the job's result
// subject and returns a channel that receives the Result envelope.
func subscribeResult(ctx context.Context, t *testing.T, conn *worker.Conn, jobID string) <-chan jobenvelope.Result {
	t.Helper()
	js := conn.JetStream()
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.ResultStream,
		Subjects:  []string{worker.SubjectResultPrefix + ">"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		t.Fatalf("ensure result stream: %v", err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "e2e-result",
		FilterSubject: worker.ResultSubject(jobID),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create result consumer: %v", err)
	}
	out := make(chan jobenvelope.Result, 1)
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var r jobenvelope.Result
		if json.Unmarshal(msg.Data(), &r) == nil && r.JobID == jobID {
			_ = msg.Ack()
			select {
			case out <- r:
			default:
			}
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		t.Fatalf("consume result subject: %v", err)
	}
	t.Cleanup(cc.Stop)
	return out
}
