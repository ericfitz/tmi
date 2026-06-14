package auth

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestReadCappedBodyUnderCap(t *testing.T) {
	body, truncated, err := readCappedBody(strings.NewReader("hello"), 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated=false for input under the cap")
	}
	if string(body) != "hello" {
		t.Fatalf("got %q, want %q", body, "hello")
	}
}

func TestReadCappedBodyAtCap(t *testing.T) {
	in := strings.Repeat("a", 16)
	body, truncated, err := readCappedBody(strings.NewReader(in), 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated=false for input exactly at the cap")
	}
	if string(body) != in {
		t.Fatalf("got %d bytes, want %d", len(body), len(in))
	}
}

func TestReadCappedBodyOverCap(t *testing.T) {
	in := strings.Repeat("a", 100)
	body, truncated, err := readCappedBody(strings.NewReader(in), 16)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated=true for oversize input")
	}
	if len(body) != 16 {
		t.Fatalf("got %d bytes, want 16", len(body))
	}
}

func TestLogBodyStringMarksTruncation(t *testing.T) {
	if got := logBodyString([]byte("abc"), false); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := logBodyString([]byte("abc"), true); got != "abc...(truncated)" {
		t.Fatalf("got %q", got)
	}
}

func TestCappedJSONDecodeFailsClosedOnOversizeBody(t *testing.T) {
	// A JSON document larger than the cap must surface as a decode error
	// (the existing error path in customTokenExchange/fetchEndpoint), never
	// as a silently-accepted partial value.
	huge := `{"access_token":"` + strings.Repeat("a", 64) + `"}`
	limit := int64(16) // smaller than the document
	var out map[string]any
	err := json.NewDecoder(io.LimitReader(strings.NewReader(huge), limit)).Decode(&out)
	if err == nil {
		t.Fatal("expected decode error for body truncated at the cap")
	}
}
