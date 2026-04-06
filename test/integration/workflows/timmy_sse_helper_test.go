package workflows

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
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

// doSSERequest makes an HTTP request to an SSE endpoint and reads the stream line-by-line.
// This avoids io.ReadAll which fails on SSE streams with "unexpected EOF" because
// the server uses chunked transfer encoding without a clean termination signal.
// Pass the httpClient and tokens directly since IntegrationClient doesn't expose them.
func doSSERequest(t *testing.T, httpClient *http.Client, tokens *framework.OAuthTokens, serverURL, method, path string, body any) []SSEEvent {
	t.Helper()

	fullURL := serverURL + path

	var httpReq *http.Request
	var err error

	if body != nil {
		bodyBytes, marshalErr := json.Marshal(body)
		framework.AssertNoError(t, marshalErr, "Failed to marshal SSE request body")
		httpReq, err = http.NewRequest(method, fullURL, bytes.NewReader(bodyBytes))
		framework.AssertNoError(t, err, "Failed to create SSE request")
		httpReq.Header.Set("Content-Type", "application/json")
	} else {
		httpReq, err = http.NewRequest(method, fullURL, nil)
		framework.AssertNoError(t, err, "Failed to create SSE request")
	}

	if tokens != nil && tokens.AccessToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := httpClient.Do(httpReq)
	framework.AssertNoError(t, err, "SSE HTTP request failed")
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		t.Fatalf("SSE request returned error status %d for %s %s", resp.StatusCode, method, path)
	}

	// Read SSE stream line-by-line using a scanner
	var events []SSEEvent
	var currentEvent, currentData string

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
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
	// Don't fail on scanner errors — SSE streams may end with EOF which is normal

	return events
}

// sseRequestFormat is a convenience type for doSSERequest path formatting
const sseSessionsPath = "/threat_models/%s/chat/sessions"
const sseMessagesPath = "/threat_models/%s/chat/sessions/%s/messages"

// formatSessionsPath returns the chat sessions path for a threat model
func formatSessionsPath(threatModelID string) string {
	return fmt.Sprintf(sseSessionsPath, threatModelID)
}

// formatMessagesPath returns the messages path for a session
func formatMessagesPath(threatModelID, sessionID string) string {
	return fmt.Sprintf(sseMessagesPath, threatModelID, sessionID)
}
