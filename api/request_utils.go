package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/api/validation"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/errcode"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/wwwauth"
	"github.com/gin-gonic/gin"
)

// Protected group names that cannot be deleted
const (
	ProtectedGroupEveryone = "everyone"
)

// WWWAuthenticateRealm identifies the protection space for Bearer token authentication.
// This is a static value for TMI's API - all protected endpoints share the same realm.
const WWWAuthenticateRealm = wwwauth.Realm

// WWWAuthenticateError represents error types per RFC 6750 section 3.1
// SEM@212287c6c02d99be7f8071b21a50666223646bec: define string type enumerating RFC 6750 WWW-Authenticate error codes (pure)
type WWWAuthenticateError string

const (
	// WWWAuthInvalidRequest indicates the request is malformed or missing parameters
	WWWAuthInvalidRequest WWWAuthenticateError = wwwauth.ErrInvalidRequest
	// WWWAuthInvalidToken indicates the token is expired, revoked, or malformed
	WWWAuthInvalidToken WWWAuthenticateError = wwwauth.ErrInvalidToken
	// WWWAuthInsufficientScope indicates the request requires higher privileges
	WWWAuthInsufficientScope WWWAuthenticateError = wwwauth.ErrInsufficientScope
)

// SetWWWAuthenticateHeader sets a RFC 6750 compliant WWW-Authenticate header.
// The header value is built by the shared internal/wwwauth package so the RFC
// 6750 format lives in one place.
//
// Parameters:
//   - c: Gin context
//   - errType: Error type (invalid_request, invalid_token, insufficient_scope) or empty for basic challenge
//   - description: Human-readable error description (optional, ignored if errType is empty)
//
// SEM@fcd7743e746718c31b33ef56fb3ba2f8ccf669c7: set the WWW-Authenticate response header for a given error type (pure)
func SetWWWAuthenticateHeader(c *gin.Context, errType WWWAuthenticateError, description string) {
	c.Header("WWW-Authenticate", wwwauth.BuildHeader(string(errType), description))
}

// sanitizeErrorMessage removes control characters (0x00-0x1F) from error messages
// to comply with OpenAPI schema pattern "^[^\x00-\x1F]*$" for error_description.
// Newlines and tabs are replaced with spaces; other control characters are removed.
// SEM@30604730ee54403d31d30e94debd8c9646ab3356: strip control characters from an error string to comply with API schema (pure)
func sanitizeErrorMessage(message string) string {
	var result strings.Builder
	result.Grow(len(message))

	for _, r := range message {
		if r >= 0x00 && r <= 0x1F {
			// Replace newlines and tabs with spaces for readability
			if r == '\n' || r == '\r' || r == '\t' {
				result.WriteRune(' ')
			}
			// Other control characters are simply removed
		} else {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// Pagination validation constants
const (
	// MaxPaginationLimit is the maximum allowed value for limit parameter
	MaxPaginationLimit = 1000
	// MaxPaginationOffset is the maximum allowed value for offset parameter
	MaxPaginationOffset = 1000000 // 1 million - reasonable for web UI pagination
)

// ValidatePaginationParams validates limit and offset parameters
// Returns a RequestError if validation fails, nil otherwise
// SEM@d8e6843d222487b09e3f3ab6f1ce56ed7ab59ac6: validate limit and offset pagination parameters against allowed bounds (pure)
func ValidatePaginationParams(limit, offset *int) *RequestError {
	if limit != nil {
		if *limit < 0 || *limit > MaxPaginationLimit {
			return InvalidInputError(fmt.Sprintf("limit must be between 0 and %d", MaxPaginationLimit))
		}
	}
	if offset != nil {
		if *offset < 0 || *offset > MaxPaginationOffset {
			return InvalidInputError(fmt.Sprintf("offset must be between 0 and %d", MaxPaginationOffset))
		}
	}
	return nil
}

// ParsePatchRequest parses JSON Patch operations from the request body
// SEM@59c58c6a840231ad2c078c9afd1e7bac7a07b651: parse and validate a JSON Patch operation array from the request body (pure)
func ParsePatchRequest(c *gin.Context) ([]PatchOperation, error) {
	bodyBytes, err := c.GetRawData()
	if err != nil {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Failed to read request body: " + err.Error(),
		}
	}

	if len(bodyBytes) == 0 {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Request body is empty",
		}
	}

	// Reset the body for later use if needed
	c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))

	var operations []PatchOperation
	if err := json.Unmarshal(bodyBytes, &operations); err != nil {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Invalid JSON Patch format: " + err.Error(),
		}
	}

	if len(operations) == 0 {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "PATCH request must contain at least one operation",
		}
	}

	return operations, nil
}

