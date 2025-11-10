package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCloseClientChannel_ThreadSafety verifies that closeClientChannel is thread-safe
// and prevents double-close panics
func TestCloseClientChannel_ThreadSafety(t *testing.T) {
	client := &WebSocketClient{
		Send:      make(chan []byte, 256),
		UserID:    "test-user",
		UserEmail: "test@example.com",
		UserName:  "Test User",
	}

	// Try to close from multiple goroutines simultaneously
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.closeClientChannel()
		}()
	}

	wg.Wait()

	// Verify closing flag is set
	client.closingMu.RLock()
	assert.True(t, client.closing, "closing flag should be set")
	client.closingMu.RUnlock()

	// Verify channel is closed by trying to receive (should not block)
	select {
	case _, ok := <-client.Send:
		assert.False(t, ok, "channel should be closed")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for closed channel")
	}
}

// TestCloseClientChannel_IdempotentClose verifies that closing twice is safe
func TestCloseClientChannel_IdempotentClose(t *testing.T) {
	client := &WebSocketClient{
		Send:      make(chan []byte, 256),
		UserID:    "test-user",
		UserEmail: "test@example.com",
		UserName:  "Test User",
	}

	// First close
	client.closeClientChannel()

	// Second close should not panic
	assert.NotPanics(t, func() {
		client.closeClientChannel()
	}, "second close should not panic")
}

// TestSendToClient_RaceCondition simulates the race condition between
// sendToClient and channel closure
func TestSendToClient_RaceCondition(t *testing.T) {
	// Create a session with a client
	session := &DiagramSession{
		ID:        "test-session",
		DiagramID: "test-diagram",
		Clients:   make(map[*WebSocketClient]bool),
	}

	client := &WebSocketClient{
		Send:      make(chan []byte, 256),
		UserID:    "test-user",
		UserEmail: "test@example.com",
		UserName:  "Test User",
		Session:   session,
	}

	session.Clients[client] = true

	// Start goroutine that continuously sends messages
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var sendWg sync.WaitGroup
	sendWg.Add(1)
	go func() {
		defer sendWg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				session.sendToClient(client, map[string]interface{}{
					"message_type": "test",
					"data":         "test message",
				})
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Close the channel after a short delay (simulates host disconnect)
	time.Sleep(50 * time.Millisecond)
	client.closeClientChannel()

	// Wait for sender to finish
	sendWg.Wait()

	// Test should complete without panic
	assert.True(t, true, "test completed without panic")
}

// TestHostDisconnectDuringOperation simulates the exact scenario from the panic:
// participant sends operation, host disconnects, operation handler tries to send rejection
// This is a focused test that simulates the race condition without full session setup
func TestHostDisconnectDuringOperation(t *testing.T) {
	// Create a simple session for testing channel closure
	session := &DiagramSession{
		ID:        "test-session",
		DiagramID: "test-diagram",
		Clients:   make(map[*WebSocketClient]bool),
	}

	// Create participant client
	participantClient := &WebSocketClient{
		Session:   session,
		UserID:    "participant-user",
		UserName:  "Participant User",
		UserEmail: "participant@example.com",
		Send:      make(chan []byte, 256),
	}

	session.Clients[participantClient] = true

	// Simulate concurrent operations:
	// 1. Thread sending messages to participant (like operation rejection)
	// 2. Thread closing channel (like handleHostDisconnection)
	var wg sync.WaitGroup

	// Goroutine 1: Continuously send messages to participant
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			session.sendToClient(participantClient, map[string]interface{}{
				"message_type": "test_message",
				"index":        i,
			})
			time.Sleep(time.Millisecond)
		}
	}()

	// Goroutine 2: Close channel mid-stream (simulates handleHostDisconnection)
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		participantClient.closeClientChannel()
	}()

	// Wait for both operations - should not panic
	wg.Wait()

	// Verify channel is closed safely
	participantClient.closingMu.RLock()
	isClosing := participantClient.closing
	participantClient.closingMu.RUnlock()

	assert.True(t, isClosing, "participant channel should be closed")
}

// TestConcurrentClientUnregister tests multiple clients closing channels simultaneously
func TestConcurrentClientUnregister(t *testing.T) {
	// Create multiple clients
	clients := make([]*WebSocketClient, 10)
	for i := 0; i < 10; i++ {
		clients[i] = &WebSocketClient{
			UserID:    "user-" + string(rune(i)),
			UserEmail: "user" + string(rune(i)) + "@example.com",
			UserName:  "User " + string(rune(i)),
			Send:      make(chan []byte, 256),
		}
	}

	// Close all clients concurrently (simulates mass disconnect)
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		c := client
		go func() {
			defer wg.Done()
			c.closeClientChannel()
		}()
	}

	wg.Wait()

	// Verify all clients have closing flag set and no panic occurred
	for i, client := range clients {
		client.closingMu.RLock()
		isClosing := client.closing
		client.closingMu.RUnlock()
		assert.True(t, isClosing, "client %d should have closing flag set", i)
	}
}

// TestSendToClientWithClosedChannel verifies sendToClient handles closed channels gracefully
func TestSendToClientWithClosedChannel(t *testing.T) {
	session := &DiagramSession{
		ID:        "test-session",
		DiagramID: "test-diagram",
		Clients:   make(map[*WebSocketClient]bool),
	}

	client := &WebSocketClient{
		Send:      make(chan []byte, 256),
		UserID:    "test-user",
		UserEmail: "test@example.com",
		UserName:  "Test User",
		Session:   session,
	}

	// Close channel before sending
	client.closeClientChannel()

	// Attempt to send should not panic
	require.NotPanics(t, func() {
		session.sendToClient(client, map[string]interface{}{
			"message_type": "test",
		})
	}, "sendToClient should not panic with closed channel")
}

// TestCloseClientChannelConcurrentWithSend tests the specific race window
// between checking the closing flag and sending to the channel
func TestCloseClientChannelConcurrentWithSend(t *testing.T) {
	// Run this test multiple times to increase chance of catching race conditions
	for iteration := 0; iteration < 100; iteration++ {
		session := &DiagramSession{
			ID:        "test-session",
			DiagramID: "test-diagram",
			Clients:   make(map[*WebSocketClient]bool),
		}

		client := &WebSocketClient{
			Send:      make(chan []byte, 256),
			UserID:    "test-user",
			UserEmail: "test@example.com",
			UserName:  "Test User",
			Session:   session,
		}

		// Start many concurrent senders
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					session.sendToClient(client, map[string]interface{}{
						"message_type": "test",
						"index":        j,
					})
				}
			}()
		}

		// Close channel while senders are active
		time.Sleep(time.Millisecond)
		client.closeClientChannel()

		wg.Wait()
	}

	// If we get here without panic, test passes
	assert.True(t, true, "completed 100 iterations without panic")
}
