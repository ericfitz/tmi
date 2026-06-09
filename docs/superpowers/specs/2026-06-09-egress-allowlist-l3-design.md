# Server-side L3 egress for `egress: allowlist` workers (#443)

**Date:** 2026-06-09
**Issue:** [#443](https://github.com/ericfitz/tmi/issues/443) — `fix(platform): egress:allowlist NetworkPolicy permits DNS but not L3 egress to allowlisted hosts`
**Status:** Design approved; ready for implementation plan.

## Problem

The controller-rendered Kubernetes `NetworkPolicy` for the `egress: allowlist`
posture opens egress to NATS (4222) and cluster DNS (53) **only** — it adds no
L3 egress rule for the allowlisted hosts. Because the policy sets
`policyTypes: [Egress]`, all other egress is denied. So an `egress: allowlist`
worker can *resolve* its allowlisted host but can never *connect* to it.

Concretely, `tmi-chunk-embed` (`egress: allowlist`, host `api.openai.com`) can
resolve `api.openai.com` but cannot open a TCP/443 connection to it, so the
chunk-embed stage cannot reach its embedding endpoint in-cluster, and
`TestWorkersE2E_PlaintextJob` times out on the embed step.

The current spec field — `spec.allowlist.hosts: []string` — is a list of bare
**hostnames**. Standard `networking.k8s.io/v1` NetworkPolicy has no FQDN field
(only `ipBlock` CIDRs, pod/namespace selectors, and ports), so those hostnames
are enforced nowhere. The "host-level allowlisting itself is enforced
in-worker" comment in `render_networkpolicy.go` describes work that was never
built.

## Constraints (decided during brainstorming)

- **CNI-portable.** The mechanism must work with any CNI that enforces standard
  `networkingv1.NetworkPolicy` (the kind+Calico e2e tier is the reference). No
  dependency on Cilium `toFQDNs`, Calico-Enterprise domain policy, or any
  managed-CNI native FQDN feature.
- **Must support external targets,** not only in-cluster ones.
- **No client-side controls.** An in-process egress guard inside the worker is
  rejected as the *primary* control: the process we are trying to contain (a
  parser CVE, prompt-injection, a supply-chain compromise) is the same process
  that would enforce the rule, so a compromise bypasses it. The control must be
  server-side (CNI-enforced NetworkPolicy, or infrastructure outside the
  workload).
- **No new TMI component.** Common deployment is cloud-managed Kubernetes
  (EKS/GKE/AKS); adding an always-on egress-proxy pod is cost, deployment, and
  support burden we will not take on for a single egress hop.

## Research findings that shaped the design

(Full citations captured in the brainstorming session.)

1. **In-cluster model endpoints** (e.g. a Nomic embedder) are a cluster
   pod/Service or an in-VPC VM — a pod/namespace selector or a stable private
   CIDR. Exactly expressible in standard NetworkPolicy.
2. **First-party cloud MaaS via a private endpoint** (AWS Bedrock PrivateLink,
   Azure OpenAI Private Endpoint, GCP Vertex Private Service Connect) allocates
   the endpoint IP **from the customer's own subnet CIDR**, so it is reachable
   by an `ipBlock` rule scoped to that subnet. Caveat: requires the operator to
   (a) provision the private endpoint and (b) override DNS (private DNS zone) so
   the hostname resolves to the private IP — without that, the name resolves to
   the public IP and the private path is silently bypassed. Allowlist the
   **subnet CIDR**, not the host IP (AWS does not guarantee a fixed endpoint IP).
3. **Public SaaS model APIs do not expose narrow, allowlistable CIDRs.**
   `api.openai.com` is Cloudflare-fronted; Gemini's public endpoint sits behind
   Google's huge shared frontends; Cohere/Voyage publish nothing. IP-allowlisting
   public SaaS is not viable — that case must be handled by FQDN/SNI enforcement
   at infrastructure outside the cluster (cloud egress firewall: AWS Network
   Firewall, Azure Firewall, GCP Cloud NGFW / Secure Web Proxy), or a managed-CNI
   native FQDN policy. TMI documents this; it does not build it.

## Design principle

> `egress: allowlist` is enforced **server-side** as L3 CIDR/selector rules in a
> standard NetworkPolicy. The controller renders exact egress to operator-declared,
> enforceable targets (an in-cluster selector, or a stable CIDR for an in-cluster
> VM / cloud private endpoint). The residual public-SaaS-by-FQDN case is an
> explicit operator **infrastructure** responsibility (cloud egress firewall /
> managed-CNI FQDN policy), documented — never faked with an in-process guard or
> a bundled proxy. The cloud metadata IP (`169.254.169.254`) is never reachable.

## Changes

### 1. CRD schema — `AllowlistEgress` becomes enforceable

`api/platform/v1alpha1/tmicomponent_types.go`. Replace the `Hosts` field with
enforceable targets:

```go
// AllowlistEgress declares the server-side-enforceable egress targets for a
// component with egress: allowlist. At least one of CIDRs, ClusterPeers, or
// OpenInternet must be set (enforced by ValidateComponent). The cloud metadata
// IP is never reachable regardless of what is declared here.
type AllowlistEgress struct {
	// CIDRs are stable destination ranges rendered as NetworkPolicy ipBlock
	// egress rules. Use for an in-cluster VM, a cloud private-endpoint subnet,
	// or a known API VIP. Validation rejects 0.0.0.0/0 and any range covering
	// the metadata IP (169.254.169.254).
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`

	// ClusterPeers are in-cluster destinations rendered as namespace/pod-selector
	// egress rules (e.g. an in-cluster embedder Service's pods).
	// +optional
	ClusterPeers []ClusterPeer `json:"clusterPeers,omitempty"`

	// OpenInternet, when true, renders broad egress (0.0.0.0/0 minus RFC1918 and
	// minus the metadata IP) on the declared ports. Host-exactness is DELEGATED
	// to operator infrastructure (cloud egress firewall / managed-CNI FQDN
	// policy). This is the explicit escape hatch for a target that cannot be
	// reduced to a CIDR (public-SaaS model APIs).
	// +optional
	OpenInternet bool `json:"openInternet,omitempty"`

	// Ports the egress rules apply to. Defaults to TCP/443 when empty.
	// +optional
	Ports []int32 `json:"ports,omitempty"`
}

