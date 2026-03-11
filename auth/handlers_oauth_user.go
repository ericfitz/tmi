package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// extractEmailWithFallback extracts email from userInfo/claims with fallback to synthetic email
func (h *Handlers) extractEmailWithFallback(c *gin.Context, providerID string, userInfo *UserInfo, claims *IDTokenClaims) (string, error) {
	email := userInfo.Email
	if email == "" && claims != nil {
		email = claims.Email
	}

	if email == "" {
		// Enhanced logging for email retrieval failure
		claimsEmail := "<no_claims>"
		if claims != nil {
			claimsEmail = claims.Email
		}
		slogging.Get().WithContext(c).Warn("OAuth provider returned empty email - using fallback (provider: %s, user_id: %s, name: %s, userInfo.Email: %s, claims.Email: %s, email_verified: %v)",
			providerID, userInfo.ID, userInfo.Name, userInfo.Email, claimsEmail, userInfo.EmailVerified)

		// Fallback: use provider user ID as email identifier
		// This handles cases where:
		// - GitHub user has private email or unverified email
		// - Provider doesn't return email in userinfo or ID token claims
		if userInfo.ID == "" {
			slogging.Get().WithContext(c).Error("OAuth provider returned no email and no user ID (provider: %s, name: %s)", providerID, userInfo.Name)
			return "", fmt.Errorf("no email or user ID found")
		}

		// Create synthetic email from provider ID and user ID
		// Format: <provider>-<user_id>@<provider>.oauth.tmi
		email = fmt.Sprintf("%s-%s@%s.oauth.tmi", providerID, userInfo.ID, providerID)
		slogging.Get().WithContext(c).Info("Using fallback email for OAuth user (provider: %s, user_id: %s, fallback_email: %s)",
			providerID, userInfo.ID, email)
	}

	return email, nil
}

// userMatchType indicates how a user was matched during login
type userMatchType int

const (
	userMatchNone          userMatchType = iota // No match found, need to create new user
	userMatchProviderID                         // Matched by provider + provider_user_id (strongest)
	userMatchProviderEmail                      // Matched by provider + email
	userMatchEmailOnly                          // Matched by email only (sparse record)
)

// findOrCreateUser implements tiered user matching strategy:
// 1. Provider + Provider ID (strongest) - can update email and name
// 2. Provider + Email - can update name
// 3. Email only (sparse record) - can update provider, provider_id, and name
// Returns the user, match type, and any error
func (h *Handlers) findOrCreateUser(ctx context.Context, c *gin.Context, providerID, providerUserID, email, name string, emailVerified bool) (User, userMatchType, error) {
	logger := slogging.Get().WithContext(c)

	// Tier 1: Try to match by provider + provider_user_id (strongest match)
	user, err := h.service.GetUserByProviderID(ctx, providerID, providerUserID)
	if err == nil {
		logger.Debug("User matched by provider+provider_id: provider=%s, provider_id=%s, email=%s",
			providerID, providerUserID, user.Email)
		return user, userMatchProviderID, nil
	}

	// Tier 2: Try to match by provider + email
	user, err = h.service.GetUserByProviderAndEmail(ctx, providerID, email)
	if err == nil {
		logger.Debug("User matched by provider+email: provider=%s, email=%s, existing_provider_id=%s",
			providerID, email, user.ProviderUserID)
		return user, userMatchProviderEmail, nil
	}

	// Tier 3: Try to match by email only (sparse record or different provider)
	user, err = h.service.GetUserByEmail(ctx, email)
	if err == nil {
		// Check if this is a sparse record (no provider set) or a different provider
		if user.Provider == "" {
			logger.Debug("User matched by email only (sparse record): email=%s", email)
			return user, userMatchEmailOnly, nil
		}
		// User exists with a different provider - this is a conflict
		// For now, we'll treat it as a sparse record match to allow completing it
		// In a multi-provider setup, you might want to link accounts instead
		logger.Info("User matched by email with different provider: email=%s, existing_provider=%s, new_provider=%s",
			email, user.Provider, providerID)
		return user, userMatchEmailOnly, nil
	}

	// No match found - need to create new user
	logger.Debug("No existing user found, will create new: provider=%s, provider_id=%s, email=%s",
		providerID, providerUserID, email)

	nowTime := time.Now()
	newUser := User{
		Provider:       providerID,
		ProviderUserID: providerUserID,
		Email:          email,
		Name:           name,
		EmailVerified:  emailVerified,
		CreatedAt:      nowTime,
		ModifiedAt:     nowTime,
		LastLogin:      &nowTime,
	}

	createdUser, err := h.service.CreateUser(ctx, newUser)
	if err != nil {
		logger.Error("Failed to create new user: email=%s, name=%s, error=%v", email, name, err)
		return User{}, userMatchNone, fmt.Errorf("failed to create user: %w", err)
	}

	return createdUser, userMatchNone, nil
}

// updateUserOnLogin updates user fields based on match type and OAuth data
func (h *Handlers) updateUserOnLogin(ctx context.Context, c *gin.Context, user *User, matchType userMatchType, providerID, providerUserID, email, name string, emailVerified bool) error {
	logger := slogging.Get().WithContext(c)
	updateNeeded := false

	now := time.Now()
	user.LastLogin = &now
	user.ModifiedAt = now

	switch matchType {
	case userMatchProviderID:
		// Strongest match - can update email and name if changed
		if email != "" && user.Email != email {
			logger.Info("Updating user email on login: old=%s, new=%s (matched by provider_id)", user.Email, email)
			user.Email = email
			updateNeeded = true
		}
		if name != "" && user.Name != name {
			logger.Info("Updating user name on login: old=%s, new=%s (matched by provider_id)", user.Name, name)
			user.Name = name
			updateNeeded = true
		}

	case userMatchProviderEmail:
		// Medium match - can update name and provider_user_id if empty
		if user.ProviderUserID == "" && providerUserID != "" {
			logger.Info("Completing user record with provider_id: user=%s, provider_id=%s", user.Email, providerUserID)
			user.ProviderUserID = providerUserID
			updateNeeded = true
		}
		if name != "" && user.Name != name {
			logger.Info("Updating user name on login: old=%s, new=%s (matched by provider+email)", user.Name, name)
			user.Name = name
			updateNeeded = true
		}

	case userMatchEmailOnly:
		// Sparse record match - update provider, provider_id, and name
		if user.Provider == "" && providerID != "" {
			logger.Info("Completing sparse user record with provider: user=%s, provider=%s", user.Email, providerID)
			user.Provider = providerID
			updateNeeded = true
		}
		if user.ProviderUserID == "" && providerUserID != "" {
			logger.Info("Completing sparse user record with provider_id: user=%s, provider_id=%s", user.Email, providerUserID)
			user.ProviderUserID = providerUserID
			updateNeeded = true
		}
		if name != "" && user.Name != name {
			logger.Info("Updating user name on login: old=%s, new=%s (matched by email only)", user.Name, name)
			user.Name = name
			updateNeeded = true
		}
	}

	// Always update email_verified status (one-way: false -> true)
	if emailVerified && !user.EmailVerified {
		user.EmailVerified = true
		updateNeeded = true
	}

	if updateNeeded {
		if err := h.service.UpdateUser(ctx, *user); err != nil {
			logger.Error("Failed to update user profile during login: %v (user_id: %s)", err, user.InternalUUID)
			return err
		}
	}

	return nil
}
