# Server-side L3 egress for `egress: allowlist` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `egress: allowlist` posture enforce real, server-side L3 egress: the controller renders standard `NetworkPolicy` rules to operator-declared CIDRs / in-cluster selectors (or a broad "open-internet minus private/metadata" rule), so an allowlist worker can actually connect to its endpoint while everything else — especially the cloud metadata IP — stays denied.

**Architecture:** Replace the unenforceable `AllowlistEgress.Hosts []string` with enforceable targets (`CIDRs`, `ClusterPeers`, `OpenInternet`, `Ports`). The controller's `RenderNetworkPolicy` turns these into `ipBlock` / selector egress rules. No in-process guard, no proxy, no new component. Public-SaaS-by-FQDN exactness is delegated to operator infrastructure and documented.

**Tech Stack:** Go, `k8s.io/api/networking/v1`, controller-gen (CRD + deepcopy), kind+Calico e2e tier, `make` targets only.

**Design spec:** `docs/superpowers/specs/2026-06-09-egress-allowlist-l3-design.md`

**Conventions (read once):**
- Never run `go`/`go test` directly — use `make` targets. Relevant: `make generate-platform-crd`, `make test-platform` (controller unit tests via envtest), `make build-server`, `make lint`.
- Structured logging only (`internal/slogging`) — N/A here (no new logging).
- Commit messages: conventional commits, footer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`, and reference `#443`.
- The package `internal/platform/controller` must compile green after every task (dropping `Hosts` breaks existing references — Task 1 fixes them all in one shot).

---

### Task 1: Rework the CRD types (drop `Hosts`, add enforceable targets) and fix all references

**Files:**
- Modify: `api/platform/v1alpha1/tmicomponent_types.go` (the `AllowlistEgress` struct, ~lines 47-52)
- Regenerate: `api/platform/v1alpha1/zz_generated.deepcopy.go`, `config/crd/bases/tmi.dev_tmicomponents.yaml`
- Modify (fix compile): `internal/platform/controller/validation.go:20-23`, `internal/platform/controller/validation_test.go`, `internal/platform/controller/render_networkpolicy_test.go`

- [ ] **Step 1: Replace the `AllowlistEgress` struct and add `ClusterPeer`**

In `api/platform/v1alpha1/tmicomponent_types.go`, replace the existing `AllowlistEgress` (the struct with the `Hosts` field) with:

```go
// AllowlistEgress declares the server-side-enforceable egress targets for a
// component with egress: allowlist. At least one of CIDRs, ClusterPeers, or
// OpenInternet must be set (enforced by ValidateComponent). The cloud metadata
// IP (169.254.169.254) is never reachable regardless of what is declared here.
type AllowlistEgress struct {
	// CIDRs are stable destination ranges rendered as NetworkPolicy ipBlock
	// egress rules. Use for an in-cluster VM, a cloud private-endpoint subnet,
	// or a known API VIP. Validation rejects 0.0.0.0/0 and any range covering
	// the metadata IP.
	// +optional
	CIDRs []string `json:"cidrs,omitempty"`
	// ClusterPeers are in-cluster destinations rendered as namespace/pod-selector
	// egress rules (e.g. an in-cluster embedder Service's pods).
	// +optional
	ClusterPeers []ClusterPeer `json:"clusterPeers,omitempty"`
	// OpenInternet, when true, renders broad egress (0.0.0.0/0 minus RFC1918 and
	// minus the metadata IP) on the declared ports. Host-exactness is DELEGATED
	// to operator infrastructure (cloud egress firewall / managed-CNI FQDN
	// policy). The explicit escape hatch for a target that cannot be reduced to
	// a CIDR (public-SaaS model APIs).
	// +optional
	OpenInternet bool `json:"openInternet,omitempty"`
	// Ports the egress rules apply to. Defaults to TCP/443 when empty.
	// +optional
	Ports []int32 `json:"ports,omitempty"`
}

// ClusterPeer selects an in-cluster egress destination by namespace and pod
// labels. At least one of NamespaceSelector / PodSelector must be set.
type ClusterPeer struct {
	// NamespaceSelector matches destination namespaces by label. When empty,
	// the rule is not namespace-scoped (matches pods in any namespace by the
	// PodSelector).
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

- [ ] **Step 2: Regenerate deepcopy + CRD YAML**

Run: `make generate-platform-crd`
Expected: regenerates `zz_generated.deepcopy.go` (now with `CIDRs`/`ClusterPeers`/`Ports` deep-copy and a `ClusterPeer` DeepCopy) and updates `config/crd/bases/tmi.dev_tmicomponents.yaml` (the `allowlist` schema now shows `cidrs`/`clusterPeers`/`openInternet`/`ports`, no `hosts`). No manual edits to generated files.

- [ ] **Step 3: Fix the existing references so the package compiles**

`internal/platform/controller/validation.go` lines 20-23 currently read `len(c.Spec.Allowlist.Hosts)`. Temporarily change the body of the `EgressAllowlist` block to the final logic (Task 2 adds tests for it, but write it now so it compiles and is correct):

```go
	if c.Spec.Egress == platformv1alpha1.EgressAllowlist {
		if err := validateAllowlist(c.Spec.Allowlist); err != nil {
			return err
		}
	}
