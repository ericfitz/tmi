package auth

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// DelegationTokenTTL is the wall-clock budget a delegation token is valid
// for. The threat-model spec (T18) calls for a tight bound — the addon's
// invocation window — so that a leaked token has a small attack surface.
// 60 seconds matches the addon-invocation budget called out in #358.
const DelegationTokenTTL = 60 * time.Second

// IssueAddonDelegationToken mints a short-lived JWT that impersonates the
// invoker for one addon-invocation write-back. The token's claims:
//
//   - `sub` is the invoker's provider_user_id (matches the invoker's normal
//     login token), so existing JWT middleware and downstream ACL checks
//     resolve to the invoker without modification.
//   - `email`, `name`, `idp`, `groups`, `tmi_is_security_reviewer` are
//     copied from the invoker. `tmi_is_administrator` is FORCED to false
//     regardless of the invoker's actual administrator membership — a
//     delegation token never grants admin authority, so the addon cannot
//     escape its mandate even if invoked by an admin.
//   - `delegation` carries the addon/delivery/threat-model scope.
//   - `aud` is the issuer (self-issued, like normal user tokens) so the
//     existing JWT validator accepts it.
//   - `exp` is now+DelegationTokenTTL.
//
// Callers (the webhook delivery worker) should call this once per delivery
// attempt — the previous attempt's token will have expired by the time a
// retry fires, and minting fresh tokens keeps the invoker's revocation /
// group-membership state current.
func (s *Service) IssueAddonDelegationToken(
	ctx context.Context,
	invoker *User,
	addonID, deliveryID, threatModelID uuid.UUID,
) (string, error) {
	if invoker == nil {
		return "", fmt.Errorf("invoker user is required")
	}
	if invoker.ProviderUserID == "" {
		return "", fmt.Errorf("invoker has no provider_user_id (cannot mint delegation token for %s)", invoker.InternalUUID)
	}

	now := time.Now()
	expiresAt := now.Add(DelegationTokenTTL)
	issuer := s.deriveIssuer()

	notAdmin := false
	claims := &Claims{
		Email:            invoker.Email,
		EmailVerified:    invoker.EmailVerified,
		Name:             fmt.Sprintf("[Addon Delegation: %s] %s", addonID, invoker.Name),
		IdentityProvider: invoker.Provider,
		Groups:           invoker.Groups,
		IsAdministrator:  &notAdmin,
		Delegation: &DelegationContext{
			AddonID:       addonID.String(),
			DeliveryID:    deliveryID.String(),
			ThreatModelID: threatModelID.String(),
		},
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   invoker.ProviderUserID,
			Audience:  jwt.ClaimStrings{issuer},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
		},
	}

	// Enrich tmi-managed group claims (security_reviewer membership) from the
	// invoker's current state so the delegation token reflects revocations
	// since the original login. Administrator is intentionally not propagated
	// — the constant `notAdmin` above is the floor for delegation tokens.
	if s.claimsEnricher != nil && invoker.InternalUUID != "" {
		_, isSecReviewer, tmiGroups, enrichErr := s.claimsEnricher.EnrichClaims(
			ctx, invoker.InternalUUID, invoker.Provider, invoker.Groups,
		)
		if enrichErr == nil {
			claims.IsSecurityReviewer = &isSecReviewer
			if len(tmiGroups) > 0 {
				// Filter administrators group out before merging — delegation
				// tokens never carry the administrators marker.
				filtered := make([]string, 0, len(tmiGroups))
				for _, g := range tmiGroups {
					if g != administratorsGroupName {
						filtered = append(filtered, g)
					}
				}
				claims.Groups = mergeGroupsForDelegation(claims.Groups, filtered)
			}
		}
	}

	tokenString, err := s.keyManager.CreateToken(claims)
	if err != nil {
		return "", fmt.Errorf("failed to create delegation token: %w", err)
	}
	return tokenString, nil
}

// administratorsGroupName is the marker string for the global Administrators
// group. Stripped from delegation tokens (admin authority must never be
// propagated through an addon invocation, T18).
const administratorsGroupName = "administrators"

// mergeGroupsForDelegation deduplicates two group slices and additionally
// strips the administrators marker. Mirrors mergeGroups in service.go but
// with the admin-floor enforced.
func mergeGroupsForDelegation(existing, additional []string) []string {
	merged := make([]string, 0, len(existing)+len(additional))
	for _, g := range existing {
		if g == administratorsGroupName {
			continue
		}
		if !slices.Contains(merged, g) {
			merged = append(merged, g)
		}
	}
	for _, g := range additional {
		if g == administratorsGroupName {
			continue
		}
		if !slices.Contains(merged, g) {
			merged = append(merged, g)
		}
	}
	return merged
}
