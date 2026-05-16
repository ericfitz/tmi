# TMI Component Platform — Foundation (Plan 1 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Kubernetes-native foundation of the TMI Component Platform — the `TMIComponent` CRD, a custom controller that reconciles it into a Deployment + NetworkPolicy + KEDA ScaledObject + NATS JetStream wiring — with no worker logic and no monolith changes.

**Architecture:** A Kubebuilder-scaffolded controller (`controller-runtime`) lives in a new `cmd/component-controller/` and `internal/platform/` tree. It watches `TMIComponent` custom resources and, for each, renders four child objects. NATS JetStream and KEDA are installed as cluster dependencies via manifests under `deployments/k8s/platform/`. This plan produces a controller that is independently testable: apply a `TMIComponent` CR to a `kind` cluster, observe the child objects appear; delete the CR, observe them removed. No `tmi-extractor`, no `tmi-chunk-embed`, no monolith integration — those are Plans 2 and 3.

**Tech Stack:** Go 1.26, `sigs.k8s.io/controller-runtime`, Kubebuilder v4 scaffolding, `kind` + Calico for e2e, KEDA v2, NATS JetStream, `envtest` for controller unit tests.

---

## Scope

**This plan (Plan 1) delivers:**
- `deployments/k8s/platform/` — NATS JetStream and KEDA install manifests, plus a `kind` cluster config that uses Calico CNI.
- The `TMIComponent` CRD (Go API types + generated CRD YAML).
- The custom controller: reconciles a `TMIComponent` CR into a Deployment, a NetworkPolicy, a KEDA `ScaledObject`, and a JetStream stream/consumer.
- Controller unit tests (`envtest`) and a `kind`+Calico e2e smoke test.
- Makefile targets for building, testing, and deploying the controller.

**This plan does NOT deliver (later plans):**
- `cmd/extractor/`, `cmd/chunkembed/` worker binaries and logic — **Plan 2**.
- Job-envelope schema, Object Store wiring, `extraction_jobs` table, result-consumer, the `202` request-path change, OpenAPI changes — **Plan 3**.
- Any change to the monolith (`api/`, `cmd/server/`, `auth/`) — **Plan 3**.

**Framework decision (made here, not in the spec):** the controller uses `sigs.k8s.io/controller-runtime` with Kubebuilder v4 project scaffolding. Rationale: it is the de facto standard Go operator framework, pure Go (no extra runtime), and integrates with `envtest` for fast controller unit tests. Operator SDK was considered and rejected as an unnecessary layer on top of the same library.

---

## File Structure

| Path | Responsibility |
|---|---|
| `deployments/k8s/platform/kind-cluster.yml` | `kind` cluster config disabling the default CNI so Calico can be installed (NetworkPolicy enforcement) |
| `deployments/k8s/platform/calico.yml` | Calico install manifest (vendored, pinned version) |
| `deployments/k8s/platform/nats.yml` | NATS JetStream StatefulSet + Service, JetStream enabled |
| `deployments/k8s/platform/keda.yml` | KEDA v2 install manifest (vendored, pinned version) |
| `api/platform/v1alpha1/groupversion_info.go` | CRD group/version registration (`tmi.dev/v1alpha1`) |
| `api/platform/v1alpha1/tmicomponent_types.go` | `TMIComponent` Go API types — the CRD schema |
| `api/platform/v1alpha1/zz_generated.deepcopy.go` | Generated DeepCopy methods (do not hand-edit) |
| `config/crd/bases/tmi.dev_tmicomponents.yaml` | Generated CRD YAML (Kubebuilder convention path) |
| `internal/platform/controller/tmicomponent_controller.go` | The reconciler — watches `TMIComponent`, orchestrates rendering |
| `internal/platform/controller/render_deployment.go` | Renders the worker Deployment from a `TMIComponent` |
| `internal/platform/controller/render_networkpolicy.go` | Renders the NetworkPolicy from the `egress` posture |
| `internal/platform/controller/render_scaledobject.go` | Renders the KEDA `ScaledObject` |
| `internal/platform/controller/render_jetstream.go` | Ensures the JetStream stream/consumer for the component's subjects |
| `internal/platform/controller/validation.go` | Validates a `TMIComponent` (egress-vs-inputMode consistency) |
| `cmd/component-controller/main.go` | Controller entrypoint — manager setup, registers the reconciler |
| `internal/platform/controller/*_test.go` | `envtest` unit tests per file above |
| `test/e2e/platform/foundation_e2e_test.go` | `kind`+Calico e2e smoke test |
| `Makefile` | New targets (see Task 12) |

---

## Task 1: Kind cluster config with Calico CNI

**Files:**
- Create: `deployments/k8s/platform/kind-cluster.yml`
- Create: `deployments/k8s/platform/calico.yml`

- [ ] **Step 1: Write the kind cluster config**

Create `deployments/k8s/platform/kind-cluster.yml`:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: tmi-platform
networking:
  # Disable the default CNI (kindnet). kindnet does NOT enforce
  # NetworkPolicy; Calico (installed separately) does. Egress
  # isolation tests are meaningless without a real CNI.
  disableDefaultCNI: true
  podSubnet: "192.168.0.0/16"
nodes:
  - role: control-plane
  - role: worker
```

- [ ] **Step 2: Vendor the Calico manifest**

Run:
```bash
curl -fsSL https://raw.githubusercontent.com/projectcalico/calico/v3.28.0/manifests/calico.yaml \
  -o deployments/k8s/platform/calico.yml
```
Expected: `deployments/k8s/platform/calico.yml` created (~250 KB). Pin to v3.28.0 — do not use `latest`.

- [ ] **Step 3: Verify the cluster boots with Calico**

Run:
```bash
kind create cluster --config deployments/k8s/platform/kind-cluster.yml
kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/calico.yml
kubectl --context kind-tmi-platform wait --for=condition=Ready nodes --all --timeout=180s
```
Expected: both nodes reach `Ready` (they stay `NotReady` until Calico is applied — that confirms the default CNI is off).

- [ ] **Step 4: Tear down**

Run: `kind delete cluster --name tmi-platform`
Expected: cluster removed.

- [ ] **Step 5: Commit**

```bash
git add deployments/k8s/platform/kind-cluster.yml deployments/k8s/platform/calico.yml
git commit -m "build(platform): add kind cluster config with Calico CNI"
```

---

## Task 2: NATS JetStream install manifest

**Files:**
- Create: `deployments/k8s/platform/nats.yml`

- [ ] **Step 1: Write the NATS JetStream manifest**

Create `deployments/k8s/platform/nats.yml`. This is a minimal single-replica JetStream server for dev/e2e (production sizing is a separate concern):

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: tmi-platform
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: nats-config
  namespace: tmi-platform
data:
  nats.conf: |
    jetstream {
      store_dir: "/data/jetstream"
      max_memory_store: 256MB
      max_file_store: 2GB
    }
    http: 8222
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nats
  namespace: tmi-platform
spec:
  serviceName: nats
  replicas: 1
  selector:
    matchLabels: { app: nats }
  template:
    metadata:
      labels: { app: nats }
    spec:
      containers:
        - name: nats
          image: nats:2.10-alpine
          args: ["-c", "/etc/nats/nats.conf"]
          ports:
            - { name: client, containerPort: 4222 }
            - { name: monitor, containerPort: 8222 }
          volumeMounts:
            - { name: config, mountPath: /etc/nats }
            - { name: data, mountPath: /data/jetstream }
      volumes:
        - name: config
          configMap: { name: nats-config }
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: nats
  namespace: tmi-platform
spec:
  selector: { app: nats }
  ports:
    - { name: client, port: 4222, targetPort: 4222 }
    - { name: monitor, port: 8222, targetPort: 8222 }
```

