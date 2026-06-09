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

func TestValidateComponent_AllowlistRequiresTarget(t *testing.T) {
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
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{OpenInternet: true}
	if err := ValidateComponent(c); err != nil {
		t.Fatalf("expected source-locator + egress:allowlist (with openInternet) to be valid, got %v", err)
	}
}

func TestValidateComponent_AllowlistWithOpenInternetIsValid(t *testing.T) {
	c := comp(platformv1alpha1.EgressAllowlist, platformv1alpha1.InputContentRef)
	c.Spec.Allowlist = &platformv1alpha1.AllowlistEgress{OpenInternet: true}
	if err := ValidateComponent(c); err != nil {
		t.Fatalf("expected egress:allowlist with openInternet to be valid, got %v", err)
	}
}

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
