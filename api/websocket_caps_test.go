package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeSession constructs a DiagramSession with the given clients
// already registered. It bypasses the Register channel because the
// hub's pump is not running in unit tests.
func fakeSession(t *testing.T, hub *WebSocketHub, diagramID string, clients []*WebSocketClient) *DiagramSession {
	t.Helper()
	s := &DiagramSession{
		ID:        "test-session-" + diagramID,
		DiagramID: diagramID,
		State:     SessionStateActive,
		Clients:   make(map[*WebSocketClient]bool, len(clients)),
		Hub:       hub,
	}
	for _, c := range clients {
		c.Session = s
		c.Hub = hub
		s.Clients[c] = true
	}
	hub.mu.Lock()
	hub.Diagrams[diagramID] = s
	hub.mu.Unlock()
	return s
}

func newTestHub() *WebSocketHub {
	return &WebSocketHub{
		Diagrams: make(map[string]*DiagramSession),
	}
}

// TestCountUserConnections_AcrossSessions pins that the per-user
// counter aggregates across every session on the hub, not just the
// session the upgrade is targeting.
func TestCountUserConnections_AcrossSessions(t *testing.T) {
	hub := newTestHub()

	alice := func() *WebSocketClient { return &WebSocketClient{UserID: "alice@example.com"} }
	bob := func() *WebSocketClient { return &WebSocketClient{UserID: "bob@example.com"} }

	fakeSession(t, hub, "diag-1", []*WebSocketClient{alice(), alice(), bob()})
	fakeSession(t, hub, "diag-2", []*WebSocketClient{alice(), bob(), bob()})

	assert.Equal(t, 3, hub.CountUserConnections("alice@example.com"))
	assert.Equal(t, 3, hub.CountUserConnections("bob@example.com"))
	assert.Equal(t, 0, hub.CountUserConnections("nobody@example.com"))
	assert.Equal(t, 0, hub.CountUserConnections(""), "empty userID must not collapse anonymous slots")
}

// TestCountSessionParticipants returns the count for the named diagram
// session, or zero when no session exists.
func TestCountSessionParticipants(t *testing.T) {
	hub := newTestHub()
	fakeSession(t, hub, "diag-X", []*WebSocketClient{
		{UserID: "alice@example.com"},
		{UserID: "bob@example.com"},
		{UserID: "charlie@example.com"},
	})
	assert.Equal(t, 3, hub.CountSessionParticipants("diag-X"))
	assert.Equal(t, 0, hub.CountSessionParticipants("diag-missing"))
}

// TestConnectionCapConstants pins the documented defaults so a future
// edit that bumps them past the issue's stated values shows up in
// review.
func TestConnectionCapConstants(t *testing.T) {
	assert.Equal(t, 5, MaxConnectionsPerUser, "per-user cap must default to 5 — see #351")
	assert.Equal(t, 50, MaxParticipantsPerSession, "per-session cap must default to 50 — see #351")
}
