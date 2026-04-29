package saml

import (
	"fmt"
	"strings"

	"github.com/crewjam/saml"
	"github.com/ericfitz/tmi/internal/slogging"
)

// UserInfo represents user information extracted from SAML assertion
type UserInfo struct {
	ID            string
	IDType        string // Type of identifier: "subject-id", "pairwise-id", "nameid"
	Email         string
	EmailVerified bool
	Name          string
	GivenName     string
	FamilyName    string
	Picture       string
	Locale        string
	IdP           string
	Groups        []string
}

// buildAttributeMap extracts all attributes from the assertion into a map
func buildAttributeMap(assertion *saml.Assertion) map[string][]string {
	attributeMap := make(map[string][]string)
	if len(assertion.AttributeStatements) == 0 {
		return attributeMap
	}

	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			var values []string
			for _, value := range attr.Values {
				values = append(values, value.Value)
			}
			attributeMap[attr.Name] = values
			// Also store by FriendlyName if available
			if attr.FriendlyName != "" {
				attributeMap[attr.FriendlyName] = values
			}
		}
	}
	return attributeMap
}

// extractUserID extracts user ID with hierarchical priority
// Priority: 1. subject-id, 2. pairwise-id, 3. NameID
func extractUserID(assertion *saml.Assertion, attributeMap map[string][]string) (string, string) {
	// Check for subject-id attribute (persistent identifier)
	if subjectID := getAttributeValue(attributeMap, "urn:oasis:names:tc:SAML:attribute:subject-id"); subjectID != "" {
		return subjectID, "subject-id"
	}
	if subjectID := getAttributeValue(attributeMap, "subject-id"); subjectID != "" {
		return subjectID, "subject-id"
	}

	// Check for pairwise-id attribute (privacy-preserving identifier)
	if pairwiseID := getAttributeValue(attributeMap, "urn:oasis:names:tc:SAML:attribute:pairwise-id"); pairwiseID != "" {
		return pairwiseID, "pairwise-id"
	}
	if pairwiseID := getAttributeValue(attributeMap, "pairwise-id"); pairwiseID != "" {
		return pairwiseID, "pairwise-id"
	}

	// Fallback to NameID
	if assertion.Subject != nil && assertion.Subject.NameID != nil {
		return assertion.Subject.NameID.Value, "nameid"
	}

	return "", ""
}

