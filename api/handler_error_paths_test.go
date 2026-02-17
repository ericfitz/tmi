package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Tests for buildWebSocketURL ---

func TestBuildWebSocketURL(t *testing.T) {
	handler := &CellHandler{}

	t.Run("default_no_tls", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams/d-123/cells")
		// Don't set any TLS context — defaults should apply
		url := handler.buildWebSocketURL(c, "d-123")
		assert.Equal(t, "ws://", url[:5], "Should use ws:// without TLS")
		assert.Contains(t, url, "/ws/diagrams/d-123")
	})

	t.Run("tls_enabled_no_subject_name", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams/d-123/cells")
		c.Set("tlsEnabled", true)
		url := handler.buildWebSocketURL(c, "d-123")
		assert.True(t, strings.HasPrefix(url, "wss://"), "Should use wss:// with TLS enabled")
		assert.Contains(t, url, "/ws/diagrams/d-123")
	})

	t.Run("tls_enabled_with_subject_name_default_port", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams/d-123/cells")
		c.Set("tlsEnabled", true)
		c.Set("tlsSubjectName", "api.example.com")
		c.Set("serverPort", "443")
		url := handler.buildWebSocketURL(c, "d-123")
		assert.Equal(t, "wss://api.example.com/ws/diagrams/d-123", url,
			"Default HTTPS port (443) should NOT be appended to the host")
	})

	t.Run("tls_enabled_with_subject_name_custom_port", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams/d-123/cells")
		c.Set("tlsEnabled", true)
		c.Set("tlsSubjectName", "api.example.com")
		c.Set("serverPort", "8443")
		url := handler.buildWebSocketURL(c, "d-123")
		assert.Equal(t, "wss://api.example.com:8443/ws/diagrams/d-123", url,
			"Custom port should be appended to the host")
	})

	t.Run("subject_name_without_tls_ignored", func(t *testing.T) {
		// BUG DOCUMENTATION: If tlsSubjectName is set but tlsEnabled is false,
		// the subject name is ignored because the condition is `tlsSubjectName != "" && tlsEnabled`.
		// This is correct behavior — subject name only matters for TLS connections.
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams/d-123/cells")
		c.Set("tlsEnabled", false)
		c.Set("tlsSubjectName", "api.example.com")
		c.Set("serverPort", "8443")
		url := handler.buildWebSocketURL(c, "d-123")
		assert.True(t, strings.HasPrefix(url, "ws://"), "Should use ws:// when TLS is disabled")
		// Host comes from request, not subject name
		assert.NotContains(t, url, "api.example.com")
	})

	t.Run("wrong_type_in_context_uses_defaults", func(t *testing.T) {
		// If context values are the wrong type, they should be silently ignored
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams/d-123/cells")
		c.Set("tlsEnabled", "yes")     // string instead of bool
		c.Set("serverPort", 8080)      // int instead of string
		c.Set("tlsSubjectName", 12345) // int instead of string
		url := handler.buildWebSocketURL(c, "d-123")
		assert.True(t, strings.HasPrefix(url, "ws://"), "Wrong type for tlsEnabled should default to false")
		assert.Contains(t, url, "/ws/diagrams/d-123")
	})

	t.Run("empty_diagram_id", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/diagrams//cells")
		url := handler.buildWebSocketURL(c, "")
		assert.True(t, strings.HasSuffix(url, "/ws/diagrams/"),
			"Empty diagram ID produces trailing slash in URL path")
	})
}

// --- Tests for parseThreatModelFilters ---

func TestParseThreatModelFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("no_filters_returns_nil", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models")
		filters := parseThreatModelFilters(c)
		assert.Nil(t, filters, "No query params should return nil filters")
	})

	t.Run("owner_filter", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?owner=alice@example.com")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.Equal(t, "alice@example.com", *filters.Owner)
	})

	t.Run("name_filter", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?name=MyModel")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.Equal(t, "MyModel", *filters.Name)
	})

	t.Run("status_filter", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?status=active")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.Equal(t, "active", *filters.Status)
	})

	t.Run("valid_created_after_rfc3339", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?created_after=2025-01-01T00:00:00Z")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		require.NotNil(t, filters.CreatedAfter)
		assert.Equal(t, 2025, filters.CreatedAfter.Year())
	})

	t.Run("invalid_created_after_silently_ignored", func(t *testing.T) {
		// BUG DOCUMENTATION: Invalid timestamp formats are silently ignored — no 400 error.
		// The function just skips the filter. This means a client could send
		// "created_after=yesterday" and get unfiltered results without knowing
		// their filter was silently dropped.
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?created_after=not-a-date")
		filters := parseThreatModelFilters(c)
		assert.Nil(t, filters, "Invalid date should not set a filter, resulting in nil (no valid filters)")
	})

	t.Run("invalid_modified_before_silently_ignored", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?modified_before=2025/01/01")
		filters := parseThreatModelFilters(c)
		assert.Nil(t, filters, "Non-RFC3339 date format should be silently ignored")
	})

	t.Run("multiple_filters_combined", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet,
			"/threat-models?owner=alice&status=active&name=MyModel")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.Equal(t, "alice", *filters.Owner)
		assert.Equal(t, "active", *filters.Status)
		assert.Equal(t, "MyModel", *filters.Name)
	})

	t.Run("all_timestamp_filters", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet,
			"/threat-models?created_after=2025-01-01T00:00:00Z&created_before=2025-12-31T23:59:59Z&modified_after=2025-06-01T00:00:00Z&modified_before=2025-06-30T23:59:59Z&status_updated_after=2025-03-01T00:00:00Z&status_updated_before=2025-09-30T23:59:59Z")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.NotNil(t, filters.CreatedAfter)
		assert.NotNil(t, filters.CreatedBefore)
		assert.NotNil(t, filters.ModifiedAfter)
		assert.NotNil(t, filters.ModifiedBefore)
		assert.NotNil(t, filters.StatusUpdatedAfter)
		assert.NotNil(t, filters.StatusUpdatedBefore)
	})

	t.Run("description_filter", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?description=important")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.Equal(t, "important", *filters.Description)
	})

	t.Run("issue_uri_filter", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?issue_uri=https://github.com/org/repo/issues/42")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters)
		assert.Equal(t, "https://github.com/org/repo/issues/42", *filters.IssueUri)
	})

	t.Run("unknown_query_param_ignored", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/threat-models?unknown_param=value")
		filters := parseThreatModelFilters(c)
		assert.Nil(t, filters, "Unknown params should not create filters")
	})

	t.Run("mixed_valid_and_invalid_timestamps", func(t *testing.T) {
		// Valid created_after, invalid created_before
		c, _ := CreateTestGinContext(http.MethodGet,
			"/threat-models?created_after=2025-01-01T00:00:00Z&created_before=invalid")
		filters := parseThreatModelFilters(c)
		require.NotNil(t, filters, "At least one valid filter should return non-nil")
		assert.NotNil(t, filters.CreatedAfter)
		assert.Nil(t, filters.CreatedBefore, "Invalid timestamp should be nil, not error")
	})
}

// --- Tests for sanitizeErrorMessage ---

func TestSanitizeErrorMessage(t *testing.T) {
	t.Run("clean_string_unchanged", func(t *testing.T) {
		result := sanitizeErrorMessage("Normal error message")
		assert.Equal(t, "Normal error message", result)
	})

	t.Run("newlines_replaced_with_spaces", func(t *testing.T) {
		result := sanitizeErrorMessage("line1\nline2\nline3")
		assert.Equal(t, "line1 line2 line3", result)
	})

	t.Run("carriage_return_replaced_with_space", func(t *testing.T) {
		result := sanitizeErrorMessage("line1\r\nline2")
		assert.Equal(t, "line1  line2", result, "Both \\r and \\n should be replaced with spaces")
	})

	t.Run("tabs_replaced_with_spaces", func(t *testing.T) {
		result := sanitizeErrorMessage("field1\tfield2")
		assert.Equal(t, "field1 field2", result)
	})

	t.Run("null_bytes_removed", func(t *testing.T) {
		result := sanitizeErrorMessage("hello\x00world")
		assert.Equal(t, "helloworld", result, "Null bytes should be removed entirely")
	})

	t.Run("other_control_chars_removed", func(t *testing.T) {
		// Bell, backspace, form feed, vertical tab
		result := sanitizeErrorMessage("a\x07b\x08c\x0cd\x0be")
		assert.Equal(t, "abcde", result)
	})

	t.Run("empty_string", func(t *testing.T) {
		result := sanitizeErrorMessage("")
		assert.Equal(t, "", result)
	})

	t.Run("unicode_preserved", func(t *testing.T) {
		result := sanitizeErrorMessage("Error: données invalides 数据错误")
		assert.Equal(t, "Error: données invalides 数据错误", result)
	})
}

// --- Tests for truncateBeforeStackTrace ---