- [ ] **Step 2: Verify NATS starts with JetStream enabled**

Run:
```bash
kind create cluster --config deployments/k8s/platform/kind-cluster.yml
kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/calico.yml
kubectl --context kind-tmi-platform wait --for=condition=Ready nodes --all --timeout=180s
kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/nats.yml
kubectl --context kind-tmi-platform -n tmi-platform wait --for=condition=Ready pod/nats-0 --timeout=120s
kubectl --context kind-tmi-platform -n tmi-platform exec nats-0 -- wget -qO- localhost:8222/jsz
```
Expected: the `/jsz` endpoint returns JSON containing `"streams":0` — confirms JetStream is enabled.

- [ ] **Step 3: Tear down**

Run: `kind delete cluster --name tmi-platform`

- [ ] **Step 4: Commit**

```bash
git add deployments/k8s/platform/nats.yml
git commit -m "build(platform): add NATS JetStream install manifest"
```

---

## Task 3: KEDA install manifest

**Files:**
- Create: `deployments/k8s/platform/keda.yml`

- [ ] **Step 1: Vendor the KEDA manifest**

Run:
```bash
curl -fsSL https://github.com/kedacore/keda/releases/download/v2.14.0/keda-2.14.0.yaml \
  -o deployments/k8s/platform/keda.yml
```
Expected: `deployments/k8s/platform/keda.yml` created. Pin to v2.14.0.

- [ ] **Step 2: Verify KEDA installs**

Run:
```bash
kind create cluster --config deployments/k8s/platform/kind-cluster.yml
kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/calico.yml
kubectl --context kind-tmi-platform wait --for=condition=Ready nodes --all --timeout=180s
kubectl --context kind-tmi-platform apply --server-side -f deployments/k8s/platform/keda.yml
kubectl --context kind-tmi-platform -n keda wait --for=condition=Available deployment/keda-operator --timeout=180s
```
Expected: `keda-operator` deployment becomes `Available`.

- [ ] **Step 3: Tear down**

Run: `kind delete cluster --name tmi-platform`

- [ ] **Step 4: Commit**

```bash
git add deployments/k8s/platform/keda.yml
git commit -m "build(platform): add KEDA v2.14 install manifest"
```

---

## Task 4: TMIComponent CRD API types — group/version registration

**Files:**
- Create: `api/platform/v1alpha1/groupversion_info.go`

- [ ] **Step 1: Add controller-runtime dependencies**

Run:
```bash
go get sigs.k8s.io/controller-runtime@v0.18.4
go get k8s.io/api@v0.30.0
go get k8s.io/apimachinery@v0.30.0
```
Expected: `go.mod` updated with the three modules.

- [ ] **Step 2: Write the group/version registration**

Create `api/platform/v1alpha1/groupversion_info.go`:

```go
// Package v1alpha1 contains the TMIComponent custom resource API for the
// TMI Component Platform.
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupVersion is the group/version for the TMI Component Platform API.
var GroupVersion = schema.GroupVersion{Group: "tmi.dev", Version: "v1alpha1"}

// SchemeBuilder registers the API types into a runtime scheme.
var SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

// AddToScheme adds the types in this group/version to a scheme.
var AddToScheme = SchemeBuilder.AddToScheme
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./api/platform/...`
Expected: builds with no output. (`TMIComponent` is not registered yet — that is Task 5.)

- [ ] **Step 4: Commit**

```bash
git add api/platform/v1alpha1/groupversion_info.go go.mod go.sum
git commit -m "feat(platform): register tmi.dev/v1alpha1 CRD group"
```

---

## Task 5: TMIComponent CRD API types — the TMIComponent type

**Files:**
- Modify: `api/platform/v1alpha1/groupversion_info.go` (register the type)
- Create: `api/platform/v1alpha1/tmicomponent_types.go`

- [ ] **Step 1: Write the TMIComponent type**

Create `api/platform/v1alpha1/tmicomponent_types.go`. These types are the CRD schema; the `+kubebuilder` markers drive CRD-YAML generation:

```go
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EgressPosture controls the NetworkPolicy the controller renders.
// +kubebuilder:validation:Enum=none;fetch-controlled;allowlist
type EgressPosture string

const (
	// EgressNone denies all egress except NATS. Used by parse-only workers.
	EgressNone EgressPosture = "none"
	// EgressFetchControlled allows guarded outbound fetch. RESERVED — not
	// exercised until the code extractor (a later issue).
	EgressFetchControlled EgressPosture = "fetch-controlled"
	// EgressAllowlist allows egress to specific hosts only.
	EgressAllowlist EgressPosture = "allowlist"
)

// InputMode declares how a component receives job input.
// +kubebuilder:validation:Enum=content-ref;source-locator
type InputMode string

const (
	// InputContentRef means the job carries an object_ref to bytes the
	// monolith already placed in the JetStream Object Store.
	InputContentRef InputMode = "content-ref"
	// InputSourceLocator means the job carries a URL the worker fetches.
	// RESERVED — not exercised until the code extractor.
	InputSourceLocator InputMode = "source-locator"
)

// SecretRef points to a key in a Kubernetes Secret. Secrets are NEVER
// inlined in a TMIComponent — only referenced.
type SecretRef struct {
	// Name is the logical name the worker uses to find this secret.
	Name string `json:"name"`
	// SecretName is the Kubernetes Secret object name.
	SecretName string `json:"secretName"`
	// SecretKey is the key within that Secret.
	SecretKey string `json:"secretKey"`
}

// AllowlistEgress lists hosts a component with egress: allowlist may reach.
type AllowlistEgress struct {
	// Hosts is the set of DNS names allowed for outbound traffic.
	Hosts []string `json:"hosts,omitempty"`
}

// ScalingSpec configures the KEDA ScaledObject.
type ScalingSpec struct {
	// +kubebuilder:validation:Minimum=0
	MinReplicas int32 `json:"minReplicas"`
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`
	// QueueDepthTarget is the JetStream pending-message count per replica
	// KEDA scales toward.
	// +kubebuilder:validation:Minimum=1
	QueueDepthTarget int32 `json:"queueDepthTarget"`
}

// ScratchVolume requests a capped, ephemeral emptyDir mounted writable.
type ScratchVolume struct {
	// MountPath is where the emptyDir is mounted in the worker container.
	MountPath string `json:"mountPath"`
	// SizeLimit caps the emptyDir (e.g. "512Mi"). Serialized as a string
	// in YAML; validated as a Kubernetes quantity at admission time.
	SizeLimit resource.Quantity `json:"sizeLimit"`
}

// TMIComponentSpec is the desired state of a component type.
type TMIComponentSpec struct {
	// Image is the worker container image.
	Image string `json:"image"`
	// JobSubjects are the JetStream subjects this component consumes.
	// +kubebuilder:validation:MinItems=1
	JobSubjects []string `json:"jobSubjects"`
	// InputMode declares how the component receives job input.
	InputMode InputMode `json:"inputMode"`
	// Egress is the network posture; drives the rendered NetworkPolicy.
	Egress EgressPosture `json:"egress"`
	// Allowlist is required when Egress is "allowlist", ignored otherwise.
	// +optional
	Allowlist *AllowlistEgress `json:"allowlist,omitempty"`
	// Config holds non-secret component-local config values, passed to the
	// worker as environment variables.
	// +optional
	Config map[string]string `json:"config,omitempty"`
	// SecretRefs reference Kubernetes Secrets wired into the worker pod.
	// +optional
	SecretRefs []SecretRef `json:"secretRefs,omitempty"`
	// Resources sets the worker pod CPU/memory limits (the cgroup caps).
	Resources corev1.ResourceRequirements `json:"resources"`
	// ScratchVolume optionally requests a writable emptyDir.
	// +optional
	ScratchVolume *ScratchVolume `json:"scratchVolume,omitempty"`
	// Scaling configures KEDA autoscaling.
	Scaling ScalingSpec `json:"scaling"`
}