// ParseRequestBody parses JSON request body into the specified type
// SEM@e890b588ef2cb844c92f6ddd0d56e797bb39b7e2: parse, validate, and deserialize a JSON request body into a typed value (pure)
func ParseRequestBody[T any](c *gin.Context) (T, error) {
	var zero T

	bodyBytes, err := c.GetRawData()
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Failed to read request body: " + err.Error(),
		}
	}

	if len(bodyBytes) == 0 {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Request body is empty",
		}
	}

	// Reset the body for later binding
	c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))

	// Validate JSON syntax before processing to prevent panics from malformed JSON
	// This catches edge cases like zero-width Unicode characters, fullwidth brackets,
	// and other malformed JSON that could cause json.Unmarshal to panic
	if !json.Valid(bodyBytes) {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Request body contains invalid JSON",
		}
	}

	// Check for duplicate keys in JSON object (RFC 8259 recommends unique keys)
	if err := checkDuplicateJSONKeys(bodyBytes); err != nil {
		return zero, err
	}

	// Pre-process the JSON to handle invalid UUID values
	cleanedJSON, err := sanitizeJSONForUUIDs(bodyBytes)
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Failed to process JSON: " + err.Error(),
		}
	}

	var result T
	if err := json.Unmarshal(cleanedJSON, &result); err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    errcode.InvalidInput,
			Message: "Invalid JSON format: " + err.Error(),
		}
	}

	return result, nil
}

// sanitizeJSONForUUIDs cleans up JSON by converting invalid UUID values to null
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: null out invalid or empty UUID fields in a JSON object (pure)
func sanitizeJSONForUUIDs(jsonBytes []byte) ([]byte, error) {
	var rawData map[string]any
	if err := json.Unmarshal(jsonBytes, &rawData); err != nil {
		// If it's not an object, return as-is (might be an array)
		return jsonBytes, nil //nolint:nilerr // intentional fallback for non-object JSON
	}

	// List of fields that should contain UUIDs
	uuidFields := []string{
		"id", "threat_model_id", "diagram_id", "cell_id", "parent_id",
		"session_id", "cell", "parent", "entity_id", "webhook_id",
	}

	for _, field := range uuidFields {
		if value, exists := rawData[field]; exists {
			if strValue, ok := value.(string); ok {
				// If it's an empty string or invalid UUID, set to nil
				if strValue == "" || strValue == "undefined" || !isValidUUIDString(strValue) {
					rawData[field] = nil
				}
			}
		}
	}

	return json.Marshal(rawData)
}

// isValidUUIDString checks if a string is a valid UUID format (RFC 4122)
// Validates:
// - Exact length of 36 characters
// - Hyphens at positions 8, 13, 18, 23
// - Only hexadecimal digits (0-9, a-f, A-F) in other positions
// - Rejects Unicode characters, zero-width characters, and other non-ASCII
// SEM@6b48405e24a141b1435b1254c823b48f10b2f676: validate a string against RFC 4122 UUID format (pure)
func isValidUUIDString(s string) bool {
	// First check byte length to reject multi-byte UTF-8 sequences early
	if len(s) != 36 {
		return false
	}

	// Check that all characters are ASCII (reject Unicode like Telugu, Korean, etc.)
	for _, r := range s {
		if r > 127 {
			return false
		}
	}

	// Check UUID format: 8-4-4-4-12 hex digits with hyphens
	// Example: 550e8400-e29b-41d4-a716-446655440000
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHexDigit(r) {
				return false
			}
		}
	}
	return true
}