// mapAttribute extracts a single attribute value if configured
func mapAttribute(attributeMap map[string][]string, attributeMapping map[string]string, key string) string {
	if attr, ok := attributeMapping[key]; ok {
		if values, exists := attributeMap[attr]; exists && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// mapUserAttributes maps configured attributes to user info fields
func mapUserAttributes(userInfo *UserInfo, attributeMap map[string][]string, config *SAMLConfig) {
	if config.AttributeMapping == nil {
		return
	}

	// Email
	if email := mapAttribute(attributeMap, config.AttributeMapping, "email"); email != "" {
		userInfo.Email = email
		userInfo.EmailVerified = true // SAML assertions are considered verified
	}

	// Name
	if name := mapAttribute(attributeMap, config.AttributeMapping, "name"); name != "" {
		userInfo.Name = name
	}

	// Given name
	if givenName := mapAttribute(attributeMap, config.AttributeMapping, "given_name"); givenName != "" {
		userInfo.GivenName = givenName
	}

	// Family name
	if familyName := mapAttribute(attributeMap, config.AttributeMapping, "family_name"); familyName != "" {
		userInfo.FamilyName = familyName
	}

	// Groups
	if groupsAttr, ok := config.AttributeMapping["groups"]; ok {
		if values, exists := attributeMap[groupsAttr]; exists {
			userInfo.Groups = filterGroups(values, config.GroupPrefix)
		}
	}
}

// wellKnownEmailAttributes lists SAML attribute names and FriendlyNames
// that commonly carry the user's email address across IdPs. Listed in
// preference order: vendor-specific URIs first, then standard URIs,
// then short FriendlyNames / OIDC-style claims.
var wellKnownEmailAttributes = []string{
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	"urn:oid:0.9.2342.19200300.100.1.3", // RFC 4524 mail
	"emailaddress",
	"email",
	"mail",
}

// wellKnownNameAttributes lists SAML attribute names and FriendlyNames
// that commonly carry the user's display / full name.
var wellKnownNameAttributes = []string{
	"http://schemas.microsoft.com/identity/claims/displayname",
	"urn:oid:2.16.840.1.113730.3.1.241", // Netscape/LDAP displayName
	"displayname",
	"displayName",
	"name",
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
}

// wellKnownGivenNameAttributes lists common given-name attributes.
var wellKnownGivenNameAttributes = []string{
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname",
	"urn:oid:2.5.4.42", // RFC 4519 givenName
	"givenName",
	"firstName",
	"given_name",
}

// wellKnownFamilyNameAttributes lists common family-name attributes.
var wellKnownFamilyNameAttributes = []string{
	"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
	"urn:oid:2.5.4.4", // RFC 4519 sn
	"surname",
	"sn",
	"lastName",
	"family_name",
}

// firstWellKnownValue returns the first non-empty value found in
// attributeMap for any of the candidate attribute names.
func firstWellKnownValue(attributeMap map[string][]string, candidates []string) string {
	for _, name := range candidates {
		if v := getAttributeValue(attributeMap, name); v != "" {
			return v
		}
	}
	return ""
}

// applyWellKnownFallbacks fills in user info fields the operator-configured
// attribute mapping did not populate, by trying a curated list of common
// SAML attribute names and FriendlyNames. This is a defensive layer so that
// a missing or misconfigured AttributeMapping does not silently produce
// synthetic identifiers (see issue #303).
func applyWellKnownFallbacks(userInfo *UserInfo, attributeMap map[string][]string) {
	logger := slogging.Get()

	if userInfo.Email == "" {
		if v := firstWellKnownValue(attributeMap, wellKnownEmailAttributes); v != "" {
			userInfo.Email = v
			userInfo.EmailVerified = true
			logger.Debug("SAML email populated from well-known fallback: %s", v)
		}
	}
	if userInfo.Name == "" {
		if v := firstWellKnownValue(attributeMap, wellKnownNameAttributes); v != "" {
			userInfo.Name = v
			logger.Debug("SAML name populated from well-known fallback: %s", v)
		}
	}
	if userInfo.GivenName == "" {
		if v := firstWellKnownValue(attributeMap, wellKnownGivenNameAttributes); v != "" {
			userInfo.GivenName = v
		}
	}
	if userInfo.FamilyName == "" {
		if v := firstWellKnownValue(attributeMap, wellKnownFamilyNameAttributes); v != "" {
			userInfo.FamilyName = v
		}
	}

	// If we still have no Name but we now have given + family, compose them.
	if userInfo.Name == "" && userInfo.GivenName != "" && userInfo.FamilyName != "" {
		userInfo.Name = userInfo.GivenName + " " + userInfo.FamilyName
	}
}

// extractGroups attempts to extract groups using configured attribute name
func extractGroups(userInfo *UserInfo, attributeMap map[string][]string, config *SAMLConfig) {
	if len(userInfo.Groups) == 0 && config.GroupAttributeName != "" {
		if values, exists := attributeMap[config.GroupAttributeName]; exists {
			userInfo.Groups = filterGroups(values, config.GroupPrefix)
		}
	}
}

// applyEmailFallback generates email if not present
func applyEmailFallback(userInfo *UserInfo, config *SAMLConfig) {
	if userInfo.Email != "" || userInfo.ID == "" {
		return
	}

	// Check if ID looks like an email
	if strings.Contains(userInfo.ID, "@") {
		userInfo.Email = userInfo.ID
	} else {
		// Generate a synthetic email
		userInfo.Email = fmt.Sprintf("%s@%s.saml.tmi", userInfo.ID, config.ID)
	}
}

// applyNameFallback generates name from email prefix if not present
func applyNameFallback(userInfo *UserInfo) {
	if userInfo.Name == "" && userInfo.Email != "" {
		parts := strings.Split(userInfo.Email, "@")
		userInfo.Name = parts[0]
	}
}

// ExtractUserInfo extracts user information and groups from SAML assertion
func ExtractUserInfo(assertion *saml.Assertion, config *SAMLConfig) (*UserInfo, error) {
	if assertion == nil {
		return nil, fmt.Errorf("assertion is nil")
	}

	userInfo := &UserInfo{
		IdP: config.ID,
	}

	// Extract attributes from the assertion
	attributeMap := buildAttributeMap(assertion)

	// DEBUG: Log all received SAML attributes for troubleshooting
	// This helps diagnose attribute mapping issues with different IdPs
	logger := slogging.Get()
	logger.Debug("SAML attribute extraction starting for provider: %s", config.ID)
	logger.Debug("SAML assertion contains %d attributes", len(attributeMap))
	for attrName, attrValues := range attributeMap {
		logger.Debug("SAML attribute: %s = %v", attrName, attrValues)
	}
	if config.AttributeMapping != nil {
		logger.Debug("SAML configured attribute mappings:")
		for key, mapping := range config.AttributeMapping {
			logger.Debug("  %s -> %s", key, mapping)
		}
	}

	// Extract user ID with hierarchical priority
	userInfo.ID, userInfo.IDType = extractUserID(assertion, attributeMap)

	// Map attributes using configuration
	mapUserAttributes(userInfo, attributeMap, config)

	// For any field the operator config did not populate, try a curated set
	// of well-known SAML attribute names. This protects against IdP/operator
	// misconfiguration silently degrading to synthetic identifiers (#303).
	applyWellKnownFallbacks(userInfo, attributeMap)

	// Extract groups using fallback method
	extractGroups(userInfo, attributeMap, config)

	// Apply fallbacks for missing fields
	applyEmailFallback(userInfo, config)
	applyNameFallback(userInfo)

	// DEBUG: Log extracted user info
	logger.Debug("SAML extracted UserInfo: ID=%s, IDType=%s, Email=%s, Name=%s, GivenName=%s, FamilyName=%s",
		userInfo.ID, userInfo.IDType, userInfo.Email, userInfo.Name, userInfo.GivenName, userInfo.FamilyName)

	return userInfo, nil
}

// filterGroups filters groups by optional prefix
func filterGroups(groups []string, prefix string) []string {
	if prefix == "" {
		return groups
	}

	var filtered []string
	for _, group := range groups {
		if strings.HasPrefix(group, prefix) {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

// getAttributeValue safely retrieves an attribute value from the attribute map
func getAttributeValue(attributeMap map[string][]string, attributeName string) string {
	if values, exists := attributeMap[attributeName]; exists && len(values) > 0 {
		return values[0]
	}
	return ""
}

// GetAttributeValue safely retrieves an attribute value from the assertion
func GetAttributeValue(assertion *saml.Assertion, attributeName string) string {
	if assertion == nil || len(assertion.AttributeStatements) == 0 {
		return ""
	}

	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if attr.Name == attributeName || attr.FriendlyName == attributeName {
				if len(attr.Values) > 0 {
					return attr.Values[0].Value
				}
			}
		}
	}
	return ""
}

// GetAttributeValues safely retrieves all values for an attribute from the assertion
func GetAttributeValues(assertion *saml.Assertion, attributeName string) []string {
	if assertion == nil || len(assertion.AttributeStatements) == 0 {
		return nil
	}

	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			if attr.Name == attributeName || attr.FriendlyName == attributeName {
				var values []string
				for _, v := range attr.Values {
					values = append(values, v.Value)
				}
				return values
			}
		}
	}
	return nil
}