// TMIComponentStatus is the observed state.
type TMIComponentStatus struct {
	// Conditions follows the standard Kubernetes condition pattern.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the .metadata.generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced,shortName=tmicomp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Egress",type=string,JSONPath=`.spec.egress`
// +kubebuilder:printcolumn:name="Input",type=string,JSONPath=`.spec.inputMode`

// TMIComponent declares a TMI Component Platform worker type.
type TMIComponent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TMIComponentSpec   `json:"spec,omitempty"`
	Status TMIComponentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TMIComponentList is a list of TMIComponent.
type TMIComponentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TMIComponent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TMIComponent{}, &TMIComponentList{})
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./api/platform/...`
Expected: **fails** — `TMIComponent does not implement runtime.Object (missing DeepCopyObject)`. DeepCopy methods are generated in Task 6. This failure is expected and confirms the next task is needed.

- [ ] **Step 3: Commit**

```bash
git add api/platform/v1alpha1/tmicomponent_types.go
git commit -m "feat(platform): add TMIComponent CRD API types"
```

---

## Task 6: Generate DeepCopy methods and CRD YAML

**Files:**
- Create: `api/platform/v1alpha1/zz_generated.deepcopy.go` (generated)
- Create: `config/crd/bases/tmi.dev_tmicomponents.yaml` (generated)

- [ ] **Step 1: Install controller-gen**

Run:
```bash
go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.3
```
Expected: `controller-gen` installed to `$(go env GOPATH)/bin`.

- [ ] **Step 2: Generate DeepCopy methods**

Run:
```bash
$(go env GOPATH)/bin/controller-gen object paths=./api/platform/v1alpha1/...
```
Expected: `api/platform/v1alpha1/zz_generated.deepcopy.go` created.

- [ ] **Step 3: Generate the CRD YAML**

Run:
```bash
$(go env GOPATH)/bin/controller-gen crd paths=./api/platform/v1alpha1/... output:crd:dir=config/crd/bases
```
Expected: `config/crd/bases/tmi.dev_tmicomponents.yaml` created.

- [ ] **Step 4: Verify the API package now builds**

Run: `go build ./api/platform/...`
Expected: builds with no output (the Task 5 failure is now resolved).

- [ ] **Step 5: Verify the CRD applies to a cluster**

Run:
```bash
kind create cluster --config deployments/k8s/platform/kind-cluster.yml
kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/calico.yml
kubectl --context kind-tmi-platform wait --for=condition=Ready nodes --all --timeout=180s
kubectl --context kind-tmi-platform apply -f config/crd/bases/tmi.dev_tmicomponents.yaml
kubectl --context kind-tmi-platform get crd tmicomponents.tmi.dev
kind delete cluster --name tmi-platform
```
Expected: `kubectl get crd` lists `tmicomponents.tmi.dev`.

- [ ] **Step 6: Commit**

```bash
git add api/platform/v1alpha1/zz_generated.deepcopy.go config/crd/bases/tmi.dev_tmicomponents.yaml
git commit -m "feat(platform): generate TMIComponent DeepCopy and CRD YAML"
```

---

## Task 7: TMIComponent validation

**Files:**
- Create: `internal/platform/controller/validation.go`
- Test: `internal/platform/controller/validation_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/platform/controller/validation_test.go`:

```go
package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
)

func comp(egress platformv1alpha1.EgressPosture, mode platformv1alpha1.InputMode) *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		Spec: platformv1alpha1.TMIComponentSpec{Egress: egress, InputMode: mode},
	}
}

func TestValidateComponent_SourceLocatorRequiresEgress(t *testing.T) {
	// source-locator + egress:none is a contradiction: a worker that must
	// fetch its own input cannot have all egress denied.
	err := ValidateComponent(comp(platformv1alpha1.EgressNone, platformv1alpha1.InputSourceLocator))
	if err == nil {
		t.Fatal("expected error for source-locator + egress:none, got nil")
	}
}

func TestValidateComponent_ContentRefWithNoneIsValid(t *testing.T) {
	err := ValidateComponent(comp(platformv1alpha1.EgressNone, platformv1alpha1.InputContentRef))
	if err != nil {
		t.Fatalf("expected content-ref + egress:none to be valid, got %v", err)
	}
}