// isHexDigit checks if a rune is a valid hexadecimal digit (0-9, a-f, A-F)
// SEM@553b9943f20c84a0b6983adbf6a7099330bb94c2: check whether a rune is a valid hexadecimal digit (pure)
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// checkDuplicateJSONKeys checks for duplicate keys in a JSON object
// RFC 8259 recommends unique keys, and duplicate keys can cause unexpected behavior
// SEM@e890b588ef2cb844c92f6ddd0d56e797bb39b7e2: reject a JSON payload that contains duplicate object keys (pure)
func checkDuplicateJSONKeys(jsonBytes []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(jsonBytes)))
	return checkDuplicateKeysInDecoder(dec, "")
}

// checkDuplicateKeysInDecoder recursively checks for duplicate keys in JSON
// SEM@e890b588ef2cb844c92f6ddd0d56e797bb39b7e2: recursively detect duplicate keys in a streaming JSON decoder (pure)
func checkDuplicateKeysInDecoder(dec *json.Decoder, path string) error {
	// Read opening token
	t, err := dec.Token()
	if err != nil {
		return nil //nolint:nilerr // let json.Unmarshal handle syntax errors
	}

	switch t {
	case json.Delim('{'):
		// Object - check for duplicate keys
		keys := make(map[string]bool)
		for dec.More() {
			// Read key
			keyToken, err := dec.Token()
			if err != nil {
				return nil //nolint:nilerr // let json.Unmarshal handle syntax errors
			}

			key, ok := keyToken.(string)
			if !ok {
				continue
			}

			if keys[key] {
				return &RequestError{
					Status:  http.StatusBadRequest,
					Code:    errcode.InvalidInput,
					Message: fmt.Sprintf("Duplicate key '%s' in JSON object", key),
				}
			}
			keys[key] = true

			// Recursively check the value
			keyPath := key
			if path != "" {
				keyPath = path + "." + key
			}
			if err := checkDuplicateKeysInDecoder(dec, keyPath); err != nil {
				return err
			}
		}
		// Read closing brace
		_, _ = dec.Token()

	case json.Delim('['):
		// Array - check each element
		for dec.More() {
			if err := checkDuplicateKeysInDecoder(dec, path); err != nil {
				return err
			}
		}
		// Read closing bracket
		_, _ = dec.Token()

	default:
		// Primitive value - nothing to check
	}

	return nil
}

// IsUserAdministrator checks if the authenticated user is an administrator
// Returns (isAdmin bool, error). Returns false if there's any error or if administrator check is not available.
// SEM@ea4348bffa66284d10fa60dbe3b7ea079942bab0: check whether the authenticated user belongs to the administrators group (reads DB)
func IsUserAdministrator(c *gin.Context) (bool, error) {
	logger := slogging.Get().WithContext(c)

	isAdmin, err := IsGroupMemberFromContext(c, GroupAdministrators)
	if err != nil {
		logger.Debug("IsUserAdministrator: membership check failed: %v", err)
		return false, nil
	}

	return isAdmin, nil
}

// RequestError represents an error that should be returned as an HTTP response
// SEM@ccde596a38d5a3032cf965f5fbc2a4ffb144e534: structured HTTP error carrying status code, machine code, and human message (pure)
type RequestError struct {
	Status  int
	Code    errcode.Code
	Message string
	Details *ErrorDetails
	// markdownTooComplexBytes, when non-zero, marks a markdown link gate
	// rejection (#1013) so HandleRequestError can log who sent it; it is the
	// rejected content's size, never the content.
	markdownTooComplexBytes int
}

// ErrorDetails provides structured context for errors
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: optional structured context attached to a request error (pure)
type ErrorDetails struct {
	Code       *string        `json:"code,omitempty"`
	Context    map[string]any `json:"context,omitempty"`
	Suggestion *string        `json:"suggestion,omitempty"`
}

// SEM@c9dfddf1e0b3e1f0e3423564ea4d4a997e4fdc45: return the error message string for a RequestError (pure)
func (e *RequestError) Error() string {
	return e.Message
}

