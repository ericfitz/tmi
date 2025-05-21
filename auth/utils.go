package auth

import (
	"io"
	"log"
)

// closeBody is a helper function to close a response body and check the error
func closeBody(c io.Closer) {
	if err := c.Close(); err != nil {
		log.Printf("Error closing response body: %v", err)
	}
}
