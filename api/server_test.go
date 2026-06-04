package api

import (
	"testing"

	"github.com/ericfitz/tmi/internal/worker"
)

func TestServer_SetExtractionDeps_NATSAvailability(t *testing.T) {
	s := &Server{}
	if s.AsyncExtractionAvailable() {
		t.Fatal("expected async unavailable with no NATS conn")
	}
	s.SetExtractionNATS(&worker.Conn{})
	if !s.AsyncExtractionAvailable() {
		t.Fatal("expected async available once NATS conn is set")
	}
}
