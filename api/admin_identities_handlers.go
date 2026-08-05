package api

// admin_identities_handlers.go — GET /admin/users/{user_id}/identities and
// DELETE /admin/users/{user_id}/identities/{identity_id} (#646).
//
// Mirrors my_identities_handlers.go's response shape and error routing, but
// operates on a target user (identified by the user_id path parameter)
// instead of the authenticated caller. The primary identity lives on the
// users table and has no linked_identities row, so it is structurally not
// addressable by the delete operation; unknown identity ids return 404.
// Content-provider tokens are untouched here — admins revoke those via the
// existing /admin/users/{user_id}/content_tokens endpoints.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
)

// AdminListUserIdentities handles GET /admin/users/{user_id}/identities.
// Returns the target user's primary identity (from the user record) and all
// of their linked identities.
// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: fetch a target user's primary and linked identities for an admin (reads DB)
func (s *Server) AdminListUserIdentities(c *gin.Context, userId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)
	logger.Debug("[SERVER_INTERFACE] AdminListUserIdentities called user_id=%s", userId.String())

	if s.linkedIdentityStore == nil {
		logger.Error("AdminListUserIdentities: linkedIdentityStore not wired")
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Identity store not available",
		})
		return
	}

	ctx := c.Request.Context()

	user, err := GlobalUserStore.Get(ctx, userId)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else {
			logger.Error("AdminListUserIdentities: failed to get user: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get user",
			})
		}
		return
	}

	rows, err := s.linkedIdentityStore.ListByUser(ctx, userId.String())
	if err != nil {
		logger.Error("AdminListUserIdentities: store error: %v", err)
		// Route through HandleRequestError so 503s carry Retry-After and errors
		// are sanitized/truncated consistently with the rest of the API (#665).
		HandleRequestError(c, StoreErrorToRequestError(err, "linked identities not found", "Failed to retrieve linked identities"))
		return
	}

	// Build linked array.
	// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: shape a linked identity record for the admin identities list response
	type linkedIdentityResponse struct {
		ID             string  `json:"id"`
		Provider       string  `json:"provider"`
		ProviderUserID string  `json:"provider_user_id"`
		Email          string  `json:"email,omitempty"`
		Name           string  `json:"name,omitempty"`
		LinkedAt       string  `json:"linked_at"`
		LastUsedAt     *string `json:"last_used_at,omitempty"`
	}

	linked := make([]linkedIdentityResponse, 0, len(rows))
	for _, row := range rows {
		truncatedSub := string(row.ProviderUserID)
		if len(truncatedSub) > 8 {
			truncatedSub = truncatedSub[:8] + "…"
		}
		entry := linkedIdentityResponse{
			ID:             string(row.ID),
			Provider:       string(row.Provider),
			ProviderUserID: truncatedSub,
			LinkedAt:       row.LinkedAt.UTC().Format(time.RFC3339),
		}
		if row.Email != "" {
			entry.Email = string(row.Email)
		}
		if row.Name != "" {
			entry.Name = string(row.Name)
		}
		if row.LastUsedAt != nil {
			s := row.LastUsedAt.UTC().Format(time.RFC3339)
			entry.LastUsedAt = &s
		}
		linked = append(linked, entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"primary": gin.H{
			"provider": user.Provider,
			"email":    string(user.Email),
			"name":     user.Name,
		},
		"linked": linked,
	})
}

// AdminDeleteUserIdentity handles DELETE /admin/users/{user_id}/identities/{identity_id}.
// Removes a linked identity from the target user's account. The primary
// identity has no linked_identities row and so cannot be addressed here;
// unknown or foreign identity ids return 404.
// SEM@a18fd6d1633a3e16fa7f6eba88b562d23e5ae4b6: delete a target user's linked identity and audit the unlink (mutates DB)
func (s *Server) AdminDeleteUserIdentity(c *gin.Context, userId openapi_types.UUID, identityId openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)
	idStr := identityId.String()
	logger.Debug("[SERVER_INTERFACE] AdminDeleteUserIdentity called user_id=%s id=%s", userId.String(), idStr)

	if s.linkedIdentityStore == nil {
		logger.Error("AdminDeleteUserIdentity: linkedIdentityStore not wired")
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Identity store not available",
		})
		return
	}

	ctx := c.Request.Context()

	if _, err := GlobalUserStore.Get(ctx, userId); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			HandleRequestError(c, &RequestError{
				Status:  http.StatusNotFound,
				Code:    "not_found",
				Message: "User not found",
			})
		} else {
			logger.Error("AdminDeleteUserIdentity: failed to get user: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to get user",
			})
		}
		return
	}

	userUUIDStr := userId.String()

	// Load the linked identity before deletion so we can audit it. This is
	// safe because Delete will fail with ErrLinkedIdentityNotFound if the ID
	// belongs to a different user.
	rows, err := s.linkedIdentityStore.ListByUser(ctx, userUUIDStr)
	var linkedProvider, linkedSub string
	if err == nil {
		for _, row := range rows {
			if strings.EqualFold(string(row.ID), idStr) {
				linkedProvider = string(row.Provider)
				linkedSub = string(row.ProviderUserID)
				break
			}
		}
	}

	if err := s.linkedIdentityStore.Delete(ctx, idStr, userUUIDStr); err != nil {
		if errors.Is(err, auth.ErrLinkedIdentityNotFound) {
			c.JSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Linked identity not found",
			})
			return
		}
		logger.Error("AdminDeleteUserIdentity: delete failed: %v", err)
		// Route through HandleRequestError so 503s carry Retry-After and errors
		// are sanitized/truncated consistently with the rest of the API (#665).
		HandleRequestError(c, StoreErrorToRequestError(err, "linked identity not found", "Failed to delete linked identity"))
		return
	}

	// Audit the unlink event. The actor is the administrator performing the
	// call (from the JWT auth context), matching DeleteMyIdentity's pattern.
	// IdentityLinkActor has no field for a separate target user, so the
	// target user_id is not carried in the audit row beyond this handler's
	// own debug log line above.
	if s.identityLinkAuditor != nil && linkedProvider != "" {
		actor := auth.IdentityLinkActor{
			Email:          c.GetString("userEmail"),
			Provider:       c.GetString("userIdP"),
			ProviderUserID: c.GetString("userID"), // "userID" = provider_user_id (JWT sub) in jwt_auth.go
			DisplayName:    c.GetString("userDisplayName"),
			UserUUID:       c.GetString("userInternalUUID"),
		}
		_ = s.identityLinkAuditor.LogUnlink(ctx, actor, linkedProvider, linkedSub)
	}

	c.Status(http.StatusNoContent)
}
