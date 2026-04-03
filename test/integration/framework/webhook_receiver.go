package framework

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ChallengeMode controls how the webhook receiver responds to challenge requests.
type ChallengeMode int

const (
	// ChallengeAutoRespond echoes back the challenge value correctly.
	ChallengeAutoRespond ChallengeMode = iota
	// ChallengeIgnore responds 200 with an empty body (no challenge echo).
	ChallengeIgnore
	// ChallengeWrongResponse responds with an incorrect challenge value.
	ChallengeWrongResponse
)

// ReceivedDelivery represents a webhook delivery or challenge received by the test server.
type ReceivedDelivery struct {
	Headers        http.Header
	Body           []byte
	ReceivedAt     time.Time
	EventType      string // X-Webhook-Event
	DeliveryID     string // X-Webhook-Delivery-Id
	SubscriptionID string // X-Webhook-Subscription-Id
	Signature      string // X-Webhook-Signature
}

// WebhookReceiver is an in-process HTTP test server that records webhook deliveries
// for assertion in integration tests.
type WebhookReceiver struct {
	Server        *httptest.Server
	deliveries    []ReceivedDelivery
	challenges    []ReceivedDelivery
	mu            sync.Mutex
	challengeMode ChallengeMode
	callbackMode  string        // "" = sync, "async" = respond with X-TMI-Callback header
	statusCode    int           // override response status (default 200)
	failCount     int           // return error status for first N deliveries, then 200
	deliveryCount int           // tracks total delivery attempts (for failCount logic)
	responseDelay time.Duration // sleep before responding (for timeout testing)
}

// ReceiverOption is a functional option for configuring a WebhookReceiver.
type ReceiverOption func(*WebhookReceiver)

// WithChallengeMode sets the challenge response behavior.
func WithChallengeMode(mode ChallengeMode) ReceiverOption {
	return func(r *WebhookReceiver) {
		r.challengeMode = mode
	}
}

// WithCallbackMode sets the callback mode. Use "" for sync or "async" for
// the X-TMI-Callback header.
func WithCallbackMode(mode string) ReceiverOption {
	return func(r *WebhookReceiver) {
		r.callbackMode = mode
	}
}

// WithStatusCode sets an override HTTP response status code.
func WithStatusCode(code int) ReceiverOption {
	return func(r *WebhookReceiver) {
		r.statusCode = code
	}
}

// WithFailCount configures the receiver to return the error statusCode for the
// first n deliveries, then 200 for subsequent ones. Useful for retry testing.
func WithFailCount(n int) ReceiverOption {
	return func(r *WebhookReceiver) {
		r.failCount = n
	}
}

// WithResponseDelay sets a delay before the receiver reads the body and responds.
// This causes the HTTP client to time out if the delay exceeds the client timeout.
func WithResponseDelay(d time.Duration) ReceiverOption {
	return func(r *WebhookReceiver) {
		r.responseDelay = d
	}
}

// NewWebhookReceiver creates and starts a new webhook test receiver.
func NewWebhookReceiver(opts ...ReceiverOption) *WebhookReceiver {
	r := &WebhookReceiver{
		challengeMode: ChallengeAutoRespond,
		statusCode:    200,
	}

	for _, opt := range opts {
		opt(r)
	}

	r.Server = httptest.NewServer(http.HandlerFunc(r.handler))
	return r
}

