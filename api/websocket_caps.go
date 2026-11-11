package api

// WebSocket connection caps (T20/T21).
//
// CountUserConnections walks every active session and counts how many
// of its clients identify as the given user. Hub callers compare the
// result to MaxConnectionsPerUser before completing an upgrade.
//
// CountSessionParticipants returns the number of clients currently in
// the named diagram session. Hub callers compare it to
// MaxParticipantsPerSession before completing an upgrade.
//
// The bookkeeping is held in the existing Hub.Diagrams map; no
// dedicated index is maintained because the caller traverses on the
// (relatively rare) upgrade path only.

// MaxConnectionsPerUser is the cap on simultaneous WebSocket
// connections held by a single user across all sessions on this hub.
// Set to zero to disable the cap.
const MaxConnectionsPerUser = 5

// MaxParticipantsPerSession is the cap on clients inside one diagram
// session. Set to zero to disable the cap.
const MaxParticipantsPerSession = 50

// CountUserConnections returns the number of clients on this hub whose
// UserID matches the argument. Excluding the empty string is
// intentional — anonymous / pre-auth slots must not collapse into a
// single bucket.
func (h *WebSocketHub) CountUserConnections(userID string) int {
	if userID == "" {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, session := range h.Diagrams {
		session.mu.RLock()
		for client := range session.Clients {
			if client.UserID == userID {
				total++
			}
		}
		session.mu.RUnlock()
	}
	return total
}

// CountSessionParticipants returns the number of currently-registered
// clients on the session for the given diagram, or 0 if no session
// exists yet.
func (h *WebSocketHub) CountSessionParticipants(diagramID string) int {
	h.mu.RLock()
	session, ok := h.Diagrams[diagramID]
	h.mu.RUnlock()
	if !ok {
		return 0
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return len(session.Clients)
}
