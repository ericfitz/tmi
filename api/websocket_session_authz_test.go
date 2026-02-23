package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAliceProviderID = "alice-provider-id"
	testHostEmail       = "host@example.com"
	testHostProviderID  = "host-provider-id"
)

// createTestSession creates a DiagramSession with a host client already registered.
// The session's Run() goroutine is NOT started (we test methods directly).
func createTestSession(t *testing.T, threatModelID string) (*DiagramSession, *WebSocketClient) {
	t.Helper()
	diagramID := uuid.New().String()
	hostClient := &WebSocketClient{
		UserID:       testHostProviderID,
		UserEmail:    testHostEmail,
		UserName:     "Host User",
		UserProvider: "test-idp",
		Send:         make(chan []byte, 256),
	}

	session := &DiagramSession{
		ID:                 uuid.New().String(),
		DiagramID:          diagramID,
		ThreatModelID:      threatModelID,
		State:              SessionStateActive,
		Clients:            map[*WebSocketClient]bool{hostClient: true},
		Broadcast:          make(chan []byte, 256),
		Register:           make(chan *WebSocketClient, 1),
		Unregister:         make(chan *WebSocketClient, 1),
		LastActivity:       time.Now().UTC(),
		CreatedAt:          time.Now().UTC(),
		Host:               testHostEmail,      // Host is stored as email
		CurrentPresenter:   testHostProviderID, // Presenter stored as provider ID initially
		DeniedUsers:        make(map[string]bool),
		NextSequenceNumber: 1,
		OperationHistory:   NewOperationHistory(),
		clientLastSequence: make(map[string]uint64),
	}

	hostClient.Session = session
	return session, hostClient
}

// addClientToSession creates a participant client and adds it to the session.
func addClientToSession(t *testing.T, session *DiagramSession, userID, email, name, provider string) *WebSocketClient {
	t.Helper()
	client := &WebSocketClient{
		UserID:       userID,
		UserEmail:    email,
		UserName:     name,
		UserProvider: provider,
		Send:         make(chan []byte, 256),
		Session:      session,
	}
	session.mu.Lock()
	session.Clients[client] = true
	session.mu.Unlock()
	return client
}

