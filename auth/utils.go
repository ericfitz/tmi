package auth

import (
	"io"

	"github.com/ericfitz/tmi/internal/logging"
)

// closeBody is a helper function to close a response body and check the error
func closeBody(c io.Closer) {
	if err := c.Close(); err != nil {
		logging.Get().Error("Error closing response body: %v", err)
	}
}
