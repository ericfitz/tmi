// Package controller implements the TMIComponent reconciler for the TMI
// Component Platform.
package controller

import (
	"errors"
	"fmt"
	"net"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
)

// ValidateComponent checks a TMIComponent spec for internal consistency
// beyond what the CRD OpenAPI schema can express.
func ValidateComponent(c *platformv1alpha1.TMIComponent) error {
	if c.Spec.InputMode == platformv1alpha1.InputSourceLocator &&
		c.Spec.Egress == platformv1alpha1.EgressNone {
		return errors.New("inputMode=source-locator is incompatible with egress=none: " +
			"a worker that fetches its own input requires egress")
	}
	if c.Spec.Egress == platformv1alpha1.EgressAllowlist {
		if err := validateAllowlist(c.Spec.Allowlist); err != nil {
			return err
		}
	}
	return nil
}

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
