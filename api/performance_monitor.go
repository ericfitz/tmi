package api

import (
	"context"
	"log"
	"sync"
	"time"
)

// PerformanceMonitor tracks collaboration system performance metrics
type PerformanceMonitor struct {
	// Session metrics
	SessionMetrics map[string]*SessionPerformanceData
	mu             sync.RWMutex

	// Global counters
	TotalOperations       int64
	TotalMessages         int64
	TotalConnections      int64
	TotalDisconnections   int64
	TotalStateCorrections int64

	// Performance tracking
	OperationLatencies  []time.Duration
	MessageSizes        []int
	ConnectionDurations []time.Duration

	// Context for shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// SessionPerformanceData tracks performance metrics for a single collaboration session
type SessionPerformanceData struct {
	SessionID    string
	DiagramID    string
	StartTime    time.Time
	LastActivity time.Time

	// Operation metrics
	OperationCount   int64
	OperationLatency time.Duration
	AverageLatency   time.Duration

	// Message metrics
	MessageCount  int64
	BytesSent     int64
	BytesReceived int64

	// Participant metrics
	ParticipantCount int
	MaxParticipants  int
	PeakConcurrency  int

	// Error metrics
	ConflictCount        int64
	StateCorrectionCount int64
	ResyncRequestCount   int64
	AuthDeniedCount      int64

	// Connection quality
	DisconnectionCount int64
	ReconnectionCount  int64
	AverageMessageSize float64
}

// OperationPerformance tracks individual operation performance
type OperationPerformance struct {
	OperationID      string
	UserID           string
	StartTime        time.Time
	ProcessingTime   time.Duration
	ValidationTime   time.Duration
	BroadcastTime    time.Duration
	TotalTime        time.Duration
	CellCount        int
	StateChanged     bool
	ConflictDetected bool
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor() *PerformanceMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	pm := &PerformanceMonitor{
		SessionMetrics:      make(map[string]*SessionPerformanceData),
		OperationLatencies:  make([]time.Duration, 0, 1000),
		MessageSizes:        make([]int, 0, 1000),
		ConnectionDurations: make([]time.Duration, 0, 1000),
		ctx:                 ctx,
		cancel:              cancel,
	}

	// Start background monitoring
	go pm.startPerformanceReporting()

	return pm
}

// RecordSessionStart records the start of a new collaboration session
func (pm *PerformanceMonitor) RecordSessionStart(sessionID, diagramID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.SessionMetrics[sessionID] = &SessionPerformanceData{
		SessionID:          sessionID,
		DiagramID:          diagramID,
		StartTime:          time.Now(),
		LastActivity:       time.Now(),
		ParticipantCount:   0,
		MaxParticipants:    0,
		AverageMessageSize: 0.0,
	}

	log.Printf("PERFORMANCE: Session %s started for diagram %s", sessionID, diagramID)
}

// RecordSessionEnd records the end of a collaboration session
func (pm *PerformanceMonitor) RecordSessionEnd(sessionID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if session, exists := pm.SessionMetrics[sessionID]; exists {
		duration := time.Since(session.StartTime)
		pm.ConnectionDurations = append(pm.ConnectionDurations, duration)

		log.Printf("PERFORMANCE: Session %s ended, duration: %v, operations: %d, messages: %d",
			sessionID, duration, session.OperationCount, session.MessageCount)

		delete(pm.SessionMetrics, sessionID)
	}
}

// RecordOperation records performance metrics for a diagram operation
func (pm *PerformanceMonitor) RecordOperation(perf *OperationPerformance) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.TotalOperations++
	pm.OperationLatencies = append(pm.OperationLatencies, perf.TotalTime)

	// Update session metrics
	if session, exists := pm.SessionMetrics[perf.OperationID]; exists {
		session.OperationCount++
		session.OperationLatency += perf.TotalTime
		session.AverageLatency = session.OperationLatency / time.Duration(session.OperationCount)
		session.LastActivity = time.Now()

		if perf.ConflictDetected {
			session.ConflictCount++
		}
	}

	// Log slow operations
	if perf.TotalTime > 100*time.Millisecond {
		log.Printf("PERFORMANCE WARNING: Slow operation %s took %v (validation: %v, processing: %v, broadcast: %v)",
			perf.OperationID, perf.TotalTime, perf.ValidationTime, perf.ProcessingTime, perf.BroadcastTime)
	}
}

