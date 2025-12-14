package framework

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// AssertStatusCode asserts that response has expected status code
func AssertStatusCode(t *testing.T, resp *Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Errorf("Expected status code %d, got %d\nResponse body: %s",
			expected, resp.StatusCode, string(resp.Body))
	}
}

// AssertStatusOK asserts status is 200 OK
func AssertStatusOK(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 200)
}

// AssertStatusCreated asserts status is 201 Created
func AssertStatusCreated(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 201)
}

// AssertStatusNoContent asserts status is 204 No Content
func AssertStatusNoContent(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 204)
}

// AssertStatusBadRequest asserts status is 400 Bad Request
func AssertStatusBadRequest(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 400)
}

// AssertStatusUnauthorized asserts status is 401 Unauthorized
func AssertStatusUnauthorized(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 401)
}

// AssertStatusForbidden asserts status is 403 Forbidden
func AssertStatusForbidden(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 403)
}

// AssertStatusNotFound asserts status is 404 Not Found
func AssertStatusNotFound(t *testing.T, resp *Response) {
	t.Helper()
	AssertStatusCode(t, resp, 404)
}

// AssertJSONField asserts that a JSON field exists and has expected value
func AssertJSONField(t *testing.T, resp *Response, field string, expected interface{}) {
	t.Helper()

	var data map[string]interface{}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	actual, ok := data[field]
	if !ok {
		t.Errorf("Field '%s' not found in response: %s", field, string(resp.Body))
		return
	}

	if actual != expected {
		t.Errorf("Field '%s' expected '%v', got '%v'", field, expected, actual)
	}
}

// AssertJSONFieldExists asserts that a JSON field exists (any value)
func AssertJSONFieldExists(t *testing.T, resp *Response, field string) interface{} {
	t.Helper()

	var data map[string]interface{}
	if err := json.Unmarshal(resp.Body, &data); err != nil {
		t.Fatalf("Failed to parse JSON response: %v", err)
	}

	val, ok := data[field]
	if !ok {
		t.Fatalf("Field '%s' not found in response: %s", field, string(resp.Body))
	}

	return val
}

// AssertJSONFieldNotEmpty asserts that a JSON field exists and is not empty
func AssertJSONFieldNotEmpty(t *testing.T, resp *Response, field string) {
	t.Helper()

	val := AssertJSONFieldExists(t, resp, field)

	switch v := val.(type) {
	case string:
		if v == "" {
			t.Errorf("Field '%s' is empty string", field)
		}
	case nil:
		t.Errorf("Field '%s' is null", field)
	}
}

// AssertValidUUID asserts that a JSON field contains a valid UUID
func AssertValidUUID(t *testing.T, resp *Response, field string) string {
	t.Helper()

	val := AssertJSONFieldExists(t, resp, field)
	uuidStr, ok := val.(string)
	if !ok {
		t.Fatalf("Field '%s' is not a string: %T", field, val)
	}

	if _, err := uuid.Parse(uuidStr); err != nil {
		t.Errorf("Field '%s' is not a valid UUID: %s (error: %v)", field, uuidStr, err)
	}

	return uuidStr
}

// AssertValidTimestamp asserts that a JSON field contains a valid RFC3339 timestamp
func AssertValidTimestamp(t *testing.T, resp *Response, field string) time.Time {
	t.Helper()

	val := AssertJSONFieldExists(t, resp, field)
	tsStr, ok := val.(string)
	if !ok {
		t.Fatalf("Field '%s' is not a string: %T", field, val)
	}

	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		t.Errorf("Field '%s' is not a valid RFC3339 timestamp: %s (error: %v)", field, tsStr, err)
	}

	return ts
}

// AssertTimestampOrder asserts that timestamp1 < timestamp2
func AssertTimestampOrder(t *testing.T, ts1, ts2 time.Time, ts1Name, ts2Name string) {
	t.Helper()

	if !ts1.Before(ts2) && !ts1.Equal(ts2) {
		t.Errorf("Timestamp order violation: %s (%v) should be before or equal to %s (%v)",
			ts1Name, ts1, ts2Name, ts2)
	}
}