func TestTruncateBeforeStackTrace(t *testing.T) {
	t.Run("empty_string_returns_unknown", func(t *testing.T) {
		result := truncateBeforeStackTrace("")
		assert.Equal(t, "Unknown error", result)
	})

	t.Run("no_markers_returns_original", func(t *testing.T) {
		msg := "Simple error without stack trace"
		result := truncateBeforeStackTrace(msg)
		assert.Equal(t, msg, result)
	})

	t.Run("truncates_at_STACK_TRACE_START", func(t *testing.T) {
		msg := "panic: runtime error--- STACK_TRACE_START ---\ngoroutine 1 [running]:\nmain.go:42"
		result := truncateBeforeStackTrace(msg)
		assert.Equal(t, "panic: runtime error", result)
	})

	t.Run("truncates_at_Stack_trace_newline", func(t *testing.T) {
		msg := "something failed\nStack trace:\n  at main.go:42\n  at caller.go:10"
		result := truncateBeforeStackTrace(msg)
		assert.Equal(t, "something failed", result)
	})

	t.Run("truncates_at_goroutine_marker", func(t *testing.T) {
		msg := "unexpected error goroutine 1 [running]:\nmain.main()\n\t/app/main.go:42"
		result := truncateBeforeStackTrace(msg)
		assert.Equal(t, "unexpected error", result)
	})

	t.Run("first_marker_wins", func(t *testing.T) {
		// Multiple markers — the first one encountered in the markers list should win
		msg := "error--- STACK_TRACE_START ---more\nStack trace:\ngoroutine"
		result := truncateBeforeStackTrace(msg)
		assert.Equal(t, "error", result)
	})

	t.Run("marker_at_start_returns_empty_trimmed", func(t *testing.T) {
		msg := "--- STACK_TRACE_START ---\ngoroutine 1"
		result := truncateBeforeStackTrace(msg)
		assert.Equal(t, "", result, "Marker at start should produce empty string after TrimSpace")
	})
}

// --- Tests for HandleRequestError extended paths ---

func TestHandleRequestError_Extended(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("unauthorized_sets_www_authenticate_header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "invalid_token",
			Message: "Token expired",
		}
		HandleRequestError(c, err)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		wwwAuth := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, wwwAuth, `Bearer realm="tmi"`)
		assert.Contains(t, wwwAuth, `error="invalid_token"`)
		assert.Contains(t, wwwAuth, "Token expired")
	})

	t.Run("429_with_retry_after_header", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := &RequestError{
			Status:  http.StatusTooManyRequests,
			Code:    "rate_limited",
			Message: "Too many requests",
			Details: &ErrorDetails{
				Context: map[string]interface{}{
					"retry_after": 30,
				},
			},
		}
		HandleRequestError(c, err)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Equal(t, "30", w.Header().Get("Retry-After"))
	})

	t.Run("429_without_retry_after_in_context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := &RequestError{
			Status:  http.StatusTooManyRequests,
			Code:    "rate_limited",
			Message: "Too many requests",
			Details: &ErrorDetails{
				Context: map[string]interface{}{
					"bucket": "api",
				},
			},
		}
		HandleRequestError(c, err)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Empty(t, w.Header().Get("Retry-After"),
			"Retry-After should not be set when retry_after is not in context")
	})

	t.Run("429_retry_after_wrong_type_ignored", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := &RequestError{
			Status:  http.StatusTooManyRequests,
			Code:    "rate_limited",
			Message: "Too many requests",
			Details: &ErrorDetails{
				Context: map[string]interface{}{
					"retry_after": "30s", // string instead of int
				},
			},
		}
		HandleRequestError(c, err)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Empty(t, w.Header().Get("Retry-After"),
			"Retry-After should not be set when retry_after is wrong type")
	})

	t.Run("message_truncated_to_1000_chars", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		longMessage := strings.Repeat("x", 1500)
		err := &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: longMessage,
		}
		HandleRequestError(c, err)

		var response Error
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Len(t, response.ErrorDescription, 1000,
			"Message should be truncated to exactly 1000 characters (997 + '...')")
		assert.True(t, strings.HasSuffix(response.ErrorDescription, "..."),
			"Truncated message should end with '...'")
	})

	t.Run("message_exactly_1000_not_truncated", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		exactMessage := strings.Repeat("y", 1000)
		err := &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: exactMessage,
		}
		HandleRequestError(c, err)

		var response Error
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, exactMessage, response.ErrorDescription,
			"Message at exactly 1000 chars should not be truncated")
	})

	t.Run("control_chars_sanitized_in_request_error", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "field\x00has\nnull\tbytes",
		}
		HandleRequestError(c, err)

		var response Error
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.NotContains(t, response.ErrorDescription, "\x00")
		assert.NotContains(t, response.ErrorDescription, "\n")
		assert.Contains(t, response.ErrorDescription, "field")
		assert.Contains(t, response.ErrorDescription, "has")
	})

	t.Run("generic_error_with_stack_trace_truncated", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := errors.New("database connection failed goroutine 1 [running]:\nmain.main()")
		HandleRequestError(c, err)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		var response Error
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "server_error", response.Error)
		assert.NotContains(t, response.ErrorDescription, "goroutine",
			"Stack trace should be truncated from generic error messages")
		assert.Contains(t, response.ErrorDescription, "database connection failed")
	})

	t.Run("request_error_with_details", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		code := "VALIDATION_001"
		suggestion := "Check the field format"
		err := &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Validation failed",
			Details: &ErrorDetails{
				Code:       &code,
				Suggestion: &suggestion,
				Context: map[string]interface{}{
					"field": "name",
				},
			},
		}
		HandleRequestError(c, err)

		var response Error
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotNil(t, response.Details)
		assert.Equal(t, &code, response.Details.Code)
		assert.Equal(t, &suggestion, response.Details.Suggestion)
		require.NotNil(t, response.Details.Context)
		ctx := *response.Details.Context
		assert.Equal(t, "name", ctx["field"])
	})

	t.Run("request_error_with_empty_details_context", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)

		err := &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Error",
			Details: &ErrorDetails{
				Context: map[string]interface{}{}, // empty, not nil
			},
		}
		HandleRequestError(c, err)

		var response Error
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.NotNil(t, response.Details)
		assert.Nil(t, response.Details.Context,
			"Empty details context should be serialized as null, not empty object")
	})
}

