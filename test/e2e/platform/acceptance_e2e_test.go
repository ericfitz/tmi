//go:build e2e

package platform_e2e

// Acceptance tests for issue #347 (Plan 4). Each test maps to one of the five
// acceptance criteria in the design spec
// (docs/superpowers/specs/2026-05-16-extractor-component-isolation-design.md
// §"Acceptance criteria (verified in the e2e tier)"):
//
//  1. TestAcceptance_CrashIsolation  — an extractor crash does not affect the
//     rest of the system, and the platform recovers.
//  2. TestAcceptance_EgressDenied    — the egress:none sandbox has no DNS and
//     cannot reach the metadata IP, the cluster DNS, or any external host —
//     only NATS. Verified against the real Calico CNI.
//  3. TestAcceptance_WallClockTimeout — a job that exceeds the wall-clock
//     budget produces a clean extraction_limit:timeout failure result.
//  4. TestAcceptance_CgroupOOMKill   — a workload that exceeds its cgroup
//     memory limit is OOM-killed by the kernel, not allowed to expand.
//  5. TestAcceptance_DeadLetter      — a job that can never be processed
//     exhausts MaxDeliver and fires a JetStream MAX_DELIVERIES advisory (the
//     dead-letter trigger), while the rest of the system is unaffected.
//
// Prerequisites (wired by `make test-e2e-acceptance`): `make e2e-platform-up`,
// the component-controller running against the cluster, both TMIComponent CRs
// deployed, busybox loaded into kind, and a NATS port-forward on
// localhost:4222.
//
// Note on the "main API server": the monolith is not deployed in this cluster.
// The criteria about the API server being "unaffected" are structural — the
// monolith is a separate process that falls back to inline extraction when
// NATS is absent (Plan 3). These tests therefore verify the in-cluster blast
// radius (NATS and the sibling worker stay healthy) plus recovery, which is
// the part of the property that is observable in the e2e tier.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	nats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// runNonce makes every job_id unique per test-binary invocation. The workers
// carry an in-process idempotency cache that acks a repeated job_id WITHOUT
// reprocessing (correct production behavior for redelivered jobs), and the
// result stream is WorkQueue-retained — so fixed job_ids would collide across
// runs and hang waiting for a result that is never republished. A per-run
// nonce sidesteps both.
var runNonce = strconv.FormatInt(time.Now().UnixNano(), 36)

// jobID builds a per-run-unique job id from a stable test-local name.
func jobID(name string) string { return "accept-" + name + "-" + runNonce }

const (
	natsLocalURL = "nats://127.0.0.1:4222"
	platformNS   = "tmi-platform"
	// docxContentType selects a bounded (wall-clock-governed) OOXML extractor.
	docxContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	// probeImage is loaded into kind by the make target; pinned for offline
	// determinism.
	probeImage = "busybox:1.36"
)

