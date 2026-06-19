package slogging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/loggingingestion"
)

// OCICloudWriter implements CloudLogWriter for OCI Logging service.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: buffered log writer that batches structured entries to the OCI Logging service (mutates shared state)
type OCICloudWriter struct {
	client       loggingingestion.LoggingClient
	logID        string
	source       string
	subject      string
	batchSize    int
	flushTimeout time.Duration

	// Buffering
	buffer    []loggingingestion.LogEntry
	bufferMu  sync.Mutex
	lastFlush time.Time

	// Health tracking
	healthy     bool
	healthMu    sync.RWMutex
	lastHealthy time.Time

	// Shutdown
	closed   bool
	closedMu sync.RWMutex
}

// OCICloudWriterConfig configures the OCI cloud writer.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: configuration for the OCI Logging writer, including log OCID, batch size, and flush interval (pure)
type OCICloudWriterConfig struct {
	// LogID is the OCID of the OCI Log to write to (required)
	LogID string

	// Source identifies the log source (e.g., "tmi-server")
	Source string

	// Subject is an optional subject for log entries
	Subject string

	// BatchSize is the number of entries to batch before flushing (default: 100)
	BatchSize int

	// FlushTimeout is how often to flush even if batch isn't full (default: 5s)
	FlushTimeout time.Duration

	// ConfigProvider is the OCI configuration provider (optional, uses default if nil)
	ConfigProvider common.ConfigurationProvider
}

// NewOCICloudWriter creates a new OCI Logging writer.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: build an OCI cloud log writer with auto-detected principal auth and start its background flusher
func NewOCICloudWriter(ctx context.Context, config OCICloudWriterConfig) (*OCICloudWriter, error) {
	// Set defaults
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.FlushTimeout <= 0 {
		config.FlushTimeout = 5 * time.Second
	}
	if config.Source == "" {
		config.Source = "tmi-server"
	}

	// Create OCI config provider
	// Priority: 1) Explicit config, 2) Resource Principal (for Container Instances/Functions),
	//           3) Instance Principal (for VMs), 4) Default (~/.oci/config for local development)
	configProvider := config.ConfigProvider
	if configProvider == nil {
		// Try Resource Principal first (used in OCI Container Instances and Functions)
		resourcePrincipal, err := auth.ResourcePrincipalConfigurationProvider()
		if err == nil {
			configProvider = resourcePrincipal
		} else {
			// Try Instance Principal next (used in OCI VMs)
			instancePrincipal, instErr := auth.InstancePrincipalConfigurationProvider()
			if instErr == nil {
				configProvider = instancePrincipal
			} else {
				// Fall back to default config provider (for local development)
				configProvider = common.DefaultConfigProvider()
			}
		}
	}

	// Create logging ingestion client
	client, err := loggingingestion.NewLoggingClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI logging client: %w", err)
	}

	w := &OCICloudWriter{
		client:       client,
		logID:        config.LogID,
		source:       config.Source,
		subject:      config.Subject,
		batchSize:    config.BatchSize,
		flushTimeout: config.FlushTimeout,
		buffer:       make([]loggingingestion.LogEntry, 0, config.BatchSize),
		lastFlush:    time.Now(),
		healthy:      true,
		lastHealthy:  time.Now(),
	}

	// Start background flusher
	go w.backgroundFlusher(ctx)

	return w, nil
}

// Write implements io.Writer for compatibility.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: dispatch a raw byte payload as an info-level log entry to OCI Logging (mutates shared state)
func (w *OCICloudWriter) Write(p []byte) (int, error) {
	// Parse as JSON log entry if possible
	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     slog.LevelInfo,
		Message:   string(p),
	}

	if err := w.WriteLog(context.Background(), entry); err != nil {
		return 0, err
	}

	return len(p), nil
}

// WriteLog sends a structured log entry to OCI Logging.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: buffer a structured log entry and flush to OCI Logging when the batch is full (mutates shared state)
func (w *OCICloudWriter) WriteLog(ctx context.Context, entry LogEntry) error {
	w.closedMu.RLock()
	if w.closed {
		w.closedMu.RUnlock()
		return nil
	}
	w.closedMu.RUnlock()

	// Convert to OCI log entry
	ociEntry := w.toOCIEntry(entry)

	w.bufferMu.Lock()
	w.buffer = append(w.buffer, ociEntry)

	// Flush if batch is full
	if len(w.buffer) >= w.batchSize {
		entries := w.buffer
		w.buffer = make([]loggingingestion.LogEntry, 0, w.batchSize)
		w.bufferMu.Unlock()
		return w.flush(ctx, entries)
	}

	w.bufferMu.Unlock()
	return nil
}

