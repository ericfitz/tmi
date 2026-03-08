package api

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockAuditService implements AuditServiceInterface for testing the debouncer.
type mockAuditService struct {
	mu        sync.Mutex
	calls     []AuditParams
	returnErr error
}

func newMockAuditService() *mockAuditService {
	return &mockAuditService{}
}

func (m *mockAuditService) RecordMutation(_ context.Context, params AuditParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, params)
	return m.returnErr
}

func (m *mockAuditService) getCalls() []AuditParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]AuditParams, len(m.calls))
	copy(result, m.calls)
	return result
}

func (m *mockAuditService) GetThreatModelAuditTrail(_ context.Context, _ string, _, _ int, _ *AuditFilters) ([]AuditEntryResponse, int, error) {
	return nil, 0, nil
}

func (m *mockAuditService) GetObjectAuditTrail(_ context.Context, _, _ string, _, _ int) ([]AuditEntryResponse, int, error) {
	return nil, 0, nil
}

func (m *mockAuditService) GetAuditEntry(_ context.Context, _ string) (*AuditEntryResponse, error) {
	return nil, nil
}

func (m *mockAuditService) GetSnapshot(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (m *mockAuditService) DeleteThreatModelAudit(_ context.Context, _ string) error {
	return nil
}

func (m *mockAuditService) PruneAuditEntries(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockAuditService) PruneVersionSnapshots(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockAuditService) PurgeTombstones(_ context.Context) (int, error) {
	return 0, nil
}

func newTestDebouncer(svc *mockAuditService) *AuditDebouncer {
	d := NewAuditDebouncer(svc)
	// Use very short delays for testing
	d.wsDelay = 50 * time.Millisecond
	d.restDelay = 100 * time.Millisecond
	return d
}

func TestDebouncer_SingleRecord(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat_model",
		ObjectID:      "obj1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"old"}`),
		CurrentState:  []byte(`{"name":"new"}`),
	}, false)

	if d.PendingCount() != 1 {
		t.Errorf("expected 1 pending, got %d", d.PendingCount())
	}

	// Wait for REST delay to expire
	time.Sleep(200 * time.Millisecond)

	if d.PendingCount() != 0 {
		t.Errorf("expected 0 pending after flush, got %d", d.PendingCount())
	}

	calls := svc.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call to RecordMutation, got %d", len(calls))
	}

	if calls[0].ObjectID != "obj1" {
		t.Errorf("expected ObjectID 'obj1', got %q", calls[0].ObjectID)
	}
}

func TestDebouncer_CoalescesMutations(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	// Send 3 rapid mutations to the same entity
	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "diagram",
		ObjectID:      "d1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"v1"}`),
		CurrentState:  []byte(`{"name":"v2"}`),
	}, true) // WebSocket

	time.Sleep(10 * time.Millisecond)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "diagram",
		ObjectID:      "d1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"v2"}`),
		CurrentState:  []byte(`{"name":"v3"}`),
	}, true)

	time.Sleep(10 * time.Millisecond)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "diagram",
		ObjectID:      "d1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"v3"}`),
		CurrentState:  []byte(`{"name":"v4"}`),
	}, true)

	// Should still be pending (1 coalesced entry)
	if d.PendingCount() != 1 {
		t.Errorf("expected 1 pending (coalesced), got %d", d.PendingCount())
	}

	// Wait for WS delay to expire
	time.Sleep(150 * time.Millisecond)

	calls := svc.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 coalesced call, got %d", len(calls))
	}

	// Should use first pre-state and latest post-state
	if string(calls[0].PreviousState) != `{"name":"v1"}` {
		t.Errorf("expected PreviousState from first op, got %q", string(calls[0].PreviousState))
	}
	if string(calls[0].CurrentState) != `{"name":"v4"}` {
		t.Errorf("expected CurrentState from last op, got %q", string(calls[0].CurrentState))
	}

	// Change summary should mention coalescing
	if calls[0].ChangeSummary == nil {
		t.Fatal("expected ChangeSummary to be set")
	}
	if !contains(*calls[0].ChangeSummary, "3 changes coalesced") {
		t.Errorf("expected coalesced summary, got %q", *calls[0].ChangeSummary)
	}
}