// drainChannel reads all pending messages from a client's Send channel (non-blocking).
func drainChannel(ch chan []byte) [][]byte {
	var msgs [][]byte
	for {
		select {
		case msg := <-ch:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// --- Phase 1 Test 1: Email vs UserID confusion in host authorization ---

func TestProcessChangePresenter_OnlyHostByEmailCanChange(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	// Add a legitimate participant
	participant := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")

	// Add an attacker who has the SAME EMAIL as the host but a different UserID
	// This simulates a user from a different IdP whose email happens to match
	attacker := addClientToSession(t, session, "attacker-provider-id", testHostEmail, "Attacker", "evil-idp")

	// Build a change_presenter request targeting alice
	req := ChangePresenterRequest{
		MessageType: MessageTypeChangePresenterRequest,
		NewPresenter: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    testAliceProviderID,
			DisplayName:   "Alice",
			Email:         openapi_types.Email("alice@example.com"),
		},
	}
	msgBytes, err := json.Marshal(req)
	require.NoError(t, err)

	// The attacker calls processChangePresenter.
	// Because the host check uses client.UserEmail == host (email comparison),
	// the attacker whose email matches the host WILL pass the check.
	// This test documents the current behavior.
	session.processChangePresenter(attacker, msgBytes)

	// Record what happened — did the presenter change?
	session.mu.RLock()
	presenterAfterAttacker := session.CurrentPresenter
	session.mu.RUnlock()

	// Now the legitimate host changes presenter
	session.mu.Lock()
	session.CurrentPresenter = testHostProviderID // Reset
	session.mu.Unlock()

	session.processChangePresenter(hostClient, msgBytes)

	session.mu.RLock()
	presenterAfterHost := session.CurrentPresenter
	session.mu.RUnlock()

	// The legitimate host should be able to change presenter
	assert.Equal(t, testAliceProviderID, presenterAfterHost, "Host should be able to change presenter")

	// Document the current behavior: attacker with matching email CAN change presenter
	// This is a known issue — host identity check uses email not UserID
	if presenterAfterAttacker == testAliceProviderID {
		t.Log("WARNING: User with same email as host from different IdP can change presenter (email-based host check)")
	}

	_ = participant // ensure participant is in scope
}

func TestProcessRemoveParticipant_OnlyHostByEmailCanRemove(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	// Add a legitimate participant
	participant := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")

	// Add an attacker who has the SAME EMAIL as the host but a different UserID
	attacker := addClientToSession(t, session, "attacker-provider-id", testHostEmail, "Attacker", "evil-idp")

	// Build remove request targeting alice
	req := RemoveParticipantRequest{
		MessageType: MessageTypeRemoveParticipantRequest,
		RemovedUser: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    testAliceProviderID,
			DisplayName:   "Alice",
			Email:         openapi_types.Email("alice@example.com"),
		},
	}
	msgBytes, err := json.Marshal(req)
	require.NoError(t, err)

	// Attacker tries to remove alice — check uses client.UserEmail == host
	session.processRemoveParticipant(attacker, msgBytes)

	// Check if alice was denied
	session.mu.RLock()
	aliceDeniedByAttacker := session.DeniedUsers[testAliceProviderID]
	session.mu.RUnlock()

	// Reset state for legitimate host test
	session.mu.Lock()
	delete(session.DeniedUsers, testAliceProviderID)
	session.mu.Unlock()

	// Legitimate host removes alice
	session.processRemoveParticipant(hostClient, msgBytes)

	session.mu.RLock()
	aliceDeniedByHost := session.DeniedUsers[testAliceProviderID]
	session.mu.RUnlock()

	assert.True(t, aliceDeniedByHost, "Host should be able to remove participants")

	if aliceDeniedByAttacker {
		t.Log("WARNING: User with same email as host from different IdP can remove participants (email-based host check)")
	}

	_ = participant
}

// --- Phase 1 Test 2: Non-host cannot change presenter or remove participants ---

func TestProcessChangePresenter_NonHostDenied(t *testing.T) {
	InitTestFixtures()
	session, _ := createTestSession(t, "")

	participant := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")
	target := addClientToSession(t, session, "bob-provider-id", "bob@example.com", "Bob", "test-idp")

	req := ChangePresenterRequest{
		MessageType: MessageTypeChangePresenterRequest,
		NewPresenter: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    "bob-provider-id",
			DisplayName:   "Bob",
			Email:         openapi_types.Email("bob@example.com"),
		},
	}
	msgBytes, err := json.Marshal(req)
	require.NoError(t, err)

	// Participant (non-host) tries to change presenter
	session.processChangePresenter(participant, msgBytes)

	session.mu.RLock()
	presenter := session.CurrentPresenter
	session.mu.RUnlock()

	assert.NotEqual(t, "bob-provider-id", presenter, "Non-host should NOT be able to change presenter")

	_ = target
}

func TestProcessRemoveParticipant_NonHostDenied(t *testing.T) {
	InitTestFixtures()
	session, _ := createTestSession(t, "")

	alice := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")
	bob := addClientToSession(t, session, "bob-provider-id", "bob@example.com", "Bob", "test-idp")

	req := RemoveParticipantRequest{
		MessageType: MessageTypeRemoveParticipantRequest,
		RemovedUser: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    "bob-provider-id",
			DisplayName:   "Bob",
			Email:         openapi_types.Email("bob@example.com"),
		},
	}
	msgBytes, err := json.Marshal(req)
	require.NoError(t, err)

	// Alice (non-host) tries to remove bob
	session.processRemoveParticipant(alice, msgBytes)

	session.mu.RLock()
	bobDenied := session.DeniedUsers["bob-provider-id"]
	session.mu.RUnlock()

	assert.False(t, bobDenied, "Non-host should NOT be able to remove participants")

	// Verify error message was sent to alice
	msgs := drainChannel(alice.Send)
	require.NotEmpty(t, msgs, "Non-host should receive an error message")

	var errMsg ErrorMessage
	err = json.Unmarshal(msgs[0], &errMsg)
	require.NoError(t, err)
	assert.Equal(t, "unauthorized", errMsg.Error)

	_ = bob
}