// toOCIEntry converts a LogEntry to OCI LogEntry format.
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: convert a structured log entry to the OCI LogEntry wire format (pure)
func (w *OCICloudWriter) toOCIEntry(entry LogEntry) loggingingestion.LogEntry {
	// Build data map
	data := make(map[string]any)
	data["level"] = entry.Level.String()
	data["message"] = entry.Message

	if entry.Source != "" {
		data["source"] = entry.Source
	}

	// Add all attributes
	maps.Copy(data, entry.Attrs)

	// Serialize to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		// Build fallback JSON safely without string interpolation
		fallback := map[string]string{
			"message": entry.Message,
			"error":   "failed to marshal log data",
		}
		jsonData, _ = json.Marshal(fallback)
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())

	return loggingingestion.LogEntry{
		Data: new(string(jsonData)),
		Id:   new(id),
		Time: &common.SDKTime{Time: entry.Timestamp},
	}
}

// Flush forces any buffered logs to be sent immediately.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: send all buffered log entries to OCI Logging immediately (mutates shared state)
func (w *OCICloudWriter) Flush(ctx context.Context) error {
	w.bufferMu.Lock()
	if len(w.buffer) == 0 {
		w.bufferMu.Unlock()
		return nil
	}

	entries := w.buffer
	w.buffer = make([]loggingingestion.LogEntry, 0, w.batchSize)
	w.bufferMu.Unlock()

	return w.flush(ctx, entries)
}

// flush sends a batch of entries to OCI Logging.
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: send a batch of log entries to OCI Logging and update health status (mutates shared state)
func (w *OCICloudWriter) flush(ctx context.Context, entries []loggingingestion.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	request := loggingingestion.PutLogsRequest{
		LogId: new(w.logID),
		PutLogsDetails: loggingingestion.PutLogsDetails{
			Specversion: new("1.0"),
			LogEntryBatches: []loggingingestion.LogEntryBatch{
				{
					Entries:             entries,
					Source:              new(w.source),
					Type:                new("tmi.application"),
					Subject:             new(w.subject),
					Defaultlogentrytime: &common.SDKTime{Time: time.Now()},
				},
			},
		},
	}

	_, err := w.client.PutLogs(ctx, request)
	if err != nil {
		w.setHealthy(false)
		return fmt.Errorf("failed to put logs to OCI: %w", err)
	}

	w.setHealthy(true)
	w.lastFlush = time.Now()
	return nil
}

// backgroundFlusher periodically flushes the buffer.
// SEM@65af9b7db2850b6e18076df15ed522c8df4bb64c: periodically flush buffered log entries to OCI Logging until context is cancelled (mutates shared state)
func (w *OCICloudWriter) backgroundFlusher(ctx context.Context) {
	ticker := time.NewTicker(w.flushTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.closedMu.RLock()
			if w.closed {
				w.closedMu.RUnlock()
				return
			}
			w.closedMu.RUnlock()

			// Flush if we have entries and haven't flushed recently
			w.bufferMu.Lock()
			if len(w.buffer) > 0 && time.Since(w.lastFlush) >= w.flushTimeout {
				entries := w.buffer
				w.buffer = make([]loggingingestion.LogEntry, 0, w.batchSize)
				w.bufferMu.Unlock()

				// Best effort flush - don't block on errors
				_ = w.flush(ctx, entries)
			} else {
				w.bufferMu.Unlock()
			}
		}
	}
}

// Close releases resources and flushes any remaining logs.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: flush remaining log entries and mark the writer as closed (mutates shared state)
func (w *OCICloudWriter) Close() error {
	w.closedMu.Lock()
	if w.closed {
		w.closedMu.Unlock()
		return nil
	}
	w.closed = true
	w.closedMu.Unlock()

	// Final flush
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return w.Flush(ctx)
}

// Name returns the provider name.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return the provider identifier string for this writer (pure)
func (w *OCICloudWriter) Name() string {
	return "oci-logging"
}

// IsHealthy returns true if OCI Logging is reachable.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: report whether the OCI Logging endpoint was reachable on last flush (pure)
func (w *OCICloudWriter) IsHealthy(ctx context.Context) bool {
	w.healthMu.RLock()
	defer w.healthMu.RUnlock()
	return w.healthy
}

// setHealthy updates the health status.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: update the writer's health flag and last-healthy timestamp (mutates shared state)
func (w *OCICloudWriter) setHealthy(healthy bool) {
	w.healthMu.Lock()
	defer w.healthMu.Unlock()
	w.healthy = healthy
	if healthy {
		w.lastHealthy = time.Now()
	}
}

// LastHealthyTime returns when the writer was last healthy.
// SEM@24f7dadfcf515c1af48310c466e75a45e19d6e3b: return the timestamp of the most recent successful OCI Logging flush (pure)
func (w *OCICloudWriter) LastHealthyTime() time.Time {
	w.healthMu.RLock()
	defer w.healthMu.RUnlock()
	return w.lastHealthy
}
