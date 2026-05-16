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

func TestValidateComponent_SourceLocatorWithAllowlistIsValid(t *testing.T) {
	// source-locator IS valid when egress is not "none" — a fetching
	// worker with allowlist egress can reach its source.
	c := comp(platformv1alpha1.EgressAllowlist, platformv1alpha1.InputSourceLocator)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{Hosts: []string{"git.example.com"}}
	if err := ValidateComponent(c); err != nil {
		t.Fatalf("expected source-locator + egress:allowlist (with hosts) to be valid, got %v", err)
	}
}

func TestValidateComponent_AllowlistWithHostsIsValid(t *testing.T) {
	c := comp(platformv1alpha1.EgressAllowlist, platformv1alpha1.InputContentRef)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{Hosts: []string{"api.openai.com"}}
	if err := ValidateComponent(c); err != nil {
		t.Fatalf("expected egress:allowlist with hosts to be valid, got %v", err)
	}
}