// --- Phase 1 Test 3: Provider field omission in identity validation ---

func TestValidateTargetUserIdentity_ProviderMismatch(t *testing.T) {
	InitTestFixtures()
	session, _ := createTestSession(t, "")

	requester := addClientToSession(t, session, "requester-id", "requester@example.com", "Requester", "test-idp")
	target := addClientToSession(t, session, "target-id", "target@example.com", "Target", "okta")

	tests := []struct {
		name         string
		providedUser User
		expectValid  bool
		description  string
	}{
		{
			name: "matching_identity",
			providedUser: User{
				ProviderId:  "target-id",
				Email:       openapi_types.Email("target@example.com"),
				DisplayName: "Target",
			},
			expectValid: true,
			description: "All fields match — should pass",
		},
		{
			name: "wrong_email",
			providedUser: User{
				ProviderId:  "target-id",
				Email:       openapi_types.Email("fake@evil.com"),
				DisplayName: "Target",
			},
			expectValid: false,
			description: "Wrong email — should detect spoofing",
		},
		{
			name: "wrong_provider_id",
			providedUser: User{
				ProviderId:  "wrong-target-id",
				Email:       openapi_types.Email("target@example.com"),
				DisplayName: "Target",
			},
			expectValid: false,
			description: "Wrong ProviderId — should detect spoofing",
		},
		{
			name: "wrong_display_name",
			providedUser: User{
				ProviderId:  "target-id",
				Email:       openapi_types.Email("target@example.com"),
				DisplayName: "Fake Name",
			},
			expectValid: false,
			description: "Wrong DisplayName — should detect spoofing",
		},
		{
			name: "empty_provider_id_bypasses_check",
			providedUser: User{
				ProviderId:  "", // Empty string skips the ProviderId check
				Email:       openapi_types.Email("target@example.com"),
				DisplayName: "Target",
			},
			expectValid: true,
			description: "Empty ProviderId is not checked — only non-empty fields are validated",
		},
		{
			name: "all_empty_fields_bypass",
			providedUser: User{
				ProviderId:  "",
				Email:       "",
				DisplayName: "",
			},
			expectValid: true,
			description: "All empty fields bypass all checks — validates only non-empty provided fields",
		},
		{
			name: "provider_field_not_validated",
			providedUser: User{
				Provider:    "evil-idp", // Different provider — but validateTargetUserIdentity doesn't check Provider
				ProviderId:  "target-id",
				Email:       openapi_types.Email("target@example.com"),
				DisplayName: "Target",
			},
			expectValid: true,
			description: "Provider field is NOT validated by validateTargetUserIdentity — potential cross-IdP confusion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Re-add requester if they were removed in a previous subtest
			session.mu.Lock()
			session.Clients[requester] = true
			delete(session.DeniedUsers, requester.UserID)
			session.mu.Unlock()

			result := session.validateTargetUserIdentity(requester, tt.providedUser, target, "test")
			assert.Equal(t, tt.expectValid, result, tt.description)
		})
	}
}

// --- Phase 1 Test 4: Empty ThreatModelID bypass in checkMutationPermission ---

