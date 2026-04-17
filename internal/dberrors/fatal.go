package dberrors

import (
	"os"
	"sync"
	"sync/atomic"

	"github.com/ericfitz/tmi/internal/slogging"
)

// exitFunc is a seam for tests. Production code calls os.Exit.
var exitFunc = os.Exit

var (
	fatalHandlerMu sync.Mutex
	fatalHandler   func(error)
	fatalCount     atomic.Uint32
)

// SetFatalHandler installs a handler invoked on the first HandleFatal call.
// Intended for the server to initiate graceful shutdown on fatal conditions.
// Subsequent HandleFatal calls bypass the handler and exit immediately.
func SetFatalHandler(h func(error)) {
	fatalHandlerMu.Lock()
	defer fatalHandlerMu.Unlock()
	fatalHandler = h
}

// HandleFatal logs a fatal error and terminates the calling goroutine.
// Called by services when they detect a fatal condition (DB permission denied,
// crypto failure, etc.). HandleFatal does not return to the caller.
//
// On the first call, if a handler has been installed via SetFatalHandler,
// the handler runs and HandleFatal then blocks the calling goroutine
// indefinitely. This lets the server drain in-flight requests while keeping
// the corrupted caller from continuing.
//
// If no handler is installed, or if HandleFatal has already been called once,
// the process exits immediately with code 1.
func HandleFatal(err error) {
	slogging.Get().Error("Fatal error, shutting down: %v", err)

	// Re-entrant fatal: skip graceful path and exit immediately.
	if fatalCount.Add(1) > 1 {
		exitFunc(1)
		return
	}

	fatalHandlerMu.Lock()
	h := fatalHandler
	fatalHandlerMu.Unlock()

	if h == nil {
		exitFunc(1)
		return
	}

	h(err)

	// Corrupted caller must not continue. Block until the process exits.
	select {}
}