```

Add a stub `validateAllowlist` at the bottom of the file so it compiles (Task 2 fills it in via TDD):

```go
// validateAllowlist is fully implemented in Task 2.
func validateAllowlist(a *platformv1alpha1.AllowlistEgress) error {
	if a == nil || (len(a.CIDRs) == 0 && len(a.ClusterPeers) == 0 && !a.OpenInternet) {
		return fmt.Errorf("egress=allowlist requires at least one of spec.allowlist.cidrs, clusterPeers, or openInternet")
	}
	return nil
}
```

In `internal/platform/controller/validation_test.go`: the tests `TestValidateComponent_SourceLocatorWithAllowlistIsValid` and `TestValidateComponent_AllowlistWithHostsIsValid` set `Allowlist: &...{Hosts: ...}`. Change both to `&platformv1alpha1.AllowlistEgress{OpenInternet: true}`. Rename `TestValidateComponent_AllowlistWithHostsIsValid` → `TestValidateComponent_AllowlistWithOpenInternetIsValid` and `TestValidateComponent_AllowlistRequiresHosts` → `TestValidateComponent_AllowlistRequiresTarget`.

In `internal/platform/controller/render_networkpolicy_test.go`: `TestRenderNetworkPolicy_AllowlistAddsDNS` sets `&...{Hosts: []string{"api.openai.com"}}`. Change to `&platformv1alpha1.AllowlistEgress{OpenInternet: true}`. (This test asserts `len(Egress) == 2`; with `OpenInternet` the count becomes 3 — update the assertion to `>= 2` and keep the DNS-rule checks by finding the DNS rule explicitly. To keep this task small, change the test to locate the DNS rule by scanning for a rule whose ports include 53 rather than indexing `Egress[1]`.) Concretely replace the body with:

```go
func TestRenderNetworkPolicy_AllowlistAddsDNS(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{OpenInternet: true}
	np := RenderNetworkPolicy(c)
	var dns *networkingv1.NetworkPolicyEgressRule
	for i := range np.Spec.Egress {
		for _, p := range np.Spec.Egress[i].Ports {
			if p.Port.IntValue() == 53 {
				dns = &np.Spec.Egress[i]
			}
		}
	}
	if dns == nil {
		t.Fatal("egress:allowlist must include a DNS (port 53) rule")
	}
	if len(dns.Ports) != 2 {
		t.Fatalf("DNS rule expected 2 ports (UDP+TCP), got %d", len(dns.Ports))
	}
	if len(dns.To) != 1 {
		t.Fatalf("DNS egress rule must have exactly one To peer, got %d", len(dns.To))
	}
}
```

- [ ] **Step 4: Build to confirm the package compiles**

Run: `make build-server`
Expected: builds clean (the binary embeds the platform types; controller binary builds too). If `fmt` is not yet imported in `validation.go`, it already is (the file uses `fmt.Errorf`).

- [ ] **Step 5: Run controller unit tests (existing, adjusted)**

Run: `make test-platform`
Expected: PASS. The adjusted tests compile and pass against the stub `validateAllowlist` and the updated render test.

- [ ] **Step 6: Commit**

```bash
git add api/platform/v1alpha1/ config/crd/bases/tmi.dev_tmicomponents.yaml internal/platform/controller/
git commit -m "feat(platform): make egress:allowlist targets enforceable in the CRD (#443)

