package api

import (
	"fmt"
	"net/http"
	"reflect"
)

// ValidateDuplicateSubjects checks for duplicate subjects in authorization list
func ValidateDuplicateSubjects(authList []Authorization) error {
	subjectMap := make(map[string]bool)

	for _, auth := range authList {
		if _, exists := subjectMap[auth.Subject]; exists {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
			}
		}
		subjectMap[auth.Subject] = true
	}

	return nil
}

// ValidateOwnerNotInAuthList checks that the owner is not duplicated in the authorization list
func ValidateOwnerNotInAuthList(owner string, authList []Authorization) error {
	for _, auth := range authList {
		if auth.Subject == owner {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Duplicate authorization subject with owner: %s", auth.Subject),
			}
		}
	}
	return nil
}

// ApplyOwnershipTransferRule applies the business rule that when ownership changes,
// the original owner should be preserved in the authorization list with owner role
func ApplyOwnershipTransferRule(authList []Authorization, originalOwner, newOwner string) []Authorization {
	if originalOwner == newOwner {
		return authList // No ownership change
	}

	// Check if the original owner is already in the authorization list
	originalOwnerFound := false
	for i := range authList {
		if authList[i].Subject == originalOwner {
			// Make sure the original owner has the Owner role
			authList[i].Role = RoleOwner
			originalOwnerFound = true
			break
		}
	}

	// If the original owner isn't in the list, add them
	if !originalOwnerFound {
		authList = append(authList, Authorization{
			Subject: originalOwner,
			Role:    RoleOwner,
		})
	}

	return authList
}

// ExtractOwnershipChangesFromOperations extracts owner and authorization changes from patch operations
func ExtractOwnershipChangesFromOperations(operations []PatchOperation) (newOwner string, newAuth []Authorization, hasOwnerChange, hasAuthChange bool) {
	for _, op := range operations {
		if op.Op == "replace" || op.Op == "add" {
			switch op.Path {
			case "/owner":
				if ownerVal, ok := op.Value.(string); ok && ownerVal != "" {
					newOwner = ownerVal
					hasOwnerChange = true
				}
			case "/authorization":
				if authVal, ok := op.Value.([]interface{}); ok {
					newAuth = convertInterfaceToAuthList(authVal)
					hasAuthChange = true
				}
			}
		}
	}
	return newOwner, newAuth, hasOwnerChange, hasAuthChange
}

// convertInterfaceToAuthList converts []interface{} to []Authorization
func convertInterfaceToAuthList(authList []interface{}) []Authorization {
	result := make([]Authorization, 0, len(authList))

	for _, authItem := range authList {
		if auth, ok := authItem.(map[string]interface{}); ok {
			var authObj Authorization
			if subject, ok := auth["subject"].(string); ok {
				authObj.Subject = subject
			}
			if role, ok := auth["role"].(string); ok {
				authObj.Role = Role(role)
			}
			result = append(result, authObj)
		}
	}

	return result
}

// ValidateAuthorizationEntries validates individual authorization entries
func ValidateAuthorizationEntries(authList []Authorization) error {
	for _, auth := range authList {
		if auth.Subject == "" {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Authorization subject cannot be empty",
			}
		}
	}
	return nil
}

// ValidateAuthorizationEntriesWithFormat validates authorization entries with format checking
func ValidateAuthorizationEntriesWithFormat(authList []Authorization) error {
	for i, auth := range authList {
		// Validate subject format
		if auth.Subject == "" {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Authorization subject at index %d cannot be empty", i),
			}
		}

		if len(auth.Subject) > 255 {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Authorization subject '%s' exceeds maximum length of 255 characters", auth.Subject),
			}
		}

		// Validate role is valid
		if auth.Role != RoleReader && auth.Role != RoleWriter && auth.Role != RoleOwner {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Invalid role '%s' for subject '%s'. Must be one of: reader, writer, owner", auth.Role, auth.Subject),
			}
		}
	}
	return nil
}
