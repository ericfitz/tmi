package controller

import (
	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// natsPort is the JetStream client port every component is allowed to reach.
const natsPort = 4222

// platformNamespace is the namespace the shared NATS server runs in.
const platformNamespace = "tmi-platform"

// componentPodLabels are the pod labels the controller stamps on worker pods
// and selects on in the NetworkPolicy.
func componentPodLabels(c *platformv1alpha1.TMIComponent) map[string]string {
	return map[string]string{"tmi.dev/component": c.Name}
}

// natsPeer selects the NATS server pod (app=nats in the platform namespace).
// Egress rules scope their To peer to this so "NATS only" actually means
// NATS only — not "port 4222 to any destination".
func natsPeer() networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": platformNamespace},
		},
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "nats"},
		},
	}
}

// dnsPeer selects the cluster DNS pods (CoreDNS: k8s-app=kube-dns in
// kube-system). Without this scope a DNS egress rule would permit port 53
// to every destination.
func dnsPeer() networkingv1.NetworkPolicyPeer {
	return networkingv1.NetworkPolicyPeer{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"},
		},
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"k8s-app": "kube-dns"},
		},
	}
}

// RenderNetworkPolicy builds the NetworkPolicy for a component from its
// egress posture. The Egress policy type is always set so a limited egress
// list is an enforced deny. Every egress rule carries an explicit To peer
// selector — a rule with ports but no To matches all destinations, so
// scoping the peers is what makes "egress: none" mean "NATS only". The
// controller always renders this as a cluster-layer backstop, independent
// of any in-code egress guarding.
func RenderNetworkPolicy(c *platformv1alpha1.TMIComponent) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: c.Name, Namespace: c.Namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: componentPodLabels(c)},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}

	// Every posture permits egress to NATS — without it a worker cannot
	// receive jobs or publish results. The To peer scopes this to the
	// NATS pod alone, not all destinations on port 4222.
	natsRule := networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{natsPeer()},
		Ports: []networkingv1.NetworkPolicyPort{
			{Port: intOrStringPtr(intstr.FromInt(natsPort))},
		},
	}
	np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{natsRule}

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
			// One ipBlock egress rule per declared CIDR (in-cluster VM, cloud
			// private-endpoint subnet, or known VIP). Each rule gets its own
			// ports slice (no shared backing array).
			for _, cidr := range al.CIDRs {
				np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
					To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr}}},
					Ports: resolveEgressPorts(al.Ports),
				})
			}

			// One selector egress rule per declared in-cluster peer.
			for _, p := range al.ClusterPeers {
				// Validation rejects a peer with no selector, but be defensive:
				// a NetworkPolicyPeer with nil namespace+pod selectors and no
				// IPBlock matches ALL pods in ALL namespaces — never emit that.
				if len(p.NamespaceSelector) == 0 && len(p.PodSelector) == 0 {
					continue
				}
				peer := networkingv1.NetworkPolicyPeer{}
				if len(p.NamespaceSelector) > 0 {
					peer.NamespaceSelector = &metav1.LabelSelector{MatchLabels: p.NamespaceSelector}
				}
				if len(p.PodSelector) > 0 {
					peer.PodSelector = &metav1.LabelSelector{MatchLabels: p.PodSelector}
				}
				// Each rule gets its own ports slice (no shared backing array).
				ports := resolveEgressPorts(al.Ports)
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
					Ports: resolveEgressPorts(al.Ports),
				})
			}
		}
	}
	// EgressFetchControlled is RESERVED. Until the T3 egress library lands
	// (a later issue) the controller renders the same NATS-only policy as
	// egress:none; the in-code guard is what relaxes it. No L3 widening here.

	return np
}

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
