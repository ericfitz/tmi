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

func TestCloudWatchdog_HealthTransitionEmitsWarnThenInfo(t *testing.T) {
	fake := &fakeCloudWriter{healthy: true}

	cloudHandler := NewCloudLogHandler(CloudLogHandlerConfig{
		LocalHandler: slog.NewTextHandler(&bytes.Buffer{}, nil),
		CloudWriter:  fake,
		Level:        slog.LevelInfo,
		BufferSize:   1,
		AsyncWrites:  false,
	})
	t.Cleanup(func() { _ = cloudHandler.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w := newCloudWatchdog(cloudHandler, fake, slogger, 50*time.Millisecond, 0)
	t.Cleanup(w.Stop)

	// Give the watchdog goroutine time to start and seed lastHealthy before
	// we change the health state.
	time.Sleep(10 * time.Millisecond)

	// Flip to unhealthy.
	fake.setHealthy(false)
	time.Sleep(200 * time.Millisecond)
	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink unhealthy")) {
		t.Fatalf("expected unhealthy Warn; got %q", buf.String())
	}

	// Flip back to healthy.
	buf.Reset()
	fake.setHealthy(true)
	time.Sleep(200 * time.Millisecond)
	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink healthy")) {
		t.Fatalf("expected healthy Info; got %q", buf.String())
	}
}

func TestCloudWatchdog_RecoveryAfterFailures(t *testing.T) {
	fake := &fakeCloudWriter{healthy: true}

	cloudHandler := NewCloudLogHandler(CloudLogHandlerConfig{
		LocalHandler: slog.NewTextHandler(&bytes.Buffer{}, nil),
		CloudWriter:  fake,
		Level:        slog.LevelInfo,
		BufferSize:   1,
		AsyncWrites:  false,
	})
	t.Cleanup(func() { _ = cloudHandler.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w := newCloudWatchdog(cloudHandler, fake, slogger, 50*time.Millisecond, 5)
	t.Cleanup(w.Stop)

	logger := slog.New(cloudHandler)
	// Send 6 writes: first fills buffer, writes 2-6 go sync → 5 errors → threshold met.
	fake.setWriteErr(errors.New("boom"))
	for i := 0; i < 6; i++ {
		logger.Info("trigger")
	}
	// Wait for first Warn (one tick ~50ms), but not long enough for a second tick
	// (which would emit recovery before we can test for it separately).
	time.Sleep(70 * time.Millisecond)
	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("expected failing-writes Warn; got %q", buf.String())
	}

	// Stop generating errors; wait for a quiet poll, then expect recovery Info.
	buf.Reset()
	fake.setWriteErr(nil)
	time.Sleep(200 * time.Millisecond)
	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink writes recovered")) {
		t.Fatalf("expected recovery Info; got %q", buf.String())
	}

	// New burst of failures should re-trigger the Warn.
	buf.Reset()
	fake.setWriteErr(errors.New("boom"))
	for i := 0; i < 6; i++ {
		logger.Info("trigger")
	}
	time.Sleep(200 * time.Millisecond)
	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("expected re-armed failing-writes Warn; got %q", buf.String())
	}
}

func TestCloudWatchdog_ThresholdZeroDisablesAlarm(t *testing.T) {
	fake := &fakeCloudWriter{healthy: true}

	cloudHandler := NewCloudLogHandler(CloudLogHandlerConfig{
		LocalHandler: slog.NewTextHandler(&bytes.Buffer{}, nil),
		CloudWriter:  fake,
		Level:        slog.LevelInfo,
		BufferSize:   1,
		AsyncWrites:  false,
	})
	t.Cleanup(func() { _ = cloudHandler.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w := newCloudWatchdog(cloudHandler, fake, slogger, 50*time.Millisecond, 0)
	t.Cleanup(w.Stop)

	logger := slog.New(cloudHandler)
	fake.setWriteErr(errors.New("boom"))
	for i := 0; i < 50; i++ {
		logger.Info("trigger")
	}
	time.Sleep(200 * time.Millisecond)

	if bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("alarm fired despite threshold=0; got %q", buf.String())
	}
}
