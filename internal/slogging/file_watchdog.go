package slogging

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/natefinch/lumberjack.v2"
)

// pollInterval is the fallback period for the polling check. On platforms
// (e.g. macOS/kqueue) where rapid create+delete sequences can be coalesced by
// the OS and delivered as a single event or dropped entirely, the ticker
// catches deletions that fsnotify missed.
const pollInterval = 500 * time.Millisecond

// logFileWatchdog observes the directory containing the active log file and,
// when that file is unlinked or renamed and not replaced (i.e., not a
// lumberjack-internal rotation), calls Rotate() on the lumberjack logger to
// reopen the file at its original path.
//
// Two complementary mechanisms are used:
//   - fsnotify directory watch: low-latency detection of rename/remove events.
//   - Periodic poll (pollInterval): fallback for platforms where rapid
//     create+delete events are coalesced by the OS kernel (e.g. macOS kqueue).
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

// handleMissing is called when the active log file is known to be absent.
// It calls lumberjack.Rotate() which closes the old (unlinked) FD and opens a
// fresh file at the same path.
func (w *logFileWatchdog) handleMissing(eventDesc string) {
	if err := w.fileLog.Rotate(); err != nil {
		w.slogger.Warn("log file watchdog: reopen failed",
			"path", w.activePath,
			"event", eventDesc,
			"error", err.Error())
		return
	}
	w.slogger.Warn("log file unlinked or replaced; reopening",
		"path", w.activePath,
		"event", eventDesc)
}

func (w *logFileWatchdog) run() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

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
			w.handleMissing(ev.Op.String())

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			if err != nil {
				w.slogger.Warn("log file watchdog: watcher error", "error", err.Error())
			}

		case <-ticker.C:
			// Polling fallback: on platforms where rapid create+delete
			// sequences are coalesced (macOS kqueue), the fsnotify event may
			// be dropped. Check directly.
			if _, err := os.Stat(w.activePath); os.IsNotExist(err) {
				w.handleMissing("poll")
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