// HandleRequestError sends an appropriate HTTP error response
// SEM@5e107bce8eca9a7b10483bef568d3497a8b76f93: dispatch an HTTP error response for a request error with appropriate headers (pure)
func HandleRequestError(c *gin.Context, err error) {
	var reqErr *RequestError
	if errors.As(err, &reqErr) {
		// Sanitize error message to remove control characters per OpenAPI schema
		sanitizedMessage := sanitizeErrorMessage(reqErr.Message)
		// Truncate to maxLength defined in OpenAPI Error schema (1000 chars)
		if len(sanitizedMessage) > 1000 {
			sanitizedMessage = sanitizedMessage[:997] + "..."
		}
		response := Error{
			Error:            ErrorError(reqErr.Code),
			ErrorDescription: sanitizedMessage,
		}

		if reqErr.markdownTooComplexBytes > 0 {
			// Identifiers only (the logger adds user and request id): a burst
			// of these is what a gate cost attack looks like.
			slogging.Get().WithContext(c).Warn("markdown link gate rejected content as too complex to validate: method=%s route=%s bytes=%d",
				c.Request.Method, c.FullPath(), reqErr.markdownTooComplexBytes)
		}

		// Add details if provided
		if reqErr.Details != nil {
			response.Details = &struct {
				Code       *string         `json:"code,omitempty"`
				Context    *map[string]any `json:"context,omitempty"`
				Suggestion *string         `json:"suggestion,omitempty"`
			}{
				Code: reqErr.Details.Code,
				Context: func() *map[string]any {
					if len(reqErr.Details.Context) > 0 {
						return &reqErr.Details.Context
					}
					return nil
				}(),
				Suggestion: reqErr.Details.Suggestion,
			}
		}

		// Add WWW-Authenticate header for 401 Unauthorized responses per RFC 6750
		if reqErr.Status == http.StatusUnauthorized {
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, reqErr.Message)
		}

		// Add Retry-After header for 429 Too Many Requests responses per RFC 6585
		if reqErr.Status == http.StatusTooManyRequests && reqErr.Details != nil {
			if retryAfter, ok := reqErr.Details.Context["retry_after"]; ok {
				if retryAfterInt, ok := retryAfter.(int); ok {
					c.Header("Retry-After", fmt.Sprintf("%d", retryAfterInt))
				}
			}
		}

		// Add Retry-After header for 503 Service Unavailable responses (#665),
		// consistent with the 429 path and the auth/cache 503s. Defaults to 30s
		// when no explicit hint is carried in Details.Context.
		if reqErr.Status == http.StatusServiceUnavailable {
			retryAfter := 30
			if reqErr.Details != nil {
				if ra, ok := reqErr.Details.Context["retry_after"]; ok {
					if raInt, ok := ra.(int); ok {
						retryAfter = raInt
					}
				}
			}
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
		}

		c.JSON(reqErr.Status, response)
		c.Abort()
	} else {
		// SECURITY: Truncate error message before any stack trace markers to prevent
		// information disclosure in HTTP responses (defense against CWE-209).
		// This ensures that any unexpected errors with stack traces are safely handled
		// before being sent to external clients.
		errorMsg := truncateBeforeStackTrace(err.Error())
		// Also sanitize to remove control characters per OpenAPI schema
		sanitizedMsg := sanitizeErrorMessage("Internal server error: " + errorMsg)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            ErrorError(errcode.ServerError),
			ErrorDescription: sanitizedMsg,
		})
		c.Abort()
	}
}

// InvalidInputError creates a RequestError for validation failures
// SEM@c9dfddf1e0b3e1f0e3423564ea4d4a997e4fdc45: build a 400 RequestError for input validation failures (pure)
func InvalidInputError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusBadRequest,
		Code:    errcode.InvalidInput,
		Message: message,
	}
}

// InvalidIDError creates a RequestError for invalid ID formats
// SEM@c9dfddf1e0b3e1f0e3423564ea4d4a997e4fdc45: build a 400 RequestError for invalid resource ID format (pure)
func InvalidIDError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusBadRequest,
		Code:    errcode.InvalidID,
		Message: message,
	}
}

