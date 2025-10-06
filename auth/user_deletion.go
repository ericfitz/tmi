package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DeletionResult contains statistics about the user deletion operation
type DeletionResult struct {
	ThreatModelsTransferred int    `json:"threat_models_transferred"`
	ThreatModelsDeleted     int    `json:"threat_models_deleted"`
	UserEmail               string `json:"user_email"`
}

// DeletionChallenge contains challenge information for user deletion
type DeletionChallenge struct {
	ChallengeText string    `json:"challenge_text"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// GenerateDeletionChallenge creates a challenge token for user deletion
func (s *Service) GenerateDeletionChallenge(ctx context.Context, userEmail string) (*DeletionChallenge, error) {
	// Generate random challenge token
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("failed to generate challenge token: %w", err)
	}
	token := base64.URLEncoding.EncodeToString(b)

	// Create challenge text
	challengeText := fmt.Sprintf(
		"I am user %s. I want to delete all my data. I acknowledge that this action is irreversible and will irrevocably delete all threat models for which I am the owner, and all references to my user account. My challenge token is %s.",
		userEmail, token,
	)

	// Store challenge in Redis with 3-minute expiration
	challengeKey := fmt.Sprintf("user_deletion_challenge:%s", userEmail)
	expiresAt := time.Now().Add(3 * time.Minute)

	err := s.dbManager.Redis().Set(ctx, challengeKey, token, 3*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to store challenge token: %w", err)
	}

	slogging.Get().Info("User deletion challenge issued: user=%s, expires_at=%s", userEmail, expiresAt.Format(time.RFC3339))

	return &DeletionChallenge{
		ChallengeText: challengeText,
		ExpiresAt:     expiresAt,
	}, nil
}

// ValidateDeletionChallenge verifies the challenge string matches the stored token
func (s *Service) ValidateDeletionChallenge(ctx context.Context, userEmail, challengeText string) error {
	// Retrieve stored token from Redis
	challengeKey := fmt.Sprintf("user_deletion_challenge:%s", userEmail)
	storedToken, err := s.dbManager.Redis().Get(ctx, challengeKey)
	if err != nil {
		slogging.Get().Error("Failed to retrieve challenge token for user %s: %v", userEmail, err)
		return fmt.Errorf("invalid or expired challenge")
	}

	// Extract token from challenge text
	expectedPrefix := fmt.Sprintf(
		"I am user %s. I want to delete all my data. I acknowledge that this action is irreversible and will irrevocably delete all threat models for which I am the owner, and all references to my user account. My challenge token is ",
		userEmail,
	)
	expectedSuffix := "."

	if len(challengeText) <= len(expectedPrefix)+len(expectedSuffix) {
		slogging.Get().Error("SECURITY: Challenge text format mismatch for user %s", userEmail)
		return fmt.Errorf("invalid challenge format")
	}

	if challengeText[:len(expectedPrefix)] != expectedPrefix || challengeText[len(challengeText)-1:] != expectedSuffix {
		slogging.Get().Error("SECURITY: Challenge text structure mismatch for user %s", userEmail)
		return fmt.Errorf("invalid challenge format")
	}

	providedToken := challengeText[len(expectedPrefix) : len(challengeText)-1]

	// Compare tokens
	if providedToken != storedToken {
		slogging.Get().Error("SECURITY: Challenge token mismatch for user %s (expected: %.10s..., got: %.10s...)",
			userEmail, storedToken, providedToken)
		return fmt.Errorf("invalid challenge token")
	}

	// Delete the challenge from Redis (single use)
	_ = s.dbManager.Redis().Del(ctx, challengeKey)

	return nil
}

// DeleteUserAndData deletes a user and handles ownership transfer for threat models
func (s *Service) DeleteUserAndData(ctx context.Context, userEmail string) (*DeletionResult, error) {
	db := s.dbManager.Postgres().GetDB()

	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			slogging.Get().Error("Failed to rollback user deletion transaction: %v", err)
		}
	}()

	result := &DeletionResult{
		UserEmail: userEmail,
	}

	// Get all threat models owned by user
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM threat_models WHERE owner_email = $1`,
		userEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to query owned threat models: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slogging.Get().Error("Failed to close rows: %v", err)
		}
	}()

	var threatModelIDs []string
	for rows.Next() {
		var tmID string
		if err := rows.Scan(&tmID); err != nil {
			return nil, fmt.Errorf("failed to scan threat model ID: %w", err)
		}
		threatModelIDs = append(threatModelIDs, tmID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating threat models: %w", err)
	}

	// Process each threat model
	for _, tmID := range threatModelIDs {
		// Find alternate owner
		var altOwner string
		err := tx.QueryRowContext(ctx, `
			SELECT user_email FROM threat_model_access
			WHERE threat_model_id = $1
			  AND role = 'owner'
			  AND user_email != $2
			LIMIT 1`,
			tmID, userEmail).Scan(&altOwner)

		if err == sql.ErrNoRows {
			// No alternate owner - delete threat model (cascades to children)
			_, err = tx.ExecContext(ctx,
				`DELETE FROM threat_models WHERE id = $1`,
				tmID)
			if err != nil {
				return nil, fmt.Errorf("failed to delete threat model %s: %w", tmID, err)
			}
			result.ThreatModelsDeleted++
			slogging.Get().Debug("Deleted threat model %s (no alternate owner)", tmID)
		} else if err != nil {
			return nil, fmt.Errorf("failed to find alternate owner for threat model %s: %w", tmID, err)
		} else {
			// Transfer ownership to alternate owner
			_, err = tx.ExecContext(ctx,
				`UPDATE threat_models SET owner_email = $1, modified_at = $2 WHERE id = $3`,
				altOwner, time.Now().UTC(), tmID)
			if err != nil {
				return nil, fmt.Errorf("failed to transfer ownership of threat model %s: %w", tmID, err)
			}

			// Remove deleting user's permissions
			_, err = tx.ExecContext(ctx, `
				DELETE FROM threat_model_access
				WHERE threat_model_id = $1 AND user_email = $2`,
				tmID, userEmail)
			if err != nil {
				return nil, fmt.Errorf("failed to remove user permissions from threat model %s: %w", tmID, err)
			}

			result.ThreatModelsTransferred++
			slogging.Get().Debug("Transferred ownership of threat model %s to %s", tmID, altOwner)
		}
	}

	// Clean up any remaining permissions (reader/writer on other threat models)
	_, err = tx.ExecContext(ctx,
		`DELETE FROM threat_model_access WHERE user_email = $1`,
		userEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to clean up remaining permissions: %w", err)
	}

	// Delete user record (cascades to user_providers)
	deleteResult, err := tx.ExecContext(ctx,
		`DELETE FROM users WHERE email = $1`,
		userEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := deleteResult.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, fmt.Errorf("user not found: %s", userEmail)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	slogging.Get().Info("User deleted successfully: email=%s, transferred=%d, deleted=%d",
		userEmail, result.ThreatModelsTransferred, result.ThreatModelsDeleted)

	return result, nil
}