Drop unenforceable AllowlistEgress.Hosts; add CIDRs, ClusterPeers,
OpenInternet, Ports. Regenerate deepcopy and CRD YAML. Adjust existing
controller tests to the new schema.

Refs #443

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Allowlist validation (`validateAllowlist`)

**Files:**
- Modify: `internal/platform/controller/validation.go` (flesh out `validateAllowlist`)
- Test: `internal/platform/controller/validation_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/platform/controller/validation_test.go`:

```go
func allowlistComp(a *platformv1alpha1.AllowlistEgress) *platformv1alpha1.TMIComponent {
	c := comp(platformv1alpha1.EgressAllowlist, platformv1alpha1.InputContentRef)
	c.Spec.Allowlist = a
	return c
}

func TestValidateAllowlist_CIDRTargetIsValid(t *testing.T) {
	err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{CIDRs: []string{"10.1.2.0/24"}}))
	if err != nil {
		t.Fatalf("expected valid CIDR target, got %v", err)
	}
}

func TestValidateAllowlist_ClusterPeerTargetIsValid(t *testing.T) {
	err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{
		ClusterPeers: []platformv1alpha1.ClusterPeer{{PodSelector: map[string]string{"app": "embedder"}}},
	}))
	if err != nil {
		t.Fatalf("expected valid clusterPeer target, got %v", err)
	}
}

func TestValidateAllowlist_NoTargetRejected(t *testing.T) {
	if err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{})); err == nil {
		t.Fatal("expected error for allowlist with no target")
	}
}

func TestValidateAllowlist_BadCIDRRejected(t *testing.T) {
	if err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{CIDRs: []string{"not-a-cidr"}})); err == nil {
		t.Fatal("expected error for unparseable CIDR")
	}
}

func TestValidateAllowlist_DefaultRouteRejected(t *testing.T) {
	if err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{CIDRs: []string{"0.0.0.0/0"}})); err == nil {
		t.Fatal("expected error for 0.0.0.0/0 (use openInternet instead)")
	}
}

func TestValidateAllowlist_MetadataCIDRRejected(t *testing.T) {
	if err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{CIDRs: []string{"169.254.0.0/16"}})); err == nil {
		t.Fatal("expected error for a CIDR covering the metadata IP")
	}
}

func TestValidateAllowlist_BadPortRejected(t *testing.T) {
	if err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{OpenInternet: true, Ports: []int32{0}})); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestValidateAllowlist_EmptyClusterPeerRejected(t *testing.T) {
	if err := ValidateComponent(allowlistComp(&platformv1alpha1.AllowlistEgress{
		ClusterPeers: []platformv1alpha1.ClusterPeer{{}},
	})); err == nil {
		t.Fatal("expected error for a clusterPeer with no selector")
	}
}
```

- [ ] **Step 2: Run to verify failures**

Run: `make test-platform`
Expected: the new tests FAIL (stub `validateAllowlist` accepts bad CIDRs / ports / empty peers).

- [ ] **Step 3: Implement `validateAllowlist`**

Replace the stub in `internal/platform/controller/validation.go`. Ensure imports include `net` and `fmt`:

