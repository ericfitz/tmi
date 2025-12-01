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
//
// Group principals are skipped (no enrichment needed).
func EnrichAuthorizationEntry(ctx context.Context, db *sql.DB, auth *Authorization) error {
	logger := slogging.Get()

	// Skip enrichment for group principals - they don't have user records
	if auth.PrincipalType == "group" {
		logger.Debug("Skipping enrichment for group principal: provider=%s, provider_id=%s",
			auth.Provider, auth.ProviderId)
		return nil
	}

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
	// Note: provider_user_id can be NULL for sparse users (created before first OAuth login)
	var internalUUID, provider, email, name string
	var providerUserID sql.NullString
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
	// NOTE: Do NOT set auth.ProviderId = internalUUID!
	// ProviderId should remain as the user-provided identifier (email or OAuth sub)
	// The database_store.go saveAuthorizationTx() will resolve it to internal_uuid when saving
	auth.Provider = provider
	if auth.Email == nil || string(*auth.Email) == "" {
		emailAddr := openapi_types.Email(email)
		auth.Email = &emailAddr
	}
	if auth.DisplayName == nil || *auth.DisplayName == "" {
		auth.DisplayName = &name
	}

	providerIDStr := "<null>"
	if providerUserID.Valid {
		providerIDStr = providerUserID.String
	}
	logger.Debug("Enriched authorization entry: provider=%s, internal_uuid=%s, provider_user_id=%s, email=%s, name=%s, keeping provider_id=%s",
		provider, internalUUID, providerIDStr, email, name, auth.ProviderId)

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

	// Use email as fallback name if no name provided
	displayName := ""
	if auth.DisplayName != nil {
		displayName = *auth.DisplayName
	}
	if displayName == "" && email != "" {
		displayName = email
	}

	// Sparse users can be created with just email (provider_user_id will be null until first login)
	logger.Info("Creating sparse user record: provider=%s, provider_user_id=%s, email=%s",
		auth.Provider, providerUserID, email)

	// Use different insert strategy based on what we have
	var err error
	if providerUserID != "" {
		// Have provider_user_id - insert with ON CONFLICT on (provider, provider_user_id)
		query := `
			INSERT INTO users (provider, provider_user_id, email, name, email_verified, created_at, modified_at)
			VALUES ($1, $2, NULLIF($3, ''), $4, false, NOW(), NOW())
			ON CONFLICT (provider, provider_user_id) DO NOTHING
		`
		_, err = db.ExecContext(ctx, query, auth.Provider, providerUserID, email, displayName)
	} else if email != "" {
		// Only have email - insert with ON CONFLICT on (provider, email)
		query := `
			INSERT INTO users (provider, provider_user_id, email, name, email_verified, created_at, modified_at)
			VALUES ($1, NULL, $2, $3, false, NOW(), NOW())
			ON CONFLICT (provider, email) DO NOTHING
		`
		_, err = db.ExecContext(ctx, query, auth.Provider, email, displayName)
	} else {
		return &RequestError{
			Status:  400,
			Code:    "validation_failed",
			Message: "either provider_id or email must be provided for sparse user creation",
		}
	}

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