func TestValidateComponent_AllowlistRequiresHosts(t *testing.T) {
	c := comp(platformv1alpha1.EgressAllowlist, platformv1alpha1.InputContentRef)
	err := ValidateComponent(c)
	if err == nil {
		t.Fatal("expected error for egress:allowlist with no allowlist hosts, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/controller/ -run TestValidateComponent -v`
Expected: FAIL — `undefined: ValidateComponent`.

- [ ] **Step 3: Write the implementation**

Create `internal/platform/controller/validation.go`:

```go
// Package controller implements the TMIComponent reconciler for the TMI
// Component Platform.
package controller

import (
	"fmt"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
)

// ValidateComponent checks a TMIComponent spec for internal consistency
// beyond what the CRD OpenAPI schema can express.
func ValidateComponent(c *platformv1alpha1.TMIComponent) error {
	if c.Spec.InputMode == platformv1alpha1.InputSourceLocator &&
		c.Spec.Egress == platformv1alpha1.EgressNone {
		return fmt.Errorf("inputMode=source-locator is incompatible with egress=none: " +
			"a worker that fetches its own input requires egress")
	}
	if c.Spec.Egress == platformv1alpha1.EgressAllowlist {
		if c.Spec.Allowlist == nil || len(c.Spec.Allowlist.Hosts) == 0 {
			return fmt.Errorf("egress=allowlist requires spec.allowlist.hosts to be non-empty")
		}
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/platform/controller/ -run TestValidateComponent -v`
Expected: PASS — all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/controller/validation.go internal/platform/controller/validation_test.go
git commit -m "feat(platform): validate TMIComponent egress/inputMode consistency"
```

---

## Task 8: Render the NetworkPolicy from the egress posture

**Files:**
- Create: `internal/platform/controller/render_networkpolicy.go`
- Test: `internal/platform/controller/render_networkpolicy_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/platform/controller/render_networkpolicy_test.go`:

```go
package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func namedComp(name string, egress platformv1alpha1.EgressPosture) *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tmi-platform"},
		Spec:       platformv1alpha1.TMIComponentSpec{Egress: egress},
	}
}

func TestRenderNetworkPolicy_NoneAllowsOnlyNats(t *testing.T) {
	np := RenderNetworkPolicy(namedComp("tmi-extractor", platformv1alpha1.EgressNone))
	if np.Name != "tmi-extractor" || np.Namespace != "tmi-platform" {
		t.Fatalf("unexpected name/namespace: %s/%s", np.Namespace, np.Name)
	}
	// egress:none renders exactly one egress rule — to NATS on 4222.
	if len(np.Spec.Egress) != 1 {
		t.Fatalf("egress:none expected 1 egress rule (NATS only), got %d", len(np.Spec.Egress))
	}
	if len(np.Spec.Egress[0].Ports) != 1 || np.Spec.Egress[0].Ports[0].Port.IntValue() != 4222 {
		t.Fatal("egress:none rule must permit only NATS port 4222")
	}
}

func TestRenderNetworkPolicy_AlwaysDeniesByDefault(t *testing.T) {
	np := RenderNetworkPolicy(namedComp("tmi-extractor", platformv1alpha1.EgressNone))
	// The policy must include the Egress policy type so an empty/limited
	// egress list is actually a deny, not an absence of policy.
	found := false
	for _, pt := range np.Spec.PolicyTypes {
		if pt == "Egress" {
			found = true
		}
	}
	if !found {
		t.Fatal("NetworkPolicy must declare the Egress policy type")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/controller/ -run TestRenderNetworkPolicy -v`
Expected: FAIL — `undefined: RenderNetworkPolicy`.

- [ ] **Step 3: Write the implementation**

Create `internal/platform/controller/render_networkpolicy.go`:

```go
package controller

import (
	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// natsPort is the JetStream client port every component is allowed to reach.
const natsPort = 4222

// componentPodLabels are the pod labels the controller stamps on worker pods
// and selects on in the NetworkPolicy.
func componentPodLabels(c *platformv1alpha1.TMIComponent) map[string]string {
	return map[string]string{"tmi.dev/component": c.Name}
}

// RenderNetworkPolicy builds the NetworkPolicy for a component from its
// egress posture. The Egress policy type is always set so a limited egress
// list is an enforced deny. The controller always renders this as a
// cluster-layer backstop, independent of any in-code egress guarding.
func RenderNetworkPolicy(c *platformv1alpha1.TMIComponent) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: c.Name, Namespace: c.Namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: componentPodLabels(c)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}

	// Every posture permits egress to NATS — without it a worker cannot
	// receive jobs or publish results.
	natsRule := networkingv1.NetworkPolicyEgressRule{
		Ports: []networkingv1.NetworkPolicyPort{
			{Port: ptr(intstr.FromInt(natsPort))},
		},
	}
	np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{natsRule}

	if c.Spec.Egress == platformv1alpha1.EgressAllowlist && c.Spec.Allowlist != nil {
		// allowlist adds DNS (port 53) so hostnames resolve; host-level
		// allowlisting itself is enforced in-worker. The NetworkPolicy
		// widens egress to DNS but no further at the L3 layer.
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Port: ptr(intstr.FromInt(53)), Protocol: protoPtr("UDP")},
				{Port: ptr(intstr.FromInt(53)), Protocol: protoPtr("TCP")},
			},
		})
	}
	// EgressFetchControlled is RESERVED. Until the T3 egress library lands
	// (a later issue) the controller renders the same NATS-only policy as
	// egress:none; the in-code guard is what relaxes it. No L3 widening here.

	return np
}

func ptr(v intstr.IntOrString) *intstr.IntOrString { return &v }

func protoPtr(p string) *corev1Protocol {
	proto := corev1Protocol(p)
	return &proto
}
```

- [ ] **Step 4: Fix the protocol import**

The `protoPtr` helper above references a placeholder. Replace the bottom of `render_networkpolicy.go` — delete the `protoPtr` function and the `corev1Protocol` reference, and instead add the real import. Change the import block to:

```go
import (
	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)
```

And replace the `protoPtr` function with:

```go
func protoPtr(p corev1.Protocol) *corev1.Protocol { return &p }
```

And in the allowlist block, change the two `protoPtr("UDP")` / `protoPtr("TCP")` calls to `protoPtr(corev1.ProtocolUDP)` and `protoPtr(corev1.ProtocolTCP)`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/platform/controller/ -run TestRenderNetworkPolicy -v`
Expected: PASS — both tests.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/controller/render_networkpolicy.go internal/platform/controller/render_networkpolicy_test.go
git commit -m "feat(platform): render NetworkPolicy from TMIComponent egress posture"
```

---

## Task 9: Render the worker Deployment

**Files:**
- Create: `internal/platform/controller/render_deployment.go`
- Test: `internal/platform/controller/render_deployment_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/platform/controller/render_deployment_test.go`:

```go
package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func deployComp() *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-extractor", Namespace: "tmi-platform"},
		Spec: platformv1alpha1.TMIComponentSpec{
			Image:     "tmi/extractor:dev",
			InputMode: platformv1alpha1.InputContentRef,
			Egress:    platformv1alpha1.EgressNone,
			Config:    map[string]string{"WALL_CLOCK_SECONDS": "30"},
			SecretRefs: []platformv1alpha1.SecretRef{
				{Name: "embed", SecretName: "embed-creds", SecretKey: "api-key"},
			},
		},
	}
}

func TestRenderDeployment_HardensPodSecurity(t *testing.T) {
	d := RenderDeployment(deployComp())
	pod := d.Spec.Template.Spec
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatal("pod must runAsNonRoot")
	}
	ctr := pod.Containers[0]
	if ctr.SecurityContext == nil {
		t.Fatal("container securityContext missing")
	}
	if ctr.SecurityContext.ReadOnlyRootFilesystem == nil || !*ctr.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("container must have readOnlyRootFilesystem=true (hard invariant)")
	}
	if ctr.SecurityContext.Capabilities == nil || len(ctr.SecurityContext.Capabilities.Drop) == 0 {
		t.Fatal("container must drop all capabilities")
	}
}

func TestRenderDeployment_ConfigBecomesEnvVars(t *testing.T) {
	d := RenderDeployment(deployComp())
	ctr := d.Spec.Template.Spec.Containers[0]
	var found bool
	for _, e := range ctr.Env {
		if e.Name == "WALL_CLOCK_SECONDS" && e.Value == "30" {
			found = true
		}
	}
	if !found {
		t.Fatal("spec.config entries must become container env vars")
	}
}

func TestRenderDeployment_SecretRefBecomesEnvFromSecret(t *testing.T) {
	d := RenderDeployment(deployComp())
	ctr := d.Spec.Template.Spec.Containers[0]
	var found bool
	for _, e := range ctr.Env {
		if e.Name == "embed" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			if e.ValueFrom.SecretKeyRef.Name == "embed-creds" &&
				e.ValueFrom.SecretKeyRef.Key == "api-key" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("secretRefs must become env vars sourced from the named Secret")
	}
}

func TestRenderDeployment_ScratchVolumeWhenRequested(t *testing.T) {
	c := deployComp()
	c.Spec.ScratchVolume = &platformv1alpha1.ScratchVolume{MountPath: "/scratch", SizeLimit: resource.MustParse("256Mi")}
	d := RenderDeployment(c)
	pod := d.Spec.Template.Spec
	if len(pod.Volumes) != 1 || pod.Volumes[0].EmptyDir == nil {
		t.Fatal("scratchVolume must render exactly one emptyDir volume")
	}
	if pod.Volumes[0].EmptyDir.SizeLimit == nil {
		t.Fatal("scratch emptyDir must be size-capped")
	}
	if _, ok := volumeMountByPath(pod.Containers[0], "/scratch"); !ok {
		t.Fatal("scratch volume must be mounted at the requested path")
	}
}

