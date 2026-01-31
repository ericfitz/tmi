package slogging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/loggingingestion"
)

// OCICloudWriter implements CloudLogWriter for OCI Logging service.
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
func (w *OCICloudWriter) toOCIEntry(entry LogEntry) loggingingestion.LogEntry {
	// Build data map
	data := make(map[string]interface{})
	data["level"] = entry.Level.String()
	data["message"] = entry.Message

	if entry.Source != "" {
		data["source"] = entry.Source
	}

	// Add all attributes
	for k, v := range entry.Attrs {
		data[k] = v
	}

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
		Data: common.String(string(jsonData)),
		Id:   common.String(id),
		Time: &common.SDKTime{Time: entry.Timestamp},
	}
}

// Flush forces any buffered logs to be sent immediately.
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
func (w *OCICloudWriter) flush(ctx context.Context, entries []loggingingestion.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	request := loggingingestion.PutLogsRequest{
		LogId: common.String(w.logID),
		PutLogsDetails: loggingingestion.PutLogsDetails{
			Specversion: common.String("1.0"),
			LogEntryBatches: []loggingingestion.LogEntryBatch{
				{
					Entries:             entries,
					Source:              common.String(w.source),
					Type:                common.String("tmi.application"),
					Subject:             common.String(w.subject),
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
				_ = w.flush(context.Background(), entries)
			} else {
				w.bufferMu.Unlock()
			}
		}
	}
}

// Close releases resources and flushes any remaining logs.
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
func (w *OCICloudWriter) Name() string {
	return "oci-logging"
}

// IsHealthy returns true if OCI Logging is reachable.
func (w *OCICloudWriter) IsHealthy(ctx context.Context) bool {
	w.healthMu.RLock()
	defer w.healthMu.RUnlock()
	return w.healthy
}

// setHealthy updates the health status.
func (w *OCICloudWriter) setHealthy(healthy bool) {
	w.healthMu.Lock()
	defer w.healthMu.Unlock()
	w.healthy = healthy
	if healthy {
		w.lastHealthy = time.Now()
	}
}

// LastHealthyTime returns when the writer was last healthy.
func (w *OCICloudWriter) LastHealthyTime() time.Time {
	w.healthMu.RLock()
	defer w.healthMu.RUnlock()
	return w.lastHealthy
}
