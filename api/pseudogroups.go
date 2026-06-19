package api

// IsPseudoGroup checks if a group name is a recognized pseudo-group
// Pseudo-groups are special groups with predefined behavior that don't come from IdPs
// SEM@6124bff108947c0b35d793f38a2bff9f438768ce: validate whether a group name is a recognized built-in pseudo-group (pure)
func IsPseudoGroup(groupName string) bool {
	switch groupName {
	case EveryonePseudoGroup:
		return true
	default:
		return false
	}
}

// GetPseudoGroupIdP returns the appropriate IdP value for a pseudo-group
// Pseudo-groups are cross-IdP by design, so this returns nil
// SEM@6124bff108947c0b35d793f38a2bff9f438768ce: return nil IdP for a pseudo-group, indicating cross-provider scope (pure)
func GetPseudoGroupIdP(groupName string) *string {
	if IsPseudoGroup(groupName) {
		return nil
	}
	// For non-pseudo-groups, the caller should specify the IdP
	return nil
}

// ValidateAuthorizationWithPseudoGroups validates authorization entries
// and applies pseudo-group specific rules
// SEM@6124bff108947c0b35d793f38a2bff9f438768ce: validate authorization entries applying pseudo-group-specific rules (pure)
func ValidateAuthorizationWithPseudoGroups(authList []Authorization) error {
	// First run standard validation
	if err := ValidateAuthorizationEntriesWithFormat(authList); err != nil {
		return err
	}

	// Additional validation for pseudo-groups
	// Currently, pseudo-groups don't require additional validation beyond standard checks
	// The IdP field is ignored for pseudo-groups during authorization checks

	return nil
}

// NormalizePseudoGroupAuthorization ensures pseudo-group authorization entries
// have the correct Provider value (BuiltInProvider for cross-provider pseudo-groups)
// SEM@192fb026aa596416ded7413d23092ccd1733ad90: normalize a single authorization entry to set the built-in provider for pseudo-groups (pure)
func NormalizePseudoGroupAuthorization(auth Authorization) Authorization {
	if auth.PrincipalType == AuthorizationPrincipalTypeGroup && IsPseudoGroup(auth.ProviderId) {
		// Set Provider to BuiltInProvider for pseudo-groups to ensure cross-provider behavior
		auth.Provider = BuiltInProvider
	}
	return auth
}

// NormalizePseudoGroupAuthorizationList applies normalization to a list of authorization entries
// SEM@6124bff108947c0b35d793f38a2bff9f438768ce: normalize a list of authorization entries to set the built-in provider for pseudo-groups (pure)
func NormalizePseudoGroupAuthorizationList(authList []Authorization) []Authorization {
	normalized := make([]Authorization, len(authList))
	for i, auth := range authList {
		normalized[i] = NormalizePseudoGroupAuthorization(auth)
	}
	return normalized
}
