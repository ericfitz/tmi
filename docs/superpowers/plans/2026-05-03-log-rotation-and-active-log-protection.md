# Log rotation + active-log deletion protection — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `logs/tmi.log` recoverable when externally deleted, prevent cleanup scripts from doing the deletion behind a running server, and surface cloud-log-sink degradation as visible Warn entries.

**Architecture:** Two server-side watchdog components in `internal/slogging` (one fsnotify-based, one polling), plus running-server guards in `scripts/manage-server.py` and `scripts/clean.py`. New configurable `cloud_error_threshold` knob. Documentation polish on existing rotation knobs (inline comments + new wiki page).

**Tech Stack:** Go (slog + lumberjack + fsnotify), Python 3.11+ (uv-managed scripts), YAML config.

**Spec:** [docs/superpowers/specs/2026-05-03-log-rotation-and-active-log-protection-design.md](../specs/2026-05-03-log-rotation-and-active-log-protection-design.md)

**Issue:** [#372](https://github.com/ericfitz/tmi/issues/372)

---

## File Structure

| File | Status | Responsibility |
|---|---|---|
| `internal/slogging/file_watchdog.go` | new | fsnotify-based watchdog: detects unlink/rename of `tmi.log`, calls `lumberjack.Logger.Rotate()` |
| `internal/slogging/file_watchdog_test.go` | new | Unit tests for the file watchdog |
| `internal/slogging/cloud_watchdog.go` | new | Periodic poll: health-check transitions + consecutive-failure threshold for cloud sink |
| `internal/slogging/cloud_watchdog_test.go` | new | Unit tests for the cloud watchdog |
| `internal/slogging/logger.go` | modify | Wire both watchdogs into `NewLogger`/`Close`; add `Config.CloudErrorThreshold` |
| `internal/config/config.go` | modify | Add `LoggingConfig.CloudErrorThreshold`; expand inline doc comments on existing fields |
| `internal/config/migratable_settings.go` | modify | Register `logging.cloud_error_threshold` |
| `cmd/server/main.go` | modify | Plumb `CloudErrorThreshold` from `cfg.Logging` into the logger config |
| `config-development.yml` | modify | Comments only; show the new `cloud_error_threshold` key (commented-out at default) |
| `config-development-oci.yml` | modify | Same |
| `scripts/manage-server.py` | modify | Add `_running_server_pid()` helper; guard inside `_clean_logs` |
| `scripts/clean.py` | modify | `clean_logs()` calls `manage-server.py stop` first |
| `go.mod` / `go.sum` | modify | Add direct dep on `github.com/fsnotify/fsnotify` |

Out-of-tree:
- New GitHub wiki page **"Operator Guide: Logging & Log Rotation"** (created manually after merge; documented in this plan but not auto-applied).

---

## Task 1: Add fsnotify direct dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add fsnotify as a direct dependency**

Run:
```
go get github.com/fsnotify/fsnotify@latest
```
Expected: `go.mod` and `go.sum` updated; the module appears in `require ()` block as a direct (un-indirect) dependency.

- [ ] **Step 2: Tidy and verify build**

Run:
```
go mod tidy
make build-server
```
Expected: build succeeds; bin/tmiserver produced.

- [ ] **Step 3: Commit**

```
git add go.mod go.sum
git commit -m "deps: add fsnotify for log file watchdog (#372)"
```

---

## Task 2: `logFileWatchdog` — failing test (reopen on delete)

**Files:**
- Create: `internal/slogging/file_watchdog_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/slogging/file_watchdog_test.go`:

```go
package slogging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// waitForFile polls for the existence of path for up to timeout.
func waitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func TestLogFileWatchdog_ReopensOnDelete(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "tmi.log")

	lj := &lumberjack.Logger{Filename: logPath, MaxSize: 100, MaxBackups: 0, MaxAge: 0}
	t.Cleanup(func() { _ = lj.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w, err := newLogFileWatchdog(lj, slogger)
	if err != nil {
		t.Fatalf("newLogFileWatchdog: %v", err)
	}
	t.Cleanup(w.Stop)

	if _, err := lj.Write([]byte("first\n")); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	if err := os.Remove(logPath); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if !waitForFile(logPath, 2*time.Second) {
		t.Fatalf("active log not recreated within 2s")
	}

	if _, err := lj.Write([]byte("second\n")); err != nil {
		t.Fatalf("post-reopen write: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read recreated log: %v", err)
	}
	if !bytes.Contains(data, []byte("second")) {
		t.Fatalf("recreated log missing second write; got %q", data)
	}

	if !bytes.Contains(buf.Bytes(), []byte("log file unlinked or replaced")) {
		t.Fatalf("expected reopen Warn in slogger output; got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```
make test-unit name=TestLogFileWatchdog_ReopensOnDelete
```
Expected: FAIL — `newLogFileWatchdog` undefined.

---

## Task 3: `logFileWatchdog` — minimal implementation

**Files:**
- Create: `internal/slogging/file_watchdog.go`

- [ ] **Step 1: Implement the watchdog**

Create `internal/slogging/file_watchdog.go`:

```go
package slogging

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/natefinch/lumberjack.v2"
)

