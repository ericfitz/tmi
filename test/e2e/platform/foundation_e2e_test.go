//go:build e2e

// Package platform_e2e verifies the TMI Component Platform foundation against
// a real kind cluster with Calico (NetworkPolicy actually enforced).
package platform_e2e

import (
	"os/exec"
	"strings"
	"testing"
)

// kubectl runs kubectl against the kind-tmi-platform context and returns stdout.
func kubectl(t *testing.T, args ...string) string {
	t.Helper()
	full := append([]string{"--context", "kind-tmi-platform"}, args...)
	out, err := exec.Command("kubectl", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("kubectl %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestFoundation_ReconcilesChildObjects(t *testing.T) {
	// Assumes the cluster is up and the controller is deployed
	// (make e2e-platform-up performs that setup).
	const cr = `
apiVersion: tmi.dev/v1alpha1
kind: TMIComponent
metadata:
  name: e2e-probe
  namespace: tmi-platform
spec:
  image: cgr.dev/chainguard/static:latest
  jobSubjects: ["jobs.extract.probe"]
  inputMode: content-ref
  egress: none
  resources:
    limits: { cpu: "100m", memory: "64Mi" }
  scaling: { minReplicas: 0, maxReplicas: 2, queueDepthTarget: 5 }
`
	applyStdin(t, cr)
	defer func() {
		// Cleanup is best-effort: log a warning rather than t.Fatalf so a
		// cleanup hiccup cannot mask the test's real failure.
		cmd := exec.Command("kubectl", "--context", "kind-tmi-platform",
			"-n", "tmi-platform", "delete", "tmicomponent", "e2e-probe", "--ignore-not-found")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("cleanup warning: kubectl delete tmicomponent e2e-probe: %v\n%s", err, out)
		}
	}()

	// The controller must reconcile a Deployment and a NetworkPolicy.
	kubectl(t, "-n", "tmi-platform", "wait", "--for=create",
		"deployment/e2e-probe", "--timeout=60s")
	kubectl(t, "-n", "tmi-platform", "wait", "--for=create",
		"networkpolicy/e2e-probe", "--timeout=60s")

	// The NetworkPolicy must declare the Egress policy type (a real deny).
	np := kubectl(t, "-n", "tmi-platform", "get", "networkpolicy", "e2e-probe",
		"-o", "jsonpath={.spec.policyTypes}")
	if !strings.Contains(np, "Egress") {
		t.Fatalf("NetworkPolicy must enforce Egress, got policyTypes=%s", np)
	}
}

func applyStdin(t *testing.T, manifest string) {
	t.Helper()
	cmd := exec.Command("kubectl", "--context", "kind-tmi-platform", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("kubectl apply failed: %v\n%s", err, out)
	}
}