// dialNATS opens a worker.Conn to the port-forwarded in-cluster NATS.
func dialNATS(ctx context.Context, t *testing.T) *worker.Conn {
	t.Helper()
	conn, err := worker.Connect(ctx, worker.Config{NATSURL: natsLocalURL, ComponentName: "acceptance"})
	if err != nil {
		t.Fatalf("connect to in-cluster NATS (is the port-forward up?): %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// awaitResult subscribes (ephemeral consumer, no durable so tests never
// collide on the WorkQueue result stream) to a single job's result subject and
// returns a channel delivering the Result envelope.
func awaitResult(ctx context.Context, t *testing.T, conn *worker.Conn, jobID string) <-chan jobenvelope.Result {
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

// publishExtractJob puts a source blob and publishes an extract job for it.
func publishExtractJob(ctx context.Context, t *testing.T, conn *worker.Conn, job jobenvelope.Job, source []byte) {
	t.Helper()
	if source != nil {
		ref, err := conn.PutPayload(ctx, job.JobID+"/source", source)
		if err != nil {
			t.Fatalf("put source payload: %v", err)
		}
		job.Input.ObjectRef = ref
		job.Input.ByteSize = int64(len(source))
	}
	if err := jobenvelope.Validate(job); err != nil {
		t.Fatalf("invalid test job envelope: %v", err)
	}
	jb, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := conn.Publish(ctx, worker.SubjectExtractPrefix+"acceptance", jb); err != nil {
		t.Fatalf("publish extract job: %v", err)
	}
}

// podRestartCount returns the summed container restart count for pods matching
// the label selector in the platform namespace.
func podRestartCount(t *testing.T, selector string) int {
	t.Helper()
	out := kubectl(t, "-n", platformNS, "get", "pods", "-l", selector,
		"-o", "jsonpath={.items[*].status.containerStatuses[*].restartCount}")
	total := 0
	for _, f := range strings.Fields(out) {
		var n int
		if _, err := fmt.Sscanf(f, "%d", &n); err == nil {
			total += n
		}
	}
	return total
}

// clusterIP returns the ClusterIP of a service in the platform namespace.
func clusterIP(t *testing.T, svc string) string {
	t.Helper()
	return strings.TrimSpace(kubectl(t, "-n", platformNS, "get", "svc", svc, "-o", "jsonpath={.spec.clusterIP}"))
}

// coreDNSClusterIP returns the kube-dns service ClusterIP.
func coreDNSClusterIP(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("kubectl", "--context", "kind-tmi-platform",
		"-n", "kube-system", "get", "svc", "kube-dns",
		"-o", "jsonpath={.spec.clusterIP}").CombinedOutput()
	if err != nil {
		t.Fatalf("get kube-dns ClusterIP: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// -----------------------------------------------------------------------------
// 1. Crash isolation
// -----------------------------------------------------------------------------

func TestAcceptance_CrashIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn := dialNATS(ctx, t)

	// Baseline: NATS and the sibling chunk-embed worker must be unharmed by an
	// extractor crash. Record their restart counts before we kill the extractor.
	natsRestartsBefore := podRestartCount(t, "app=nats")
	chunkEmbedRestartsBefore := podRestartCount(t, "tmi.dev/component=tmi-chunk-embed")

	// Crash: forcibly delete every extractor pod (a SIGKILL-equivalent abrupt
	// termination — the e2e stand-in for a parser CVE / segfault taking the
	// process down).
	out := kubectl(t, "-n", platformNS, "delete", "pod",
		"-l", "tmi.dev/component=tmi-extractor", "--grace-period=0", "--force", "--ignore-not-found")
	t.Logf("crashed extractor pod(s): %s", strings.TrimSpace(out))

	// Blast radius: give the system a moment, then assert NATS and chunk-embed
	// neither restarted nor went unready as a consequence of the crash.
	time.Sleep(8 * time.Second)
	if got := podRestartCount(t, "app=nats"); got != natsRestartsBefore {
		t.Fatalf("NATS restarted as a result of the extractor crash: before=%d after=%d",
			natsRestartsBefore, got)
	}
	if got := podRestartCount(t, "tmi.dev/component=tmi-chunk-embed"); got != chunkEmbedRestartsBefore {
		t.Fatalf("chunk-embed restarted as a result of the extractor crash: before=%d after=%d",
			chunkEmbedRestartsBefore, got)
	}
	natsReady := kubectl(t, "-n", platformNS, "get", "pod", "nats-0",
		"-o", "jsonpath={.status.conditions[?(@.type==\"Ready\")].status}")
	if !strings.Contains(natsReady, "True") {
		t.Fatalf("NATS pod not Ready after extractor crash: %q", natsReady)
	}

	// Recovery: a job published AFTER the crash must still be processed. The
	// platform brings a replacement extractor up (KEDA scales on queue depth)
	// and the job routes to it — proving the crash did not wedge the pipeline.
	// Publishing post-crash (not before) makes "recovery" non-racy: no warm
	// pre-crash pod could have serviced this job.
	jid := jobID("crash-recovery")
	results := awaitResult(ctx, t, conn, jid)
	publishExtractJob(ctx, t, conn, jobenvelope.Job{
		JobID:       jid,
		ContentType: docxContentType,
		Limits:      jobenvelope.Limits{WallClock: jobenvelope.Duration(time.Nanosecond)},
	}, []byte("post-crash recovery payload"))

	select {
	case res := <-results:
		t.Logf("post-crash recovery result: status=%s reason=%s", res.Status, res.ReasonCode)
		if res.Status != jobenvelope.StatusFailed || res.ReasonCode != "extraction_limit:timeout" {
			t.Fatalf("unexpected recovery result: status=%s reason=%s", res.Status, res.ReasonCode)
		}
	case <-ctx.Done():
		t.Fatal("platform did not recover: no result after extractor crash (KEDA cold start may exceed the deadline)")
	}
}

// -----------------------------------------------------------------------------
// 2. Egress denial (verified against the real Calico CNI)
// -----------------------------------------------------------------------------

func TestAcceptance_EgressDenied(t *testing.T) {
	natsIP := clusterIP(t, "nats")
	dnsIP := coreDNSClusterIP(t)

	// Govern the probe with the SAME egress rules the controller rendered for
	// the real extractor, but under a probe-only label so the extractor's
	// ReplicaSet never adopts or evicts the probe. deriveEgressProbePolicy
	// copies .spec.egress / .spec.policyTypes from the live tmi-extractor
	// NetworkPolicy verbatim — so this verifies Calico enforces the actual
	// controller output, not a hand-written stand-in.
	const probeLabelKey, probeLabelVal = "tmi.dev/role", "egress-probe"
	deriveEgressProbePolicy(t, "egress-probe-np", probeLabelKey, probeLabelVal)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "networkpolicy", "egress-probe-np", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete egress-probe-np: %v\n%s", err, out)
		}
	}()

	// The probe pod runs a shell so we can observe each attempt (the real
	// extractor image is distroless — no shell).
	//
	// Each line prints "<name>: REACHABLE" or "<name>: blocked". -w 3 bounds
	// each attempt so a dropped (not refused) packet times out quickly.
	script := fmt.Sprintf(`
echo "nats: $(nc -w 3 -z %s 4222 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "dns_resolve: $(timeout 5 nslookup nats.tmi-platform.svc >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "metadata: $(nc -w 3 -z 169.254.169.254 80 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "coredns: $(nc -w 3 -z %s 53 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "external: $(nc -w 3 -z 1.1.1.1 443 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "done"
`, natsIP, dnsIP)

	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: egress-probe
  namespace: %s
  labels:
    tmi.dev/role: egress-probe
spec:
  restartPolicy: Never
  containers:
    - name: probe
      image: %s
      command: ["sh", "-c"]
      args: [%q]
`, platformNS, probeImage, script)

	applyStdin(t, manifest)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "pod", "egress-probe", "--grace-period=0", "--force", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete egress-probe: %v\n%s", err, out)
		}
	}()

	// Wait for the probe to finish its checks.
	kubectl(t, "-n", platformNS, "wait", "--for=jsonpath={.status.phase}=Succeeded",
		"pod/egress-probe", "--timeout=90s")
	logs := kubectl(t, "-n", platformNS, "logs", "egress-probe")
	t.Logf("egress-probe results:\n%s", logs)

	got := parseProbeLines(logs)
	// The one permitted destination.
	if got["nats"] != "REACHABLE" {
		t.Errorf("expected NATS reachable under egress:none (positive control), got %q", got["nats"])
	}
	// Everything else must be denied at L3 by the NetworkPolicy.
	for _, k := range []string{"dns_resolve", "metadata", "coredns", "external"} {
		if got[k] != "blocked" {
			t.Errorf("egress to %q must be blocked under egress:none, got %q", k, got[k])
		}
	}
}

// deriveEgressProbePolicy creates a NetworkPolicy named npName that reuses the
// live tmi-extractor policy's egress rules and policyTypes verbatim, retargeted
// to a probe-only pod label. This lets a probe pod be governed by the exact
// controller-rendered egress:none rules without sharing the extractor's
// component label (which the extractor ReplicaSet selects on).
func deriveEgressProbePolicy(t *testing.T, npName, labelKey, labelVal string) {
	t.Helper()
	deriveEgressProbePolicyFrom(t, "tmi-extractor", npName, labelKey, labelVal)
}

// podIP returns the pod IP of a pod in the platform namespace, failing the
// test if it is empty (pod not yet scheduled/assigned an IP).
func podIP(t *testing.T, name string) string {
	t.Helper()
	ip := strings.TrimSpace(kubectl(t, "-n", platformNS, "get", "pod", name, "-o", "jsonpath={.status.podIP}"))
	if ip == "" {
		t.Fatalf("pod %s has no IP", name)
	}
	return ip
}

// deriveEgressProbePolicyFrom creates a NetworkPolicy named npName that reuses
// the live srcPolicyName policy's egress rules and policyTypes verbatim,
// retargeted to a probe-only pod label. This verifies Calico enforces the exact
// controller-rendered egress rules (egress:none for tmi-extractor, or the
// allowlist rules for an egress:allowlist component) against a probe pod that
// does not share the source component's label.
func deriveEgressProbePolicyFrom(t *testing.T, srcPolicyName, npName, labelKey, labelVal string) {
	t.Helper()
	raw := kubectl(t, "-n", platformNS, "get", "networkpolicy", srcPolicyName, "-o", "json")
	var src map[string]any
	if err := json.Unmarshal([]byte(raw), &src); err != nil {
		t.Fatalf("unmarshal live %s NetworkPolicy: %v", srcPolicyName, err)
	}
	srcSpec, _ := src["spec"].(map[string]any)
	if srcSpec == nil {
		t.Fatalf("live %s NetworkPolicy has no spec", srcPolicyName)
	}
	np := map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata":   map[string]any{"name": npName, "namespace": platformNS},
		"spec": map[string]any{
			"podSelector": map[string]any{"matchLabels": map[string]any{labelKey: labelVal}},
			"policyTypes": srcSpec["policyTypes"],
			"egress":      srcSpec["egress"],
		},
	}
	out, err := json.Marshal(np)
	if err != nil {
		t.Fatalf("marshal derived NetworkPolicy: %v", err)
	}
	applyStdin(t, string(out))
}

// parseProbeLines turns "key: value" log lines into a map.
func parseProbeLines(logs string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(logs, "\n") {
		if i := strings.Index(line, ": "); i > 0 {
			m[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+2:])
		}
	}
	return m
}

// -----------------------------------------------------------------------------
// 2b. Allowlist egress (verified against the real Calico CNI) — issue #443
// -----------------------------------------------------------------------------

// TestAcceptance_AllowlistEgress proves the egress:allowlist posture enforces
// real, server-side L3 egress: a worker governed by a controller-rendered
// allowlist NetworkPolicy can reach ONLY its declared in-cluster target (a stub
// pod selected via clusterPeers) plus NATS, while the cloud metadata IP and an
// unrelated external host stay denied. This satisfies issue #443 acceptance
// criterion 1 (amended): reach the declared selector target and nothing else,
// metadata always denied, verified against Calico.
func TestAcceptance_AllowlistEgress(t *testing.T) {
	natsIP := clusterIP(t, "nats")

	// 1. In-cluster stub the allowlist will target: a busybox pod with a
	//    long-lived nc HTTP listener on 8443. We only need it reachable at L3,
	//    so a canned HTTP/1.1 200 is sufficient (the probe uses nc -z).
	const stubLabelKey, stubLabelVal = "tmi.dev/role", "embed-stub"
	stub := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: embed-stub
  namespace: %s
  labels:
    %s: %s
spec:
  restartPolicy: Never
  containers:
    - name: stub
      image: %s
      command: ["sh", "-c"]
      args: ["while true; do printf 'HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok' | nc -l -p 8443 -q 1; done"]
`, platformNS, stubLabelKey, stubLabelVal, probeImage)
	applyStdin(t, stub)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "pod", "embed-stub", "--grace-period=0", "--force", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete embed-stub: %v\n%s", err, out)
		}
	}()
	kubectl(t, "-n", platformNS, "wait", "--for=condition=Ready", "pod/embed-stub", "--timeout=90s")
	stubIP := podIP(t, "embed-stub")

	// 2. A live allowlist component whose NetworkPolicy targets the stub pods.
	//    We apply a real TMIComponent CR so the test exercises the actual
	//    controller-rendered output, not a hand-written stand-in.
	cr := fmt.Sprintf(`
apiVersion: tmi.dev/v1alpha1
kind: TMIComponent
metadata:
  name: allowlist-acc
  namespace: %s
spec:
  image: %s
  jobSubjects: ["jobs.acc.>"]
  inputMode: content-ref
  egress: allowlist
  allowlist:
    clusterPeers:
      - podSelector: { %s: %s }
        ports: [8443]
  resources:
    requests: { cpu: 50m, memory: 64Mi }
    limits: { cpu: 100m, memory: 128Mi }
  scaling: { minReplicas: 0, maxReplicas: 1, queueDepthTarget: 1 }
`, platformNS, probeImage, stubLabelKey, stubLabelVal)
	applyStdin(t, cr)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "tmicomponent", "allowlist-acc", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete allowlist-acc: %v\n%s", err, out)
		}
	}()
	// Wait for the controller to render the NetworkPolicy.
	kubectl(t, "-n", platformNS, "wait",
		"--for=jsonpath={.metadata.name}=allowlist-acc",
		"networkpolicy/allowlist-acc", "--timeout=60s")

	// 3. Clone the rendered allowlist policy onto a probe-only label so the probe
	//    pod is governed by the exact controller output without sharing the
	//    component's selector label.
	const probeLabelKey, probeLabelVal = "tmi.dev/role", "allowlist-probe"
	deriveEgressProbePolicyFrom(t, "allowlist-acc", "allowlist-probe-np", probeLabelKey, probeLabelVal)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "networkpolicy", "allowlist-probe-np", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete allowlist-probe-np: %v\n%s", err, out)
		}
	}()

	// 4. Probe: stub reachable, NATS reachable, metadata + unrelated external
	//    blocked. -w 3 bounds each attempt so a dropped packet times out fast.
	script := fmt.Sprintf(`
echo "stub: $(nc -w 3 -z %s 8443 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "nats: $(nc -w 3 -z %s 4222 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "metadata: $(nc -w 3 -z 169.254.169.254 80 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "external: $(nc -w 3 -z 1.1.1.1 443 >/dev/null 2>&1 && echo REACHABLE || echo blocked)"
echo "done"
`, stubIP, natsIP)
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: allowlist-probe
  namespace: %s
  labels:
    %s: %s
