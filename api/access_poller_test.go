package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAccessPoller_Creation(t *testing.T) {
	sources := NewContentSourceRegistry()
	poller := NewAccessPoller(sources, nil, time.Minute, 7*24*time.Hour)
	assert.NotNil(t, poller)
	assert.Equal(t, time.Minute, poller.interval)
	assert.Equal(t, 7*24*time.Hour, poller.maxAge)
}

func TestAccessPoller_StopSignal(t *testing.T) {
	sources := NewContentSourceRegistry()
	poller := NewAccessPoller(sources, nil, time.Hour, 7*24*time.Hour)
	poller.Start()
	// Should not panic on stop
	poller.Stop()
}