func volumeMountByPath(c corev1.Container, path string) (corev1.VolumeMount, bool) {
	for _, m := range c.VolumeMounts {
		if m.MountPath == path {
			return m, true
		}
	}
	return corev1.VolumeMount{}, false
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/controller/ -run TestRenderDeployment -v`
Expected: FAIL — `undefined: RenderDeployment`.

- [ ] **Step 3: Write the implementation**

Create `internal/platform/controller/render_deployment.go`:

```go
package controller

import (
	"sort"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// nonRootUID is the high UID worker containers run as.
const nonRootUID int64 = 65532

// RenderDeployment builds the worker Deployment for a TMIComponent.
// Pod hardening (readOnlyRootFilesystem, runAsNonRoot, all caps dropped,
// RuntimeDefault seccomp) is applied unconditionally — it is a hard
// platform invariant, not a per-component option.
func RenderDeployment(c *platformv1alpha1.TMIComponent) *appsv1.Deployment {
	labels := componentPodLabels(c)

	env := configEnv(c)
	env = append(env, secretEnv(c)...)

	ctr := corev1.Container{
		Name:      "worker",
		Image:     c.Spec.Image,
		Env:       env,
		Resources: c.Spec.Resources,
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem:   boolPtr(true),
			AllowPrivilegeEscalation: boolPtr(false),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
	}

	pod := corev1.PodSpec{
		Containers: []corev1.Container{ctr},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   boolPtr(true),
			RunAsUser:      int64Ptr(nonRootUID),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}

	if c.Spec.ScratchVolume != nil {
		// SizeLimit is already a resource.Quantity (validated at admission
		// time by the CRD schema), so no parsing is needed here.
		sizeLimit := c.Spec.ScratchVolume.SizeLimit
		pod.Volumes = []corev1.Volume{{
			Name: "scratch",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &sizeLimit},
			},
		}}
		pod.Containers[0].VolumeMounts = []corev1.VolumeMount{{
			Name: "scratch", MountPath: c.Spec.ScratchVolume.MountPath,
		}}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: c.Name, Namespace: c.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       pod,
			},
		},
	}
}

// configEnv turns spec.config into sorted env vars (sorted for deterministic
// output so reconcile does not thrash on map iteration order).
func configEnv(c *platformv1alpha1.TMIComponent) []corev1.EnvVar {
	keys := make([]string, 0, len(c.Spec.Config))
	for k := range c.Spec.Config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		env = append(env, corev1.EnvVar{Name: k, Value: c.Spec.Config[k]})
	}
	return env
}

// secretEnv turns secretRefs into env vars sourced from K8s Secrets.
func secretEnv(c *platformv1alpha1.TMIComponent) []corev1.EnvVar {
	env := make([]corev1.EnvVar, 0, len(c.Spec.SecretRefs))
	for _, ref := range c.Spec.SecretRefs {
		env = append(env, corev1.EnvVar{
			Name: ref.Name,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ref.SecretName},
					Key:                  ref.SecretKey,
				},
			},
		})
	}
	return env
}

func boolPtr(b bool) *bool    { return &b }
func int64Ptr(i int64) *int64 { return &i }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/platform/controller/ -run TestRenderDeployment -v`
Expected: PASS — all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/controller/render_deployment.go internal/platform/controller/render_deployment_test.go
git commit -m "feat(platform): render hardened worker Deployment from TMIComponent"
```

---

## Task 10: Render the KEDA ScaledObject

**Files:**
- Create: `internal/platform/controller/render_scaledobject.go`
- Test: `internal/platform/controller/render_scaledobject_test.go`

KEDA's `ScaledObject` is a CRD owned by KEDA. To avoid adding a KEDA Go-module dependency, the controller renders it as an `unstructured.Unstructured` object — controller-runtime can create/apply unstructured objects directly.

- [ ] **Step 1: Write the failing test**

Create `internal/platform/controller/render_scaledobject_test.go`:

```go
package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func scaledComp() *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-extractor", Namespace: "tmi-platform"},
		Spec: platformv1alpha1.TMIComponentSpec{
			JobSubjects: []string{"jobs.extract.ooxml"},
			Scaling: platformv1alpha1.ScalingSpec{
				MinReplicas: 0, MaxReplicas: 10, QueueDepthTarget: 5,
			},
		},
	}
}

func TestRenderScaledObject_TargetsTheDeployment(t *testing.T) {
	so := RenderScaledObject(scaledComp())
	if so.GetName() != "tmi-extractor" || so.GetNamespace() != "tmi-platform" {
		t.Fatalf("unexpected name/namespace: %s/%s", so.GetNamespace(), so.GetName())
	}
	if so.GetAPIVersion() != "keda.sh/v1alpha1" || so.GetKind() != "ScaledObject" {
		t.Fatalf("unexpected GVK: %s %s", so.GetAPIVersion(), so.GetKind())
	}
	ref, _, _ := unstructuredNestedString(so.Object, "spec", "scaleTargetRef", "name")
	if ref != "tmi-extractor" {
		t.Fatalf("scaleTargetRef.name = %q, want tmi-extractor", ref)
	}
}

func TestRenderScaledObject_UsesNatsJetStreamTrigger(t *testing.T) {
	so := RenderScaledObject(scaledComp())
	triggers, found, _ := unstructuredNestedSlice(so.Object, "spec", "triggers")
	if !found || len(triggers) != 1 {
		t.Fatalf("expected exactly one trigger, found=%v len=%d", found, len(triggers))
	}
	trig := triggers[0].(map[string]interface{})
	if trig["type"] != "nats-jetstream" {
		t.Fatalf("trigger type = %v, want nats-jetstream", trig["type"])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/platform/controller/ -run TestRenderScaledObject -v`
Expected: FAIL — `undefined: RenderScaledObject`, `undefined: unstructuredNestedString`, `undefined: unstructuredNestedSlice`.

- [ ] **Step 3: Write the implementation**

Create `internal/platform/controller/render_scaledobject.go`:

```go
package controller

import (
	"strconv"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// natsMonitoringEndpoint is the in-cluster NATS monitoring URL KEDA's
// nats-jetstream scaler queries for stream/consumer pending counts.
const natsMonitoringEndpoint = "http://nats.tmi-platform.svc:8222"

// RenderScaledObject builds the KEDA ScaledObject for a component as an
// unstructured object, avoiding a KEDA Go-module dependency. KEDA scales
// the worker Deployment on JetStream pending-message depth.
func RenderScaledObject(c *platformv1alpha1.TMIComponent) *unstructured.Unstructured {
	// One subject -> one stream/consumer pair (see render_jetstream.go).
	streamName := streamNameFor(c)
	consumerName := consumerNameFor(c)

	so := &unstructured.Unstructured{}
	so.SetAPIVersion("keda.sh/v1alpha1")
	so.SetKind("ScaledObject")
	so.SetName(c.Name)
	so.SetNamespace(c.Namespace)
	so.Object["spec"] = map[string]interface{}{
		"scaleTargetRef": map[string]interface{}{"name": c.Name},
		"minReplicaCount": int64(c.Spec.Scaling.MinReplicas),
		"maxReplicaCount": int64(c.Spec.Scaling.MaxReplicas),
		"triggers": []interface{}{
			map[string]interface{}{
				"type": "nats-jetstream",
				"metadata": map[string]interface{}{
					"natsServerMonitoringEndpoint": natsMonitoringEndpoint,
					"account":                      "$G",
					"stream":                       streamName,
					"consumer":                     consumerName,
					"lagThreshold":                 strconv.Itoa(int(c.Spec.Scaling.QueueDepthTarget)),
				},
			},
		},
	}
	return so
}

// unstructuredNestedString is a thin wrapper used by tests and callers.
func unstructuredNestedString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	return unstructured.NestedString(obj, fields...)
}

// unstructuredNestedSlice is a thin wrapper used by tests and callers.
func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	return unstructured.NestedSlice(obj, fields...)
}
```