// RecordMessage records metrics for WebSocket message handling
func (pm *PerformanceMonitor) RecordMessage(sessionID string, messageSize int, processingTime time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.TotalMessages++
	pm.MessageSizes = append(pm.MessageSizes, messageSize)

	if session, exists := pm.SessionMetrics[sessionID]; exists {
		session.MessageCount++
		session.BytesReceived += int64(messageSize)

		// Update average message size
		session.AverageMessageSize = float64(session.BytesReceived) / float64(session.MessageCount)
		session.LastActivity = time.Now()
	}

	// Log large messages
	if messageSize > 10*1024 { // 10KB
		log.Printf("PERFORMANCE WARNING: Large message %d bytes in session %s", messageSize, sessionID)
	}
}

// RecordConnection records connection events
func (pm *PerformanceMonitor) RecordConnection(sessionID string, connect bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if connect {
		pm.TotalConnections++
		if session, exists := pm.SessionMetrics[sessionID]; exists {
			session.ParticipantCount++
			if session.ParticipantCount > session.MaxParticipants {
				session.MaxParticipants = session.ParticipantCount
				session.PeakConcurrency = session.ParticipantCount
			}
		}
	} else {
		pm.TotalDisconnections++
		if session, exists := pm.SessionMetrics[sessionID]; exists {
			session.ParticipantCount--
			session.DisconnectionCount++
		}
	}
}

// RecordStateCorrection records state correction events
func (pm *PerformanceMonitor) RecordStateCorrection(sessionID, userID, reason string, cellCount int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.TotalStateCorrections++

	if session, exists := pm.SessionMetrics[sessionID]; exists {
		session.StateCorrectionCount++
		session.LastActivity = time.Now()
	}

	log.Printf("PERFORMANCE: State correction sent to %s in session %s, reason: %s, cells: %d",
		userID, sessionID, reason, cellCount)
}

// RecordResyncRequest records resync request events
func (pm *PerformanceMonitor) RecordResyncRequest(sessionID, userID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if session, exists := pm.SessionMetrics[sessionID]; exists {
		session.ResyncRequestCount++
		session.LastActivity = time.Now()
	}

	log.Printf("PERFORMANCE: Resync requested by %s in session %s", userID, sessionID)
}

// RecordAuthorizationDenied records authorization denial events
func (pm *PerformanceMonitor) RecordAuthorizationDenied(sessionID, userID, reason string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if session, exists := pm.SessionMetrics[sessionID]; exists {
		session.AuthDeniedCount++
		session.LastActivity = time.Now()
	}

	log.Printf("PERFORMANCE: Authorization denied for %s in session %s, reason: %s",
		userID, sessionID, reason)
}

// GetSessionMetrics returns current session performance data
func (pm *PerformanceMonitor) GetSessionMetrics() map[string]*SessionPerformanceData {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Return a copy to avoid concurrent access issues
	result := make(map[string]*SessionPerformanceData)
	for k, v := range pm.SessionMetrics {
		sessionCopy := *v // Copy the struct
		result[k] = &sessionCopy
	}

	return result
}

