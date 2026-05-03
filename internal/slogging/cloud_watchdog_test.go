package slogging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeCloudWriter is a controllable CloudLogWriter for tests.
type fakeCloudWriter struct {
	mu          sync.Mutex
	healthy     bool
	writeErr    error
	writeCalls  int64
	healthCalls int64
}

func (f *fakeCloudWriter) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeCloudWriter) WriteLog(_ context.Context, _ LogEntry) error {
	atomic.AddInt64(&f.writeCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writeErr
}
func (f *fakeCloudWriter) Flush(_ context.Context) error { return nil }
func (f *fakeCloudWriter) Close() error                  { return nil }
func (f *fakeCloudWriter) Name() string                  { return "fake" }
func (f *fakeCloudWriter) IsHealthy(_ context.Context) bool {
	atomic.AddInt64(&f.healthCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthy
}

func (f *fakeCloudWriter) setHealthy(b bool) { f.mu.Lock(); f.healthy = b; f.mu.Unlock() }
func (f *fakeCloudWriter) setWriteErr(e error) {
	f.mu.Lock()
	f.writeErr = e
	f.mu.Unlock()
}

func TestCloudWatchdog_FailureThresholdEmitsOneWarn(t *testing.T) {
	fake := &fakeCloudWriter{healthy: true}

	cloudHandler := NewCloudLogHandler(CloudLogHandlerConfig{
		LocalHandler: slog.NewTextHandler(&bytes.Buffer{}, nil),
		CloudWriter:  fake,
		Level:        slog.LevelInfo,
		BufferSize:   1,
		AsyncWrites:  false, // synchronous for deterministic counts
	})
	t.Cleanup(func() { _ = cloudHandler.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w := newCloudWatchdog(cloudHandler, fake, slogger, 50*time.Millisecond, 5)
	t.Cleanup(w.Stop)

	// Cause 5 errors. Use the slog interface so the handler increments errorCount.
	// We send 6 writes: the first fills the 1-slot buffer (no sync write), and
	// writes 2-6 each hit the sync path because the buffer stays full with no
	// async drainer running (AsyncWrites: false). That gives exactly 5 errors.
	fake.setWriteErr(errors.New("boom"))
	logger := slog.New(cloudHandler)
	for i := 0; i < 6; i++ {
		logger.Info("trigger")
	}

	// Wait for first watchdog tick to fire the Warn.
	time.Sleep(70 * time.Millisecond)

	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("expected failing-writes Warn; got %q", buf.String())
	}

	// While errors are still ongoing (no quiet poll yet), trigger more failures.
	// The Warn must NOT repeat because failureWarnEmitted is still true.
	// Send more errors immediately and observe the next tick.
	buf.Reset()
	for i := 0; i < 6; i++ {
		logger.Info("trigger")
	}
	// Wait for exactly one more tick — still mid-burst, so no recovery has fired.
	time.Sleep(60 * time.Millisecond)
	if bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("unexpected duplicate Warn; got %q", buf.String())
	}
}
