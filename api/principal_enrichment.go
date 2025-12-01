package api

import (
	"database/sql"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/internal/slogging"
)

// enrichUserPrincipal retrieves user information and returns a User object with Principal fields populated
// Returns nil if user not found (for graceful degradation)
func enrichUserPrincipal(tx *sql.Tx, internalUUID string) (*User, error) {
	logger := slogging.Get()

	var provider, displayName, emailStr string
	var providerUserID sql.NullString

	query := `
		SELECT provider, provider_user_id, name, email
		FROM users
		WHERE internal_uuid = $1
	`

	err := tx.QueryRow(query, internalUUID).Scan(
		&provider,
		&providerUserID,
		&displayName,
		&emailStr,
	)

	if err == sql.ErrNoRows {
		logger.Warn("User not found for internal_uuid: %s", internalUUID)
		return nil, nil // Return nil for graceful degradation
	}

	if err != nil {
		return nil, fmt.Errorf("failed to enrich user principal: %w", err)
	}

	// For ProviderId: use provider_user_id if available, otherwise use email
	// This handles sparse users (provider_user_id is NULL until first login)
	var providerID string
	if providerUserID.Valid && providerUserID.String != "" {
		providerID = providerUserID.String
	} else {
		providerID = emailStr
	}

	return &User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      provider,
		ProviderId:    providerID,
		DisplayName:   displayName,
		Email:         openapi_types.Email(emailStr),
	}, nil
}

// enrichGroupPrincipal retrieves group information and returns a Principal object for a group
// Returns nil if group not found (for graceful degradation)
func enrichGroupPrincipal(tx *sql.Tx, internalUUID string) (*Principal, error) {
	logger := slogging.Get()

	// Handle "everyone" pseudo-group special case
	if internalUUID == EveryonePseudoGroupUUID {
		displayName := "Everyone"
		return &Principal{
			PrincipalType: PrincipalPrincipalTypeGroup,
			Provider:      "*",
			ProviderId:    EveryonePseudoGroup,
			DisplayName:   &displayName,
		}, nil
	}

	var provider, groupName, displayName string
	var email sql.NullString

	query := `
		SELECT provider, group_name, name
		FROM groups
		WHERE internal_uuid = $1
	`

	err := tx.QueryRow(query, internalUUID).Scan(
		&provider,
		&groupName,
		&displayName,
	)

	if err == sql.ErrNoRows {
		logger.Warn("Group not found for internal_uuid: %s", internalUUID)
		return nil, nil // Return nil for graceful degradation
	}

	if err != nil {
		return nil, fmt.Errorf("failed to enrich group principal: %w", err)
	}

	principal := &Principal{
		PrincipalType: PrincipalPrincipalTypeGroup,
		Provider:      provider,
		ProviderId:    groupName,
		DisplayName:   &displayName,
	}

	// Set optional email if available
	if email.Valid {
		emailAddr := openapi_types.Email(email.String)
		principal.Email = &emailAddr
	}

	return principal, nil
}