```go
// metadataIP is the cloud instance-metadata address that must never be
// reachable from any worker.
var metadataIP = net.ParseIP("169.254.169.254")

// validateAllowlist enforces that an egress:allowlist component declares at
// least one server-side-enforceable target and that no target widens egress to
// the default route or the metadata IP.
func validateAllowlist(a *platformv1alpha1.AllowlistEgress) error {
	if a == nil || (len(a.CIDRs) == 0 && len(a.ClusterPeers) == 0 && !a.OpenInternet) {
		return fmt.Errorf("egress=allowlist requires at least one of spec.allowlist.cidrs, clusterPeers, or openInternet")
	}
	for _, c := range a.CIDRs {
		_, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			return fmt.Errorf("egress=allowlist: invalid CIDR %q: %w", c, err)
		}
		if ones, _ := ipnet.Mask.Size(); ones == 0 {
			return fmt.Errorf("egress=allowlist: CIDR %q is the default route; use openInternet instead", c)
		}
		if ipnet.Contains(metadataIP) {
			return fmt.Errorf("egress=allowlist: CIDR %q covers the metadata IP %s, which must never be reachable", c, metadataIP)
		}
	}
	for i, p := range a.ClusterPeers {
		if len(p.NamespaceSelector) == 0 && len(p.PodSelector) == 0 {
			return fmt.Errorf("egress=allowlist: clusterPeers[%d] must set namespaceSelector and/or podSelector", i)
		}
		if err := validatePorts(p.Ports); err != nil {
			return fmt.Errorf("egress=allowlist: clusterPeers[%d]: %w", i, err)
		}
	}
	if err := validatePorts(a.Ports); err != nil {
		return fmt.Errorf("egress=allowlist: %w", err)
	}
	return nil
}

// validatePorts rejects out-of-range port numbers; an empty list is valid
// (rendering defaults it to TCP/443).
func validatePorts(ports []int32) error {
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("port %d out of range (1-65535)", p)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run to verify passes**

Run: `make test-platform`
Expected: PASS (all validation tests).

- [ ] **Step 5: Commit**

```bash
git add internal/platform/controller/validation.go internal/platform/controller/validation_test.go
git commit -m "feat(platform): validate enforceable egress:allowlist targets (#443)

Refs #443

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: NetworkPolicy rendering of allowlist targets

**Files:**
- Modify: `internal/platform/controller/render_networkpolicy.go` (allowlist branch + helpers)
- Test: `internal/platform/controller/render_networkpolicy_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/platform/controller/render_networkpolicy_test.go`:

```go
// findRuleToCIDR returns the egress rule whose first To peer is an ipBlock with
// the given CIDR, or nil.
func findRuleToCIDR(np *networkingv1.NetworkPolicy, cidr string) *networkingv1.NetworkPolicyEgressRule {
	for i := range np.Spec.Egress {
		for _, peer := range np.Spec.Egress[i].To {
			if peer.IPBlock != nil && peer.IPBlock.CIDR == cidr {
				return &np.Spec.Egress[i]
			}
		}
	}
	return nil
}

func TestRenderNetworkPolicy_AllowlistCIDR(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{CIDRs: []string{"10.1.2.0/24"}}
	np := RenderNetworkPolicy(c)
	rule := findRuleToCIDR(np, "10.1.2.0/24")
	if rule == nil {
		t.Fatal("expected an ipBlock egress rule for the declared CIDR")
	}
	if len(rule.Ports) != 1 || rule.Ports[0].Port.IntValue() != 443 {
		t.Fatalf("CIDR rule should default to TCP/443, got %+v", rule.Ports)
	}
}

func TestRenderNetworkPolicy_AllowlistClusterPeer(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{
		ClusterPeers: []platformv1alpha1.ClusterPeer{{PodSelector: map[string]string{"app": "embedder"}}},
	}
	np := RenderNetworkPolicy(c)
	found := false
	for _, r := range np.Spec.Egress {
		for _, peer := range r.To {
			if peer.PodSelector != nil && peer.PodSelector.MatchLabels["app"] == "embedder" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected a selector egress rule for the declared clusterPeer")
	}
}

func TestRenderNetworkPolicy_AllowlistOpenInternetExceptsPrivateAndMetadata(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{OpenInternet: true}
	np := RenderNetworkPolicy(c)
	rule := findRuleToCIDR(np, "0.0.0.0/0")
	if rule == nil {
		t.Fatal("openInternet must render a 0.0.0.0/0 ipBlock rule")
	}
	ex := rule.To[0].IPBlock.Except
	for _, want := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"} {
		seen := false
		for _, e := range ex {
			if e == want {
				seen = true
			}
		}
		if !seen {
			t.Errorf("openInternet ipBlock must except %s, got %v", want, ex)
		}
	}
}

func TestRenderNetworkPolicy_AllowlistCustomPorts(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{CIDRs: []string{"10.1.2.0/24"}, Ports: []int32{8443}}
	np := RenderNetworkPolicy(c)
	rule := findRuleToCIDR(np, "10.1.2.0/24")
	if rule == nil || len(rule.Ports) != 1 || rule.Ports[0].Port.IntValue() != 8443 {
		t.Fatalf("CIDR rule should honor declared port 8443, got %+v", rule)
	}
}

func TestRenderNetworkPolicy_AllowlistNeverRendersMetadataReachable(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{OpenInternet: true, CIDRs: []string{"10.1.2.0/24"}}
	np := RenderNetworkPolicy(c)
	// No rendered ipBlock may contain 169.254.169.254 without excepting it.
	for _, r := range np.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock == nil {
				continue
			}
			_, ipnet, err := net.ParseCIDR(peer.IPBlock.CIDR)
			if err != nil {
				continue
			}
			if ipnet.Contains(net.ParseIP("169.254.169.254")) {
				excepted := false
				for _, e := range peer.IPBlock.Except {
					if _, exnet, err := net.ParseCIDR(e); err == nil && exnet.Contains(net.ParseIP("169.254.169.254")) {
						excepted = true
					}
				}
				if !excepted {
					t.Fatalf("ipBlock %q makes the metadata IP reachable", peer.IPBlock.CIDR)
				}
			}
		}
	}
}
```

