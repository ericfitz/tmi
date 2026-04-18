package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
)

// ContentToken is the domain representation of a per-user OAuth token used
// by delegated content providers. Access/refresh tokens are plaintext here;
// the repository handles encryption at rest.
type ContentToken struct {
	ID                   string
	UserID               string
	ProviderID           string
	AccessToken          string //nolint:gosec // G117 - domain type holds plaintext after repository decryption; not persisted directly
	RefreshToken         string //nolint:gosec // G117 - domain type holds plaintext after repository decryption; not persisted directly
	Scopes               string
	ExpiresAt            *time.Time
	Status               string // ContentTokenStatus*
	LastRefreshAt        *time.Time
	LastError            string
	ProviderAccountID    string
	ProviderAccountLabel string
	CreatedAt            time.Time
	ModifiedAt           time.Time
}

// ContentTokenStatus constants for the Status field of ContentToken.
const (
	ContentTokenStatusActive        = "active"
	ContentTokenStatusFailedRefresh = "failed_refresh"
)

// Typed errors for content-token repository operations.
// Each wraps the corresponding dberrors sentinel so handlers can check
// either the entity-specific error or the generic category.
var (
	ErrContentTokenNotFound  = fmt.Errorf("content token: %w", dberrors.ErrNotFound)
	ErrContentTokenDuplicate = fmt.Errorf("content token: %w", dberrors.ErrDuplicate)
)

// ContentTokenRepository is the repository abstraction over user_content_tokens.
// All methods return typed errors from internal/dberrors.
type ContentTokenRepository interface {
	// GetByUserAndProvider retrieves a token by user ID and provider ID.
	// Returns ErrContentTokenNotFound if no matching token exists.
	GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)

	// ListByUser returns all tokens for the given user ID.
	ListByUser(ctx context.Context, userID string) ([]ContentToken, error)

	// Upsert creates or updates a token for the (UserID, ProviderID) pair.
	// Returns ErrContentTokenDuplicate on an unexpected unique-key conflict.
	Upsert(ctx context.Context, token *ContentToken) error

	// UpdateStatus updates the status and last_error fields for the token with the given ID.
	// Returns ErrContentTokenNotFound if the token does not exist.
	UpdateStatus(ctx context.Context, id, status, lastError string) error

	// Delete removes the token with the given ID.
	// Returns ErrContentTokenNotFound if the token does not exist.
	Delete(ctx context.Context, id string) error

	// DeleteByUserAndProvider removes the token for the given user/provider pair and
	// returns the deleted token. Returns ErrContentTokenNotFound if it did not exist.
	DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)

	// RefreshWithLock opens a transaction, SELECT ... FOR UPDATE on the row,
	// invokes fn with the current decrypted token, and persists the returned
	// token. Returns the updated token or the fn error.
	RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error)
}
