package controller

import (
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
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{Hosts: []string{"api.openai.com"}}
	np := RenderNetworkPolicy(c)
	// allowlist renders NATS + DNS = 2 egress rules.
	if len(np.Spec.Egress) != 2 {
		t.Fatalf("egress:allowlist expected 2 egress rules (NATS + DNS), got %d", len(np.Spec.Egress))
	}
	dns := np.Spec.Egress[1]
	if len(dns.Ports) != 2 {
		t.Fatalf("DNS rule expected 2 ports (UDP+TCP), got %d", len(dns.Ports))
	}
	for _, p := range dns.Ports {
		if p.Port.IntValue() != 53 {
			t.Fatalf("DNS rule port must be 53, got %d", p.Port.IntValue())
		}
	}
	// The DNS rule must scope its To peer to the cluster DNS pods.
	if len(dns.To) != 1 {
		t.Fatalf("DNS egress rule must have exactly one To peer, got %d", len(dns.To))
	}
	if dns.To[0].PodSelector == nil || dns.To[0].PodSelector.MatchLabels["k8s-app"] != "kube-dns" {
		t.Fatal("DNS egress rule To peer must select the cluster DNS pods (k8s-app=kube-dns)")
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