Add `"net"` to the test file imports.

- [ ] **Step 2: Run to verify failures**

Run: `make test-platform`
Expected: the new render tests FAIL (no CIDR/clusterPeer/openInternet rules rendered yet).

- [ ] **Step 3: Implement the allowlist rendering**

In `internal/platform/controller/render_networkpolicy.go`, replace the `if c.Spec.Egress == platformv1alpha1.EgressAllowlist { ... }` block (currently NATS+DNS only) with NATS+DNS plus the target rules:

```go
	if c.Spec.Egress == platformv1alpha1.EgressAllowlist {
		// DNS (port 53) so the worker can resolve its endpoint hostname. The
		// DNS rule is scoped to the cluster DNS pods, not all destinations.
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			To: []networkingv1.NetworkPolicyPeer{dnsPeer()},
			Ports: []networkingv1.NetworkPolicyPort{
				{Port: intOrStringPtr(intstr.FromInt(53)), Protocol: protoPtr(corev1.ProtocolUDP)},
				{Port: intOrStringPtr(intstr.FromInt(53)), Protocol: protoPtr(corev1.ProtocolTCP)},
			},
		})

		al := c.Spec.Allowlist
		if al != nil {
			defaultPorts := resolveEgressPorts(al.Ports)

			// One ipBlock egress rule per declared CIDR (in-cluster VM, cloud
			// private-endpoint subnet, or known VIP).
			for _, cidr := range al.CIDRs {
				np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
					To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr}}},
					Ports: defaultPorts,
				})
			}

			// One selector egress rule per declared in-cluster peer.
			for _, p := range al.ClusterPeers {
				peer := networkingv1.NetworkPolicyPeer{}
				if len(p.NamespaceSelector) > 0 {
					peer.NamespaceSelector = &metav1.LabelSelector{MatchLabels: p.NamespaceSelector}
				}
				if len(p.PodSelector) > 0 {
					peer.PodSelector = &metav1.LabelSelector{MatchLabels: p.PodSelector}
				}
				ports := defaultPorts
				if len(p.Ports) > 0 {
					ports = resolveEgressPorts(p.Ports)
				}
				np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
					To:    []networkingv1.NetworkPolicyPeer{peer},
					Ports: ports,
				})
			}

			// Broad egress minus private ranges and the metadata IP. Host
			// exactness is delegated to operator infrastructure.
			if al.OpenInternet {
				np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
					To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{
						CIDR:   "0.0.0.0/0",
						Except: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16"},
					}}},
					Ports: defaultPorts,
				})
			}
		}
	}
```

Add the port helper at the bottom of the file:

```go
// resolveEgressPorts maps declared port numbers to NetworkPolicyPort entries,
// defaulting to TCP/443 when none are declared.
func resolveEgressPorts(ports []int32) []networkingv1.NetworkPolicyPort {
	if len(ports) == 0 {
		return []networkingv1.NetworkPolicyPort{
			{Port: intOrStringPtr(intstr.FromInt(443)), Protocol: protoPtr(corev1.ProtocolTCP)},
		}
	}
	out := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, p := range ports {
		out = append(out, networkingv1.NetworkPolicyPort{
			Port:     intOrStringPtr(intstr.FromInt(int(p))),
			Protocol: protoPtr(corev1.ProtocolTCP),
		})
	}
	return out
}
```

(`metav1` is already imported in this file.)

- [ ] **Step 4: Run to verify passes**