// handler is the single HTTP handler for all webhook deliveries and challenges.
func (r *WebhookReceiver) handler(w http.ResponseWriter, req *http.Request) {
	// Step 1: If responseDelay > 0, sleep BEFORE reading body so the HTTP client times out.
	r.mu.Lock()
	delay := r.responseDelay
	r.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	// Step 2: Read body and extract webhook headers.
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusInternalServerError)
		return
	}
	defer req.Body.Close()

	delivery := ReceivedDelivery{
		Headers:        req.Header.Clone(),
		Body:           body,
		ReceivedAt:     time.Now(),
		EventType:      req.Header.Get("X-Webhook-Event"),
		DeliveryID:     req.Header.Get("X-Webhook-Delivery-Id"),
		SubscriptionID: req.Header.Get("X-Webhook-Subscription-Id"),
		Signature:      req.Header.Get("X-Webhook-Signature"),
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Step 3: If challenge, record and respond based on challengeMode.
	if delivery.EventType == "webhook.challenge" {
		r.challenges = append(r.challenges, delivery)

		switch r.challengeMode {
		case ChallengeAutoRespond:
			// Parse the challenge value from the body and echo it back.
			var challengeBody map[string]any
			if err := json.Unmarshal(body, &challengeBody); err == nil {
				if challengeVal, ok := challengeBody["challenge"]; ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					resp, _ := json.Marshal(map[string]any{"challenge": challengeVal})
					_, _ = w.Write(resp)
					return
				}
			}
			// If we cannot parse, fall through to a plain 200.
			w.WriteHeader(http.StatusOK)
			return

		case ChallengeIgnore:
			w.WriteHeader(http.StatusOK)
			return

		case ChallengeWrongResponse:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp, _ := json.Marshal(map[string]any{"challenge": "wrong-value"})
			_, _ = w.Write(resp)
			return
		}

		// Default: plain 200
		w.WriteHeader(http.StatusOK)
		return
	}

	// Step 4: Non-challenge delivery.
	r.deliveryCount++
	r.deliveries = append(r.deliveries, delivery)

	// Determine response status code.
	responseCode := http.StatusOK
	if r.failCount > 0 && r.deliveryCount <= r.failCount {
		responseCode = r.statusCode
	} else if r.failCount == 0 && r.statusCode != 200 {
		// No failCount set but statusCode overridden: always use it.
		responseCode = r.statusCode
	}

	// If responding 200 and callbackMode is "async", set the callback header.
	if responseCode == http.StatusOK && r.callbackMode == "async" {
		w.Header().Set("X-TMI-Callback", "async")
	}

	w.WriteHeader(responseCode)
}

// URL returns the base URL of the test server (http://127.0.0.1:PORT).
func (r *WebhookReceiver) URL() string {
	return r.Server.URL
}

// WaitForDelivery blocks until at least 1 delivery is received, then returns it.
// Fails the test if the timeout is reached.
func (r *WebhookReceiver) WaitForDelivery(t *testing.T, timeout time.Duration) ReceivedDelivery {
	t.Helper()
	deliveries := r.WaitForDeliveries(t, 1, timeout)
	return deliveries[0]
}

// WaitForDeliveries blocks until at least count deliveries are received.
// Fails the test if the timeout is reached.
func (r *WebhookReceiver) WaitForDeliveries(t *testing.T, count int, timeout time.Duration) []ReceivedDelivery {
	t.Helper()
	var result []ReceivedDelivery
	PollUntil(t, timeout, 500*time.Millisecond, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.deliveries) >= count {
			result = make([]ReceivedDelivery, len(r.deliveries))
			copy(result, r.deliveries)
			return true
		}
		return false
	}, "webhook deliveries")
	return result
}

// WaitForChallenge blocks until at least 1 challenge is received.
// Fails the test if the timeout is reached.
func (r *WebhookReceiver) WaitForChallenge(t *testing.T, timeout time.Duration) ReceivedDelivery {
	t.Helper()
	var result ReceivedDelivery
	PollUntil(t, timeout, 500*time.Millisecond, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		if len(r.challenges) > 0 {
			result = r.challenges[0]
			return true
		}
		return false
	}, "webhook challenge")
	return result
}

// Deliveries returns a copy of all recorded non-challenge deliveries.
func (r *WebhookReceiver) Deliveries() []ReceivedDelivery {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]ReceivedDelivery, len(r.deliveries))
	copy(result, r.deliveries)
	return result
}

// DeliveryCount returns the total number of delivery attempts received
// (including those that received error responses).
func (r *WebhookReceiver) DeliveryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deliveryCount
}

// Reset clears all recorded deliveries, challenges, and the delivery count.
func (r *WebhookReceiver) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries = nil
	r.challenges = nil
	r.deliveryCount = 0
}

// Close shuts down the test server.
func (r *WebhookReceiver) Close() {
	r.Server.Close()
}

// SetChallengeMode changes the challenge response behavior at runtime.
func (r *WebhookReceiver) SetChallengeMode(mode ChallengeMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.challengeMode = mode
}

// SetCallbackMode changes the callback mode at runtime.
func (r *WebhookReceiver) SetCallbackMode(mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callbackMode = mode
}

// SetStatusCode changes the response status code at runtime.
func (r *WebhookReceiver) SetStatusCode(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCode = code
}

// SetFailCount changes the fail count at runtime.
// The receiver will return the error statusCode for the first n deliveries, then 200.
func (r *WebhookReceiver) SetFailCount(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failCount = n
}

// SetResponseDelay changes the response delay at runtime.
func (r *WebhookReceiver) SetResponseDelay(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.responseDelay = d
}

// PollUntil repeatedly calls check at the given interval until it returns true
// or the timeout expires. Fails the test with msg if the timeout is reached.
func PollUntil(t *testing.T, timeout, interval time.Duration, check func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(interval)
	}
	t.Fatalf("Timed out after %s waiting for: %s", timeout, msg)
}