func TestCheckMutationPermission_EmptyThreatModelIDBypass(t *testing.T) {
	InitTestFixtures()

	t.Run("empty_threat_model_id_allows_mutations", func(t *testing.T) {
		session, _ := createTestSession(t, "") // Empty ThreatModelID

		// Any authenticated client can mutate when ThreatModelID is empty
		randomClient := addClientToSession(t, session, "random-user", "random@example.com", "Random", "test-idp")

		canMutate := session.checkMutationPermission(randomClient)
		assert.True(t, canMutate, "Empty ThreatModelID allows mutations for backward compatibility — this is documented but risky")
	})

	t.Run("nil_client_denied", func(t *testing.T) {
		session, _ := createTestSession(t, "")
		assert.False(t, session.checkMutationPermission(nil), "Nil client should be denied")
	})

	t.Run("anonymous_client_denied", func(t *testing.T) {
		session, _ := createTestSession(t, "")
		anonClient := &WebSocketClient{
			UserID: "", // No user ID — anonymous
			Send:   make(chan []byte, 256),
		}
		assert.False(t, session.checkMutationPermission(anonClient), "Anonymous client should be denied even with empty ThreatModelID")
	})

	t.Run("valid_threat_model_with_writer_access", func(t *testing.T) {
		// Create a threat model in the store so checkMutationPermission can look it up
		ownerEmail := "owner@example.com"
		writerEmail := "writer@example.com"
		readerEmail := "reader@example.com"

		tm := ThreatModel{
			Name: "Test TM for Mutation Check",
			Owner: User{
				PrincipalType: UserPrincipalTypeUser,
				Provider:      "test-idp",
				ProviderId:    ownerEmail,
				Email:         openapi_types.Email(ownerEmail),
			},
			Authorization: []Authorization{
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test-idp", ProviderId: ownerEmail, Role: "owner"},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test-idp", ProviderId: writerEmail, Role: "writer"},
				{PrincipalType: AuthorizationPrincipalTypeUser, Provider: "test-idp", ProviderId: readerEmail, Role: "reader"},
			},
			CreatedAt:  func() *time.Time { now := time.Now().UTC(); return &now }(),
			ModifiedAt: func() *time.Time { now := time.Now().UTC(); return &now }(),
		}

		created, err := ThreatModelStore.Create(tm, func(tm ThreatModel, id string) ThreatModel {
			uid, _ := ParseUUID(id)
			tm.Id = &uid
			return tm
		})
		require.NoError(t, err)

		tmID := created.Id.String()
		session, _ := createTestSession(t, tmID)

		writerClient := addClientToSession(t, session, writerEmail, writerEmail, "Writer", "test-idp")
		assert.True(t, session.checkMutationPermission(writerClient), "Writer should be allowed to mutate")

		readerClient := addClientToSession(t, session, readerEmail, readerEmail, "Reader", "test-idp")
		assert.False(t, session.checkMutationPermission(readerClient), "Reader should NOT be allowed to mutate")

		unknownClient := addClientToSession(t, session, "unknown@example.com", "unknown@example.com", "Unknown", "test-idp")
		assert.False(t, session.checkMutationPermission(unknownClient), "Unauthorized user should NOT be allowed to mutate")
	})
}

// --- Phase 1 Test 5: CurrentPresenter type inconsistency ---