// NotFoundError creates a RequestError for resource not found
// SEM@c9dfddf1e0b3e1f0e3423564ea4d4a997e4fdc45: build a 404 RequestError for a missing resource (pure)
func NotFoundError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusNotFound,
		Code:    errcode.NotFound,
		Message: message,
	}
}

// ServerError creates a RequestError for internal server errors
// SEM@c9dfddf1e0b3e1f0e3423564ea4d4a997e4fdc45: build a 500 RequestError for an internal server error (pure)
func ServerError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusInternalServerError,
		Code:    errcode.ServerError,
		Message: message,
	}
}

// ForbiddenError creates a RequestError for forbidden access
// SEM@c9dfddf1e0b3e1f0e3423564ea4d4a997e4fdc45: build a 403 RequestError for forbidden access (pure)
func ForbiddenError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusForbidden,
		Code:    errcode.Forbidden,
		Message: message,
	}
}

// NotAcceptableError creates a RequestError for content negotiation failures
// (no offered response media type matches the request's Accept header).
// SEM@29f63eb500c26288d0d3fe23737adf6fd94bdf9c: build a 406 request error for an unsatisfiable Accept header (pure)
func NotAcceptableError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusNotAcceptable,
		Code:    errcode.NotAcceptable,
		Message: message,
	}
}

// SEM@420d8bdd24035796662ee4234e3bfaa6ba1a73bf: build a 401 RequestError for unauthenticated access (pure)
func UnauthorizedError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusUnauthorized,
		Code:    errcode.Unauthorized,
		Message: message,
	}
}

// ConflictError creates a RequestError for resource conflicts
// SEM@8559837d482fde8e2f7e1a9ea5e99d2bb2414141: build a 409 RequestError for a resource conflict (pure)
func ConflictError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusConflict,
		Code:    errcode.Conflict,
		Message: message,
	}
}

// NotImplementedError creates a RequestError for features not implemented (501)
// Use for features that are defined in the API but not yet implemented,
// or when a particular provider doesn't support a feature.
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: build a 501 RequestError for an unimplemented feature (pure)
func NotImplementedError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusNotImplemented,
		Code:    errcode.NotImplemented,
		Message: message,
	}
}

// ServiceUnavailableError creates a RequestError for temporarily unavailable services (503)
// Use when a dependent service (database, Redis, external provider) is temporarily unavailable.
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: build a 503 RequestError for a temporarily unavailable dependency (pure)
func ServiceUnavailableError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusServiceUnavailable,
		Code:    errcode.ServiceUnavailable,
		Message: message,
	}
}

// StoreErrorToRequestError converts a store error to an appropriate RequestError.
// If the error is already a *RequestError, it is returned as-is (preserving its status code).
// All store errors must use typed dberrors sentinels (#271 umbrella migration is complete).
// SEM@a7cac3f9cd8e77b1dc209e5a213cb6c7f7e40a29: convert a store or dberrors error to the appropriate HTTP RequestError (pure)
func StoreErrorToRequestError(err error, notFoundMsg, serverErrorMsg string) *RequestError {
	// If already a RequestError, return it directly to preserve its status code
	var reqErr *RequestError
	if errors.As(err, &reqErr) {
		return reqErr
	}

	// A GORM BeforeSave hook rejected the entity: the caller's input is bad,
	// not the server (#921). The message is a fixed field-level string.
	// Deliberately ahead of the dberrors sentinel checks: Classify string-matches
	// messages, and a validator message must never be re-read as a 404/409.
	var valErr *validation.ValidationError
	if errors.As(err, &valErr) {
		return InvalidInputError(valErr.Error())
	}

	// Typed error checks (from repositories using dberrors)
	if errors.Is(err, dberrors.ErrNotFound) {
		return NotFoundError(notFoundMsg)
	}
	if errors.Is(err, dberrors.ErrDuplicate) {
		return ConflictError("resource already exists")
	}
	if errors.Is(err, dberrors.ErrConstraint) {
		// Fixed message, not err.Error(): the wrapped value is the raw driver
		// string, so this branch was returning ORA-… text (table, constraint
		// and column names) or the PostgreSQL SQLSTATE detail straight to the
		// caller — the only branch in this function that leaked anything
		// (#602). The underlying error is logged instead, where it is still
		// available for diagnosis.
		slogging.Get().Error("constraint violation returned to client as 400: %v", err)
		return InvalidInputError("request violates a data constraint")
	}
	if errors.Is(err, dberrors.ErrTransient) {
		// A transient DB fault -- serialization failure (ORA-08177), deadlock
		// (ORA-00060), or an ADB autoscale connection drop -- is a temporary
		// backend-unavailability condition, not a server bug, so it surfaces as
		// 503 with Retry-After (consistent with the Redis path) rather than 500
		// (#665). errors.Is matches through the retry helper's
		// "transaction failed after N attempts: %w" wrapper.
		return ServiceUnavailableError("Storage service temporarily unavailable - please retry")
	}

	return ServerError(serverErrorMsg)
}