// GetGlobalMetrics returns global performance statistics
func (pm *PerformanceMonitor) GetGlobalMetrics() GlobalPerformanceMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	metrics := GlobalPerformanceMetrics{
		TotalOperations:       pm.TotalOperations,
		TotalMessages:         pm.TotalMessages,
		TotalConnections:      pm.TotalConnections,
		TotalDisconnections:   pm.TotalDisconnections,
		TotalStateCorrections: pm.TotalStateCorrections,
		ActiveSessions:        int64(len(pm.SessionMetrics)),
	}

	// Calculate averages
	if len(pm.OperationLatencies) > 0 {
		var total time.Duration
		for _, latency := range pm.OperationLatencies {
			total += latency
		}
		metrics.AverageOperationLatency = total / time.Duration(len(pm.OperationLatencies))
	}

	if len(pm.MessageSizes) > 0 {
		var total int
		for _, size := range pm.MessageSizes {
			total += size
		}
		metrics.AverageMessageSize = float64(total) / float64(len(pm.MessageSizes))
	}

	if len(pm.ConnectionDurations) > 0 {
		var total time.Duration
		for _, duration := range pm.ConnectionDurations {
			total += duration
		}
		metrics.AverageSessionDuration = total / time.Duration(len(pm.ConnectionDurations))
	}

	return metrics
}

// GlobalPerformanceMetrics represents system-wide performance statistics
type GlobalPerformanceMetrics struct {
	TotalOperations         int64         `json:"total_operations"`
	TotalMessages           int64         `json:"total_messages"`
	TotalConnections        int64         `json:"total_connections"`
	TotalDisconnections     int64         `json:"total_disconnections"`
	TotalStateCorrections   int64         `json:"total_state_corrections"`
	ActiveSessions          int64         `json:"active_sessions"`
	AverageOperationLatency time.Duration `json:"average_operation_latency"`
	AverageMessageSize      float64       `json:"average_message_size"`
	AverageSessionDuration  time.Duration `json:"average_session_duration"`
}

// startPerformanceReporting runs background performance reporting
func (pm *PerformanceMonitor) startPerformanceReporting() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.logPerformanceSummary()
			pm.cleanupOldMetrics()
		}
	}
}

// logPerformanceSummary logs a summary of performance metrics
func (pm *PerformanceMonitor) logPerformanceSummary() {
	metrics := pm.GetGlobalMetrics()
	sessions := pm.GetSessionMetrics()

	log.Printf("PERFORMANCE SUMMARY: Sessions: %d, Operations: %d, Messages: %d, Connections: %d",
		metrics.ActiveSessions, metrics.TotalOperations, metrics.TotalMessages, metrics.TotalConnections)

	if metrics.AverageOperationLatency > 0 {
		log.Printf("PERFORMANCE SUMMARY: Avg Operation Latency: %v, Avg Message Size: %.1f bytes",
			metrics.AverageOperationLatency, metrics.AverageMessageSize)
	}

	// Log sessions with high activity
	for sessionID, session := range sessions {
		if session.OperationCount > 100 || session.MessageCount > 1000 {
			log.Printf("PERFORMANCE SUMMARY: High activity session %s - ops: %d, msgs: %d, participants: %d",
				sessionID, session.OperationCount, session.MessageCount, session.ParticipantCount)
		}
	}
}

// cleanupOldMetrics removes old metrics data to prevent memory leaks
func (pm *PerformanceMonitor) cleanupOldMetrics() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Keep only last 1000 entries for latencies and sizes
	if len(pm.OperationLatencies) > 1000 {
		pm.OperationLatencies = pm.OperationLatencies[len(pm.OperationLatencies)-1000:]
	}

	if len(pm.MessageSizes) > 1000 {
		pm.MessageSizes = pm.MessageSizes[len(pm.MessageSizes)-1000:]
	}

	if len(pm.ConnectionDurations) > 1000 {
		pm.ConnectionDurations = pm.ConnectionDurations[len(pm.ConnectionDurations)-1000:]
	}
}

// Shutdown gracefully stops the performance monitor
func (pm *PerformanceMonitor) Shutdown() {
	pm.cancel()
	pm.logPerformanceSummary()
}

// Global performance monitor instance
var GlobalPerformanceMonitor *PerformanceMonitor

// InitializePerformanceMonitoring initializes the global performance monitor
func InitializePerformanceMonitoring() {
	GlobalPerformanceMonitor = NewPerformanceMonitor()
	log.Printf("Performance monitoring initialized")
}