- [ ] **Step 4: Run the test to verify it fails differently**

Run: `go test ./internal/platform/controller/ -run TestRenderScaledObject -v`
Expected: FAIL — `undefined: streamNameFor`, `undefined: consumerNameFor`. These are defined in Task 11. This confirms Task 11 must precede a passing run.

- [ ] **Step 5: Commit the work-in-progress**

```bash
git add internal/platform/controller/render_scaledobject.go internal/platform/controller/render_scaledobject_test.go
git commit -m "feat(platform): render KEDA ScaledObject for TMIComponent (wip: needs jetstream naming)"
```

---

## Task 11: JetStream stream/consumer naming and ensure-logic

**Files:**
- Create: `internal/platform/controller/render_jetstream.go`
- Test: `internal/platform/controller/render_jetstream_test.go`

- [ ] **Step 1: Add the NATS Go client dependency**

Run:
```bash
go get github.com/nats-io/nats.go@v1.36.0
```
Expected: `go.mod` updated.

- [ ] **Step 2: Write the failing test**

Create `internal/platform/controller/render_jetstream_test.go`:

```go
package controller

import (
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func jsComp(name string, subjects ...string) *platformv1alpha1.TMIComponent {
	return &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tmi-platform"},
		Spec:       platformv1alpha1.TMIComponentSpec{JobSubjects: subjects},
	}
}

func TestStreamNameFor_IsDeterministicAndUppercase(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml")
	// JetStream stream names cannot contain dots; must be stable across reconciles.
	got := streamNameFor(c)
	if got != streamNameFor(c) {
		t.Fatal("streamNameFor must be deterministic")
	}
	if got == "" {
		t.Fatal("streamNameFor must not be empty")
	}
	for _, r := range got {
		if r == '.' || r == ' ' {
			t.Fatalf("stream name %q contains an illegal character", got)
		}
	}
}

func TestConsumerNameFor_IsDeterministic(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml")
	got := consumerNameFor(c)
	if got != consumerNameFor(c) {
		t.Fatal("consumerNameFor must be deterministic")
	}
	if got == "" {
		t.Fatal("consumerNameFor must not be empty")
	}
}

func TestStreamConfigFor_BindsAllJobSubjects(t *testing.T) {
	c := jsComp("tmi-extractor", "jobs.extract.ooxml", "jobs.extract.pdf")
	cfg := StreamConfigFor(c)
	if len(cfg.Subjects) != 2 {
		t.Fatalf("stream config must bind all job subjects, got %d", len(cfg.Subjects))
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/platform/controller/ -run 'TestStreamNameFor|TestConsumerNameFor|TestStreamConfigFor' -v`
Expected: FAIL — `undefined: streamNameFor`, `undefined: consumerNameFor`, `undefined: StreamConfigFor`.

- [ ] **Step 4: Write the implementation**

Create `internal/platform/controller/render_jetstream.go`:

```go
package controller

import (
	"strings"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/nats-io/nats.go"
)

// streamNameFor returns the JetStream stream name for a component.
// JetStream stream names cannot contain dots or spaces, so the component
// name is upcased and sanitized. Deterministic across reconciles.
func streamNameFor(c *platformv1alpha1.TMIComponent) string {
	return "TMI_" + sanitizeName(c.Name)
}

// consumerNameFor returns the durable JetStream consumer name for a component.
func consumerNameFor(c *platformv1alpha1.TMIComponent) string {
	return sanitizeName(c.Name) + "_CONSUMER"
}

func sanitizeName(s string) string {
	up := strings.ToUpper(s)
	return strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(up)
}

// StreamConfigFor returns the JetStream stream configuration that binds all
// of a component's job subjects.
func StreamConfigFor(c *platformv1alpha1.TMIComponent) *nats.StreamConfig {
	return &nats.StreamConfig{
		Name:      streamNameFor(c),
		Subjects:  append([]string(nil), c.Spec.JobSubjects...),
		Retention: nats.WorkQueuePolicy, // each job delivered to exactly one worker
		Storage:   nats.FileStorage,
	}
}
```

- [ ] **Step 5: Run the JetStream tests to verify they pass**

Run: `go test ./internal/platform/controller/ -run 'TestStreamNameFor|TestConsumerNameFor|TestStreamConfigFor' -v`
Expected: PASS — all three.

- [ ] **Step 6: Run the ScaledObject tests (now unblocked)**

Run: `go test ./internal/platform/controller/ -run TestRenderScaledObject -v`
Expected: PASS — both tests (Task 10 is now complete).

- [ ] **Step 7: Commit**

```bash
git add internal/platform/controller/render_jetstream.go internal/platform/controller/render_jetstream_test.go go.mod go.sum
git commit -m "feat(platform): add JetStream stream/consumer naming for TMIComponent"
```

---

## Task 12: The reconciler

**Files:**
- Create: `internal/platform/controller/tmicomponent_controller.go`
- Test: `internal/platform/controller/tmicomponent_controller_test.go`

The reconciler test uses `envtest` — a real API server + etcd, no kubelet. It verifies that creating a `TMIComponent` produces the child Deployment and NetworkPolicy. (JetStream stream creation needs a running NATS and is covered by the e2e test in Task 14, not here.)

- [ ] **Step 1: Add the envtest setup dependency**

Run:
```bash
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.18.4
$(go env GOPATH)/bin/setup-envtest use 1.30.0 --bin-dir ./bin/k8s
```
Expected: `setup-envtest` downloads the 1.30.0 control-plane binaries into `./bin/k8s`.

- [ ] **Step 2: Write the failing test**

Create `internal/platform/controller/tmicomponent_controller_test.go`:

