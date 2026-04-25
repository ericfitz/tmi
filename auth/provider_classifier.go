package auth

import (
	"context"
	"fmt"
)

type ProviderClassification int

const (
	// ClassificationNonOIDC is the zero value (fail-closed default): discovery
	// failed or no issuer configured. No guarantee about userinfo response shape.
	// Explicit subject_claim is required.
	ClassificationNonOIDC ProviderClassification = iota

	// ClassificationOIDCCustomUserinfo: discovery succeeds but the configured
	// userinfo URL differs from the discovery doc's userinfo_endpoint. The
	// operator is calling a non-OIDC userinfo endpoint (e.g. Microsoft Graph
	// /me instead of Microsoft's OIDC userinfo). Explicit subject_claim is
	// required.
	ClassificationOIDCCustomUserinfo

	// ClassificationOIDCCompliant: discovery succeeds AND configured userinfo
	// URL matches the discovery doc's userinfo_endpoint. Default `sub` mapping
	// is safe.
	ClassificationOIDCCompliant
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

// ValidateClassifiedProvider returns a slice of error messages describing
// reasons the provider config is not safe to enable. An empty slice means
// the provider is OK.
func ValidateClassifiedProvider(p ClassifiedProvider, cfg OAuthProviderConfig) []string {
	if p.Classification == ClassificationOIDCCompliant {
		return nil
	}

	for _, ep := range cfg.UserInfo {
		if v, ok := ep.Claims["subject_claim"]; ok && v != "" {
			return nil
		}
	}

	return []string{
		fmt.Sprintf(
			"OAuth provider %q is classified as %s; an explicit subject_claim mapping is required. "+
				"Set OAUTH_PROVIDERS_%s_USERINFO_CLAIMS_SUBJECT_CLAIM (or claims.subject_claim in YAML) "+
				"to the field name returned by the provider's primary userinfo endpoint (for example: \"id\" for GitHub or Microsoft Graph, \"sub\" for an OIDC-shaped endpoint). "+
				"If the identity claim comes from a non-primary userinfo endpoint, use USERINFO_SECONDARY_CLAIMS_ or USERINFO_ADDITIONAL_CLAIMS_ instead.",
			p.ProviderID, p.Classification, providerIDToEnvKey(p.ProviderID),
		),
	}
}

func providerIDToEnvKey(id string) string {
	out := make([]byte, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z':
			out[i] = c - ('a' - 'A')
		case c == '-':
			out[i] = '_'
		default:
			out[i] = c
		}
	}
	return string(out)
}
