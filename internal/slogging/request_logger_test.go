package slogging

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Credential and unrecognized header values must never reach the
// request/response debug log, even with redact_auth_tokens off (#1158,
// CodeQL alert 1308).

const testCredential = "s3cr3t-credential-value-abcdefghijklmnopqrstuvwxyz"

func captureContextLogger(buf *bytes.Buffer) *ContextLogger {
	return &ContextLogger{
		logger:  &Logger{level: LogLevelDebug},
		slogger: slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
		ctx:     context.Background(),
	}
}

func TestLogRequestDetails_NeverLogsCredentialHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scrub := range []bool{false, true} {
		var buf bytes.Buffer
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/threat_models", nil)
		c.Request.Header.Set("Authorization", "Bearer "+testCredential)
		c.Request.Header.Set("Proxy-Authorization", "Basic "+testCredential)
		c.Request.Header.Set("Cookie", "session="+testCredential)
		c.Request.Header.Set("X-Api-Key", testCredential)
		c.Request.Header.Set("X-Webhook-Signature", testCredential) // unknown header: default-deny
		c.Request.Header.Set("X-Request-Id", "req-123")

		logRequestDetails(c, captureContextLogger(&buf), RequestResponseLoggingConfig{RedactTokens: scrub})

		out := buf.String()
		if strings.Contains(out, testCredential) {
			t.Fatalf("scrub=%v: credential header value logged: %s", scrub, out)
		}
		if !strings.Contains(out, redactedPlaceholder) {
			t.Fatalf("scrub=%v: expected %s placeholder in %s", scrub, redactedPlaceholder, out)
		}
		if !strings.Contains(out, "req-123") {
			t.Fatalf("scrub=%v: ordinary header missing from %s", scrub, out)
		}
	}
}

func TestLogResponseDetails_NeverLogsSetCookieCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scrub := range []bool{false, true} {
		var buf bytes.Buffer
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/oauth2/callback", nil)
		c.Writer.Header().Set("Set-Cookie", "session="+testCredential)
		c.Writer.Header().Set("Content-Type", "application/json")

		logResponseDetails(c, captureContextLogger(&buf), RequestResponseLoggingConfig{RedactTokens: scrub}, nil, time.Millisecond)

		out := buf.String()
		if strings.Contains(out, testCredential) {
			t.Fatalf("scrub=%v: Set-Cookie value logged: %s", scrub, out)
		}
		if !strings.Contains(out, "application/json") {
			t.Fatalf("scrub=%v: ordinary header missing from %s", scrub, out)
		}
	}
}

func TestIsCredentialHeader(t *testing.T) {
	for _, name := range []string{"Authorization", "authorization", "PROXY-AUTHORIZATION", "Cookie", "Set-Cookie", "X-Auth-Token", "x-api-key"} {
		if !IsCredentialHeader(name) {
			t.Errorf("%q should be a credential header", name)
		}
	}
	for _, name := range []string{"Content-Type", "X-Request-Id", "User-Agent", "", "Authorization-Hint"} {
		if IsCredentialHeader(name) {
			t.Errorf("%q should not be a credential header", name)
		}
	}
}

func TestValueLoggableHeaders_ExcludesCredentials(t *testing.T) {
	for name := range valueLoggableHeaders {
		if IsCredentialHeader(name) {
			t.Errorf("credential header %q is on the value-loggable allowlist", name)
		}
		if name != strings.ToLower(name) {
			t.Errorf("allowlist entry %q must be lowercase", name)
		}
	}
}

func TestRedactHeaders_UsesSharedCredentialList(t *testing.T) {
	got := RedactHeaders(map[string][]string{"Proxy-Authorization": {"Basic " + testCredential}})
	if strings.Contains(got["Proxy-Authorization"][0], testCredential) {
		t.Fatalf("Proxy-Authorization not redacted: %v", got)
	}
}
