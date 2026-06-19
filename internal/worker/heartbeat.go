package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// defaultHeartbeatInterval is the publish cadence when none is configured.
const defaultHeartbeatInterval = 10 * time.Second

// Heartbeat is the liveness message a worker publishes on
// components.heartbeat.<component>. The monolith uses it to distinguish
// "type declared, no healthy instance" from "instances present".
// SEM@0e54997b95671dd5ea0319130f237e438d611479: heartbeat payload carrying component name, instance ID, and publish timestamp (pure)
type Heartbeat struct {
	// Component is the TMIComponent name.
	Component string `json:"component"`
	// InstanceID identifies the publishing pod/process.
	InstanceID string `json:"instance_id"`
	// SentAt is the publish timestamp.
	SentAt time.Time `json:"sent_at"`
}

// heartbeatInterval returns d, or the default when d is non-positive.
// SEM@0e54997b95671dd5ea0319130f237e438d611479: return the given interval or the default if zero or negative (pure)
func heartbeatInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultHeartbeatInterval
	}
	return d
}

// RunHeartbeat publishes a Heartbeat on the component's heartbeat subject
// every interval until ctx is cancelled. It is meant to run in its own
// goroutine; a publish failure is logged and retried on the next tick.
// SEM@71c1d8554ecca870da2bafa898b79d1c29d43ebf: publish periodic heartbeat messages to NATS until context is cancelled (mutates shared state)
func RunHeartbeat(ctx context.Context, conn *Conn, instanceID string, interval time.Duration) {
	logger := slogging.Get()
	subject := HeartbeatSubject(conn.Config().ComponentName)
	tick := time.NewTicker(heartbeatInterval(interval))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			hb := Heartbeat{
				Component:  conn.Config().ComponentName,
				InstanceID: instanceID,
				SentAt:     time.Now().UTC(),
			}
			b, err := json.Marshal(hb)
			if err != nil {
				logger.Error("worker heartbeat: marshal failed: %v", err)
				continue
			}
			if err := conn.PublishCore(subject, b); err != nil {
				logger.Warn("worker heartbeat: publish failed: %v", err)
			}
		}
	}
}