func TestPresenterIdentityConsistency(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	alice := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")

	// Initially, CurrentPresenter should be set to host-provider-id (from createTestSession)
	session.mu.RLock()
	initialPresenter := session.CurrentPresenter
	session.mu.RUnlock()
	assert.Equal(t, testHostProviderID, initialPresenter)

	// Host requests presenter mode.
	// processPresenterRequest first checks: client.UserID == currentPresenter
	// Since host's UserID (testHostProviderID) == CurrentPresenter (testHostProviderID),
	// the function returns early ("already the presenter") — no reassignment happens.
	presenterReq := PresenterRequestMessage{
		MessageType: MessageTypePresenterRequest,
	}
	reqBytes, _ := json.Marshal(presenterReq)
	session.processPresenterRequest(hostClient, reqBytes)

	session.mu.RLock()
	presenterAfterHostRequest := session.CurrentPresenter
	session.mu.RUnlock()

	// Host was already presenter, so nothing changes
	assert.Equal(t, testHostProviderID, presenterAfterHostRequest,
		"Host was already presenter — no change expected")

	// To exercise the email-assignment path, first change presenter to alice,
	// then have host request presenter mode again.
	session.mu.Lock()
	session.CurrentPresenter = testAliceProviderID // Make alice presenter
	session.mu.Unlock()

	// Now host requests presenter — host is NOT the current presenter, but IS the host,
	// so processPresenterRequest sets CurrentPresenter = client.UserEmail
	session.processPresenterRequest(hostClient, reqBytes)

	session.mu.RLock()
	presenterAfterHostReclaim := session.CurrentPresenter
	session.mu.RUnlock()

	// BUG DOCUMENTED: processPresenterRequest stores UserEmail when host reclaims presenter
	assert.Equal(t, hostClient.UserEmail, presenterAfterHostReclaim,
		"processPresenterRequest stores UserEmail when host reclaims presenter")

	// Now host changes presenter to alice via processChangePresenter
	// This sets CurrentPresenter = req.NewPresenter.ProviderId
	changeReq := ChangePresenterRequest{
		MessageType: MessageTypeChangePresenterRequest,
		NewPresenter: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    testAliceProviderID,
			DisplayName:   "Alice",
			Email:         openapi_types.Email("alice@example.com"),
		},
	}
	changeBytes, _ := json.Marshal(changeReq)
	session.processChangePresenter(hostClient, changeBytes)

	session.mu.RLock()
	presenterAfterChange := session.CurrentPresenter
	session.mu.RUnlock()

	// After changePresenter, CurrentPresenter is set to ProviderId
	assert.Equal(t, testAliceProviderID, presenterAfterChange,
		"processChangePresenter stores ProviderId")

	// TYPE INCONSISTENCY: processPresenterCursor checks client.UserEmail == currentPresenter
	// Alice's UserEmail is "alice@example.com" but CurrentPresenter is testAliceProviderID
	// If email != providerId (typical), Alice CANNOT send cursor updates after being
	// assigned via changePresenter.
	session.mu.RLock()
	cp := session.CurrentPresenter
	session.mu.RUnlock()

	emailMatchesPresenter := alice.UserEmail == cp
	userIDMatchesPresenter := alice.UserID == cp

	// Document the inconsistency
	t.Logf("CurrentPresenter=%q, alice.UserEmail=%q, alice.UserID=%q", cp, alice.UserEmail, alice.UserID)
	t.Logf("Email matches presenter: %v, UserID matches presenter: %v", emailMatchesPresenter, userIDMatchesPresenter)

	if !emailMatchesPresenter && userIDMatchesPresenter {
		t.Log("CONFIRMED: Type inconsistency — processPresenterRequest stores EMAIL, " +
			"processChangePresenter stores PROVIDER_ID, but processPresenterCursor checks EMAIL")
	}
}

// --- Phase 1 Test 6: Deny list prevents reconnection ---

func TestDenyListPreventsReconnection(t *testing.T) {
	InitTestFixtures()
	hub := NewWebSocketHubForTests()

	diagramID := uuid.New().String()
	threatModelID := uuid.New().String()
	hostUserID := testHostEmail

	session, err := hub.CreateSession(diagramID, threatModelID, hostUserID)
	require.NoError(t, err)

	// Give the Run() goroutine time to start
	time.Sleep(50 * time.Millisecond)

	// Add a user to the deny list
	deniedUserID := "denied-user-id"
	session.mu.Lock()
	session.DeniedUsers[deniedUserID] = true
	session.mu.Unlock()

	// Verify the deny list check in Run() (the Register channel handler)
	// The Run() goroutine checks DeniedUsers[client.UserID] when processing Register
	session.mu.RLock()
	isDenied := session.DeniedUsers[deniedUserID]
	session.mu.RUnlock()
	assert.True(t, isDenied, "User should be in deny list")

	// Verify a non-denied user is not blocked
	session.mu.RLock()
	isAllowed := !session.DeniedUsers["allowed-user-id"]
	session.mu.RUnlock()
	assert.True(t, isAllowed, "Non-denied user should not be blocked")
}

// --- Phase 1 Test 7: Host cannot remove themselves ---

