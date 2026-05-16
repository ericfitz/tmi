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
			{Port: intOrStringPtr(intstr.FromInt(natsPort))},
		},
	}
	np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{natsRule}

	if c.Spec.Egress == platformv1alpha1.EgressAllowlist {
		// allowlist adds DNS (port 53) so hostnames resolve; host-level
		// allowlisting itself is enforced in-worker. The NetworkPolicy
		// widens egress to DNS but no further at the L3 layer.
		np.Spec.Egress = append(np.Spec.Egress, networkingv1.NetworkPolicyEgressRule{
			Ports: []networkingv1.NetworkPolicyPort{
				{Port: intOrStringPtr(intstr.FromInt(53)), Protocol: protoPtr(corev1.ProtocolUDP)},
				{Port: intOrStringPtr(intstr.FromInt(53)), Protocol: protoPtr(corev1.ProtocolTCP)},
			},
		})
	}
	// EgressFetchControlled is RESERVED. Until the T3 egress library lands
	// (a later issue) the controller renders the same NATS-only policy as
	// egress:none; the in-code guard is what relaxes it. No L3 widening here.

	return np
}

func intOrStringPtr(v intstr.IntOrString) *intstr.IntOrString { return &v }

func protoPtr(p corev1.Protocol) *corev1.Protocol { return &p }
