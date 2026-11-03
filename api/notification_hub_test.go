package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	notifOwner    = ResolvedUser{Provider: "tmi", ProviderID: "owner-1", Email: "owner@tmi.local"}
	notifReader   = ResolvedUser{Provider: "tmi", ProviderID: "reader-1", Email: "reader@tmi.local"}
	notifOutsider = ResolvedUser{Provider: "tmi", ProviderID: "outsider-1", Email: "outsider@tmi.local"}
)

// notifTestThreatModel builds a threat model owned by notifOwner with the given extra grants.
func notifTestThreatModel(grants ...Authorization) ThreatModel {
	id := uuid.New()
	return ThreatModel{
		Id:   &id,
		Name: "Secret Project",
		Owner: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      notifOwner.Provider,
			ProviderId:    notifOwner.ProviderID,
			Email:         "owner@tmi.local",
		},
		Authorization: &grants,
	}
}

// newNotifTestClient registers a client directly in the hub's maps (no Run loop needed).
func newNotifTestClient(h *NotificationHub, user ResolvedUser, groups []string) *NotificationClient {
	c := &NotificationClient{
		ID:          uuid.New().String(),
		UserID:      user.Email,
		UserEmail:   user.Email,
		UserName:    user.Email,
		User:        user,
		Groups:      groups,
		Send:        make(chan []byte, 16),
		Hub:         h,
		ConnectedAt: time.Now().UTC(),
	}
	h.mu.Lock()
	h.clients[c.ID] = c
	h.mu.Unlock()
	return c
}

// drain returns every message currently queued for a client.
func drain(c *NotificationClient) []NotificationMessage {
	var out []NotificationMessage
	for {
		select {
		case b := <-c.Send:
			var m NotificationMessage
			if err := json.Unmarshal(b, &m); err == nil {
				out = append(out, m)
			}
		default:
			return out
		}
	}
}

func TestNotificationHub_ThreatModelEventsRequireReadAccess(t *testing.T) {
	tm := notifTestThreatModel(
		Authorization{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: notifReader.ProviderID, Role: RoleReader},
		Authorization{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: BuiltInProvider, ProviderId: "security-team", Role: RoleWriter},
	)

	h := NewNotificationHub()
	owner := newNotifTestClient(h, notifOwner, nil)
	reader := newNotifTestClient(h, notifReader, nil)
	groupMember := newNotifTestClient(h, notifOutsider, []string{"security-team"})
	outsider := newNotifTestClient(h, ResolvedUser{Provider: "tmi", ProviderID: "outsider-2"}, []string{"other-team"})
	anonymous := newNotifTestClient(h, ResolvedUser{}, nil)

	for _, eventType := range []NotificationMessageType{
		NotificationThreatModelCreated, NotificationThreatModelUpdated, NotificationThreatModelDeleted,
	} {
		h.broadcastMessage(newThreatModelNotification(eventType, notifOwner.Email, tm, "x"))
	}

	assert.Len(t, drain(owner), 3, "owner receives every event")
	assert.Len(t, drain(reader), 3, "direct reader receives every event")
	assert.Len(t, drain(groupMember), 3, "group grant receives every event")
	assert.Empty(t, drain(outsider), "non-member must not receive threat model events")
	assert.Empty(t, drain(anonymous), "client without identity must not receive threat model events")
}

func TestNotificationHub_EveryoneGrantReachesAllUsers(t *testing.T) {
	tm := notifTestThreatModel(
		Authorization{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: BuiltInProvider, ProviderId: EveryonePseudoGroup, Role: RoleReader},
	)
	h := NewNotificationHub()
	outsider := newNotifTestClient(h, notifOutsider, nil)

	h.broadcastMessage(newThreatModelNotification(NotificationThreatModelUpdated, notifOwner.Email, tm, "updated"))

	assert.Len(t, drain(outsider), 1)
}

func TestNotificationHub_ConfidentialThreatModelReachesOnlyGrantees(t *testing.T) {
	confidential := true
	tm := notifTestThreatModel(
		Authorization{PrincipalType: AuthorizationPrincipalTypeGroup, Provider: BuiltInProvider, ProviderId: ConfidentialProjectReviewersGroup, Role: RoleReader},
	)
	tm.IsConfidential = &confidential

	h := NewNotificationHub()
	reviewer := newNotifTestClient(h, notifReader, []string{ConfidentialProjectReviewersGroup})
	outsider := newNotifTestClient(h, notifOutsider, []string{"security-team"})

	h.broadcastMessage(newThreatModelNotification(NotificationThreatModelCreated, notifOwner.Email, tm, "created"))

	assert.Len(t, drain(reviewer), 1)
	assert.Empty(t, drain(outsider))
}

