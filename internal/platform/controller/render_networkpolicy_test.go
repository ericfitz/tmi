package controller

import (
	"net"
	"testing"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
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
	rule := np.Spec.Egress[0]
	if len(rule.Ports) != 1 || rule.Ports[0].Port.IntValue() != 4222 {
		t.Fatal("egress:none rule must permit only NATS port 4222")
	}
	// The rule MUST scope its To peer — a rule with ports but no To peer
	// matches ALL destinations on that port, which would defeat the sandbox.
	if len(rule.To) != 1 {
		t.Fatalf("NATS egress rule must have exactly one To peer (scoped to NATS), got %d", len(rule.To))
	}
	peer := rule.To[0]
	if peer.PodSelector == nil || peer.PodSelector.MatchLabels["app"] != "nats" {
		t.Fatal("NATS egress rule To peer must select the NATS pod (app=nats)")
	}
	if peer.NamespaceSelector == nil {
		t.Fatal("NATS egress rule To peer must scope to the NATS namespace")
	}
}

func TestRenderNetworkPolicy_AlwaysDeniesByDefault(t *testing.T) {
	np := RenderNetworkPolicy(namedComp("tmi-extractor", platformv1alpha1.EgressNone))
	// The policy must include the Egress policy type so an empty/limited
	// egress list is actually a deny, not an absence of policy.
	found := false
	for _, pt := range np.Spec.PolicyTypes {
		if pt == networkingv1.PolicyTypeEgress {
			found = true
		}
	}
	if !found {
		t.Fatal("NetworkPolicy must declare the Egress policy type")
	}
}

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

func TestRenderNetworkPolicy_FetchControlledIsNatsOnly(t *testing.T) {
	// fetch-controlled is RESERVED — until the T3 egress library lands it
	// renders NATS-only, the same as egress:none.
	np := RenderNetworkPolicy(namedComp("tmi-code-extractor", platformv1alpha1.EgressFetchControlled))
	if len(np.Spec.Egress) != 1 {
		t.Fatalf("egress:fetch-controlled (reserved) expected 1 egress rule (NATS only), got %d", len(np.Spec.Egress))
	}
}

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

func TestRenderNetworkPolicy_AllowlistEmptyClusterPeerSkipped(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{
		ClusterPeers: []platformv1alpha1.ClusterPeer{{}}, // both selectors empty
	}
	np := RenderNetworkPolicy(c)
	for _, r := range np.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock == nil && peer.PodSelector == nil && peer.NamespaceSelector == nil {
				t.Fatal("render must not emit an all-destinations peer for an empty clusterPeer")
			}
		}
	}
}

func TestRenderNetworkPolicy_AllowlistNeverRendersMetadataReachable(t *testing.T) {
	c := namedComp("tmi-chunk-embed", platformv1alpha1.EgressAllowlist)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{OpenInternet: true, CIDRs: []string{"10.1.2.0/24"}}
	np := RenderNetworkPolicy(c)
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
