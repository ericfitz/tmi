package saml

import (
	"fmt"
	"strings"

	"github.com/crewjam/saml"
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

// ExtractUserInfo extracts user information and groups from SAML assertion
func ExtractUserInfo(assertion *saml.Assertion, config *SAMLConfig) (*UserInfo, error) {
	if assertion == nil {
		return nil, fmt.Errorf("assertion is nil")
	}

	userInfo := &UserInfo{
		IdP: config.ID,
	}

	// Extract attributes from the assertion first (needed for subject-id/pairwise-id)
	attributeMap := make(map[string][]string)
	if len(assertion.AttributeStatements) > 0 {
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
	}

	// Hierarchical identifier extraction
	// Priority: 1. subject-id, 2. pairwise-id, 3. NameID

	// Check for subject-id attribute (persistent identifier)
	if subjectID := getAttributeValue(attributeMap, "urn:oasis:names:tc:SAML:attribute:subject-id"); subjectID != "" {
		userInfo.ID = subjectID
		userInfo.IDType = "subject-id"
	} else if subjectID := getAttributeValue(attributeMap, "subject-id"); subjectID != "" {
		// Also check friendly name
		userInfo.ID = subjectID
		userInfo.IDType = "subject-id"
	} else if pairwiseID := getAttributeValue(attributeMap, "urn:oasis:names:tc:SAML:attribute:pairwise-id"); pairwiseID != "" {
		// Check for pairwise-id attribute (privacy-preserving identifier)
		userInfo.ID = pairwiseID
		userInfo.IDType = "pairwise-id"
	} else if pairwiseID := getAttributeValue(attributeMap, "pairwise-id"); pairwiseID != "" {
		// Also check friendly name
		userInfo.ID = pairwiseID
		userInfo.IDType = "pairwise-id"
	} else if assertion.Subject != nil && assertion.Subject.NameID != nil {
		// Fallback to NameID
		userInfo.ID = assertion.Subject.NameID.Value
		userInfo.IDType = "nameid"
	}

	// Map attributes using configuration
	if config.AttributeMapping != nil {
		// Email
		if emailAttr, ok := config.AttributeMapping["email"]; ok {
			if values, exists := attributeMap[emailAttr]; exists && len(values) > 0 {
				userInfo.Email = values[0]
				userInfo.EmailVerified = true // SAML assertions are considered verified
			}
		}

		// Name
		if nameAttr, ok := config.AttributeMapping["name"]; ok {
			if values, exists := attributeMap[nameAttr]; exists && len(values) > 0 {
				userInfo.Name = values[0]
			}
		}

		// Given name
		if givenNameAttr, ok := config.AttributeMapping["given_name"]; ok {
			if values, exists := attributeMap[givenNameAttr]; exists && len(values) > 0 {
				userInfo.GivenName = values[0]
			}
		}

		// Family name
		if familyNameAttr, ok := config.AttributeMapping["family_name"]; ok {
			if values, exists := attributeMap[familyNameAttr]; exists && len(values) > 0 {
				userInfo.FamilyName = values[0]
			}
		}

		// Groups
		if groupsAttr, ok := config.AttributeMapping["groups"]; ok {
			if values, exists := attributeMap[groupsAttr]; exists {
				userInfo.Groups = filterGroups(values, config.GroupPrefix)
			}
		}
	}

	// Fallback: try to extract groups using GroupAttributeName
	if len(userInfo.Groups) == 0 && config.GroupAttributeName != "" {
		if values, exists := attributeMap[config.GroupAttributeName]; exists {
			userInfo.Groups = filterGroups(values, config.GroupPrefix)
		}
	}

	// Fallback: if no email, use NameID
	if userInfo.Email == "" && userInfo.ID != "" {
		// Check if ID looks like an email
		if strings.Contains(userInfo.ID, "@") {
			userInfo.Email = userInfo.ID
		} else {
			// Generate a synthetic email
			userInfo.Email = fmt.Sprintf("%s@%s.saml.tmi", userInfo.ID, config.ID)
		}
	}

	// Fallback: if no name, use email prefix
	if userInfo.Name == "" && userInfo.Email != "" {
		parts := strings.Split(userInfo.Email, "@")
		userInfo.Name = parts[0]
	}

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
