package api

// IsPseudoGroup checks if a group name is a recognized pseudo-group
// Pseudo-groups are special groups with predefined behavior that don't come from IdPs
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
func GetPseudoGroupIdP(groupName string) *string {
	if IsPseudoGroup(groupName) {
		return nil
	}
	// For non-pseudo-groups, the caller should specify the IdP
	return nil
}

// ValidateAuthorizationWithPseudoGroups validates authorization entries
// and applies pseudo-group specific rules
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
// have the correct IdP value (nil for cross-IdP pseudo-groups)
func NormalizePseudoGroupAuthorization(auth Authorization) Authorization {
	if auth.SubjectType == AuthorizationSubjectTypeGroup && IsPseudoGroup(auth.Subject) {
		// Clear the IdP for pseudo-groups to ensure cross-IdP behavior
		auth.Idp = nil
	}
	return auth
}

// NormalizePseudoGroupAuthorizationList applies normalization to a list of authorization entries
func NormalizePseudoGroupAuthorizationList(authList []Authorization) []Authorization {
	normalized := make([]Authorization, len(authList))
	for i, auth := range authList {
		normalized[i] = NormalizePseudoGroupAuthorization(auth)
	}
	return normalized
}