// codeStr returns a pointer to the string form of a details.code reason.
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a pointer to the string form of a details code (pure)
func codeStr(c errcode.Code) *string {
	s := string(c)
	return &s
}

// WithDetailCode attaches a domain-specific details.code reason to err and returns it.
// SEM@074f3ca8790e600162273d06152aff41b7223b07: attach a domain reason code to a RequestError (pure)
func WithDetailCode(err *RequestError, code errcode.Code) *RequestError {
	if err.Details == nil {
		err.Details = &ErrorDetails{}
	}
	err.Details.Code = codeStr(code)
	return err
}

// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a RequestError from status, code and message (pure)
func newRequestError(status int, code errcode.Code, message string) *RequestError {
	return &RequestError{Status: status, Code: code, Message: message}
}

// InvalidPatchError creates a RequestError for a malformed or inapplicable JSON Patch
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 400 RequestError for an invalid JSON Patch document (pure)
func InvalidPatchError(message string) *RequestError {
	return newRequestError(http.StatusBadRequest, errcode.InvalidPatch, message)
}

// InsufficientUserAuthenticationError creates a RequestError requiring step-up authentication
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 401 RequestError requiring step-up authentication (pure)
func InsufficientUserAuthenticationError(message string) *RequestError {
	return newRequestError(http.StatusUnauthorized, errcode.InsufficientUserAuthentication, message)
}

// MethodNotAllowedError creates a RequestError for an unsupported HTTP method
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 405 RequestError for an unsupported method (pure)
func MethodNotAllowedError(message string) *RequestError {
	return newRequestError(http.StatusMethodNotAllowed, errcode.MethodNotAllowed, message)
}

// GoneError creates a RequestError for a permanently removed resource
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 410 RequestError for a permanently removed resource (pure)
func GoneError(message string) *RequestError {
	return newRequestError(http.StatusGone, errcode.Gone, message)
}

// PayloadTooLargeError creates a RequestError for an oversized request body
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 413 RequestError for an oversized payload (pure)
func PayloadTooLargeError(message string) *RequestError {
	return newRequestError(http.StatusRequestEntityTooLarge, errcode.PayloadTooLarge, message)
}

// UnsupportedMediaTypeError creates a RequestError for an unaccepted Content-Type
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 415 RequestError for an unsupported content type (pure)
func UnsupportedMediaTypeError(message string) *RequestError {
	return newRequestError(http.StatusUnsupportedMediaType, errcode.UnsupportedMediaType, message)
}

// UnprocessableEntityError creates a RequestError for a well-formed request that cannot be processed
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 422 RequestError for an unprocessable request (pure)
func UnprocessableEntityError(message string) *RequestError {
	return newRequestError(http.StatusUnprocessableEntity, errcode.UnprocessableEntity, message)
}

// IfMatchRequiredError creates a RequestError for a missing If-Match header
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 428 RequestError for a missing If-Match header (pure)
func IfMatchRequiredError(message string) *RequestError {
	return newRequestError(http.StatusPreconditionRequired, errcode.IfMatchRequired, message)
}