// --- Tests for SetWWWAuthenticateHeader ---

func TestSetWWWAuthenticateHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("basic_challenge_no_error", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		SetWWWAuthenticateHeader(c, "", "")
		header := w.Header().Get("WWW-Authenticate")
		assert.Equal(t, `Bearer realm="tmi"`, header)
	})

	t.Run("invalid_token_error", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Token expired")
		header := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, header, `Bearer realm="tmi"`)
		assert.Contains(t, header, `error="invalid_token"`)
		assert.Contains(t, header, `error_description="Token expired"`)
	})

	t.Run("insufficient_scope_error", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		SetWWWAuthenticateHeader(c, WWWAuthInsufficientScope, "Admin required")
		header := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, header, `error="insufficient_scope"`)
		assert.Contains(t, header, `error_description="Admin required"`)
	})

	t.Run("error_type_with_empty_description", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		SetWWWAuthenticateHeader(c, WWWAuthInvalidRequest, "")
		header := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, header, `error="invalid_request"`)
		assert.NotContains(t, header, "error_description",
			"Empty description should not produce error_description")
	})

	t.Run("description_with_quotes_escaped", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, `Token "test" is invalid`)
		header := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, header, `error_description="Token \"test\" is invalid"`,
			"Quotes in description should be escaped per RFC 6750")
	})
}

// --- Tests for error constructor helpers ---

func TestErrorConstructors(t *testing.T) {
	t.Run("InvalidInputError", func(t *testing.T) {
		err := InvalidInputError("bad field")
		assert.Equal(t, http.StatusBadRequest, err.Status)
		assert.Equal(t, "invalid_input", err.Code)
		assert.Equal(t, "bad field", err.Message)
	})

	t.Run("InvalidIDError", func(t *testing.T) {
		err := InvalidIDError("not a UUID")
		assert.Equal(t, http.StatusBadRequest, err.Status)
		assert.Equal(t, "invalid_id", err.Code)
		assert.Equal(t, "not a UUID", err.Message)
	})

	t.Run("RequestError_implements_error", func(t *testing.T) {
		var err error = &RequestError{
			Status:  http.StatusNotFound,
			Code:    "not_found",
			Message: "Resource not found",
		}
		assert.Equal(t, "Resource not found", err.Error())
	})
}

// --- Tests for getFieldErrorMessage ---

func TestGetFieldErrorMessage(t *testing.T) {
	tests := []struct {
		field    string
		contains string
	}{
		{"id", "read-only"},
		{"created_at", "read-only"},
		{"modified_at", "managed automatically"},
		{"created_by", "read-only"},
		{"owner", "authenticated user"},
		{"diagrams", "sub-entity endpoints"},
		{"documents", "sub-entity endpoints"},
		{"threats", "sub-entity endpoints"},
		{"sourceCode", "sub-entity endpoints"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			msg := getFieldErrorMessage(tt.field)
			assert.NotEmpty(t, msg)
			assert.Contains(t, strings.ToLower(msg), strings.ToLower(tt.contains))
		})
	}

	t.Run("unknown_field_gets_default_message", func(t *testing.T) {
		msg := getFieldErrorMessage("nonexistent_field")
		assert.NotEmpty(t, msg, "Unknown fields should get a default message")
		assert.Contains(t, msg, "not allowed")
	})
}

