package api

import (
	"bufio"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Reproduces the production failure behind the tmi-ux report of
// net::ERR_HTTP2_PROTOCOL_ERROR on POST .../chat/sessions/{id}/messages.
//
// http.Server.WriteTimeout bounds the whole response write, and Go arms that
// deadline when the request header is read — so any SSE stream that outlives
// it has its remaining writes fail with i/o timeout. Measured against AWS: the
// last byte reached the browser at 10.055s against a 10s WriteTimeout, the
// handler kept generating tokens into a dead socket for another ~15s, and the
// abrupt close at handler return is what the browser reported. The server
// logged 200 throughout, because the handler itself completed fine.
//
// A streaming response must therefore clear the write deadline. The test
// streams past a deliberately short WriteTimeout and asserts the client
// receives every event including the terminator.

func sseTestEngine(t *testing.T, events int, gap time.Duration) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// JSONErrorHandler wraps the writer in bufferedResponseWriter, which is
	// part of what the deadline clear has to see through — without an Unwrap
	// chain, http.NewResponseController cannot reach the connection. Including
	// it here keeps the test faithful to the real middleware stack.
	r.Use(JSONErrorHandler())
	r.POST("/stream", func(c *gin.Context) {
		sse := NewSSEWriter(c)
		for i := 0; i < events; i++ {
			_ = sse.SendEvent("token", map[string]any{"content": "x"})
			time.Sleep(gap)
		}
		_ = sse.SendEvent("message_end", map[string]any{"done": true})
	})
	return r
}

func TestSSE_SurvivesWriteTimeout(t *testing.T) {
	const (
		writeTimeout = 300 * time.Millisecond
		events       = 12
		gap          = 60 * time.Millisecond // total ~720ms > writeTimeout
	)

	srv := httptest.NewUnstartedServer(sseTestEngine(t, events, gap))
	srv.Config.WriteTimeout = writeTimeout
	srv.Start()
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	var tokens int
	var sawEnd bool
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		switch sc.Text() {
		case "event: token":
			tokens++
		case "event: message_end":
			sawEnd = true
		}
	}
	// A truncated stream surfaces here as an unexpected EOF / reset rather than
	// a clean end, so report it precisely instead of only via the counts.
	if err := sc.Err(); err != nil {
		t.Fatalf("stream broke mid-flight after %d/%d tokens (sawEnd=%v): %v — "+
			"the write deadline was not cleared", tokens, events, sawEnd, err)
	}
	if tokens != events {
		t.Errorf("received %d token events, want %d — stream truncated at the write deadline", tokens, events)
	}
	if !sawEnd {
		t.Error("never received message_end — the tail of the stream was lost, " +
			"which is exactly what the browser saw as ERR_HTTP2_PROTOCOL_ERROR")
	}
}

// NewSSEWriter must not weaken the Cache-Control that SecurityHeaders set. It
// used to overwrite "no-store, no-cache, must-revalidate" with a bare
// "no-cache", dropping no-store — the sole finding CATS' CheckSecurityHeaders
// flagged on POST /threat_models/{id}/chat/sessions. This pins that the SSE
// path keeps the middleware's stronger value.
func TestSSE_PreservesStrongCacheControl(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())
	r.POST("/stream", func(c *gin.Context) {
		sse := NewSSEWriter(c)
		_ = sse.SendEvent("token", map[string]any{"content": "x"})
	})

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/stream", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	cc := resp.Header.Get("Cache-Control")
	if !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want it to contain no-store (SSE writer clobbered the middleware value)", cc)
	}
	if !strings.Contains(cc, "no-cache") {
		t.Errorf("Cache-Control = %q, want it to contain no-cache", cc)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

// The deadline clear has to traverse bufferedResponseWriter. gin.ResponseWriter
// is an interface that does not declare Unwrap, so embedding it promotes no
// such method and http.NewResponseController stops at the wrapper and returns
// ErrNotSupported. This pins the Unwrap that makes the chain traversable.
func TestBufferedResponseWriter_UnwrapReachesUnderlyingWriter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	blw := &bufferedResponseWriter{ResponseWriter: c.Writer, body: newBuf(), statusCode: http.StatusOK}

	u, ok := any(blw).(interface{ Unwrap() http.ResponseWriter })
	if !ok {
		t.Fatal("bufferedResponseWriter does not implement Unwrap; " +
			"http.NewResponseController cannot reach the connection through it")
	}
	if u.Unwrap() == nil {
		t.Fatal("Unwrap returned nil")
	}
}

// newBuf mirrors how JSONErrorHandler constructs the buffer.
func newBuf() *bytes.Buffer { return bytes.NewBufferString("") }

// failingWriter fails every write, standing in for a socket whose deadline has
// expired: writes error but the request context is NOT cancelled, which is the
// condition the old IsClientGone could not see.
type failingWriter struct {
	gin.ResponseWriter
	writes int
}

func (f *failingWriter) Write(b []byte) (int, error) {
	f.writes++
	return 0, errors.New("i/o timeout")
}

func (f *failingWriter) WriteString(s string) (int, error) {
	f.writes++
	return 0, errors.New("i/o timeout")
}

func TestSSEWriter_LatchesWriteFailureAndReportsClientGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil)
	fw := &failingWriter{ResponseWriter: c.Writer}
	c.Writer = fw

	w := &SSEWriter{c: c, flusher: func() {}}

	if w.IsClientGone() {
		t.Fatal("client should look reachable before any write")
	}
	if err := w.SendEvent("token", map[string]string{"content": "a"}); err == nil {
		t.Fatal("SendEvent should surface the write failure")
	}
	if !w.IsClientGone() {
		t.Fatal("after a failed write the client is unreachable — without this the " +
			"handler keeps generating LLM tokens for a dead socket")
	}
	if w.Err() == nil {
		t.Fatal("Err() should expose the latched failure")
	}

	// Subsequent sends must short-circuit rather than retry the socket.
	before := fw.writes
	for i := 0; i < 5; i++ {
		if err := w.SendToken("x"); err == nil {
			t.Fatal("send after a latched failure should keep returning the error")
		}
	}
	if fw.writes != before {
		t.Errorf("made %d further write syscalls after latching; want 0", fw.writes-before)
	}
}