// RateLimitExceededError creates a 429 RequestError; retryAfterSeconds feeds the Retry-After header
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a 429 RequestError carrying a retry-after hint (pure)
func RateLimitExceededError(message string, retryAfterSeconds int) *RequestError {
	e := newRequestError(http.StatusTooManyRequests, errcode.RateLimitExceeded, message)
	e.Details = &ErrorDetails{Context: map[string]any{"retry_after": retryAfterSeconds}}
	return e
}

// QuotaExceededError creates a RequestError for a hard cap (status 403 or 429)
// SEM@074f3ca8790e600162273d06152aff41b7223b07: build a RequestError for an exhausted hard quota (pure)
func QuotaExceededError(status int, message string) *RequestError {
	return newRequestError(status, errcode.QuotaExceeded, message)
}

// WriteErrorToRequestError maps the error from a store write that has no
// domain-specific outcome (create/update/delete fallbacks) to the response.
// It only distinguishes the transient class: a serialization failure that
// exhausted its retries (ORA-08177, SQLSTATE 40001), a deadlock, or an ADB
// connection drop is a documented 503 with Retry-After, not a server bug, so
// the client can retry instead of reporting a 500 (#900). Every operation
// documents 503 (#665), so this is safe at any handler's fallback branch.
// A GORM BeforeSave hook rejection is the caller's bad input, so it is a 400,
// as in StoreErrorToRequestError (#921).
// SEM@d8c2762cd09ad64046f317cab75b4bf7f53e48ba: classify a store write error into a RequestError: hook rejection 400, transient 503, else 500 (pure)
func WriteErrorToRequestError(err error, serverErrorMsg string) *RequestError {
	var valErr *validation.ValidationError
	if errors.As(err, &valErr) {
		return InvalidInputError(valErr.Error())
	}
	if errors.Is(err, dberrors.ErrTransient) {
		return ServiceUnavailableError("Storage service temporarily unavailable - please retry")
	}
	return ServerError(serverErrorMsg)
}

// NotFoundErrorWithDetails creates a RequestError for resource not found with additional context
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: build a 404 RequestError with structured diagnostic context (pure)
func NotFoundErrorWithDetails(message string, code errcode.Code, context map[string]any, suggestion string) *RequestError {
	return &RequestError{
		Status:  http.StatusNotFound,
		Code:    errcode.NotFound,
		Message: message,
		Details: &ErrorDetails{
			Code:       codeStr(code),
			Context:    context,
			Suggestion: &suggestion,
		},
	}
}

// ServerErrorWithDetails creates a RequestError for internal server errors with additional context
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: build a 500 RequestError with structured diagnostic context (pure)
func ServerErrorWithDetails(message string, code errcode.Code, context map[string]any, suggestion string) *RequestError {
	return &RequestError{
		Status:  http.StatusInternalServerError,
		Code:    errcode.ServerError,
		Message: message,
		Details: &ErrorDetails{
			Code:       codeStr(code),
			Context:    context,
			Suggestion: &suggestion,
		},
	}
}

// InvalidInputErrorWithDetails creates a RequestError for validation failures with additional context
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: build a 400 RequestError with structured diagnostic context (pure)
func InvalidInputErrorWithDetails(message string, code errcode.Code, context map[string]any, suggestion string) *RequestError {
	return &RequestError{
		Status:  http.StatusBadRequest,
		Code:    errcode.InvalidInput,
		Message: message,
		Details: &ErrorDetails{
			Code:       codeStr(code),
			Context:    context,
			Suggestion: &suggestion,
		},
	}
}

