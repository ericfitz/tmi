package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// DeletionResult contains statistics about the user deletion operation
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: summary of transferred and deleted threat models produced by a user deletion (pure)
type DeletionResult struct {
	ThreatModelsTransferred int    `json:"threat_models_transferred"`
	ThreatModelsDeleted     int    `json:"threat_models_deleted"`
	UserEmail               string `json:"user_email"`
}

// DeletionChallenge contains challenge information for user deletion
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: confirmation challenge text and expiry issued to a user before self-deletion (pure)
type DeletionChallenge struct {
	ChallengeText string    `json:"challenge_text"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// GenerateDeletionChallenge creates a challenge token for user deletion
// SEM@a37a0039279be689bb07be2113fe86024a410a4b: generate and store a one-time deletion confirmation challenge for the user (mutates shared state)
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

	// Store challenge in Redis with 3-minute expiration.
	// Truncate to microseconds: this value is returned directly in the response
	// (never round-tripped through the database), and the OpenAPI timestamp
	// schema permits at most 6 fractional digits, so the raw nanosecond value
	// from time.Now() would fail response validation.
	challengeKey := fmt.Sprintf("user_deletion_challenge:%s", userEmail)
	expiresAt := time.Now().Add(3 * time.Minute).Truncate(time.Microsecond)

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
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: validate the user's deletion challenge response against the stored token (reads DB)
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

// DeleteUserAndData deletes a user by email and handles ownership transfer for threat models.
// Used by the self-deletion flow (DELETE /me) where identity comes from JWT email.
// SEM@cd187b523b66aef0fa87861d3a929c2017787b86: delete a user by email and transfer or remove owned threat models (mutates shared state)
func (s *Service) DeleteUserAndData(ctx context.Context, userEmail string) (*DeletionResult, error) {
	// Sweep content-token revocations BEFORE the DB delete so the token rows
	// are still present for the hook to read. Best-effort — failures are
	// logged inside the hook and must never block deletion.
	if s.preUserDeleteHook != nil {
		// Resolve email → internal UUID so the hook can query by user ID.
		user, err := s.userRepo.GetByEmail(ctx, userEmail)
		if err == nil {
			s.preUserDeleteHook.RevokeUserTokens(ctx, user.InternalUUID)
		} else {
			slogging.Get().Warn("pre-delete hook: could not resolve email to internal UUID for user %s: %v — skipping content-token revocations", userEmail, err)
		}
	}

	repoResult, err := s.deletionRepo.DeleteUserAndData(ctx, userEmail)
	if err != nil {
		return nil, err
	}

	return &DeletionResult{
		ThreatModelsTransferred: repoResult.ThreatModelsTransferred,
		ThreatModelsDeleted:     repoResult.ThreatModelsDeleted,
		UserEmail:               repoResult.UserEmail,
	}, nil
}

// DeleteUserByInternalUUID deletes a user by internal UUID and handles ownership transfer.
// Used by admin deletion to avoid multi-hop identity resolution that can target the wrong user.
// SEM@cd187b523b66aef0fa87861d3a929c2017787b86: delete a user by internal UUID and transfer or remove owned threat models (mutates shared state)
func (s *Service) DeleteUserByInternalUUID(ctx context.Context, internalUUID string) (*DeletionResult, error) {
	// Sweep content-token revocations BEFORE the DB delete so the token rows
	// are still present for the hook to read. Best-effort — failures are
	// logged inside the hook and must never block deletion.
	if s.preUserDeleteHook != nil {
		s.preUserDeleteHook.RevokeUserTokens(ctx, internalUUID)
	}

	repoResult, err := s.deletionRepo.DeleteUserByInternalUUID(ctx, internalUUID)
	if err != nil {
		return nil, err
	}

	return &DeletionResult{
		ThreatModelsTransferred: repoResult.ThreatModelsTransferred,
		ThreatModelsDeleted:     repoResult.ThreatModelsDeleted,
		UserEmail:               repoResult.UserEmail,
	}, nil
}
