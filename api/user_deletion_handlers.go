package api

import (
	"net/http"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// UserDeletionHandler handles user self-deletion operations
type UserDeletionHandler struct {
	authService *auth.Service
}

// NewUserDeletionHandler creates a new user deletion handler
func NewUserDeletionHandler(authService *auth.Service) *UserDeletionHandler {
	return &UserDeletionHandler{
		authService: authService,
	}
}

// DeleteUserAccount handles the two-step user deletion process
// Step 1: No challenge parameter -> Generate and return challenge
// Step 2: With challenge parameter -> Validate and delete user
func (h *UserDeletionHandler) DeleteUserAccount(c *gin.Context) {
	// Get authenticated user from context
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Check if challenge parameter is provided
	challengeText := c.Query("challenge")

	if challengeText == "" {
		// Step 1: Generate challenge
		h.generateChallenge(c, userEmail)
	} else {
		// Step 2: Validate challenge and delete user
		h.deleteWithChallenge(c, userEmail, challengeText)
	}
}

// generateChallenge creates and returns a deletion challenge for the user
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

// extractBearerToken extracts the token from an Authorization header
func extractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if len(authHeader) > len(prefix) && authHeader[:len(prefix)] == prefix {
		return authHeader[len(prefix):]
	}
	return ""
}
