// Package controller implements the TMIComponent reconciler for the TMI
// Component Platform.
package controller

import (
	"errors"
	"fmt"

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

// validateAllowlist is fully implemented in a later task.
func validateAllowlist(a *platformv1alpha1.AllowlistEgress) error {
	if a == nil || (len(a.CIDRs) == 0 && len(a.ClusterPeers) == 0 && !a.OpenInternet) {
		return fmt.Errorf("egress=allowlist requires at least one of spec.allowlist.cidrs, clusterPeers, or openInternet")
	}
	return nil
}