spec:
  restartPolicy: Never
  containers:
    - name: probe
      image: %s
      command: ["sh", "-c"]
      args: [%q]
`, platformNS, probeLabelKey, probeLabelVal, probeImage, script)
	applyStdin(t, manifest)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "pod", "allowlist-probe", "--grace-period=0", "--force", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete allowlist-probe: %v\n%s", err, out)
		}
	}()

	kubectl(t, "-n", platformNS, "wait", "--for=jsonpath={.status.phase}=Succeeded",
		"pod/allowlist-probe", "--timeout=90s")
	logs := kubectl(t, "-n", platformNS, "logs", "allowlist-probe")
	t.Logf("allowlist-probe results:\n%s", logs)

	got := parseProbeLines(logs)
	if got["stub"] != "REACHABLE" {
		t.Errorf("allowlist target (stub) must be reachable, got %q", got["stub"])
	}
	if got["nats"] != "REACHABLE" {
		t.Errorf("NATS must stay reachable, got %q", got["nats"])
	}
	for _, k := range []string{"metadata", "external"} {
		if got[k] != "blocked" {
			t.Errorf("%q must be blocked under egress:allowlist (clusterPeer-only), got %q", k, got[k])
		}
	}
}

// -----------------------------------------------------------------------------
// 3. Wall-clock timeout
// -----------------------------------------------------------------------------

func TestAcceptance_WallClockTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn := dialNATS(ctx, t)

	// A bounded (OOXML) extractor with a 1ns wall-clock budget hits the deadline
	// in ExtractWithDeadline before any real parsing, so the extractor publishes
	// a clean extraction_limit:timeout failure result. The payload bytes need
	// not be a valid docx — the deadline fires first.
	jid := jobID("timeout")
	results := awaitResult(ctx, t, conn, jid)
	publishExtractJob(ctx, t, conn, jobenvelope.Job{
		JobID:       jid,
		ContentType: docxContentType,
		Limits:      jobenvelope.Limits{WallClock: jobenvelope.Duration(time.Nanosecond)},
	}, []byte("payload whose parse never starts"))

	select {
	case res := <-results:
		t.Logf("timeout result: status=%s reason=%s", res.Status, res.ReasonCode)
		if res.Status != jobenvelope.StatusFailed {
			t.Fatalf("expected failed status, got %s", res.Status)
		}
		if res.ReasonCode != "extraction_limit:timeout" {
			t.Fatalf("expected reason extraction_limit:timeout, got %q", res.ReasonCode)
		}
	case <-ctx.Done():
		t.Fatal("no timeout result envelope before deadline (KEDA cold start may exceed it)")
	}
}

// -----------------------------------------------------------------------------
// 4. cgroup OOM kill
// -----------------------------------------------------------------------------

func TestAcceptance_CgroupOOMKill(t *testing.T) {
	// This validates the platform-level backstop the design relies on: a
	// workload that expands past its container memory limit is OOM-killed by
	// the kernel cgroup, not allowed to balloon (the in-code zip-bomb caps are
	// unit-tested separately in pkg/extract). The probe writes into a
	// memory-backed volume (counts against the cgroup) far beyond a tight 32Mi
	// limit, so the kernel must OOM-kill it.
	manifest := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: oom-probe
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: balloon
      image: %s
      command: ["sh", "-c"]
      # Fill a tmpfs (memory-backed) with 256MiB against a 32Mi limit.
      args: ["dd if=/dev/zero of=/balloon/fill bs=1M count=256"]
      resources:
        limits:
          memory: 32Mi
      volumeMounts:
        - name: balloon
          mountPath: /balloon
  volumes:
    - name: balloon
      emptyDir:
        medium: Memory
`, platformNS, probeImage)

	applyStdin(t, manifest)
	defer func() {
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", platformNS, "delete", "pod", "oom-probe", "--grace-period=0", "--force", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: delete oom-probe: %v\n%s", err, out)
		}
	}()

	// The container must terminate via OOMKilled, not complete the dd.
	deadline := time.Now().Add(90 * time.Second)
	var reason string
	for time.Now().Before(deadline) {
		reason = strings.TrimSpace(kubectl(t, "-n", platformNS, "get", "pod", "oom-probe",
			"-o", "jsonpath={.status.containerStatuses[0].state.terminated.reason}"))
		if reason != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("oom-probe terminated reason: %q", reason)
	if reason != "OOMKilled" {
		t.Fatalf("expected container to be OOMKilled by the cgroup memory limit, got terminated reason %q", reason)
	}
}

