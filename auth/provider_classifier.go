package auth

import "context"

type ProviderClassification int

const (
	// ClassificationOIDCCompliant: discovery succeeds AND configured userinfo
	// URL matches the discovery doc's userinfo_endpoint. Default `sub` mapping
	// is safe.
	ClassificationOIDCCompliant ProviderClassification = iota

	// ClassificationOIDCCustomUserinfo: discovery succeeds but the configured
	// userinfo URL differs from the discovery doc's userinfo_endpoint. The
	// operator is calling a non-OIDC userinfo endpoint (e.g. Microsoft Graph
	// /me instead of Microsoft's OIDC userinfo). Explicit subject_claim is
	// required.
	ClassificationOIDCCustomUserinfo

	// ClassificationNonOIDC: discovery failed or no issuer configured. No
	// guarantee about userinfo response shape. Explicit subject_claim is
	// required.
	ClassificationNonOIDC
)

func (c ProviderClassification) String() string {
	switch c {
	case ClassificationOIDCCompliant:
		return "OIDCCompliant"
	case ClassificationOIDCCustomUserinfo:
		return "OIDCCustomUserinfo"
	case ClassificationNonOIDC:
		return "NonOIDC"
	}
	return "Unknown"
}

type ClassifiedProvider struct {
	ProviderID     string
	Classification ProviderClassification
	DiscoveryDoc   *OIDCDiscoveryDoc // nil for ClassificationNonOIDC
}

// ClassifyProvider buckets a provider based on discovery probe results and
// configured userinfo URL. Compares the *primary* userinfo endpoint only
// (cfg.UserInfo[0]); secondary/additional endpoints are operator extensions
// and never affect classification.
func ClassifyProvider(ctx context.Context, client *DiscoveryClient, providerID string, cfg OAuthProviderConfig) ClassifiedProvider {
	out := ClassifiedProvider{ProviderID: providerID, Classification: ClassificationNonOIDC}

	if cfg.Issuer == "" {
		return out
	}

	doc, _ := client.Discover(ctx, cfg.Issuer)
	if doc == nil {
		return out
	}
	out.DiscoveryDoc = doc

	if len(cfg.UserInfo) == 0 {
		out.Classification = ClassificationOIDCCustomUserinfo
		return out
	}
	if doc.UserinfoEndpoint != "" && cfg.UserInfo[0].URL == doc.UserinfoEndpoint {
		out.Classification = ClassificationOIDCCompliant
	} else {
		out.Classification = ClassificationOIDCCustomUserinfo
	}
	return out
}
