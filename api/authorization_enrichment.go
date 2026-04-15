package api

import (
	"context"
	"errors"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
func EnrichAuthorizationEntry(ctx context.Context, db *gorm.DB, auth *Authorization) error {
	logger := slogging.Get()

	// Skip enrichment for group principals - they don't have user records
	if auth.PrincipalType == AuthorizationPrincipalTypeGroup {
		logger.Debug("Skipping enrichment for group principal: provider=%s, provider_id=%s",
			auth.Provider, auth.ProviderId)
		return nil
	}

	// Validate required fields — reject empty and legacy wildcard provider
	if auth.Provider == "" || auth.Provider == "*" {
		return &RequestError{
			Status:  400,
			Code:    "validation_failed",
			Message: "provider must be a valid identity provider name (e.g., \"tmi\", \"google\", \"github\")",
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

	// Build the query based on what identifier was provided.
	// If the primary lookup fails (e.g., client sent email as provider_id),
	// fall back to alternative lookups before resorting to sparse insert.
	var user models.User
	var result *gorm.DB
	found := false

	if hasProviderID {
		// Primary path: lookup by provider + provider_id
		// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
		result = db.WithContext(ctx).
			Where(map[string]any{"provider_user_id": auth.ProviderId, "provider": auth.Provider}).
			First(&user)
		if result.Error == nil {
			found = true
		} else if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			// The provider_id might actually be an email address (common mistake).
			// Try looking up by email before creating a duplicate sparse user.
			result = db.WithContext(ctx).
				Where(map[string]any{"email": auth.ProviderId, "provider": auth.Provider}).
				First(&user)
			if result.Error == nil {
				found = true
				logger.Debug("User found by email fallback: provider_id=%s matched email in database", auth.ProviderId)
			}
		}
	} else {
		// Secondary path: lookup by provider + email
		// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
		result = db.WithContext(ctx).
			Where(map[string]any{"email": string(*auth.Email), "provider": auth.Provider}).
			First(&user)
		if result.Error == nil {
			found = true
		}
	}

	if !found && result.Error != nil && !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		logger.Error("Database error looking up user: %v", result.Error)
		return &RequestError{
			Status:  500,
			Code:    "server_error",
			Message: "Failed to lookup user",
		}
	}

	if !found {
		// User not found by any lookup strategy - perform sparse insert
		queryParam := auth.ProviderId
		if !hasProviderID {
			queryParam = string(*auth.Email)
		}
		logger.Debug("User not found in database, performing sparse insert for provider=%s, identifier=%s",
			auth.Provider, queryParam)

		insertErr := performSparseUserInsert(ctx, db, auth)
		if insertErr != nil {
			return insertErr
		}

		// After insert, query again to get the internal_uuid
		// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
		if hasProviderID {
			result = db.WithContext(ctx).
				Where(map[string]any{"provider_user_id": auth.ProviderId, "provider": auth.Provider}).
				First(&user)
		} else {
			result = db.WithContext(ctx).
				Where(map[string]any{"email": string(*auth.Email), "provider": auth.Provider}).
				First(&user)
		}
		if result.Error != nil {
			logger.Error("Failed to query user after sparse insert: %v", result.Error)
			return &RequestError{
				Status:  500,
				Code:    "server_error",
				Message: "Failed to lookup user after creation",
			}
		}
	}

	// Enrich the Authorization entry with database values.
	// Normalize ProviderId to the canonical provider_user_id from the database
	// to prevent identity mismatches (e.g., email used as provider_id).
	auth.Provider = user.Provider
	if user.ProviderUserID != nil && *user.ProviderUserID != "" {
		if auth.ProviderId != *user.ProviderUserID {
			logger.Debug("Normalizing provider_id from %q to canonical %q", auth.ProviderId, *user.ProviderUserID)
		}
		auth.ProviderId = *user.ProviderUserID
	}
	if (auth.Email == nil || string(*auth.Email) == "") && user.Email != "" {
		emailAddr := openapi_types.Email(user.Email)
		auth.Email = &emailAddr
	}
	if (auth.DisplayName == nil || *auth.DisplayName == "") && user.Name != "" {
		auth.DisplayName = &user.Name
	}

	providerIDStr := "<null>"
	if user.ProviderUserID != nil {
		providerIDStr = *user.ProviderUserID
	}
	logger.Debug("Enriched authorization entry: provider=%s, internal_uuid=%s, provider_user_id=%s, email=%s, name=%s, provider_id=%s",
		user.Provider, user.InternalUUID, providerIDStr, user.Email, user.Name, auth.ProviderId)

	return nil
}

// performSparseUserInsert creates a sparse user record that will be completed on first login
func performSparseUserInsert(ctx context.Context, db *gorm.DB, auth *Authorization) error {
	logger := slogging.Get()

	// Determine what values we have
	var email string
	var providerUserID *string
	if auth.Email != nil {
		email = string(*auth.Email)
	}
	if auth.ProviderId != "" {
		providerUserID = &auth.ProviderId
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
	logger.Info("Creating sparse user record: provider=%s, provider_user_id=%v, email=%s",
		auth.Provider, providerUserID, email)

	// Create sparse user with GORM
	user := models.User{
		Provider:       auth.Provider,
		ProviderUserID: providerUserID,
		Email:          email,
		Name:           displayName,
		EmailVerified:  false,
	}

	// Use ON CONFLICT to handle duplicates
	// GORM handles this differently - try to create, ignore if exists
	// Use clause expressions for cross-database compatibility (Oracle requires uppercase column names)
	result := db.WithContext(ctx).
		Where(map[string]any{"provider": auth.Provider}).
		Where(
			db.Where(clause.Expr{SQL: "? = ?", Vars: []any{Col(db.Name(), "provider_user_id"), providerUserID}}).
				Or(clause.Expr{SQL: "? = ?", Vars: []any{Col(db.Name(), "email"), email}}),
		).
		FirstOrCreate(&user)

	if result.Error != nil {
		logger.Error("Failed to insert sparse user record: %v", result.Error)
		return &RequestError{
			Status:  500,
			Code:    "server_error",
			Message: "Failed to create user record",
		}
	}

	return nil
}

// EnrichAuthorizationList enriches all authorization entries in a list
func EnrichAuthorizationList(ctx context.Context, db *gorm.DB, authList []Authorization) error {
	for i := range authList {
		if err := EnrichAuthorizationEntry(ctx, db, &authList[i]); err != nil {
			return err
		}
	}
	return nil
}