// isForeignKeyConstraintError checks if the error is a foreign key constraint violation.
// Checks the typed dberrors.ErrForeignKey sentinel first, then falls back to string matching
// for GORM stores not yet migrated to dberrors (#261).
// SEM@6ef45a78cc6c226116a82e4595fc1dc3f88a8ff9: detect foreign key constraint violations across PostgreSQL, Oracle, MySQL, and SQLite (pure)
func isForeignKeyConstraintError(err error) bool {
	if err == nil {
		return false
	}

	// Typed error check (from repositories using dberrors)
	if errors.Is(err, dberrors.ErrForeignKey) {
		return true
	}

	// String fallback for GORM stores not yet migrated to dberrors (#261)
	errorMessage := strings.ToLower(err.Error())

	// PostgreSQL patterns
	if strings.Contains(errorMessage, "foreign key constraint") ||
		strings.Contains(errorMessage, "violates foreign key constraint") ||
		strings.Contains(errorMessage, "fkey constraint") {
		return true
	}

	// Oracle patterns: ORA-02291 (parent key not found) and ORA-02292 (child record found)
	if strings.Contains(errorMessage, "ora-02291") ||
		strings.Contains(errorMessage, "ora-02292") ||
		(strings.Contains(errorMessage, "integrity constraint") && strings.Contains(errorMessage, "parent key not found")) {
		return true
	}

	// MySQL patterns
	if strings.Contains(errorMessage, "cannot add or update a child row") ||
		strings.Contains(errorMessage, "a foreign key constraint fails") {
		return true
	}

	// SQLite patterns
	if strings.Contains(errorMessage, "foreign key constraint failed") {
		return true
	}

	// Legacy pattern for specific constraint name
	if strings.Contains(errorMessage, "constraint") && strings.Contains(errorMessage, "owner_email") {
		return true
	}

	return false
}

// isUserAccountConfirmedDeleted performs a positive existence check on the
// user row identified by (provider, providerID) and reports true only when
// the lookup definitively confirms the row is gone. Any other outcome -- the
// row exists, GlobalUserStore is unavailable, or the lookup itself errors --
// returns false, so callers never infer "the user is gone" from a heuristic
// (e.g. an arbitrary foreign key constraint name). See #702: a foreign key
// violation on an unrelated reference (such as a non-existent authorization
// group) is not evidence that the caller's own account was deleted.
//
// provider == "" or provider == "unknown" (the ComponentHealthStatusUnknown
// fallback threat_model_handlers.go substitutes when the JWT-derived idp is
// missing from context) cannot be a real identity provider name, so a lookup
// keyed on it can never positively confirm anything -- it would either miss
// every real row (false "not deleted") or, worse, spuriously match rows that
// happen to share that placeholder provider value. Reject both up front so
// the caller lands on its generic 4xx path instead of a false 401 (1.8.3
// oracle-db-admin review).
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: validate via direct lookup that a user's account no longer exists (reads DB)
func isUserAccountConfirmedDeleted(ctx context.Context, provider, providerID string) bool {
	if GlobalUserStore == nil {
		return false
	}
	if providerID == "" || provider == "" || provider == string(ComponentHealthStatusUnknown) {
		return false
	}
	_, err := GlobalUserStore.GetByProviderAndID(ctx, provider, providerID)
	return errors.Is(err, ErrUserNotFound)
}

// truncateBeforeStackTrace removes stack trace information from error messages
// by truncating at stack trace markers to prevent disclosure in HTTP responses.
//
// SECURITY: This function is a critical security control that prevents stack trace
// information exposure in HTTP error responses. It works in conjunction with:
// - Panic recovery middleware that tags stack traces with markers
// - Error handling paths that use this function before sending responses
// - Response logging that filters stack traces from captured data
// This provides defense-in-depth against CWE-209 (Information Exposure Through Stack Traces).
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: remove stack trace content from an error string to prevent information disclosure (pure)
func truncateBeforeStackTrace(errMsg string) string {
	if errMsg == "" {
		return "Unknown error"
	}

	// Look for stack trace markers and truncate before them
	stackTraceMarkers := []string{
		"--- STACK_TRACE_START ---",
		"\nStack trace:",
		"goroutine ",
	}

	for _, marker := range stackTraceMarkers {
		if before, _, ok := strings.Cut(errMsg, marker); ok {
			return strings.TrimSpace(before)
		}
	}

	// No stack trace markers found, return original message
	return errMsg
}