```go
package controller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestReconcile_CreatesChildObjects(t *testing.T) {
	// Locate the control-plane binaries fetched by setup-envtest.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		assets, _ := filepath.Abs("../../../bin/k8s")
		entries, err := os.ReadDir(assets)
		if err != nil || len(entries) == 0 {
			t.Skip("envtest assets not found; run: setup-envtest use 1.30.0 --bin-dir ./bin/k8s")
		}
		os.Setenv("KUBEBUILDER_ASSETS", filepath.Join(assets, entries[0].Name()))
	}

	crdPath, _ := filepath.Abs("../../../config/crd/bases")
	env := &envtest.Environment{CRDDirectoryPaths: []string{crdPath}}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest start: %v", err)
	}
	defer func() { _ = env.Stop() }()

	if err := platformv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}
	k8s, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	ctx := context.Background()
	comp := &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-extractor", Namespace: "default"},
		Spec: platformv1alpha1.TMIComponentSpec{
			Image:       "tmi/extractor:dev",
			JobSubjects: []string{"jobs.extract.ooxml"},
			InputMode:   platformv1alpha1.InputContentRef,
			Egress:      platformv1alpha1.EgressNone,
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			},
			Scaling: platformv1alpha1.ScalingSpec{MinReplicas: 0, MaxReplicas: 10, QueueDepthTarget: 5},
		},
	}
	if err := k8s.Create(ctx, comp); err != nil {
		t.Fatalf("create TMIComponent: %v", err)
	}

	r := &TMIComponentReconciler{Client: k8s, Scheme: scheme.Scheme}
	key := types.NamespacedName{Name: "tmi-extractor", Namespace: "default"}
	if _, err := r.ReconcileComponent(ctx, key); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	// The child Deployment must exist.
	var dep appsv1.Deployment
	if err := waitGet(ctx, k8s, key, &dep); err != nil {
		t.Fatalf("expected child Deployment: %v", err)
	}
	// The child NetworkPolicy must exist.
	var np networkingv1.NetworkPolicy
	if err := waitGet(ctx, k8s, key, &np); err != nil {
		t.Fatalf("expected child NetworkPolicy: %v", err)
	}
	// The child ScaledObject must exist (KEDA CRD, read as unstructured).
	so := &unstructured.Unstructured{}
	so.SetAPIVersion("keda.sh/v1alpha1")
	so.SetKind("ScaledObject")
	if err := waitGet(ctx, k8s, key, so); err != nil {
		t.Fatalf("expected child ScaledObject: %v", err)
	}

	// Reconcile a second time: the reconciler MUST be idempotent. This is
	// the regression guard for the Create-then-Update resourceVersion bug —
	// the second pass exercises the AlreadyExists -> Update path.
	if _, err := r.ReconcileComponent(ctx, key); err != nil {
		t.Fatalf("second reconcile must succeed (idempotency): %v", err)
	}
	if err := waitGet(ctx, k8s, key, &appsv1.Deployment{}); err != nil {
		t.Fatalf("Deployment missing after second reconcile: %v", err)
	}
}

func waitGet(ctx context.Context, c client.Client, key types.NamespacedName, obj client.Object) error {
	var lastErr error
	for i := 0; i < 20; i++ {
		if lastErr = c.Get(ctx, key, obj); lastErr == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return lastErr
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `KUBEBUILDER_ASSETS="" go test ./internal/platform/controller/ -run TestReconcile -v`
Expected: FAIL — `undefined: TMIComponentReconciler`.

- [ ] **Step 4: Write the reconciler**

Create `internal/platform/controller/tmicomponent_controller.go`:

```go
package controller

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TMIComponentReconciler reconciles a TMIComponent into its child objects:
// a Deployment, a NetworkPolicy, and a KEDA ScaledObject. JetStream stream
// creation is handled at component startup against a live NATS and is not
// part of object reconciliation.
type TMIComponentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is the controller-runtime entrypoint.
func (r *TMIComponentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.ReconcileComponent(ctx, req.NamespacedName)
}

// ReconcileComponent renders and applies the child objects for one component.
// Split out from Reconcile so tests can drive it directly.
func (r *TMIComponentReconciler) ReconcileComponent(ctx context.Context, key types.NamespacedName) (ctrl.Result, error) {
	var comp platformv1alpha1.TMIComponent
	if err := r.Get(ctx, key, &comp); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil // deleted; owner refs garbage-collect children
		}
		return ctrl.Result{}, err
	}

	if err := ValidateComponent(&comp); err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid TMIComponent %s: %w", key, err)
	}

	dep := RenderDeployment(&comp)
	if err := controllerutil.SetControllerReference(&comp, dep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, dep); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply Deployment: %w", err)
	}

	np := RenderNetworkPolicy(&comp)
	if err := controllerutil.SetControllerReference(&comp, np, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, np); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply NetworkPolicy: %w", err)
	}

	so := RenderScaledObject(&comp)
	if err := controllerutil.SetControllerReference(&comp, so, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, so); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply ScaledObject: %w", err)
	}

	return ctrl.Result{}, nil
}