Run: `make test-platform`
Expected: PASS (all render + validation tests).

- [ ] **Step 5: Lint + build**

Run: `make lint && make build-server`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/controller/render_networkpolicy.go internal/platform/controller/render_networkpolicy_test.go
git commit -m "feat(platform): render L3 egress rules for egress:allowlist targets (#443)

CIDRs -> ipBlock rules; clusterPeers -> selector rules; openInternet ->
0.0.0.0/0 minus RFC1918 and the metadata IP. Default port TCP/443.

Refs #443

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Migrate the shipped `tmi-chunk-embed.yml` manifest

**Files:**
- Modify: `deployments/k8s/platform/components/tmi-chunk-embed.yml`

- [ ] **Step 1: Replace the `allowlist` block**

Change the `allowlist:` stanza (currently `hosts: [api.openai.com]`) to:

```yaml
  egress: allowlist
  allowlist:
    # DEFAULT: openInternet delegates host-exactness to your cloud egress
    # firewall (AWS Network Firewall / Azure Firewall / GCP Cloud NGFW) or a
    # managed-CNI FQDN policy. Metadata (169.254.169.254) is always denied and
    # RFC1918 ranges are excepted (no in-cluster lateral movement).
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

Leave `config.TMI_EMBEDDING_BASE_URL` as-is (the human-readable endpoint lives here now, not in the allowlist).

- [ ] **Step 2: Validate the manifest parses against the new CRD**

Run: `kubectl --context kind-tmi-platform apply --dry-run=client -f deployments/k8s/platform/components/tmi-chunk-embed.yml` if a cluster is up; otherwise `kubectl apply --dry-run=client --validate=false -f ...` to confirm YAML is well-formed. (Non-blocking if no cluster — Task 5/6 exercise it for real.)
Expected: no schema error about unknown field `openInternet`/`ports`, no leftover `hosts`.

- [ ] **Step 3: Commit**

```bash
git add deployments/k8s/platform/components/tmi-chunk-embed.yml
git commit -m "build(platform): migrate tmi-chunk-embed to enforceable egress allowlist (#443)

Refs #443

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: e2e acceptance test — allowlist reaches its target, nothing else

**Files:**
- Modify: `test/e2e/platform/acceptance_e2e_test.go` (add `TestAcceptance_AllowlistEgress` + in-cluster stub helper)

**Context for the implementer:** This tier requires `make e2e-platform-up` (kind + Calico), the controller deployed, and is run via `make test-e2e-acceptance`. The existing `TestAcceptance_EgressDenied` is the template: it uses `deriveEgressProbePolicy(t, name, labelKey, labelVal)` to clone the *live* controller-rendered NetworkPolicy onto a probe-only pod label, then runs a probe pod whose shell script `nc`-tests reachability and prints `<name>: REACHABLE|blocked`, parsed by `parseProbeLines`. Reuse all of these. `clusterIP(t, "nats")`, `coreDNSClusterIP(t)`, `applyStdin`, `kubectl`, `platformNS`, and `probeImage` already exist in the package.

- [ ] **Step 1: Write the test**

The test must (a) stand up a tiny in-cluster HTTP stub with a known ClusterIP, (b) ensure a live allowlist NetworkPolicy exists that targets that stub's pods via a `clusterPeers` podSelector, (c) clone it onto a probe, (d) assert the stub is REACHABLE while metadata + an unrelated external host are blocked and NATS stays reachable.

Add to `test/e2e/platform/acceptance_e2e_test.go`:

