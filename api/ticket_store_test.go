package api

import (
	"context"
	"testing"
	"time"
)

var testTicketClaims = TicketClaims{UserID: "user123", Provider: "tmi", InternalUUID: "uuid-abc", SessionID: "session456"}

func TestInMemoryTicketStore_IssueAndValidate(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	in := TicketClaims{UserID: "user123", Provider: "tmi", InternalUUID: "uuid-abc", SessionID: "session456", TokenHash: "h1", CredentialID: "c1"}
	ticket, err := store.IssueTicket(ctx, in, 30*time.Second)
	if err != nil {
		t.Fatalf("IssueTicket failed: %v", err)
	}
	if ticket == "" {
		t.Fatal("IssueTicket returned empty ticket")
	}

	out, err := store.ValidateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("ValidateTicket failed: %v", err)
	}
	if out != in {
		t.Errorf("claims round-trip mismatch: got %+v, want %+v", out, in)
	}
}

func TestInMemoryTicketStore_SingleUse(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, _ := store.IssueTicket(ctx, testTicketClaims, 30*time.Second)

	// First validation should succeed
	_, err := store.ValidateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("first ValidateTicket should succeed: %v", err)
	}

	// Second validation should fail (single-use)
	_, err = store.ValidateTicket(ctx, ticket)
	if err == nil {
		t.Fatal("second ValidateTicket should fail (single-use)")
	}
}

func TestInMemoryTicketStore_Expired(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, _ := store.IssueTicket(ctx, testTicketClaims, 1*time.Millisecond)

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	_, err := store.ValidateTicket(ctx, ticket)
	if err == nil {
		t.Fatal("ValidateTicket should fail for expired ticket")
	}
}

func TestInMemoryTicketStore_InvalidTicket(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	_, err := store.ValidateTicket(ctx, "nonexistent-ticket")
	if err == nil {
		t.Fatal("ValidateTicket should fail for invalid ticket")
	}
}

func TestRedisTicketStore_ImplementsInterface(t *testing.T) {
	// Compile-time check that RedisTicketStore implements TicketStore
	var _ TicketStore = (*RedisTicketStore)(nil)
}