// ClusterPeer selects an in-cluster egress destination by namespace and pod
// labels. At least one selector must be set.
type ClusterPeer struct {
	// NamespaceSelector matches destination namespaces by label. When empty,
	// the component's own namespace is used.
	// +optional
	NamespaceSelector map[string]string `json:"namespaceSelector,omitempty"`
	// PodSelector matches destination pods by label.
	// +optional
	PodSelector map[string]string `json:"podSelector,omitempty"`
	// Ports the rule applies to. Defaults to TCP/443 when empty.
	// +optional
	Ports []int32 `json:"ports,omitempty"`
}
```

`Hosts` is **removed**. The human-readable endpoint already lives in
`config.TMI_EMBEDDING_BASE_URL`; a field that looks like a control but isn't is
the trap that produced this bug. This is a breaking CRD change, acceptable
because the platform is unreleased (#414).

Regenerate `zz_generated.deepcopy.go` and the CRD YAML via
`make generate-platform-crd`.

### 2. Validation — `ValidateComponent`

`internal/platform/controller/validation.go`. Replace the current
`egress=allowlist` check (which requires `Allowlist.Hosts` non-empty) with:

- `egress=allowlist` requires `Allowlist != nil` **and at least one of**:
  non-empty `CIDRs`, non-empty `ClusterPeers`, or `OpenInternet == true`.
- Each `CIDRs` entry must parse as a CIDR (`net.ParseCIDR`), must not be
  `0.0.0.0/0` or `::/0`, and must not contain `169.254.169.254`.
- Each declared port (`Allowlist.Ports`, `ClusterPeer.Ports`) must be in
  `1..65535`.
- Each `ClusterPeer` must set at least one of `NamespaceSelector` / `PodSelector`.

### 3. NetworkPolicy rendering — `RenderNetworkPolicy`

`internal/platform/controller/render_networkpolicy.go`, allowlist branch:

- Keep the existing NATS egress rule (all postures).
- Keep the existing DNS egress rule (the worker still resolves its endpoint
  hostname).
- For each `CIDRs` entry: append an egress rule
  `To: [{IPBlock: {CIDR: entry}}]` on the resolved ports (declared, else TCP/443).
- For each `ClusterPeers` entry: append an egress rule with a
  `NamespaceSelector` and/or `PodSelector` peer on the resolved ports.
- If `OpenInternet`: append one egress rule
  `To: [{IPBlock: {CIDR: "0.0.0.0/0", Except: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"]}}]`
  on the resolved ports.

Metadata (`169.254.169.254`) is unreachable: it is inside `169.254.0.0/16`
(excepted from the broad rule) and forbidden in declared CIDRs by validation.

Helper: a small `resolvePorts([]int32) []networkingv1.NetworkPolicyPort`
defaulting to `[{TCP, 443}]`.

The control is entirely server-side (CNI-enforced). No in-process guard, no
proxy, no new component.

### 4. Worker — no security code

No worker change for the control. `tmi-chunk-embed` keeps reading
`TMI_EMBEDDING_BASE_URL`. An in-process allowlist is explicitly **not** the
control. (A future in-process check would be defense-in-depth only and is out
of scope for this issue.)

### 5. Shipped manifest + operator docs

`deployments/k8s/platform/components/tmi-chunk-embed.yml`: migrate to the new
schema. **Default: `openInternet: true`** so it works out-of-the-box against
public OpenAI, with a prominent comment block describing how to harden in
production:

```yaml
  egress: allowlist
  allowlist:
    # DEFAULT: openInternet delegates host-exactness to your cloud egress
    # firewall (AWS Network Firewall / Azure Firewall / GCP Cloud NGFW) or a
    # managed-CNI FQDN policy. Metadata (169.254.169.254) is always denied.
    #
    # HARDEN for production — replace openInternet with ONE of:
    #   cidrs: ["<private-endpoint-subnet-CIDR>"]   # cloud MaaS via PrivateLink/PSC
    #   clusterPeers: [{ podSelector: { app: <in-cluster-embedder> } }]
    # and ensure a private DNS override resolves the endpoint host to the
    # private IP (otherwise it resolves to the public IP and bypasses the
    # private path).
    openInternet: true
    ports: [443]
```

Wiki docs (per project policy, not `docs/`): the three deployment patterns
(in-cluster selector, cloud private endpoint + CIDR, public SaaS + infra FQDN
firewall), the private-DNS footgun, and the `openInternet` delegation semantics.

### 6. Test plan

- **Unit** — `internal/platform/controller/render_networkpolicy_test.go`:
  - `CIDRs` → ipBlock egress rules on the right ports.
  - `ClusterPeers` → namespace/pod selector egress rules.
  - `OpenInternet` → `0.0.0.0/0` with the RFC1918 + `169.254.0.0/16` excepts.
  - NATS + DNS always present; metadata never present.
- **Unit** — `internal/platform/controller/validation_test.go`:
  - allowlist with no target rejected; bad CIDR rejected; `0.0.0.0/0` rejected;
    metadata-covering CIDR rejected; out-of-range port rejected; empty
    ClusterPeer rejected.
- **e2e acceptance** — `test/e2e/platform/acceptance_e2e_test.go`, new
  `TestAcceptance_AllowlistEgress`:
  - Deploy a tiny in-cluster stub HTTP server pod with a known ClusterIP.
  - Govern a probe pod with the **actual** controller-rendered allowlist
    NetworkPolicy (reuse `deriveEgressProbePolicy`), targeting the stub.
  - Assert: stub **REACHABLE**, NATS **REACHABLE**, metadata **blocked**, an
    unrelated external host **blocked**. Satisfies acceptance criteria 1 & 3
    against Calico.
- **e2e workers** — `test/e2e/platform/workers_e2e_test.go`: point
  `tmi-chunk-embed` at the in-cluster stub embedding endpoint via
  `clusterPeers`/`cidrs` so `TestWorkersE2E_PlaintextJob` **completes**
  (criterion 2). Add a stub embedding server returning a canned vector in the
  shape the chunk-embed worker expects (OpenAI `/v1/embeddings` response).

### 7. Amended acceptance criterion

Issue #443 criterion 1 is amended from "reach allowlist host and nothing else,
verified against Calico" to:

> Reach the declared CIDR/selector targets and nothing else (metadata always
> denied), verified against Calico; public-SaaS-by-FQDN exactness is delegated to
> operator infrastructure and documented.

This will be noted on the issue.

## Out of scope

- `egress: fetch-controlled` posture (reserved; a later issue).
- Any in-process egress guard library (rejected as a primary control;
  defense-in-depth only, future).
- Building or shipping an egress proxy / egress gateway.
- CNI-specific FQDN policy rendering (Cilium/Calico-Enterprise/managed-CNI).

## Database / Oracle impact

None. This change touches the platform CRD, the controller's NetworkPolicy
rendering, Kubernetes manifests, and e2e tests only — no GORM models, no SQL, no
migrations. The `oracle-db-admin` review gate does not apply.

## Files touched

- `api/platform/v1alpha1/tmicomponent_types.go` — `AllowlistEgress` rework, new `ClusterPeer`.
- `api/platform/v1alpha1/zz_generated.deepcopy.go` — regenerated.
- `config/crd/bases/tmi.dev_tmicomponents.yaml` — regenerated.
- `internal/platform/controller/validation.go` — allowlist validation.
- `internal/platform/controller/render_networkpolicy.go` — allowlist rendering + port helper.
- `internal/platform/controller/render_networkpolicy_test.go` — unit tests.
- `internal/platform/controller/validation_test.go` — unit tests.
- `deployments/k8s/platform/components/tmi-chunk-embed.yml` — new schema, hardening docs.
- `test/e2e/platform/acceptance_e2e_test.go` — `TestAcceptance_AllowlistEgress` + in-cluster stub.
- `test/e2e/platform/workers_e2e_test.go` — point chunk-embed at in-cluster stub; stub embedding server.
- GitHub Wiki — operator egress deployment guidance (not `docs/`).
