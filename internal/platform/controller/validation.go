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
		if c.Spec.Allowlist == nil || len(c.Spec.Allowlist.Hosts) == 0 {
			return fmt.Errorf("egress=allowlist requires spec.allowlist.hosts to be non-empty")
		}
	}
	return nil
}
