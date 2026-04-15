package dberrors

import (
	"os"

	"github.com/ericfitz/tmi/internal/slogging"
)

// HandleFatal logs the error and terminates the process.
// Called by services when they detect a fatal condition (DB permission denied,
// crypto failure, etc.). Fatal errors should never reach handlers.
//
// Future improvement: #262 will add graceful shutdown before exit.
func HandleFatal(err error) {
	logger := slogging.Get()
	logger.Error("Fatal error, shutting down: %v", err)
	os.Exit(1)
}
