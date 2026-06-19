package auth

import (
	"fmt"
	"net/url"
)

// StepUpStrength classifies whether a given provider can guarantee a fresh
// interactive re-authentication on demand. Strong providers honor OIDC's
// prompt=login + max_age=0 (or SAML's ForceAuthn=true). Weak providers do not,
// and step-up against them is short-circuited with an audit marker.
//
// See docs/superpowers/specs/2026-05-10-oauth2-step-up-design.md.
// SEM@43dacd547eec5eefa97d1c84417548679f11c037: enum classifying whether a provider can enforce interactive re-authentication on demand (pure)
type StepUpStrength int

const (
	StepUpStrong StepUpStrength = iota
	StepUpWeak
)

// SEM@43dacd547eec5eefa97d1c84417548679f11c037: convert a StepUpStrength value to its human-readable string label (pure)
func (s StepUpStrength) String() string {
	switch s {
	case StepUpStrong:
		return "strong"
	case StepUpWeak:
		return "weak"
	default:
		return "unknown"
	}
}

// knownStrongProviderIDs is the explicit allowlist of provider IDs known to
// honor prompt=login/max_age=0 even when no Issuer/JWKSURL is configured (e.g.,
// the in-process tmi dev provider, which we control end-to-end).
var knownStrongProviderIDs = map[string]bool{
	"google":    true,
	"microsoft": true,
	"tmi":       true,
}

// knownWeakProviderIDs is the explicit denylist of provider IDs known to
// silently ignore prompt=login (notably GitHub).
var knownWeakProviderIDs = map[string]bool{
	"github": true,
}

// ClassifyStepUpStrength returns the step-up strength for the given provider
// config. Rules (first match wins):
//
//  1. ID in knownStrongProviderIDs → Strong
//  2. ID in knownWeakProviderIDs   → Weak
//  3. Has Issuer AND JWKSURL (i.e., OIDC)  → Strong (generic OIDC providers
//     honor prompt=login per the OIDC spec)
//  4. Otherwise → Weak (pure-OAuth2 fallback; safest default)
//
// SAML providers are classified Strong by callers via a separate path; this
// function operates on OAuth provider configs only.
// SEM@43dacd547eec5eefa97d1c84417548679f11c037: classify an OAuth provider config as strong or weak step-up based on allowlists and OIDC capability (pure)
func ClassifyStepUpStrength(cfg OAuthProviderConfig) StepUpStrength {
	if knownStrongProviderIDs[cfg.ID] {
		return StepUpStrong
	}
	if knownWeakProviderIDs[cfg.ID] {
		return StepUpWeak
	}
	if cfg.Issuer != "" && cfg.JWKSURL != "" {
		return StepUpStrong
	}
	return StepUpWeak
}

// BuildStepUpAuthorizationURL builds the upstream authorize URL for a step-up
// round-trip. For OAuth/OIDC providers it appends prompt=login and max_age=0
// to the URL returned by provider.GetAuthorizationURL(state). SAML callers
// must not use this function; they call GetAuthorizationURLForceAuthn on the
// SAML provider directly.
// SEM@381909438c48d60df5164d4ea214359f1b52ebdf: build an authorization URL that forces interactive re-authentication via prompt=login and max_age=0 (pure)
func BuildStepUpAuthorizationURL(provider Provider, cfg OAuthProviderConfig, state string) (string, error) {
	// cfg is reserved for future per-provider step-up parameter overrides
	// (e.g., providers that use a vendor-specific equivalent of prompt=login).
	_ = cfg
	raw := provider.GetAuthorizationURL(state)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid upstream authorize URL: %w", err)
	}
	q := u.Query()
	q.Set("prompt", "login")
	q.Set("max_age", "0")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// BuildIdentityLinkAuthorizationURL builds the upstream authorize URL for an
// identity-link round-trip. Appends prompt=select_account to force account
// selection at the provider. Strong providers (those that honor prompt=consent)
// also get prompt="select_account consent" so the user explicitly re-authorizes
// scope grants. SAML providers are not supported for identity-link.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: build an authorization URL that forces account selection and, for strong providers, consent (pure)
func BuildIdentityLinkAuthorizationURL(provider Provider, cfg OAuthProviderConfig, state string) (string, error) {
	raw := provider.GetAuthorizationURL(state)
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid upstream authorize URL: %w", err)
	}
	q := u.Query()
	// Default: force account selection so the user can pick a different account.
	promptValue := "select_account"
	// Strong providers (OIDC-conformant) honor prompt=consent; add it so the
	// user gets an explicit consent screen and we confirm scope grants.
	if ClassifyStepUpStrength(cfg) == StepUpStrong {
		promptValue = "select_account consent"
	}
	q.Set("prompt", promptValue)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