```go
func TestAcceptance_AllowlistEgress(t *testing.T) {
	natsIP := clusterIP(t, "nats")

	// 1. In-cluster stub embedding endpoint: an httpbin-like server pod the
	//    allowlist will target, plus a Service to give it a stable name.
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
	defer kubectl(t, "-n", platformNS, "delete", "pod", "embed-stub", "--grace-period=0", "--force", "--ignore-not-found")
	kubectl(t, "-n", platformNS, "wait", "--for=condition=Ready", "pod/embed-stub", "--timeout=90s")
	stubIP := podIP(t, "embed-stub")

	// 2. A live allowlist component whose NetworkPolicy targets the stub pods.
	//    Apply a TMIComponent and let the controller render its NetworkPolicy,
	//    OR (simpler + hermetic) build the policy via RenderNetworkPolicy-shape
	//    by applying a TMIComponent CR. We apply a CR so the test verifies the
	//    real controller output.
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
  resources: { requests: { cpu: 50m, memory: 64Mi }, limits: { cpu: 100m, memory: 128Mi } }
  scaling: { minReplicas: 0, maxReplicas: 1, queueDepthTarget: 1 }
`, platformNS, probeImage, stubLabelKey, stubLabelVal)
	applyStdin(t, cr)
	defer kubectl(t, "-n", platformNS, "delete", "tmicomponent", "allowlist-acc", "--ignore-not-found")
	// Wait for the controller to render the NetworkPolicy.
	kubectl(t, "-n", platformNS, "wait", "--for=jsonpath={.metadata.name}=allowlist-acc",
		"networkpolicy/allowlist-acc", "--timeout=60s")

	// 3. Clone the rendered policy onto a probe-only label.
	const probeLabelKey, probeLabelVal = "tmi.dev/role", "allowlist-probe"
	deriveEgressProbePolicyFrom(t, "allowlist-acc", "allowlist-probe-np", probeLabelKey, probeLabelVal)
	defer kubectl(t, "-n", platformNS, "delete", "networkpolicy", "allowlist-probe-np", "--ignore-not-found")

	// 4. Probe: stub reachable, NATS reachable, metadata + unrelated external blocked.
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
	defer kubectl(t, "-n", platformNS, "delete", "pod", "allowlist-probe", "--grace-period=0", "--force", "--ignore-not-found")
	kubectl(t, "-n", platformNS, "wait", "--for=jsonpath={.status.phase}=Succeeded", "pod/allowlist-probe", "--timeout=90s")
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
```

- [ ] **Step 2: Add the helpers this test needs**

If `podIP` and `deriveEgressProbePolicyFrom` do not already exist in the package, add them near `deriveEgressProbePolicy`. `deriveEgressProbePolicy` currently hardcodes the source policy `tmi-extractor`; generalize by extracting a `...From(srcPolicy, npName, key, val)` variant and have the original delegate to it. Implement `podIP`:

```go
func podIP(t *testing.T, name string) string {
	t.Helper()
	ip := kubectl(t, "-n", platformNS, "get", "pod", name, "-o", "jsonpath={.status.podIP}")
	if ip == "" {
		t.Fatalf("pod %s has no IP", name)
	}
	return ip
}
```

For `deriveEgressProbePolicyFrom`, read the existing `deriveEgressProbePolicy` body and parameterize the source NetworkPolicy name (it reads the live policy's `.spec.egress`/`.spec.policyTypes` and re-applies under the probe label). Keep the original signature working by delegating: `deriveEgressProbePolicy(t, np, k, v)` calls `deriveEgressProbePolicyFrom(t, "tmi-extractor", np, k, v)`.

- [ ] **Step 3: Build the test package (compile check via vet through make)**

Run: `make build-server` (sanity) then confirm the e2e file compiles by running the e2e build: `go vet ./test/e2e/platform/` is disallowed directly — instead rely on `make test-e2e-platform` which builds with the `e2e` tag. If no cluster is available in this environment, at minimum ensure the file has no syntax errors by checking it compiles under the build tag locally is acceptable to defer to Step 4.

- [ ] **Step 4: Run the acceptance suite (requires cluster)**

Run:
```bash
make e2e-platform-up
make build-component-controller   # and deploy controller per existing e2e docs
make test-e2e-acceptance
```
Expected: `TestAcceptance_AllowlistEgress` PASS (stub+nats REACHABLE; metadata+external blocked). If the environment cannot bring up kind, mark this step blocked and note it for the human; the unit tier (Tasks 2-3) already proves the rendering.

- [ ] **Step 5: Commit**

```bash
git add test/e2e/platform/acceptance_e2e_test.go
git commit -m "test(platform): e2e acceptance for egress:allowlist L3 reachability (#443)

Refs #443

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: e2e workers — `TestWorkersE2E_PlaintextJob` completes against an in-cluster stub embedder

**Files:**
- Modify: `test/e2e/platform/workers_e2e_test.go`
- Modify: `deployments/k8s/platform/components/tmi-chunk-embed.yml` is the *shipped* default (openInternet); the e2e deploys an override CR pointing at the stub. Add the override + stub under `test/e2e/platform/` (inline manifest in the test, matching the Task 5 pattern).

