package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// EnrichAuthorizationEntry enriches a single Authorization entry by looking up missing fields
// from the users table. The caller must provide:
//   - provider: REQUIRED - the identity provider name
//   - EXACTLY ONE OF: provider_id (email/OAuth sub) OR email
//
// The function will lookup the user in the database and fill in missing fields.
// For new users (not yet in database), it performs a sparse insert that will be
// completed when the user logs in via OAuth.
func EnrichAuthorizationEntry(ctx context.Context, db *sql.DB, auth *Authorization) error {
	logger := slogging.Get()

	// Validate required fields
	if auth.Provider == "" {
		return &RequestError{
			Status:  400,
			Code:    "validation_failed",
			Message: "provider is required for authorization entries",
		}
	}

	// Validate that exactly one identifier is provided
	hasProviderID := auth.ProviderId != ""
	hasEmail := auth.Email != nil && string(*auth.Email) != ""

	if !hasProviderID && !hasEmail {
		return &RequestError{
			Status:  400,
			Code:    "validation_failed",
			Message: "either provider_id or email must be provided for authorization entries",
		}
	}

	// Note: We allow both to be provided, but we'll use provider_id as primary

	// Build the query based on what identifier was provided
	var query string
	var queryParam string

	if hasProviderID {
		// Primary path: lookup by provider_id
		query = `
			SELECT internal_uuid, provider, provider_user_id, email, name
			FROM users
			WHERE provider_user_id = $1 AND provider = $2
		`
		queryParam = auth.ProviderId
	} else {
		// Secondary path: lookup by email
		query = `
			SELECT internal_uuid, provider, provider_user_id, email, name
			FROM users
			WHERE email = $1 AND provider = $2
		`
		queryParam = string(*auth.Email)
	}

	// Query the database
	var internalUUID, provider, providerUserID, email, name string
	err := db.QueryRowContext(ctx, query, queryParam, auth.Provider).Scan(
		&internalUUID, &provider, &providerUserID, &email, &name,
	)

	if err == sql.ErrNoRows {
		// User not found in database - perform sparse insert
		logger.Debug("User not found in database, performing sparse insert for provider=%s, identifier=%s",
			auth.Provider, queryParam)

		insertErr := performSparseUserInsert(ctx, db, auth)
		if insertErr != nil {
			return insertErr
		}

		// After insert, query again to get the internal_uuid
		err = db.QueryRowContext(ctx, query, queryParam, auth.Provider).Scan(
			&internalUUID, &provider, &providerUserID, &email, &name,
		)
		if err != nil {
			logger.Error("Failed to query user after sparse insert: %v", err)
			return &RequestError{
				Status:  500,
				Code:    "server_error",
				Message: "Failed to lookup user after creation",
			}
		}
	} else if err != nil {
		logger.Error("Database error looking up user: %v", err)
		return &RequestError{
			Status:  500,
			Code:    "server_error",
			Message: fmt.Sprintf("Failed to lookup user: %v", err),
		}
	}

	// Enrich the Authorization entry with database values
	auth.ProviderId = internalUUID // Use internal_uuid as the canonical identifier
	auth.Provider = provider
	if auth.Email == nil || string(*auth.Email) == "" {
		emailAddr := openapi_types.Email(email)
		auth.Email = &emailAddr
	}
	if auth.DisplayName == nil || *auth.DisplayName == "" {
		auth.DisplayName = &name
	}

	logger.Debug("Enriched authorization entry: provider=%s, internal_uuid=%s, email=%s, name=%s",
		provider, internalUUID, email, name)

	return nil
}

// performSparseUserInsert creates a sparse user record that will be completed on first login
func performSparseUserInsert(ctx context.Context, db *sql.DB, auth *Authorization) error {
	logger := slogging.Get()

	// Determine what values we have
	var email, providerUserID string
	if auth.Email != nil {
		email = string(*auth.Email)
	}
	if auth.ProviderId != "" {
		providerUserID = auth.ProviderId
	}

	// If we only have email, use it as provider_user_id temporarily
	if providerUserID == "" && email != "" {
		providerUserID = email
	}

	// If we only have provider_user_id, we can't populate email (leave it null)
	// The OAuth flow will populate it later

	logger.Info("Creating sparse user record: provider=%s, provider_user_id=%s, email=%s",
		auth.Provider, providerUserID, email)

	insertQuery := `
		INSERT INTO users (provider, provider_user_id, email, name, email_verified, created_at, modified_at)
		VALUES ($1, $2, NULLIF($3, ''), COALESCE(NULLIF($4, ''), $3), false, NOW(), NOW())
		ON CONFLICT (provider, provider_user_id) DO NOTHING
	`

	// Use email as fallback name if no name provided
	displayName := ""
	if auth.DisplayName != nil {
		displayName = *auth.DisplayName
	}

	_, err := db.ExecContext(ctx, insertQuery, auth.Provider, providerUserID, email, displayName)
	if err != nil {
		logger.Error("Failed to insert sparse user record: %v", err)
		return &RequestError{
			Status:  500,
			Code:    "server_error",
			Message: "Failed to create user record",
		}
	}

	return nil
}

// EnrichAuthorizationList enriches all authorization entries in a list
func EnrichAuthorizationList(ctx context.Context, db *sql.DB, authList []Authorization) error {
	for i := range authList {
		if err := EnrichAuthorizationEntry(ctx, db, &authList[i]); err != nil {
			return err
		}
	}
	return nil
}