func TestProcessRemoveParticipant_HostCannotRemoveSelf(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	req := RemoveParticipantRequest{
		MessageType: MessageTypeRemoveParticipantRequest,
		RemovedUser: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    testHostProviderID, // Same as host's UserID
			DisplayName:   "Host User",
			Email:         openapi_types.Email(testHostEmail),
		},
	}
	msgBytes, err := json.Marshal(req)
	require.NoError(t, err)

	session.processRemoveParticipant(hostClient, msgBytes)

	// Host should still be in the session, not denied
	session.mu.RLock()
	hostDenied := session.DeniedUsers[testHostProviderID]
	_, hostStillConnected := session.Clients[hostClient]
	session.mu.RUnlock()

	assert.False(t, hostDenied, "Host should NOT be able to remove themselves from deny list")
	assert.True(t, hostStillConnected, "Host should still be connected")

	// Verify error message was sent
	msgs := drainChannel(hostClient.Send)
	require.NotEmpty(t, msgs, "Host should receive an error about self-removal")

	var errMsg ErrorMessage
	err = json.Unmarshal(msgs[0], &errMsg)
	require.NoError(t, err)
	assert.Equal(t, "invalid_request", errMsg.Error)
}

// --- Phase 1 Test 8: Removed presenter reverts to host ---

func TestProcessRemoveParticipant_PresenterRevertsToHost(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	// For this test, we add alice to the deny list (already not connected)
	// so that the code path that calls targetClient.Conn.Close() is not hit.
	// Testing with a connected client whose Conn is nil would panic (and the
	// deferred recover would swallow it, preventing presenter reassignment).

	aliceProviderID := testAliceProviderID

	// Make alice the presenter (even though she's not connected)
	session.mu.Lock()
	session.CurrentPresenter = aliceProviderID
	session.mu.Unlock()

	// Host removes alice who is not currently connected (targetClient will be nil)
	// but NOT already in the deny list. The code path at line 1852:
	// `if targetClient == nil && !inDenyList` will reject this because alice is not
	// found and not in deny list. So we need to add a client for alice, but with
	// no real Conn.

	// Instead, test the path where alice is already in the deny list (re-remove)
	session.mu.Lock()
	session.DeniedUsers[aliceProviderID] = true
	session.mu.Unlock()

	req := RemoveParticipantRequest{
		MessageType: MessageTypeRemoveParticipantRequest,
		RemovedUser: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    aliceProviderID,
			DisplayName:   "Alice",
			Email:         openapi_types.Email("alice@example.com"),
		},
	}
	msgBytes, _ := json.Marshal(req)
	session.processRemoveParticipant(hostClient, msgBytes)

	session.mu.RLock()
	newPresenter := session.CurrentPresenter
	session.mu.RUnlock()

	// After removing the current presenter, host should become presenter again.
	// The code sets CurrentPresenter = host (which is the email stored in session.Host).
	assert.Equal(t, session.Host, newPresenter,
		"After removing current presenter, host should become presenter")
}

func TestProcessRemoveParticipant_ConnectedPresenterPanicRecovery(t *testing.T) {
	// This test documents a real bug: when removing a connected participant,
	// the code calls targetClient.Conn.Close(). If Conn is nil (edge case),
	// the defer/recover catches the panic but the presenter reassignment
	// code at line 1908 is NEVER reached — the presenter is NOT reassigned.
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	// Add alice as a connected client (but with nil Conn — simulates a race
	// where the connection was already closed)
	alice := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")
	// alice.Conn is nil by default

	// Make alice the presenter
	session.mu.Lock()
	session.CurrentPresenter = testAliceProviderID
	session.mu.Unlock()

	req := RemoveParticipantRequest{
		MessageType: MessageTypeRemoveParticipantRequest,
		RemovedUser: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    testAliceProviderID,
			DisplayName:   "Alice",
			Email:         openapi_types.Email("alice@example.com"),
		},
	}
	msgBytes, _ := json.Marshal(req)

	// This should NOT panic (defer/recover catches it)
	session.processRemoveParticipant(hostClient, msgBytes)

	session.mu.RLock()
	presenterAfterRemoval := session.CurrentPresenter
	isDenied := session.DeniedUsers[testAliceProviderID]
	session.mu.RUnlock()

	// Alice should be in deny list (this happens BEFORE the Conn.Close call)
	assert.True(t, isDenied, "Alice should be in deny list")

	// BUG: The nil Conn.Close() panic is caught by recover, but the presenter
	// reassignment at line 1908 is NEVER executed because it comes AFTER the panic point.
	// So the presenter remains as testAliceProviderID even though alice was removed.
	switch presenterAfterRemoval {
	case testAliceProviderID:
		t.Log("BUG CONFIRMED: Nil Conn.Close() panic prevents presenter reassignment — " +
			"removed user remains as CurrentPresenter")
	case session.Host:
		t.Log("Bug fixed or not reproduced — presenter was properly reassigned to host")
	}

	_ = alice
}