func TestDebouncer_FlushAll(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	// Buffer mutations for two different entities
	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat",
		ObjectID:      "t1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"a"}`),
		CurrentState:  []byte(`{"name":"b"}`),
	}, false)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat",
		ObjectID:      "t2",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"c"}`),
		CurrentState:  []byte(`{"name":"d"}`),
	}, false)

	if d.PendingCount() != 2 {
		t.Errorf("expected 2 pending, got %d", d.PendingCount())
	}

	d.FlushAll()

	if d.PendingCount() != 0 {
		t.Errorf("expected 0 pending after FlushAll, got %d", d.PendingCount())
	}

	calls := svc.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls after FlushAll, got %d", len(calls))
	}
}

func TestDebouncer_FlushEntity(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	// Buffer mutations for two entities
	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "diagram",
		ObjectID:      "d1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"a"}`),
		CurrentState:  []byte(`{"name":"b"}`),
	}, true)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "diagram",
		ObjectID:      "d2",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"c"}`),
		CurrentState:  []byte(`{"name":"d"}`),
	}, true)

	if d.PendingCount() != 2 {
		t.Errorf("expected 2 pending, got %d", d.PendingCount())
	}

	// Flush only d1
	d.FlushEntity("diagram", "d1")

	if d.PendingCount() != 1 {
		t.Errorf("expected 1 pending after FlushEntity, got %d", d.PendingCount())
	}

	calls := svc.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call after FlushEntity, got %d", len(calls))
	}
	if calls[0].ObjectID != "d1" {
		t.Errorf("expected flushed entity d1, got %q", calls[0].ObjectID)
	}
}

func TestDebouncer_DifferentEntitiesNotCoalesced(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	// Two different entities should have separate pending entries
	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat",
		ObjectID:      "t1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"a"}`),
		CurrentState:  []byte(`{"name":"b"}`),
	}, false)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat",
		ObjectID:      "t2",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"c"}`),
		CurrentState:  []byte(`{"name":"d"}`),
	}, false)

	if d.PendingCount() != 2 {
		t.Errorf("expected 2 separate pending entries, got %d", d.PendingCount())
	}

	d.FlushAll()

	calls := svc.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 separate calls, got %d", len(calls))
	}
}

func TestDebouncer_WebSocketVsRESTDelay(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)
	// WS delay: 50ms, REST delay: 100ms

	// WebSocket mutation
	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "diagram",
		ObjectID:      "d1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"a"}`),
		CurrentState:  []byte(`{"name":"b"}`),
	}, true)

	// After 75ms, WS should have flushed but REST would not have
	time.Sleep(75 * time.Millisecond)

	calls := svc.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected WS mutation to flush after 75ms, got %d calls", len(calls))
	}
}

func TestDebouncer_ConcurrentAccess(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	var wg sync.WaitGroup
	// Concurrently buffer mutations to 10 different entities
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d.RecordOrBuffer(context.Background(), AuditParams{
				ThreatModelID: "tm1",
				ObjectType:    "threat",
				ObjectID:      string(rune('a' + idx)),
				ChangeType:    "updated",
				PreviousState: []byte(`{"name":"old"}`),
				CurrentState:  []byte(`{"name":"new"}`),
			}, false)
		}(i)
	}
	wg.Wait()

	if d.PendingCount() != 10 {
		t.Errorf("expected 10 pending entries, got %d", d.PendingCount())
	}

	d.FlushAll()

	calls := svc.getCalls()
	if len(calls) != 10 {
		t.Errorf("expected 10 flushed calls, got %d", len(calls))
	}
}

func TestDebouncer_NoAuditIfNoMeaningfulChange(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	// Buffer a mutation where first and last state are identical
	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat_model",
		ObjectID:      "obj1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"same"}`),
		CurrentState:  []byte(`{"name":"same"}`),
	}, false)

	d.FlushAll()

	calls := svc.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for no-op change, got %d", len(calls))
	}
}

func TestDebouncer_FlushAllStopsTimers(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	d.RecordOrBuffer(context.Background(), AuditParams{
		ThreatModelID: "tm1",
		ObjectType:    "threat",
		ObjectID:      "t1",
		ChangeType:    "updated",
		PreviousState: []byte(`{"name":"a"}`),
		CurrentState:  []byte(`{"name":"b"}`),
	}, false)

	d.FlushAll()

	// Wait longer than the REST delay to ensure no duplicate flush from timer
	time.Sleep(200 * time.Millisecond)

	calls := svc.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected exactly 1 call (timer should be stopped), got %d", len(calls))
	}
}

func TestDebouncer_FlushEntityNonExistent(t *testing.T) {
	svc := newMockAuditService()
	d := newTestDebouncer(svc)

	// Flushing a non-existent entity should be a no-op
	d.FlushEntity("diagram", "nonexistent")

	calls := svc.getCalls()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for non-existent entity, got %d", len(calls))
	}
}

func TestDebounceKey(t *testing.T) {
	key := debounceKey("threat", "abc-123")
	if key != "threat:abc-123" {
		t.Errorf("expected 'threat:abc-123', got %q", key)
	}
}

// contains checks if substr is in s (avoids importing strings for one use).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