**Context:** `TestWorkersE2E_PlaintextJob` currently accepts `StatusCompleted` OR `StatusFailed` because the embedding endpoint is unreachable. With an in-cluster stub embedder that returns an OpenAI-shaped `/v1/embeddings` response, and a chunk-embed CR whose `allowlist.clusterPeers` targets the stub, the job should reach `StatusCompleted`.

- [ ] **Step 1: Add an in-cluster stub embedding server**

The stub must answer `POST /v1/embeddings` with `{"data":[{"embedding":[0.0, ...],"index":0}],"model":"stub","object":"list"}`. Reuse `probeImage` only if it can run such a server; otherwise deploy a tiny inline server. Simplest reliable option: a small Python one-liner in a Chainguard python image, or extend `cmd/worker-probe` to add an `--embed-stub` mode. **Decision:** add an `--embed-stub` flag to `cmd/worker-probe/main.go` that serves the canned response on `:8443` (TLS optional — chunk-embed's `TMI_EMBEDDING_BASE_URL` can be `http://embed-stub.tmi-platform.svc:8443/v1` for the test). This keeps the stub in the probe image already loaded into kind.

Implement in `cmd/worker-probe/main.go`: when `--embed-stub` is passed, start an `http.Server` on `:8443` with a handler for `/v1/embeddings` returning the canned JSON (dimension matching `TMI_EMBEDDING_MODEL` is not validated by the stub; return a fixed-length vector, e.g. 1536 zeros for `text-embedding-3-small`). Use `internal/slogging` for any logging.

- [ ] **Step 2: Wire the e2e test to deploy the stub + an override chunk-embed CR**

In `test/e2e/platform/workers_e2e_test.go`, before publishing the plaintext job, deploy: the embed-stub pod+Service (label it, expose `:8443`), and apply a `tmi-chunk-embed` CR override with:
```yaml
  config:
    TMI_EMBEDDING_BASE_URL: http://embed-stub.tmi-platform.svc:8443/v1
    ...
  allowlist:
    clusterPeers:
      - podSelector: { tmi.dev/role: embed-stub }
        ports: [8443]
```
(Drop `openInternet`; the stub is in-cluster.) Ensure cleanup via `defer`.

- [ ] **Step 3: Tighten the assertion**

Change the accept-either check (the `res.Status != StatusCompleted && res.Status != StatusFailed` block) to require `StatusCompleted`, now that the embedding endpoint is reachable. Update the comment that explained why failure was tolerated.

- [ ] **Step 4: Run the worker e2e (requires cluster)**

Run:
```bash
make e2e-platform-up
make test-e2e-workers
```
Expected: `TestWorkersE2E_PlaintextJob` reaches `StatusCompleted`. If kind is unavailable here, mark blocked and note for the human.

- [ ] **Step 5: Build + lint (worker-probe change)**

Run: `make build-server && make lint`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/worker-probe/ test/e2e/platform/workers_e2e_test.go
git commit -m "test(platform): drive chunk-embed e2e to completion via in-cluster stub embedder (#443)

Refs #443

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] `make lint` — clean.
- [ ] `make build-server` — clean.
- [ ] `make test-platform` — all controller unit + validation + render tests pass.
- [ ] `make test-unit` — no regressions.
- [ ] e2e tiers (if a cluster is available): `make test-e2e-acceptance`, `make test-e2e-workers` pass.
- [ ] Run the `security-regression` skill (this change touches egress / NetworkPolicy — a security-sensitive path).
- [ ] Note on issue #443 the amended acceptance criterion (criterion 1 → "declared CIDR/selector targets, metadata always denied; public-SaaS FQDN delegated to infra").
- [ ] GitHub Wiki: add operator egress deployment guidance (three patterns + private-DNS footgun). Not in `docs/`.

## Notes / decisions baked in

- No DB changes — `oracle-db-admin` gate does not apply.
- No in-process egress guard and no proxy — the control is the CNI-enforced NetworkPolicy (server-side).
- `Hosts` removed from the CRD (breaking, acceptable pre-release).
- Shipped manifest defaults to `openInternet: true`; production hardening is documented.