// --- Tests for NewCellHandler constructors ---

func TestNewCellHandler(t *testing.T) {
	t.Run("with_nil_dependencies", func(t *testing.T) {
		handler := NewCellHandler(nil, nil, nil, nil)
		assert.NotNil(t, handler)
		assert.Nil(t, handler.metadataStore)
		assert.Nil(t, handler.db)
		assert.Nil(t, handler.cache)
		assert.Nil(t, handler.cacheInvalidator)
	})

	t.Run("simple_constructor", func(t *testing.T) {
		handler := NewCellHandlerSimple()
		assert.NotNil(t, handler)
	})
}

// --- Tests for ValidateAuthenticatedUser ---

func TestValidateAuthenticatedUser_RolePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("both_email_and_provider_id", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")

		email, providerID, _, err := ValidateAuthenticatedUser(c)
		assert.NoError(t, err)
		assert.Equal(t, "alice@example.com", email)
		assert.Equal(t, "alice-provider-id", providerID)
	})

	t.Run("with_role", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Set("userRole", Role("owner"))

		email, providerID, role, err := ValidateAuthenticatedUser(c)
		assert.NoError(t, err)
		assert.Equal(t, "alice@example.com", email)
		assert.Equal(t, "alice-provider-id", providerID)
		assert.Equal(t, Role("owner"), role)
	})

	t.Run("no_role_returns_empty_role", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		// Don't set userRole

		_, _, role, err := ValidateAuthenticatedUser(c)
		assert.NoError(t, err)
		assert.Equal(t, Role(""), role, "Missing role should return empty Role, not error")
	})

	t.Run("wrong_role_type_returns_error", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userEmail", "alice@example.com")
		c.Set("userID", "alice-provider-id")
		c.Set("userRole", "owner") // string, not Role type

		_, _, _, err := ValidateAuthenticatedUser(c)
		assert.Error(t, err, "Wrong type for userRole should return error")
		var reqErr *RequestError
		if errors.As(err, &reqErr) {
			assert.Equal(t, http.StatusInternalServerError, reqErr.Status)
		}
	})

	t.Run("missing_email", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userID", "alice-provider-id")

		_, _, _, err := ValidateAuthenticatedUser(c)
		assert.Error(t, err)
	})

	t.Run("missing_provider_id", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userEmail", "alice@example.com")

		_, _, _, err := ValidateAuthenticatedUser(c)
		assert.Error(t, err)
	})

	t.Run("neither_set", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")

		_, _, _, err := ValidateAuthenticatedUser(c)
		assert.Error(t, err)
	})

	t.Run("empty_strings_are_missing", func(t *testing.T) {
		c, _ := CreateTestGinContext(http.MethodGet, "/")
		c.Set("userEmail", "")
		c.Set("userID", "")

		_, _, _, err := ValidateAuthenticatedUser(c)
		assert.Error(t, err)
	})
}

// --- Tests for ProtectedGroupEveryone constant ---

func TestProtectedGroupConstant(t *testing.T) {
	assert.Equal(t, "everyone", ProtectedGroupEveryone,
		"Protected group constant should be 'everyone'")
}

// --- Tests for WWWAuthenticateError constants ---

func TestWWWAuthenticateErrorConstants(t *testing.T) {
	// Verify constants match RFC 6750 section 3.1
	assert.Equal(t, WWWAuthenticateError("invalid_request"), WWWAuthInvalidRequest)
	assert.Equal(t, WWWAuthenticateError("invalid_token"), WWWAuthInvalidToken)
	assert.Equal(t, WWWAuthenticateError("insufficient_scope"), WWWAuthInsufficientScope)
}

// --- Test for HandleRequestError with concurrent safe usage ---

func TestHandleRequestError_InternalServerError_Format(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)

	// Non-RequestError error should produce a sanitized 500 response
	HandleRequestError(c, fmt.Errorf("some internal\x00problem\nwith newlines"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var response Error
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, "server_error", response.Error)
	// Control characters should be sanitized
	assert.NotContains(t, response.ErrorDescription, "\x00")
	assert.NotContains(t, response.ErrorDescription, "\n")
	// Should be prefixed with "Internal server error: "
	assert.True(t, strings.HasPrefix(response.ErrorDescription, "Internal server error:"))
}