// AssertArrayLength asserts that a JSON array has expected length
func AssertArrayLength(t *testing.T, resp *Response, field string, expectedLen int) {
	t.Helper()

	val := AssertJSONFieldExists(t, resp, field)
	arr, ok := val.([]interface{})
	if !ok {
		t.Fatalf("Field '%s' is not an array: %T", field, val)
	}

	if len(arr) != expectedLen {
		t.Errorf("Array '%s' expected length %d, got %d", field, expectedLen, len(arr))
	}
}

// AssertArrayNotEmpty asserts that a JSON array is not empty
func AssertArrayNotEmpty(t *testing.T, resp *Response, field string) {
	t.Helper()

	val := AssertJSONFieldExists(t, resp, field)
	arr, ok := val.([]interface{})
	if !ok {
		t.Fatalf("Field '%s' is not an array: %T", field, val)
	}

	if len(arr) == 0 {
		t.Errorf("Array '%s' is empty", field)
	}
}

// AssertHeaderExists asserts that a response header exists
func AssertHeaderExists(t *testing.T, resp *Response, header string) string {
	t.Helper()

	val := resp.Headers.Get(header)
	if val == "" {
		t.Errorf("Header '%s' not found in response", header)
	}

	return val
}

// AssertContentType asserts that Content-Type header matches expected
func AssertContentType(t *testing.T, resp *Response, expected string) {
	t.Helper()

	actual := resp.Headers.Get("Content-Type")
	// Content-Type may have charset suffix
	if !strings.HasPrefix(actual, expected) {
		t.Errorf("Expected Content-Type '%s', got '%s'", expected, actual)
	}
}

// AssertLocationHeader asserts that Location header exists and returns its value
func AssertLocationHeader(t *testing.T, resp *Response) string {
	t.Helper()
	return AssertHeaderExists(t, resp, "Location")
}

// AssertError asserts that a response contains an error with expected message
func AssertError(t *testing.T, resp *Response, expectedMessage string) {
	t.Helper()

	var errorResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Details string `json:"details"`
	}

	if err := json.Unmarshal(resp.Body, &errorResp); err != nil {
		t.Fatalf("Failed to parse error response: %v\nBody: %s", err, string(resp.Body))
	}

	// Check if any error field contains the expected message
	combined := strings.ToLower(errorResp.Error + errorResp.Message + errorResp.Details)
	if !strings.Contains(combined, strings.ToLower(expectedMessage)) {
		t.Errorf("Expected error message to contain '%s', got: %s",
			expectedMessage, string(resp.Body))
	}
}

// AssertNoError is a helper to fail if error is not nil
func AssertNoError(t *testing.T, err error, context string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", context, err)
	}
}

// AssertStringContains asserts that a string contains a substring
func AssertStringContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("Expected string to contain '%s', got: %s", needle, haystack)
	}
}

// AssertEqual asserts that two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}, context string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", context, expected, actual)
	}
}

// AssertNotEqual asserts that two values are not equal
func AssertNotEqual(t *testing.T, unexpected, actual interface{}, context string) {
	t.Helper()
	if unexpected == actual {
		t.Errorf("%s: expected values to differ, both are %v", context, actual)
	}
}

// AssertTrue asserts that a condition is true
func AssertTrue(t *testing.T, condition bool, message string) {
	t.Helper()
	if !condition {
		t.Errorf("Assertion failed: %s", message)
	}
}

// AssertFalse asserts that a condition is false
func AssertFalse(t *testing.T, condition bool, message string) {
	t.Helper()
	if condition {
		t.Errorf("Assertion failed: %s", message)
	}
}

// ExtractID extracts a UUID from response body (common pattern after create operations)
func ExtractID(t *testing.T, resp *Response, field string) string {
	t.Helper()
	return AssertValidUUID(t, resp, field)
}

// PrettyPrintJSON pretty-prints JSON for debugging
func PrettyPrintJSON(t *testing.T, data []byte) {
	t.Helper()
	var pretty interface{}
	if err := json.Unmarshal(data, &pretty); err != nil {
		t.Logf("Raw data: %s", string(data))
		return
	}
	formatted, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		t.Logf("Raw data: %s", string(data))
		return
	}
	t.Logf("Response:\n%s", string(formatted))
}

// FailNow is a helper that combines error message and fails immediately
func FailNow(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Fatalf(format, args...)
}

// Errorf is a helper that combines error message but continues
func Errorf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Errorf(format, args...)
}
