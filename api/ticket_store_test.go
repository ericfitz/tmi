package api

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryTicketStore_IssueAndValidate(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, err := store.IssueTicket(ctx, "user123", "tmi", "uuid-abc", "session456", 30*time.Second)
	if err != nil {
		t.Fatalf("IssueTicket failed: %v", err)
	}
	if ticket == "" {
		t.Fatal("IssueTicket returned empty ticket")
	}

	userID, provider, internalUUID, sessionID, err := store.ValidateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("ValidateTicket failed: %v", err)
	}
	if userID != "user123" {
		t.Errorf("expected userID 'user123', got '%s'", userID)
	}
	if provider != "tmi" {
		t.Errorf("expected provider 'tmi', got '%s'", provider)
	}
	if internalUUID != "uuid-abc" {
		t.Errorf("expected internalUUID 'uuid-abc', got '%s'", internalUUID)
	}
	if sessionID != "session456" {
		t.Errorf("expected sessionID 'session456', got '%s'", sessionID)
	}
}

func TestInMemoryTicketStore_SingleUse(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, _ := store.IssueTicket(ctx, "user123", "tmi", "uuid-abc", "session456", 30*time.Second)

	// First validation should succeed
	_, _, _, _, err := store.ValidateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("first ValidateTicket should succeed: %v", err)
	}

	// Second validation should fail (single-use)
	_, _, _, _, err = store.ValidateTicket(ctx, ticket)
	if err == nil {
		t.Fatal("second ValidateTicket should fail (single-use)")
	}
}

func TestInMemoryTicketStore_Expired(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, _ := store.IssueTicket(ctx, "user123", "tmi", "uuid-abc", "session456", 1*time.Millisecond)

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	_, _, _, _, err := store.ValidateTicket(ctx, ticket)
	if err == nil {
		t.Fatal("ValidateTicket should fail for expired ticket")
	}
}

func TestInMemoryTicketStore_InvalidTicket(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	_, _, _, _, err := store.ValidateTicket(ctx, "nonexistent-ticket")
	if err == nil {
		t.Fatal("ValidateTicket should fail for invalid ticket")
	}
}

func TestRedisTicketStore_ImplementsInterface(t *testing.T) {
	// Compile-time check that RedisTicketStore implements TicketStore
	var _ TicketStore = (*RedisTicketStore)(nil)
}