func TestNotificationHub_SubscriptionFilterCannotWidenAccess(t *testing.T) {
	tm := notifTestThreatModel()
	h := NewNotificationHub()
	outsider := newNotifTestClient(h, notifOutsider, nil)
	outsider.Subscription = &NotificationSubscription{
		SubscribedTypes:    []NotificationMessageType{NotificationThreatModelUpdated},
		ThreatModelFilters: []string{tm.Id.String()},
	}

	h.broadcastMessage(newThreatModelNotification(NotificationThreatModelUpdated, notifOwner.Email, tm, "updated"))

	assert.Empty(t, drain(outsider))
}

func TestNotificationHub_CollaborationEventsRequireReadAccess(t *testing.T) {
	tm := notifTestThreatModel()
	h := NewNotificationHub()
	owner := newNotifTestClient(h, notifOwner, nil)
	outsider := newNotifTestClient(h, notifOutsider, nil)

	h.broadcastMessage(newCollaborationNotification(NotificationCollaborationStarted, notifOwner.Email, uuid.New().String(), "DFD", tm, "session-1"))

	assert.Len(t, drain(owner), 1)
	assert.Empty(t, drain(outsider))
}

func TestNotificationHub_ScopedEventWithoutAuthorizationIsDropped(t *testing.T) {
	h := NewNotificationHub()
	owner := newNotifTestClient(h, notifOwner, nil)

	for _, eventType := range []NotificationMessageType{
		NotificationThreatModelUpdated, NotificationThreatModelShared,
		NotificationCollaborationStarted, NotificationCollaborationEnded, NotificationCollaborationInvite,
	} {
		h.broadcastMessage(&NotificationMessage{
			MessageType: eventType,
			UserID:      notifOwner.Email,
			Timestamp:   time.Now().UTC(),
			Data:        ThreatModelNotificationData{ThreatModelID: uuid.New().String()},
		})
	}

	assert.Empty(t, drain(owner), "events without a threat model ACL snapshot fail closed")
}

func TestNotificationHub_UnknownMessageTypeIsDropped(t *testing.T) {
	h := NewNotificationHub()
	c := newNotifTestClient(h, notifOwner, nil)

	h.broadcastMessage(&NotificationMessage{MessageType: "not_a_real_type", UserID: "system", Timestamp: time.Now().UTC()})

	assert.Empty(t, drain(c))
}

func TestNotificationHub_UnscopedEventsReachEveryone(t *testing.T) {
	h := NewNotificationHub()
	outsider := newNotifTestClient(h, notifOutsider, nil)

	for _, eventType := range []NotificationMessageType{
		NotificationHeartbeat, NotificationSystemAnnouncement, NotificationSystemMaintenance,
		NotificationSystemUpdate, NotificationUserJoined, NotificationUserLeft,
	} {
		h.broadcastMessage(&NotificationMessage{MessageType: eventType, UserID: "system", Timestamp: time.Now().UTC()})
	}

	assert.Len(t, drain(outsider), 6)
}

func TestNotificationMessage_AuthorizationSnapshotNotSerialized(t *testing.T) {
	tm := notifTestThreatModel(
		Authorization{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "tmi", ProviderId: "hidden-grantee", Role: RoleReader},
	)
	msg := newThreatModelNotification(NotificationThreatModelUpdated, notifOwner.Email, tm, "updated")
	require.NotNil(t, msg.authz)

	b, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(b), "hidden-grantee"), "ACL must not leak into the wire format: %s", b)
	assert.False(t, strings.Contains(string(b), "owner-1"), "owner identity must not leak into the wire format: %s", b)
}

// TestNotificationHub_RegisterDoesNotDeadlock reproduces the hub wedging on the
// first connection: registerClient ran on the Run goroutine and did a blocking
// send on the unbuffered broadcast channel that only Run drains.
func TestNotificationHub_RegisterDoesNotDeadlock(t *testing.T) {
	h := NewNotificationHub()
	go h.Run()

	c := &NotificationClient{ID: uuid.New().String(), UserID: notifOwner.Email, User: notifOwner, Send: make(chan []byte, 16), Hub: h}
	tm := notifTestThreatModel()

	done := make(chan struct{})
	go func() {
		h.register <- c
		h.BroadcastThreatModelEvent(NotificationThreatModelUpdated, notifOwner.Email, tm, "updated")
		h.unregister <- c
		h.BroadcastThreatModelEvent(NotificationThreatModelUpdated, notifOwner.Email, tm, "updated")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("notification hub deadlocked after a client registered")
	}
}
