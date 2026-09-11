package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ericfitz/tmi/internal/slogging"
)

const wsTicketTTL = 30 * time.Second

// GetWsTicket issues a short-lived WebSocket authentication ticket.
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: issue a short-lived WebSocket ticket bound to the caller's token for revocation
func (s *Server) GetWsTicket(c *gin.Context, params GetWsTicketParams) {
	logger := slogging.GetContextLogger(c)

	if s.ticketStore == nil {
		logger.Error("TicketStore not configured")
		HandleRequestError(c, ServerError("WebSocket tickets not available"))
		return
	}

	// Get authenticated user — second return is the provider user ID (from "userID" context key)
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Authentication required")
		HandleRequestError(c, err)
		return
	}

	sessionID := params.SessionId.String()

	// Find the session in the WebSocket hub
	session := s.wsHub.FindSessionByID(sessionID)
	if session == nil {
		HandleRequestError(c, NotFoundError("Collaboration session not found"))
		return
	}

	// Authorize: check that the user has read access to the threat model
	// associated with this session. This is the same check used by
	// GetDiagramCollaborate. We use threat model access rather than checking
	// connected clients because the user hasn't connected via WebSocket yet.
	tm, err := ThreatModelStore.Get(session.ThreatModelID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Collaboration session not found"))
		return
	}

	hasReadAccess, err := CheckResourceAccessFromContext(c, user, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasReadAccess {
		// Return 404 rather than 403 to prevent resource enumeration
		HandleRequestError(c, NotFoundError("Collaboration session not found"))
		return
	}

	// Bind the ticket to the issuing token so the upgrade is refused once that
	// token or its client credential is revoked (#869).
	claims := TicketClaims{
		UserID:       user.ProviderID,
		Provider:     c.GetString("userProvider"),
		InternalUUID: c.GetString("userInternalUUID"),
		SessionID:    sessionID,
		TokenHash:    c.GetString("authTokenHash"),
		CredentialID: c.GetString("serviceAccountCredentialID"),
	}
	ticket, err := s.ticketStore.IssueTicket(c.Request.Context(), claims, wsTicketTTL)
	if err != nil {
		logger.Error("Failed to issue WebSocket ticket: %v", err)
		HandleRequestError(c, ServerError("Failed to issue ticket"))
		return
	}

	logger.Info("Issued WebSocket ticket for user %s, session %s", user.Email, sessionID)

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, WsTicketResponse{
		Ticket: ticket,
	})
}