// -----------------------------------------------------------------------------
// 5. Dead-letter on an unprocessable job
// -----------------------------------------------------------------------------

func TestAcceptance_DeadLetter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn := dialNATS(ctx, t)

	// A core-NATS subscription on the MAX_DELIVERIES advisory subject is the
	// dead-letter trigger: when a job exhausts the extractor consumer's
	// MaxDeliver, JetStream fires this advisory. The monolith's DLQ producer
	// (issue #437) consumes it and records a failed result; that monolith leg
	// is unit/integration-tested separately. Here we prove the trigger fires
	// end-to-end in-cluster.
	raw, err := nats.Connect(natsLocalURL)
	if err != nil {
		t.Fatalf("raw NATS connect: %v", err)
	}
	defer raw.Close()
	sub, err := raw.SubscribeSync(worker.SubjectMaxDeliverAdvisory)
	if err != nil {
		t.Fatalf("subscribe MAX_DELIVERIES advisory: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	natsRestartsBefore := podRestartCount(t, "app=nats")

	// A poison job: a valid envelope whose source blob does not exist. The
	// extractor's GetPayload fails every delivery, so the consumer Naks until
	// MaxDeliver is exhausted and the advisory fires.
	jid := jobID("deadletter")
	publishExtractJob(ctx, t, conn, jobenvelope.Job{
		JobID:       jid,
		ContentType: docxContentType,
		Input:       jobenvelope.Input{ObjectRef: "does-not-exist/" + jid, ByteSize: 1},
	}, nil)

	// Await the advisory for our extractor stream/consumer.
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if time.Now().After(deadline) {
			t.Fatal("no MAX_DELIVERIES advisory before deadline (KEDA cold start may exceed it)")
		}
		msg, err := sub.NextMsg(5 * time.Second)
		if err != nil {
			continue // timeout waiting for next; loop until our deadline
		}
		var adv struct {
			Stream   string `json:"stream"`
			Consumer string `json:"consumer"`
		}
		if json.Unmarshal(msg.Data, &adv) != nil {
			continue
		}
		t.Logf("advisory: stream=%s consumer=%s", adv.Stream, adv.Consumer)
		if adv.Stream == worker.StreamNameFor("tmi-extractor") {
			break // dead-letter trigger fired for the extractor — criterion met
		}
	}

	// The rest of the system is unaffected by the dead-lettered job.
	if got := podRestartCount(t, "app=nats"); got != natsRestartsBefore {
		t.Fatalf("NATS restarted during dead-letter handling: before=%d after=%d", natsRestartsBefore, got)
	}
}