// logFileWatchdog observes the directory containing the active log file and,
// when that file is unlinked or renamed and not replaced (i.e., not a
// lumberjack-internal rotation), calls Rotate() on the lumberjack logger to
// reopen the file at its original path.
type logFileWatchdog struct {
	watcher    *fsnotify.Watcher
	fileLog    *lumberjack.Logger
	activePath string
	slogger    *slog.Logger
	done       chan struct{}
	once       sync.Once
}

// newLogFileWatchdog constructs the watchdog and starts its event-loop
// goroutine. Returns a non-nil watchdog whose Stop method MUST be called to
// release resources.
func newLogFileWatchdog(lj *lumberjack.Logger, slogger *slog.Logger) (*logFileWatchdog, error) {
	activePath, err := filepath.Abs(lj.Filename)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(activePath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		return nil, err
	}

	w := &logFileWatchdog{
		watcher:    watcher,
		fileLog:    lj,
		activePath: filepath.Clean(activePath),
		slogger:    slogger,
		done:       make(chan struct{}),
	}
	go w.run()
	return w, nil
}

func (w *logFileWatchdog) run() {
	for {
		select {
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) != w.activePath {
				continue
			}
			if ev.Op&(fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			// If the active path still exists (lumberjack already created
			// a fresh file), this is a self-rotation — skip silently.
			if _, err := os.Stat(w.activePath); err == nil {
				continue
			}
			if err := w.fileLog.Rotate(); err != nil {
				w.slogger.Warn("log file watchdog: reopen failed",
					"path", w.activePath,
					"event", ev.Op.String(),
					"error", err.Error())
				continue
			}
			w.slogger.Warn("log file unlinked or replaced; reopening",
				"path", w.activePath,
				"event", ev.Op.String())

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			if err != nil {
				w.slogger.Warn("log file watchdog: watcher error", "error", err.Error())
			}

		case <-w.done:
			return
		}
	}
}

