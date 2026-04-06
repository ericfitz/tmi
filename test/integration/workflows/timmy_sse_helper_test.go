package workflows

import (
	"encoding/json"
	"strings"
)

// SSEEvent represents a parsed Server-Sent Event
type SSEEvent struct {
	Event string
	Data  string
}

// parseSSEBody parses a raw SSE response body into a slice of events.
// SSE format: "event: <name>\ndata: <json>\n\n"
func parseSSEBody(body []byte) []SSEEvent {
	var events []SSEEvent
	lines := strings.Split(string(body), "\n")

	var currentEvent string
	var currentData string

	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			currentEvent = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			currentData = after
		} else if line == "" && currentEvent != "" {
			events = append(events, SSEEvent{
				Event: currentEvent,
				Data:  currentData,
			})
			currentEvent = ""
			currentData = ""
		}
	}

	return events
}

// findSSEEvent finds the first event with the given name and unmarshals its data.
func findSSEEvent(events []SSEEvent, eventName string, target any) bool {
	for _, e := range events {
		if e.Event == eventName {
			if target != nil {
				_ = json.Unmarshal([]byte(e.Data), target)
			}
			return true
		}
	}
	return false
}

// findLastSSEEvent finds the last event with the given name and unmarshals its data.
func findLastSSEEvent(events []SSEEvent, eventName string, target any) bool {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Event == eventName {
			if target != nil {
				_ = json.Unmarshal([]byte(events[i].Data), target)
			}
			return true
		}
	}
	return false
}
