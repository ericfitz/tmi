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
