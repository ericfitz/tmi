package api

import (
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// UserDeletionHandler handles user self-deletion operations
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: HTTP handler struct for the two-step user self-deletion flow
type UserDeletionHandler struct {
	authService *auth.Service
}

// NewUserDeletionHandler creates a new user deletion handler
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: build a UserDeletionHandler wired to the given auth service (pure)
func NewUserDeletionHandler(authService *auth.Service) *UserDeletionHandler {
	return &UserDeletionHandler{
		authService: authService,
	}
}

// DeleteUserAccount handles the two-step user deletion process
// Step 1: No challenge parameter -> Generate and return challenge
// Step 2: With challenge parameter -> Validate and delete user
// SEM@c85b80a7fe0b19a3e43a1c6f9dc121ba2ccd093c: handle the two-step user self-deletion: issue a challenge or validate one and delete the account
func (h *UserDeletionHandler) DeleteUserAccount(c *gin.Context) {
	// Get authenticated user from context
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Check if challenge parameter is provided
	challengeText := c.Query("challenge")

	if challengeText == "" {
		// Step 1: Generate challenge
		h.generateChallenge(c, user.Email)
	} else {
		// Step 2: Validate challenge and delete user
		h.deleteWithChallenge(c, user.Email, challengeText)
	}
}

// generateChallenge creates and returns a deletion challenge for the user
// SEM@bd740ab90ce24a669adc1fa8b8153efbd33bac10: generate and return a deletion challenge token for the authenticated user (reads DB)
func (h *UserDeletionHandler) generateChallenge(c *gin.Context, userEmail string) {
	challenge, err := h.authService.GenerateDeletionChallenge(c.Request.Context(), userEmail)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to generate deletion challenge for user %s: %v", userEmail, err)
		HandleRequestError(c, ServerError("Failed to generate deletion challenge"))
		return
	}

	c.JSON(http.StatusOK, challenge)
}

// deleteWithChallenge validates the challenge and performs user deletion
// SEM@0538436fe19e71299239f10214d737a09cf94961: validate a deletion challenge, delete the user account and data, then blacklist the JWT (reads DB)
func (h *UserDeletionHandler) deleteWithChallenge(c *gin.Context, userEmail, challengeText string) {
	// Validate challenge
	err := h.authService.ValidateDeletionChallenge(c.Request.Context(), userEmail, challengeText)
	if err != nil {
		slogging.Get().WithContext(c).Error("Invalid deletion challenge for user %s: %v", userEmail, err)
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_challenge",
			Message: "Invalid or expired challenge",
		})
		return
	}

	// Delete user and data
	result, err := h.authService.DeleteUserAndData(c.Request.Context(), userEmail)
	if err != nil {
		slogging.Get().WithContext(c).Error("Failed to delete user %s: %v", userEmail, err)
		HandleRequestError(c, ServerError("Failed to delete user account"))
		return
	}

	// Log successful deletion with statistics
	slogging.Get().WithContext(c).Info("User account deleted: email=%s, transferred=%d, deleted=%d",
		result.UserEmail, result.ThreatModelsTransferred, result.ThreatModelsDeleted)

	// Blacklist the JWT token so it can no longer be used
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		tokenStr := extractBearerToken(authHeader)
		if tokenStr != "" {
			if err := h.authService.BlacklistToken(c.Request.Context(), tokenStr); err != nil {
				// Log but don't fail - the user has been deleted, token invalidation is best-effort
				slogging.Get().WithContext(c).Warn("Failed to blacklist token after user deletion: %v", err)
			} else {
				slogging.Get().WithContext(c).Debug("JWT token blacklisted after user deletion")
			}
		}
	}

	// Return 204 No Content for successful deletion
	c.Status(http.StatusNoContent)
}

// extractBearerToken extracts the token from an Authorization header (case-insensitive prefix)
// SEM@034968fa0e0ba8c15e9af9052b475f4d5dd72d50: extract the bearer token string from an Authorization header value (pure)
func extractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return authHeader[len(prefix):]
	}
	return ""
}
