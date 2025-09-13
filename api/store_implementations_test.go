package api

import (
	"testing"
)

// All in-memory store tests removed - use database tests instead

func TestInMemoryDocumentStore(t *testing.T) {
	t.Skip("In-memory stores removed - use database tests instead")
}

func TestInMemorySourceStore(t *testing.T) {
	t.Skip("In-memory stores removed - use database tests instead")
}

func TestInMemoryThreatStore(t *testing.T) {
	t.Skip("In-memory stores removed - use database tests instead")
}

func TestThreatModelInMemoryStore(t *testing.T) {
	t.Skip("In-memory stores removed - use database tests instead")
}

func TestDiagramInMemoryStore(t *testing.T) {
	t.Skip("In-memory stores removed - use database tests instead")
}

func TestInitializeStores(t *testing.T) {
	// NOTE: InitializeInMemoryStores test removed - function no longer exists
	// All stores now use database implementations only
}
