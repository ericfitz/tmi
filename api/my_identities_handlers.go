package api

// my_identities_handlers.go — GET /me/identities and DELETE /me/identities/{id} (#383).

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

// ListMyIdentities handles GET /me/identities.
// Returns the primary identity from JWT claims and all linked identities for
// the authenticated user.
func (s *Server) ListMyIdentities(c *gin.Context) {
	logger := slogging.Get().WithContext(c)
	logger.Debug("[SERVER_INTERFACE] ListMyIdentities called")

	// Reject service accounts.
	if IsServiceAccountRequest(c) {
		logger.Warn("Service account attempted to list identities: %s", GetUserIdentityForLogging(c))
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Service accounts cannot list identities",
		})
		return
	}

	userUUID := c.GetString("userInternalUUID")
	if userUUID == "" {
		logger.Warn("ListMyIdentities: userInternalUUID not in context")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return
	}

	// Build primary identity from JWT context values set by the JWT middleware.
	primaryProvider := c.GetString("userIdP")
	primaryEmail := c.GetString("userEmail")
	primaryName := c.GetString("userDisplayName")

	if s.linkedIdentityStore == nil {
		logger.Error("ListMyIdentities: linkedIdentityStore not wired")
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Identity store not available",
		})
		return
	}

	ctx := c.Request.Context()
	rows, err := s.linkedIdentityStore.ListByUser(ctx, userUUID)
	if err != nil {
		logger.Error("ListMyIdentities: store error: %v", err)
		reqErr := StoreErrorToRequestError(err, "linked identities not found", "Failed to retrieve linked identities")
		c.JSON(reqErr.Status, Error{
			Error:            reqErr.Code,
			ErrorDescription: reqErr.Message,
		})
		return
	}

	// Build linked array.
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
			"provider": primaryProvider,
			"email":    primaryEmail,
			"name":     primaryName,
		},
		"linked": linked,
	})
}

// DeleteMyIdentity handles DELETE /me/identities/{id}.
// Removes a linked identity from the authenticated user's account.
// Returns 404 for unknown or foreign IDs (no distinguishable response).
func (s *Server) DeleteMyIdentity(c *gin.Context, id openapi_types.UUID) {
	logger := slogging.Get().WithContext(c)
	idStr := id.String()
	logger.Debug("[SERVER_INTERFACE] DeleteMyIdentity called id=%s", idStr)

	// Reject service accounts.
	if IsServiceAccountRequest(c) {
		logger.Warn("Service account attempted to unlink identity: %s", GetUserIdentityForLogging(c))
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Service accounts cannot unlink identities",
		})
		return
	}

	userUUID := c.GetString("userInternalUUID")
	if userUUID == "" {
		logger.Warn("DeleteMyIdentity: userInternalUUID not in context")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return
	}

	if s.linkedIdentityStore == nil {
		logger.Error("DeleteMyIdentity: linkedIdentityStore not wired")
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Identity store not available",
		})
		return
	}

	ctx := c.Request.Context()

	// Load the linked identity before deletion so we can audit it.
	// We do this by listing the user's identities and finding the one with the ID.
	// This is safe because Delete will fail with ErrLinkedIdentityNotFound if the
	// ID belongs to a different user.
	rows, err := s.linkedIdentityStore.ListByUser(ctx, userUUID)
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

	if err := s.linkedIdentityStore.Delete(ctx, idStr, userUUID); err != nil {
		if errors.Is(err, auth.ErrLinkedIdentityNotFound) {
			c.JSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Linked identity not found",
			})
			return
		}
		logger.Error("DeleteMyIdentity: delete failed: %v", err)
		reqErr := StoreErrorToRequestError(err, "linked identity not found", "Failed to delete linked identity")
		c.JSON(reqErr.Status, Error{
			Error:            reqErr.Code,
			ErrorDescription: reqErr.Message,
		})
		return
	}

	// Audit the unlink event.
	if s.identityLinkAuditor != nil && linkedProvider != "" {
		actor := auth.IdentityLinkActor{
			Email:          c.GetString("userEmail"),
			Provider:       c.GetString("userIdP"),
			ProviderUserID: c.GetString("userID"), // "userID" = provider_user_id (JWT sub) in jwt_auth.go
			DisplayName:    c.GetString("userDisplayName"),
			UserUUID:       userUUID,
		}
		_ = s.identityLinkAuditor.LogUnlink(ctx, actor, linkedProvider, linkedSub)
	}

	c.Status(http.StatusNoContent)
}