// Stop signals the event loop to exit and closes the underlying watcher.
// Safe to call multiple times.
func (w *logFileWatchdog) Stop() {
	w.once.Do(func() {
		close(w.done)
		_ = w.watcher.Close()
	})
}
```

- [ ] **Step 2: Run test to verify it passes**

Run:
```
make test-unit name=TestLogFileWatchdog_ReopensOnDelete
```
Expected: PASS.

- [ ] **Step 3: Commit**

```
git add internal/slogging/file_watchdog.go internal/slogging/file_watchdog_test.go
git commit -m "feat(logging): add fsnotify watchdog for active log file (#372)"
```

---

## Task 4: `logFileWatchdog` — additional tests

**Files:**
- Modify: `internal/slogging/file_watchdog_test.go`

- [ ] **Step 1: Add the unrelated-file test**

Append to `internal/slogging/file_watchdog_test.go`:

```go
func TestLogFileWatchdog_IgnoresUnrelatedFiles(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "tmi.log")

	lj := &lumberjack.Logger{Filename: logPath, MaxSize: 100}
	t.Cleanup(func() { _ = lj.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w, err := newLogFileWatchdog(lj, slogger)
	if err != nil {
		t.Fatalf("newLogFileWatchdog: %v", err)
	}
	t.Cleanup(w.Stop)

	if _, err := lj.Write([]byte("first\n")); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	other := filepath.Join(tmp, "unrelated.txt")
	if err := os.WriteFile(other, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}
	if err := os.Remove(other); err != nil {
		t.Fatalf("remove unrelated: %v", err)
	}

	// Give the watcher a moment to (not) react.
	time.Sleep(200 * time.Millisecond)

	if bytes.Contains(buf.Bytes(), []byte("log file unlinked or replaced")) {
		t.Fatalf("watchdog reacted to unrelated file; got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test**

Run:
```
make test-unit name=TestLogFileWatchdog_IgnoresUnrelatedFiles
```
Expected: PASS.

- [ ] **Step 3: Add the lumberjack-rotation silence test**

Append to `internal/slogging/file_watchdog_test.go`:

```go
func TestLogFileWatchdog_SilentOnLumberjackRotation(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "tmi.log")

	lj := &lumberjack.Logger{Filename: logPath, MaxSize: 100, MaxBackups: 1, MaxAge: 0}
	t.Cleanup(func() { _ = lj.Close() })

	var buf bytes.Buffer
	slogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w, err := newLogFileWatchdog(lj, slogger)
	if err != nil {
		t.Fatalf("newLogFileWatchdog: %v", err)
	}
	t.Cleanup(w.Stop)

	if _, err := lj.Write([]byte("pre-rotate\n")); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Simulate a size-triggered rotation by calling Rotate() directly.
	if err := lj.Rotate(); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// Allow time for any spurious Warn to be emitted.
	time.Sleep(200 * time.Millisecond)

	if bytes.Contains(buf.Bytes(), []byte("log file unlinked or replaced")) {
		t.Fatalf("watchdog logged a Warn for self-rotation; got %q", buf.String())
	}
}
```

- [ ] **Step 4: Run test**

Run:
```
make test-unit name=TestLogFileWatchdog_SilentOnLumberjackRotation
```
Expected: PASS.

- [ ] **Step 5: Add the idempotent-stop test**

Append to `internal/slogging/file_watchdog_test.go`:

```go
func TestLogFileWatchdog_StopIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "tmi.log")

	lj := &lumberjack.Logger{Filename: logPath, MaxSize: 100}
	t.Cleanup(func() { _ = lj.Close() })

	slogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	w, err := newLogFileWatchdog(lj, slogger)
	if err != nil {
		t.Fatalf("newLogFileWatchdog: %v", err)
	}

	w.Stop()
	w.Stop() // must not panic
}
```

- [ ] **Step 6: Run test**

Run:
```
make test-unit name=TestLogFileWatchdog_StopIsIdempotent
```
Expected: PASS.

- [ ] **Step 7: Run the full file_watchdog test set**

Run:
```
make test-unit name=TestLogFileWatchdog
```
Expected: all tests PASS.

- [ ] **Step 8: Commit**

```
git add internal/slogging/file_watchdog_test.go
git commit -m "test(logging): cover unrelated-file, self-rotation, and idempotent-stop paths (#372)"
```

---

## Task 5: `cloudWatchdog` — failing test (consecutive failure threshold)

**Files:**
- Create: `internal/slogging/cloud_watchdog_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/slogging/cloud_watchdog_test.go`:

```go
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
	fake.setWriteErr(errors.New("boom"))
	logger := slog.New(cloudHandler)
	for i := 0; i < 5; i++ {
		logger.Info("trigger")
	}

	// Wait for at least one watchdog tick.
	time.Sleep(200 * time.Millisecond)

	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("expected failing-writes Warn; got %q", buf.String())
	}

	// Reset buffer and trigger more failures; expect NO additional Warn.
	buf.Reset()
	for i := 0; i < 5; i++ {
		logger.Info("trigger")
	}
	time.Sleep(200 * time.Millisecond)
	if bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("unexpected duplicate Warn; got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```
make test-unit name=TestCloudWatchdog_FailureThresholdEmitsOneWarn
```
Expected: FAIL — `newCloudWatchdog` undefined.

---

## Task 6: `cloudWatchdog` — minimal implementation

**Files:**
- Create: `internal/slogging/cloud_watchdog.go`

- [ ] **Step 1: Implement the watchdog**

Create `internal/slogging/cloud_watchdog.go`:

```go
package slogging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// cloudWatchdog polls a configured CloudLogWriter at a fixed interval and
// surfaces (a) health-check transitions and (b) bursts of consecutive write
// failures as Warn/Info entries via the provided slogger.
//
// State (lastHealthy, lastObservedErrorCount, consecutiveFailures,
// failureWarnEmitted) is observed only from the watchdog goroutine, so no
// internal locking is required.
type cloudWatchdog struct {
	cloudHandler   *CloudLogHandler
	cloudWriter    CloudLogWriter
	slogger        *slog.Logger
	pollInterval   time.Duration
	errorThreshold int
	done           chan struct{}
	once           sync.Once
}

// newCloudWatchdog constructs the watchdog and starts its goroutine. The
// caller MUST call Stop. If errorThreshold <= 0, the failure-rate alarm is
// disabled but health-check transitions are still observed.
func newCloudWatchdog(
	cloudHandler *CloudLogHandler,
	cloudWriter CloudLogWriter,
	slogger *slog.Logger,
	pollInterval time.Duration,
	errorThreshold int,
) *cloudWatchdog {
	w := &cloudWatchdog{
		cloudHandler:   cloudHandler,
		cloudWriter:    cloudWriter,
		slogger:        slogger,
		pollInterval:   pollInterval,
		errorThreshold: errorThreshold,
		done:           make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *cloudWatchdog) run() {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Seed with current observed state.
	lastHealthy := w.safeIsHealthy()
	var lastObservedErrorCount int64
	var consecutiveFailures int
	var failureWarnEmitted bool

	for {
		select {
		case <-ticker.C:
			// Health-check transition.
			healthy := w.safeIsHealthy()
			if healthy != lastHealthy {
				if healthy {
					w.slogger.Info("cloud log sink healthy", "provider", w.cloudWriter.Name())
				} else {
					w.slogger.Warn("cloud log sink unhealthy", "provider", w.cloudWriter.Name())
				}
				lastHealthy = healthy
			}

			// Error-rate alarm.
			if w.errorThreshold > 0 {
				current := w.cloudHandler.ErrorCount()
				delta := current - lastObservedErrorCount
				if delta > 0 {
					consecutiveFailures += int(delta)
					if consecutiveFailures >= w.errorThreshold && !failureWarnEmitted {
						w.slogger.Warn("cloud log sink failing writes",
							"provider", w.cloudWriter.Name(),
							"threshold", w.errorThreshold,
							"recent_errors", consecutiveFailures)
						failureWarnEmitted = true
					}
				} else if consecutiveFailures > 0 {
					if failureWarnEmitted {
						w.slogger.Info("cloud log sink writes recovered",
							"provider", w.cloudWriter.Name())
					}
					consecutiveFailures = 0
					failureWarnEmitted = false
				}
				lastObservedErrorCount = current
			}

		case <-w.done:
			return
		}
	}
}

// safeIsHealthy calls cloudWriter.IsHealthy with a short timeout and recovers
// from panics, returning false on any failure mode.
func (w *cloudWatchdog) safeIsHealthy() (healthy bool) {
	defer func() {
		if r := recover(); r != nil {
			w.slogger.Warn("cloud log sink IsHealthy panicked",
				"provider", w.cloudWriter.Name(),
				"recover", r)
			healthy = false
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.cloudWriter.IsHealthy(ctx)
}

// Stop signals the goroutine to exit. Safe to call multiple times.
func (w *cloudWatchdog) Stop() {
	w.once.Do(func() { close(w.done) })
}
```

- [ ] **Step 2: Run test to verify it passes**

Run:
```
make test-unit name=TestCloudWatchdog_FailureThresholdEmitsOneWarn
```
Expected: PASS.

- [ ] **Step 3: Commit**

```
git add internal/slogging/cloud_watchdog.go internal/slogging/cloud_watchdog_test.go
git commit -m "feat(logging): add cloud-sink health and error-rate watchdog (#372)"
```

---

## Task 7: `cloudWatchdog` — additional tests

**Files:**
- Modify: `internal/slogging/cloud_watchdog_test.go`

- [ ] **Step 1: Add the health-transition test**

Append to `internal/slogging/cloud_watchdog_test.go`:

```go
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
```

- [ ] **Step 2: Run it**

Run:
```
make test-unit name=TestCloudWatchdog_HealthTransitionEmitsWarnThenInfo
```
Expected: PASS.

- [ ] **Step 3: Add the recovery test**

Append:

```go
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
	fake.setWriteErr(errors.New("boom"))
	for i := 0; i < 5; i++ {
		logger.Info("trigger")
	}
	time.Sleep(200 * time.Millisecond)
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
	for i := 0; i < 5; i++ {
		logger.Info("trigger")
	}
	time.Sleep(200 * time.Millisecond)
	if !bytes.Contains(buf.Bytes(), []byte("cloud log sink failing writes")) {
		t.Fatalf("expected re-armed failing-writes Warn; got %q", buf.String())
	}
}
```

- [ ] **Step 4: Run it**

Run:
```
make test-unit name=TestCloudWatchdog_RecoveryAfterFailures
```
Expected: PASS.

- [ ] **Step 5: Add the threshold-disabled test**

Append:

```go
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
```

- [ ] **Step 6: Run it**

Run:
```
make test-unit name=TestCloudWatchdog_ThresholdZeroDisablesAlarm
```
Expected: PASS.

- [ ] **Step 7: Run the full cloud_watchdog test set**

Run:
```
make test-unit name=TestCloudWatchdog
```
Expected: all PASS.

- [ ] **Step 8: Commit**

```
git add internal/slogging/cloud_watchdog_test.go
git commit -m "test(logging): cover cloud watchdog health/recovery/disable paths (#372)"
```

---

## Task 8: Wire watchdogs into `Logger`

**Files:**
- Modify: `internal/slogging/logger.go`

- [ ] **Step 1: Add `CloudErrorThreshold` to `Config`**

In `internal/slogging/logger.go`, find the `Config` struct (around line 57). Add the new field at the end of the cloud-logging configuration block (after `CloudLogBufferSize`):

```go
	// CloudErrorThreshold is the number of consecutive cloud-sink write
	// failures (observed at 60-second poll intervals) before a single Warn
	// entry is emitted. The counter resets on a quiet poll. Set to 0 to
	// disable the alarm. Default applied by NewLogger: 5.
	CloudErrorThreshold int
```

- [ ] **Step 2: Add watchdog fields to `Logger`**

In the same file, find the `Logger` struct (around line 47). Add:

```go
	fileWatchdog  *logFileWatchdog
	cloudWatchdog *cloudWatchdog
```

at the end of the struct.

- [ ] **Step 3: Construct watchdogs in `NewLogger`**

Find `NewLogger` in `internal/slogging/logger.go`. Just before the `return &Logger{ ... }` block (around line 286), add:

```go
	// Apply default for the cloud error threshold.
	cloudErrorThreshold := config.CloudErrorThreshold
	if cloudErrorThreshold == 0 {
		cloudErrorThreshold = 5
	}

	// File watchdog (defense in depth against external deletion of tmi.log).
	fileWatchdog, fwErr := newLogFileWatchdog(fileLogger, slogger)
	if fwErr != nil {
		slogger.Warn("log file watchdog could not start; deletion auto-recovery disabled",
			"error", fwErr.Error())
		fileWatchdog = nil
	}

	// Cloud watchdog (only when a cloud writer is configured).
	var cwd *cloudWatchdog
	if cloudHandler != nil && config.CloudWriter != nil {
		cwd = newCloudWatchdog(
			cloudHandler,
			config.CloudWriter,
			slogger,
			60*time.Second,
			cloudErrorThreshold,
		)
	}
```

Then update the returned struct literal to include both new fields:

```go
	return &Logger{
		slogger:                     slogger,
		level:                       config.Level,
		isDev:                       config.IsDev,
		fileLogger:                  fileLogger,
		suppressUnauthenticatedLogs: config.SuppressUnauthenticatedLogs,
		cloudHandler:                cloudHandler,
		fileWatchdog:                fileWatchdog,
		cloudWatchdog:               cwd,
	}, nil
```

- [ ] **Step 4: Stop watchdogs in `Close`**

Find `Logger.Close` (around line 343). Replace its body with:

```go
func (l *Logger) Close() error {
	var errs []error

	// Stop watchdogs first — they depend on the handler/file logger below.
	if l.cloudWatchdog != nil {
		l.cloudWatchdog.Stop()
	}
	if l.fileWatchdog != nil {
		l.fileWatchdog.Stop()
	}

	// Close cloud handler to flush pending logs.
	if l.cloudHandler != nil {
		if err := l.cloudHandler.Close(); err != nil {
			errs = append(errs, fmt.Errorf("cloud handler close: %w", err))
		}
	}

	// Close file logger.
	if l.fileLogger != nil {
		if err := l.fileLogger.Close(); err != nil {
			errs = append(errs, fmt.Errorf("file logger close: %w", err))
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
```

- [ ] **Step 5: Build and run all slogging tests**

Run:
```
make build-server
make test-unit name=TestLogger
make test-unit name=TestLogFileWatchdog
make test-unit name=TestCloudWatchdog
```
Expected: build succeeds; all tests PASS.

- [ ] **Step 6: Run the full unit suite**

Run:
```
make test-unit
```
Expected: PASS — no regressions in any other package.

- [ ] **Step 7: Commit**

```
git add internal/slogging/logger.go
git commit -m "feat(logging): wire file + cloud watchdogs into Logger lifecycle (#372)"
```

---

## Task 9: Add `CloudErrorThreshold` to config + plumb through main

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/migratable_settings.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add the field to `LoggingConfig`**

In `internal/config/config.go`, modify the `LoggingConfig` struct (line 220). After `AlsoLogToConsole` and before the "Enhanced debug logging options" comment, add:

```go
	// CloudErrorThreshold is the number of consecutive cloud-sink write
	// failures observed at 60-second poll intervals before a single Warn
	// entry is emitted. Counter resets on a quiet interval. Set to 0 to
	// disable the alarm. Default: 5.
	CloudErrorThreshold int `yaml:"cloud_error_threshold" env:"TMI_LOG_CLOUD_ERROR_THRESHOLD"`
```

- [ ] **Step 2: Set the default**

In the same file, find the `Logging:` block inside the default-config constructor (search for `MaxBackups:                  10,`). Add a line after `AlsoLogToConsole: true,`:

```go
			CloudErrorThreshold:         5,
```

- [ ] **Step 3: Register the migratable setting**

In `internal/config/migratable_settings.go`, find the block that registers the existing `logging.*` settings (search for `logging.also_log_to_console`). Add after that line:

```go
		{Key: "logging.cloud_error_threshold", Value: strconv.Itoa(c.Logging.CloudErrorThreshold), Type: "int", Description: "Cloud sink consecutive-failure threshold for one-shot Warn alarm (0 disables)", Source: settingSource("TMI_LOG_CLOUD_ERROR_THRESHOLD")},
```

- [ ] **Step 4: Plumb through `cmd/server/main.go`**

In `cmd/server/main.go`, find the `slogging.Config{ ... }` literal (around line 1492). Add after `AlsoLogToConsole`:

```go
		CloudErrorThreshold:         cfg.Logging.CloudErrorThreshold,
```

- [ ] **Step 5: Build**

Run:
```
make build-server
```
Expected: success.

- [ ] **Step 6: Run unit suite**

Run:
```
make test-unit
```
Expected: all PASS — config-related tests should still pass with the new field defaulted.

- [ ] **Step 7: Commit**

```
git add internal/config/config.go internal/config/migratable_settings.go cmd/server/main.go
git commit -m "feat(config): add logging.cloud_error_threshold knob (default 5) (#372)"
```

---

## Task 10: Inline doc-comment improvements on `LoggingConfig`

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Expand the doc comments**

In `internal/config/config.go`, replace the existing `LoggingConfig` struct (lines 220–235) with:

```go
// LoggingConfig holds configuration for the slog-based logger and its
// lumberjack-backed file rotator. Most knobs map directly onto
// gopkg.in/natefinch/lumberjack.v2; see the wiki page "Operator Guide:
// Logging & Log Rotation" for full operator-facing documentation.
type LoggingConfig struct {
	// Level is the minimum log level to emit. One of: debug, info, warn, error.
	Level string `yaml:"level" env:"TMI_LOG_LEVEL"`
	// IsDev enables developer-friendly text formatting plus source file/line
	// tagging on every record. Production deployments should set this false.
	IsDev bool `yaml:"is_dev" env:"TMI_LOG_IS_DEV"`
	// IsTest forces test-mode behavior (e.g., stable timestamps).
	IsTest bool `yaml:"is_test" env:"TMI_LOG_IS_TEST"`
	// LogDir is the directory holding the active log file (tmi.log) and
	// rotated backups. Created on startup if missing. Default: "logs".
	LogDir string `yaml:"log_dir" env:"TMI_LOG_DIR"`
	// MaxAgeDays is the maximum number of days to retain rotated backup
	// files before lumberjack deletes them. 0 = retain indefinitely
	// (subject to MaxBackups). Default: 7.
	MaxAgeDays int `yaml:"max_age_days" env:"TMI_LOG_MAX_AGE_DAYS"`
	// MaxSizeMB is the maximum size of the active log file (logs/tmi.log)
	// in megabytes before lumberjack rotates it. The active file is renamed
	// with a timestamp suffix and a fresh tmi.log is opened. Default: 100.
	MaxSizeMB int `yaml:"max_size_mb" env:"TMI_LOG_MAX_SIZE_MB"`
	// MaxBackups is the maximum number of rotated backup files to retain.
	// 0 = retain all (subject to MaxAgeDays). Default: 10.
	MaxBackups int `yaml:"max_backups" env:"TMI_LOG_MAX_BACKUPS"`
	// AlsoLogToConsole, when true, mirrors every log line to stdout in
	// addition to the file. Useful in development. Default: true.
	AlsoLogToConsole bool `yaml:"also_log_to_console" env:"TMI_LOG_ALSO_LOG_TO_CONSOLE"`
	// CloudErrorThreshold is the number of consecutive cloud-sink write
	// failures observed at 60-second poll intervals before a single Warn
	// entry is emitted. Counter resets on a quiet interval. Set to 0 to
	// disable the alarm. Default: 5.
	CloudErrorThreshold int `yaml:"cloud_error_threshold" env:"TMI_LOG_CLOUD_ERROR_THRESHOLD"`

	// Enhanced debug logging options
	LogAPIRequests              bool `yaml:"log_api_requests" env:"TMI_LOG_API_REQUESTS"`
	LogAPIResponses             bool `yaml:"log_api_responses" env:"TMI_LOG_API_RESPONSES"`
	LogWebSocketMsg             bool `yaml:"log_websocket_messages" env:"TMI_LOG_WEBSOCKET_MESSAGES"`
	RedactAuthTokens            bool `yaml:"redact_auth_tokens" env:"TMI_LOG_REDACT_AUTH_TOKENS"`
	SuppressUnauthenticatedLogs bool `yaml:"suppress_unauthenticated_logs" env:"TMI_LOG_SUPPRESS_UNAUTH_LOGS"`
}
```

- [ ] **Step 2: Build**

Run:
```
make build-server
```
Expected: success.

- [ ] **Step 3: Lint**

Run:
```
make lint
```
Expected: no new lint warnings.

- [ ] **Step 4: Commit**

```
git add internal/config/config.go
git commit -m "docs(config): expand LoggingConfig field comments (#372)"
```

---

## Task 11: Inline YAML comments in dev configs

**Files:**
- Modify: `config-development.yml`
- Modify: `config-development-oci.yml`

- [ ] **Step 1: Update `config-development.yml`**

In `config-development.yml`, replace the `logging:` block (lines 183–196) with:

```yaml
logging:
  level: debug
  is_dev: true
  log_dir: logs
  # max_age_days: days to retain rotated backups (0 = forever, subject to max_backups). Default: 7.
  max_age_days: 7
  # max_size_mb: rotate the active log file when it reaches this many MB. Default: 100.
  max_size_mb: 100
  # max_backups: cap on retained rotated files (0 = unlimited, subject to max_age_days). Default: 10.
  max_backups: 10
  # also_log_to_console: mirror logs to stdout in addition to the file. Default: true (dev).
  also_log_to_console: true
  # cloud_error_threshold: consecutive cloud-sink write failures before one Warn. 0 disables. Default: 5.
  # cloud_error_threshold: 5

  # Enhanced debug logging features
  log_api_requests: true
  log_api_responses: true
  log_websocket_messages: true
  redact_auth_tokens: true
  suppress_unauthenticated_logs: true
```

- [ ] **Step 2: Update `config-development-oci.yml`**

In `config-development-oci.yml`, find the `logging:` block (around line 200). Replace with the same structure shown in Step 1, preserving any OCI-specific values that already differ (e.g., `level`, `is_dev`).

If you're unsure which values are OCI-specific, run:
```
diff config-development.yml config-development-oci.yml | rg -A 1 -B 1 'logging|max_|also_log|level:|is_dev'
```
and preserve those differences.

- [ ] **Step 3: Sanity-check the YAML**

Run:
```
python3 -c "import yaml; yaml.safe_load(open('config-development.yml'))"
python3 -c "import yaml; yaml.safe_load(open('config-development-oci.yml'))"
```
Expected: no errors.

- [ ] **Step 4: Commit**

```
git add config-development.yml config-development-oci.yml
git commit -m "docs(config): inline-document logging knobs in dev YAML (#372)"
```

---

## Task 12: `manage-server.py` — running-server guard

**Files:**
- Modify: `scripts/manage-server.py`

- [ ] **Step 1: Add the `_running_server_pid` helper**

In `scripts/manage-server.py`, find the helpers section (around line 218, before `def _clean_logs`). Add:

```python
def _running_server_pid(project_root: Path) -> int | None:
    """Return PID of a running TMI server, or None.

    Two-pronged detection:
      1. .server.pid exists and the PID is alive.
      2. ps aux shows a bin/tmiserver process (excluding grep lines).
    """
    pid_file = project_root / ".server.pid"
    if pid_file.exists():
        pid = read_pid_file(pid_file)
        if pid is not None:
            try:
                import os as _os
                _os.kill(pid, 0)
                return pid
            except (ProcessLookupError, PermissionError):
                pass

    try:
        result = subprocess.run(
            ["ps", "aux"],
            capture_output=True,
            text=True,
            check=False,
        )
        for line in result.stdout.splitlines():
            if "bin/tmiserver" in line and "grep" not in line.split():
                parts = line.split()
                if len(parts) >= 2:
                    try:
                        return int(parts[1])
                    except ValueError:
                        continue
    except Exception:
        pass

    return None
```

- [ ] **Step 2: Guard `_clean_logs`**

In the same file, modify `_clean_logs` (around line 223). Insert at the top of the function body, just after the docstring:

```python
    pid = _running_server_pid(project_root)
    if pid is not None:
        log_error(f"Cannot clean logs: TMI server is running (PID {pid}).")
        log_error("Run 'make stop-server' first.")
        sys.exit(1)
```

- [ ] **Step 3: Manual smoke test (no running server)**

Run:
```
make stop-server || true
ls logs/ 2>/dev/null
uv run scripts/manage-server.py start --config config-development.yml || true
ps aux | rg bin/tmiserver | rg -v grep || true
make stop-server
```
Expected: server starts and stops normally; the new guard does NOT trip when no server is running. (We'll wait to start a server here; for cleanliness if one's already running, just stop it first.)

- [ ] **Step 4: Manual smoke test (with running server)**

Run:
```
make start-dev
ls logs/
uv run scripts/manage-server.py start --config config-development.yml
```
Expected: the second `start` fails with a clear error pointing to `make stop-server`. `logs/` contents untouched.

Then clean up:
```
make stop-server
```

- [ ] **Step 5: Commit**

```
git add scripts/manage-server.py
git commit -m "fix(scripts): refuse to clean logs while TMI server is running (#372)"
```

---

## Task 13: `clean.py` — auto-stop before deleting logs

**Files:**
- Modify: `scripts/clean.py`

- [ ] **Step 1: Update `clean_logs` to stop the server first**

In `scripts/clean.py`, modify `clean_logs` (around line 35). Insert at the top of the function:

```python
    project_root = get_project_root()
    scripts_dir = project_root / "scripts"

    # Auto-stop any running TMI server so the cleanup that follows can't race
    # against in-flight log writes. A no-op when nothing is running.
    log_info("Stopping any running TMI server before cleaning logs...")
    run_cmd(
        ["uv", "run", str(scripts_dir / "manage-server.py"), "stop"],
        check=False,
    )
```

The existing `project_root = get_project_root()` line further down can be removed (it's now redundant) — confirm by checking the function body.

- [ ] **Step 2: Verify the function still compiles**

Run:
```
python3 -c "import ast; ast.parse(open('scripts/clean.py').read())"
```
Expected: no error.

- [ ] **Step 3: Manual smoke test (server running)**

Run:
```
make start-dev
make clean-logs
ps aux | rg bin/tmiserver | rg -v grep || true
ls logs/ || true
```
Expected: `make clean-logs` stops the server, then cleans `logs/`. No `bin/tmiserver` process remains.

- [ ] **Step 4: Manual smoke test (no server)**

Run:
```
make stop-server
make clean-logs
```
Expected: clean succeeds; the `manage-server.py stop` invocation reports it had nothing to stop, then logs are cleaned.

- [ ] **Step 5: Commit**

```
git add scripts/clean.py
git commit -m "fix(scripts): auto-stop TMI server before clean.py logs/files (#372)"
```

---

## Task 14: Manual end-to-end verification

**Files:** none (validation only)

- [ ] **Step 1: Verify file watchdog with running server**

Run:
```
make stop-server
make start-dev
sleep 3
ls -la logs/tmi.log
rm logs/tmi.log
sleep 1
ls -la logs/tmi.log
rg "log file unlinked or replaced" logs/tmi.log
```
Expected: after `rm`, `logs/tmi.log` is recreated within ~1 second. The Warn entry `log file unlinked or replaced` is present in the new file (note: the entry that *announced* the reopen is in the new file because slog writes through the freshly-opened lumberjack FD).

- [ ] **Step 2: Verify clean-everything stops server before cleaning**

Run:
```
make start-dev
ps aux | rg bin/tmiserver | rg -v grep
make clean-everything
ps aux | rg bin/tmiserver | rg -v grep || echo "no server running"
ls logs/ 2>/dev/null || echo "logs/ cleaned"
```
Expected: server stops, then logs are cleaned. No surviving `bin/tmiserver`.

- [ ] **Step 3: Verify start-server fails fast if a server is already running**

Run:
```
make start-dev
uv run scripts/manage-server.py start --config config-development.yml || true
make stop-server
```
Expected: the second start fails with the clear "Cannot clean logs" or port-in-use message; the first server was untouched.

- [ ] **Step 4: Run lint, build, and full unit suite**

Run:
```
make lint
make build-server
make test-unit
```
Expected: all PASS.

- [ ] **Step 5: Commit nothing — this task is verification only**

If any of the above steps surface issues, fix them in a follow-up task and re-run from Step 1.

---

## Task 15: Wiki page draft

**Files:** none in-tree (the wiki lives in a separate Git repo at `https://github.com/ericfitz/tmi.wiki.git`)

This task produces the **content** of a new wiki page; the actual page creation happens manually after merge. The content is recorded here so the implementer doesn't have to reconstruct it.

- [ ] **Step 1: Draft the page content**

The new page is titled **"Operator Guide: Logging & Log Rotation"**. Recommended outline:

```
# Operator Guide: Logging & Log Rotation

## What writes to the log file

TMI uses Go's structured logger (`log/slog`) backed by
[gopkg.in/natefinch/lumberjack.v2](https://pkg.go.dev/gopkg.in/natefinch/lumberjack.v2).
The active log file is `logs/tmi.log` (path configurable via `logging.log_dir`
or `TMI_LOG_DIR`). Format is JSON in production, human-readable text in
development.

## Configuration knobs

| YAML key                          | Env var                          | Default | Behavior |
|-----------------------------------|----------------------------------|---------|----------|
| logging.level                     | TMI_LOG_LEVEL                    | info    | Minimum level: debug \| info \| warn \| error |
| logging.is_dev                    | TMI_LOG_IS_DEV                   | false   | Text formatter + source tagging |
| logging.log_dir                   | TMI_LOG_DIR                      | logs    | Directory for tmi.log and backups |
| logging.max_size_mb               | TMI_LOG_MAX_SIZE_MB              | 100     | Active file size before rotation |
| logging.max_age_days              | TMI_LOG_MAX_AGE_DAYS             | 7       | Days to retain backups (0 = forever) |
| logging.max_backups               | TMI_LOG_MAX_BACKUPS              | 10      | Max retained backups (0 = unlimited) |
| logging.also_log_to_console       | TMI_LOG_ALSO_LOG_TO_CONSOLE      | true    | Mirror to stdout |
| logging.cloud_error_threshold     | TMI_LOG_CLOUD_ERROR_THRESHOLD    | 5       | Consecutive cloud-sink failures before Warn (0 disables) |

## Rotation behavior

When `logs/tmi.log` reaches `max_size_mb`, lumberjack renames it to
`tmi-<timestamp>.log` (or `.log.gz` if compression is on, which it always
is) and creates a fresh `tmi.log`. Retention is the intersection of
`max_backups` (count) and `max_age_days` (age) — files are removed when
either bound is exceeded.

## Cloud logging

When a cloud sink (e.g., OCI Logging) is configured, TMI's cloud watchdog
polls every 60 seconds and emits:

- `WARN cloud log sink unhealthy` on health-check transition unhealthy.
- `INFO cloud log sink healthy` on transition back to healthy.
- `WARN cloud log sink failing writes` after `cloud_error_threshold`
  consecutive write failures (default 5).
- `INFO cloud log sink writes recovered` after a quiet interval.

Tune the threshold via `logging.cloud_error_threshold`. Set to 0 to
disable the failure alarm but keep the health-check transitions.

## Recovering from accidental deletion

**Do not delete `logs/tmi.log` while the server is running.** A built-in
fsnotify watchdog will recreate the file within milliseconds, but writes
that occurred between unlink and reopen are lost. The cleanup scripts —
`make clean-logs`, `make clean-files`, `make clean-everything` —
auto-stop any running TMI server before deleting log files, so prefer
those over `rm`.

If the file is deleted by a third-party process while the server is
running, the watchdog emits:

    WARN log file unlinked or replaced; reopening  path=logs/tmi.log event=REMOVE

and resumes writing to a fresh file.
```

- [ ] **Step 2: Save the draft for later wiki publication**

Save the content above to `/tmp/tmi-wiki-logging-draft.md` (a temp location, not committed) for whoever publishes the wiki page after merge:

```
mkdir -p /tmp
cat > /tmp/tmi-wiki-logging-draft.md <<'EOF'
... (content above) ...
EOF
```

This task does NOT commit anything to the repo (per CLAUDE.md: do not update the `docs/` directory; wiki content is the authoritative target).

- [ ] **Step 3: Note in the PR description**

When the PR for this work is opened, mention: "Wiki page 'Operator Guide: Logging & Log Rotation' to be published manually post-merge; draft saved at /tmp/tmi-wiki-logging-draft.md during implementation."

---

## Task 16: Final task-completion gates

**Files:** none (verification only)

- [ ] **Step 1: Lint**

Run:
```
make lint
```
Expected: PASS, no new findings.

- [ ] **Step 2: Build**

Run:
```
make build-server
```
Expected: success.

- [ ] **Step 3: Unit tests**

Run:
```
make test-unit
```
Expected: all PASS.

- [ ] **Step 4: Integration tests** (optional but recommended — this is API-adjacent)

Run:
```
make test-integration
```
Expected: all PASS, no regressions in any package that interacts with logging.

- [ ] **Step 5: Oracle DB review**

Per CLAUDE.md, this is required only when DB-touching code changes. **This change touches no DB code; the oracle-db-admin subagent is not required.**

- [ ] **Step 6: Update issue**

After all gates pass, comment on issue #372 with the implementing commits and close it (current branch is `dev/1.4.0`, so the auto-close trailer in commit messages doesn't fire):

```
gh issue comment 372 --body "Implemented on branch dev/1.4.0. Server-side fsnotify watchdog (file_watchdog.go), cloud-sink watchdog (cloud_watchdog.go), running-server guards in scripts/manage-server.py and scripts/clean.py, plus inline doc comments. Wiki page 'Operator Guide: Logging & Log Rotation' to be published manually."
gh issue close 372
```

- [ ] **Step 7: End-of-task summary**

Report: tasks 1–15 complete, all gates passed, issue closed.
