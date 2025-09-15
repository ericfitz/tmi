package auth

import (
	"io"

	"github.com/ericfitz/tmi/internal/slogging"
)

// closeBody is a helper function to close a response body and check the error
func closeBody(c io.Closer) {
	if err := c.Close(); err != nil {
		slogging.Get().Error("Error closing response body: %v", err)
	}
}
