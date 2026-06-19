package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Default debounce delays
const (
	DefaultWebSocketDebounceDelay = 5 * time.Second
	DefaultRESTDebounceDelay      = 10 * time.Second
)

// pendingAudit holds the buffered state for a debounced mutation.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: buffer holding coalesced mutation state for a debounced audit entry (pure)
type pendingAudit struct {
	params         AuditParams
	firstState     []byte    // snapshot from before the first buffered operation
	latestState    []byte    // snapshot after the most recent buffered operation
	operationCount int       // number of coalesced operations
	firstSeen      time.Time // when the first operation arrived
}

// AuditDebouncer buffers rapid mutations to the same entity and coalesces
// them into a single audit entry after a period of inactivity.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: coalesces rapid mutations on the same entity into a single deferred audit record (mutates shared state)
type AuditDebouncer struct {
	auditService AuditServiceInterface
	mu           sync.Mutex
	pending      map[string]*pendingAudit // keyed by "{objectType}:{objectID}"
	timers       map[string]*time.Timer
	wsDelay      time.Duration
	restDelay    time.Duration
}

// NewAuditDebouncer creates a new debouncer with the given audit service.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: build an AuditDebouncer with default WebSocket and REST debounce delays (pure)
func NewAuditDebouncer(auditService AuditServiceInterface) *AuditDebouncer {
	return &AuditDebouncer{
		auditService: auditService,
		pending:      make(map[string]*pendingAudit),
		timers:       make(map[string]*time.Timer),
		wsDelay:      DefaultWebSocketDebounceDelay,
		restDelay:    DefaultRESTDebounceDelay,
	}
}

// debounceKey generates the map key for a given entity.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: compute the map key for a pending audit entry from object type and ID (pure)
func debounceKey(objectType, objectID string) string {
	return fmt.Sprintf("%s:%s", objectType, objectID)
}

// RecordOrBuffer either buffers a mutation for debouncing or records it immediately.
// Use isWebSocket=true for WebSocket cell operations (shorter delay),
// isWebSocket=false for REST auto-save operations (longer delay).
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: buffer a mutation audit event and reset its debounce timer, or start a new buffer (mutates shared state)
func (d *AuditDebouncer) RecordOrBuffer(ctx context.Context, params AuditParams, isWebSocket bool) {
	key := debounceKey(params.ObjectType, params.ObjectID)
	delay := d.restDelay
	if isWebSocket {
		delay = d.wsDelay
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	existing, exists := d.pending[key]
	if exists {
		// Update the pending audit with latest state
		existing.latestState = params.CurrentState
		existing.operationCount++

		// Reset timer
		if timer, ok := d.timers[key]; ok {
			timer.Reset(delay)
		}
		return
	}

	// First mutation for this entity — capture first state
	d.pending[key] = &pendingAudit{
		params:         params,
		firstState:     params.PreviousState,
		latestState:    params.CurrentState,
		operationCount: 1,
		firstSeen:      time.Now(),
	}

	// Start timer
	d.timers[key] = time.AfterFunc(delay, func() {
		d.flush(key)
	})
}

// flush sends the buffered audit entry to the audit service.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: write the coalesced pending audit entry to the audit service and clear the buffer (reads DB)
func (d *AuditDebouncer) flush(key string) {
	d.mu.Lock()
	pa, exists := d.pending[key]
	if !exists {
		d.mu.Unlock()
		return
	}
	delete(d.pending, key)
	delete(d.timers, key)
	d.mu.Unlock()

	// Build the final audit params using first and latest states
	params := pa.params
	params.PreviousState = pa.firstState
	params.CurrentState = pa.latestState

	// Generate change summary from the coalesced change
	if pa.firstState != nil && pa.latestState != nil {
		summary := GenerateChangeSummary(pa.firstState, pa.latestState)
		if pa.operationCount > 1 {
			summary = fmt.Sprintf("[%d changes coalesced] %s", pa.operationCount, summary)
		}
		params.ChangeSummary = &summary
	}

	// Only record if there are meaningful changes
	if pa.firstState != nil && pa.latestState != nil && !ShouldAudit(pa.firstState, pa.latestState) {
		return
	}

	if err := d.auditService.RecordMutation(context.Background(), params); err != nil {
		slogging.Get().Error("failed to flush debounced audit entry for %s: %v", key, err)
	}
}

// FlushAll flushes all pending audit entries immediately.
// Should be called during server shutdown or WebSocket session end.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: flush all pending debounced audit entries immediately, e.g. on server shutdown (reads DB)
func (d *AuditDebouncer) FlushAll() {
	d.mu.Lock()
	keys := make([]string, 0, len(d.pending))
	for key := range d.pending {
		keys = append(keys, key)
	}
	// Stop all timers
	for key, timer := range d.timers {
		timer.Stop()
		delete(d.timers, key)
	}
	d.mu.Unlock()

	// Flush each pending entry
	for _, key := range keys {
		d.flush(key)
	}
}

// FlushEntity flushes the pending audit entry for a specific entity immediately.
// Useful when a WebSocket session for a diagram ends.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: flush the pending audit entry for a specific entity immediately (reads DB)
func (d *AuditDebouncer) FlushEntity(objectType, objectID string) {
	key := debounceKey(objectType, objectID)

	d.mu.Lock()
	if timer, exists := d.timers[key]; exists {
		timer.Stop()
		delete(d.timers, key)
	}
	d.mu.Unlock()

	d.flush(key)
}

// PendingCount returns the number of pending (un-flushed) audit entries.
// Useful for testing.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: return the number of buffered audit entries awaiting flush (pure)
func (d *AuditDebouncer) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}