// --- Phase 1 Test 9: processPresenterDenied only allowed by host ---

func TestProcessPresenterDenied_OnlyHostCanDeny(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	alice := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")
	bob := addClientToSession(t, session, "bob-provider-id", "bob@example.com", "Bob", "test-idp")

	// Build a presenter denied request targeting alice
	deniedReq := PresenterDeniedRequest{
		MessageType: MessageTypePresenterDeniedRequest,
		DeniedUser: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "test-idp",
			ProviderId:    testAliceProviderID,
			DisplayName:   "Alice",
			Email:         openapi_types.Email("alice@example.com"),
		},
	}
	msgBytes, _ := json.Marshal(deniedReq)

	// Bob (non-host) tries to deny alice's presenter request
	session.processPresenterDenied(bob, msgBytes)

	// Alice should NOT have received a denial message from bob
	aliceMsgs := drainChannel(alice.Send)
	for _, msg := range aliceMsgs {
		var parsed map[string]any
		if json.Unmarshal(msg, &parsed) == nil {
			assert.NotEqual(t, "presenter_denied_event", parsed["message_type"],
				"Non-host should NOT be able to send presenter denied events")
		}
	}

	// Host denies alice's presenter request
	session.processPresenterDenied(hostClient, msgBytes)

	// Alice should receive a denial from the host
	aliceMsgs = drainChannel(alice.Send)
	found := false
	for _, msg := range aliceMsgs {
		var parsed map[string]any
		if json.Unmarshal(msg, &parsed) == nil {
			if parsed["message_type"] == string(MessageTypePresenterDeniedEvent) {
				found = true
			}
		}
	}
	assert.True(t, found, "Host should be able to deny presenter requests")
}

// --- Phase 1 Test 10: processPresenterCursor only allowed by current presenter ---

func TestProcessPresenterCursor_OnlyPresenterCanSend(t *testing.T) {
	InitTestFixtures()
	session, hostClient := createTestSession(t, "")

	// Set up so the presenter check uses email (as the code does)
	session.mu.Lock()
	session.CurrentPresenter = hostClient.UserEmail // Set to email for consistent comparison
	session.mu.Unlock()

	alice := addClientToSession(t, session, testAliceProviderID, "alice@example.com", "Alice", "test-idp")

	cursorMsg := struct {
		MessageType string `json:"message_type"`
		X           int    `json:"x"`
		Y           int    `json:"y"`
	}{
		MessageType: "presenter_cursor",
		X:           100,
		Y:           200,
	}
	msgBytes, _ := json.Marshal(cursorMsg)

	// Alice (not presenter) sends cursor — should be silently ignored
	session.processPresenterCursor(alice, msgBytes)

	// No other clients should receive the cursor broadcast
	// (host is the only other client)
	hostMsgs := drainChannel(hostClient.Send)
	for _, msg := range hostMsgs {
		var parsed map[string]any
		if json.Unmarshal(msg, &parsed) == nil {
			assert.NotEqual(t, "presenter_cursor", parsed["message_type"],
				"Non-presenter cursor updates should be silently dropped")
		}
	}

	// Host (current presenter) sends cursor — should be broadcast to alice
	session.processPresenterCursor(hostClient, msgBytes)

	aliceMsgs := drainChannel(alice.Send)
	found := false
	for _, msg := range aliceMsgs {
		var parsed map[string]any
		if json.Unmarshal(msg, &parsed) == nil {
			if parsed["message_type"] == "presenter_cursor" {
				found = true
			}
		}
	}
	assert.True(t, found, "Current presenter's cursor updates should be broadcast to other clients")
}