// apply creates the object, or updates it in place if it already exists.
// On update it first fetches the live object to capture its resourceVersion
// (required by the API server for optimistic concurrency), then carries that
// resourceVersion onto the freshly-rendered object before the Update call.
func (r *TMIComponentReconciler) apply(ctx context.Context, obj client.Object) error {
	err := r.Create(ctx, obj)
	if err == nil {
		return nil
	}
	if !errors.IsAlreadyExists(err) {
		return err
	}
	// Object exists: fetch the live copy to obtain its resourceVersion,
	// then update the rendered object in place.
	existing := obj.DeepCopyObject().(client.Object)
	key := client.ObjectKeyFromObject(obj)
	if err := r.Get(ctx, key, existing); err != nil {
		return err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	return r.Update(ctx, obj)
}

// SetupWithManager registers the reconciler and its owned child types.
func (r *TMIComponentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Owns the typed children so the controller re-reconciles on child drift.
	// The KEDA ScaledObject is unstructured and not watched here; drift
	// correction for it is tracked as a follow-up.
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.TMIComponent{}).
		Owns(&appsv1.Deployment{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Complete(r)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `KUBEBUILDER_ASSETS="" go test ./internal/platform/controller/ -run TestReconcile -v`
Expected: PASS. (If the test skips with "envtest assets not found", re-run Step 1.)

- [ ] **Step 6: Run the full controller package test suite**

Run: `KUBEBUILDER_ASSETS="" go test ./internal/platform/controller/ -v`
Expected: PASS — all tests from Tasks 7–12.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/controller/tmicomponent_controller.go internal/platform/controller/tmicomponent_controller_test.go go.mod go.sum
git commit -m "feat(platform): add TMIComponent reconciler"
```

---

## Task 13: Controller entrypoint

**Files:**
- Create: `cmd/component-controller/main.go`

- [ ] **Step 1: Write the entrypoint**

Create `cmd/component-controller/main.go`. Logging uses `internal/slogging` per project rules — the standard `log` package is forbidden:

```go
// Command component-controller runs the TMI Component Platform controller.
package main

import (
	"os"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/ericfitz/tmi/internal/platform/controller"
	"github.com/ericfitz/tmi/internal/slogging"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

func main() {
	logger := slogging.Get()

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		logger.Error("add client-go scheme: %v", err)
		os.Exit(1)
	}
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		logger.Error("add platform scheme: %v", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme})
	if err != nil {
		logger.Error("create manager: %v", err)
		os.Exit(1)
	}

	reconciler := &controller.TMIComponentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		logger.Error("setup reconciler: %v", err)
		os.Exit(1)
	}

	logger.Info("starting TMI Component Platform controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("manager exited: %v", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./cmd/component-controller/`
Expected: builds with no output.

- [ ] **Step 3: Verify the slogging signature matches**

Run: `rg "func.*Error\(" internal/slogging/logger.go`
Expected: confirms `Error` accepts a format string + args. If the real signature differs (e.g. structured key/value), adjust the four `logger.Error(...)` calls to match the actual `internal/slogging` API before committing — do not change `internal/slogging`.

- [ ] **Step 4: Commit**

```bash
git add cmd/component-controller/main.go
git commit -m "feat(platform): add component-controller entrypoint"
```

---

## Task 14: Kind + Calico end-to-end smoke test

**Files:**
- Create: `test/e2e/platform/foundation_e2e_test.go`

This test is the gated e2e tier. It is guarded by a build tag so it does not run in the per-PR `make test-unit` tier.

- [ ] **Step 1: Write the e2e test**

Create `test/e2e/platform/foundation_e2e_test.go`:

```go
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
	defer kubectl(t, "-n", "tmi-platform", "delete", "tmicomponent", "e2e-probe", "--ignore-not-found")

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
```

- [ ] **Step 2: Verify the test compiles under the e2e tag**

Run: `go vet -tags e2e ./test/e2e/platform/`
Expected: no output (compiles; not executed without a cluster).

- [ ] **Step 3: Commit**

```bash
git add test/e2e/platform/foundation_e2e_test.go
git commit -m "test(platform): add kind+Calico foundation e2e smoke test"
```

---

## Task 15: Makefile targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Inspect the Makefile's structure**

Run: `rg -n "^\.PHONY|^test-unit:|^build-server:" Makefile | head -20`
Expected: shows how targets and `.PHONY` are declared, so the new targets match house style.

- [ ] **Step 2: Add the platform targets**

Append to `Makefile` (adjust indentation to tabs — Make requires tab-indented recipes):

```makefile
## --- TMI Component Platform (Plan 1: foundation) ---

GOPATH_BIN := $(shell go env GOPATH)/bin

.PHONY: build-component-controller generate-platform-crd test-platform e2e-platform-up e2e-platform-down test-e2e-platform

build-component-controller:  ## Build the component-controller binary
	go build -o bin/component-controller ./cmd/component-controller/

generate-platform-crd:  ## Regenerate TMIComponent DeepCopy methods and CRD YAML
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.3
	$(GOPATH_BIN)/controller-gen object paths=./api/platform/v1alpha1/...
	$(GOPATH_BIN)/controller-gen crd paths=./api/platform/v1alpha1/... output:crd:dir=config/crd/bases

test-platform:  ## Run platform controller unit tests (downloads envtest assets if needed)
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@if [ -f $(CURDIR)/bin/k8s/1.30.0-darwin-arm64/kube-apiserver ] || [ -f $(CURDIR)/bin/k8s/1.30.0-linux-amd64/kube-apiserver ] || [ -f $(CURDIR)/bin/k8s/1.30.0-linux-arm64/kube-apiserver ]; then \
		ASSETS=$$(ls -d $(CURDIR)/bin/k8s/1.30.0-* 2>/dev/null | head -1); \
		echo "Using cached envtest binaries at $$ASSETS"; \
		KUBEBUILDER_ASSETS="$$ASSETS" go test ./internal/platform/... ./api/platform/...; \
	else \
		KUBEBUILDER_ASSETS="$$($(GOPATH_BIN)/setup-envtest use 1.30.0 --bin-dir $(CURDIR)/bin/k8s -p path)" \
		go test ./internal/platform/... ./api/platform/...; \
	fi

e2e-platform-up:  ## Create the kind cluster and install platform dependencies (NATS, KEDA, CRD)
	kind create cluster --config deployments/k8s/platform/kind-cluster.yml
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/calico.yml
	kubectl --context kind-tmi-platform wait --for=condition=Ready nodes --all --timeout=180s
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/nats.yml
	kubectl --context kind-tmi-platform apply --server-side -f deployments/k8s/platform/keda.yml
	kubectl --context kind-tmi-platform apply -f config/crd/bases/tmi.dev_tmicomponents.yaml

e2e-platform-down:  ## Delete the kind platform cluster
	kind delete cluster --name tmi-platform

test-e2e-platform:  ## Run the platform e2e tests (requires e2e-platform-up + controller deployed)
	go test -tags e2e ./test/e2e/platform/ -v
```

The `test-platform` recipe prefers envtest binaries already cached under `bin/k8s/` and only falls back to a network `setup-envtest use` download when none are present — `setup-envtest`'s remote index fetch can time out in some environments.

- [ ] **Step 3: Verify the targets parse**

Run: `make build-component-controller`
Expected: `bin/component-controller` built.

Run: `make test-platform`
Expected: platform unit tests pass.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "build(platform): add Makefile targets for the component platform"
```

---

## Task 16: Lint, full build, and final verification

**Files:** none (verification only)

- [ ] **Step 1: Run gofmt**

Run: `gofmt -l api/platform/ internal/platform/ cmd/component-controller/`
Expected: no output (all files formatted). If any file is listed, run `gofmt -w` on it and amend the relevant commit.

- [ ] **Step 2: Run the linter**

Run: `make lint`
Expected: passes. Fix any issues in the new files. The generated `zz_generated.deepcopy.go` may produce warnings — if `make lint` flags only that file, confirm whether the project's lint config excludes generated files (it excludes `api/api.go`); if not, add `api/platform/v1alpha1/zz_generated.deepcopy.go` to the same exclusion.

- [ ] **Step 3: Build everything**

Run: `make build-server && make build-component-controller`
Expected: both binaries build — confirms the new packages did not break the monolith build.

- [ ] **Step 4: Run the monolith unit tests**

Run: `make test-unit`
Expected: passes — confirms no regression. The new `api/platform/...` package adds no monolith behavior, so this should be unaffected.

- [ ] **Step 5: Run the platform tests**

Run: `make test-platform`
Expected: all platform unit tests pass.

- [ ] **Step 6: Full e2e dry run**

Run:
```bash
make e2e-platform-up
make build-component-controller
# Deploy the controller into the cluster — load the image and run it.
# For this smoke run, run the controller out-of-cluster against the kind context:
KUBECONFIG="$(kind get kubeconfig --name tmi-platform > /tmp/tmi-kc && echo /tmp/tmi-kc)" \
  ./bin/component-controller &
CONTROLLER_PID=$!
sleep 5
make test-e2e-platform
kill $CONTROLLER_PID
make e2e-platform-down
```
Expected: `test-e2e-platform` passes — the controller reconciles the probe `TMIComponent` into a Deployment and an enforced-Egress NetworkPolicy.

- [ ] **Step 7: Final commit if any fixes were made**

```bash
git add -A
git commit -m "chore(platform): lint and formatting fixes for the component platform foundation"
```

---

## Self-Review

**Spec coverage** — this plan covers the foundation subsystem of the #347 spec:
- `TMIComponent` CRD — Tasks 4–6. ✓
- Custom controller reconciling Deployment + NetworkPolicy + ScaledObject — Tasks 8–12. ✓
- KEDA install + ScaledObject — Tasks 3, 10. ✓
- NATS JetStream install + stream naming — Tasks 2, 11. ✓
- Three-valued egress posture (`none`/`fetch-controlled`/`allowlist`) — Task 5 (types), Task 8 (NetworkPolicy rendering). `fetch-controlled` is reserved, rendered same as `none` until the T3 library — explicitly noted. ✓
- Read-only-root invariant + capped `emptyDir` scratch — Task 9. ✓
- `deployments/k8s/platform/` manifests — Tasks 1–3. ✓
- Three-tier test model: process-mode unit tests (`envtest`, Task 12) and gated kind+Calico e2e (Task 14). ✓
- `egress`-vs-`inputMode` validation — Task 7. ✓

**Deliberately out of scope** (Plans 2–3, stated in the Scope section): worker binaries, job-envelope schema, Object Store, `extraction_jobs` table, result-consumer, `202` request-path, OpenAPI changes, monolith integration. No gap — these are sequenced into later plans.

**Placeholder scan** — no "TBD"/"implement later". Task 8 Step 4 deliberately corrects placeholder code introduced in Step 3 (a guided fix, with full replacement code shown), and Task 10 Steps 4–5 deliberately commit a known-failing WIP that Task 11 completes — both are explicit, sequenced, and the engineer is told exactly what to expect.

**Type consistency** — checked: `streamNameFor`/`consumerNameFor`/`StreamConfigFor` (Task 11) are referenced by `RenderScaledObject` (Task 10); `componentPodLabels` (Task 8) is reused by `RenderDeployment` (Task 9); `TMIComponentReconciler` fields (`Client`, `Scheme`) are consistent between the reconciler (Task 12) and `main.go` (Task 13); `ValidateComponent`/`RenderDeployment`/`RenderNetworkPolicy`/`RenderScaledObject` signatures match their reconciler call sites.

**Known follow-up** — Task 16 Step 6 runs the controller out-of-cluster for the smoke test. Deploying the controller *as a pod* (its own image + RBAC manifest) is intentionally deferred: it is not needed to validate reconciliation logic and belongs with the worker-image work in Plan 2. Noted here so it is not lost.
