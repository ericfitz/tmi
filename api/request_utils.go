package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Protected group names that cannot be deleted
const (
	ProtectedGroupEveryone = "everyone"
)

// Pagination validation constants
const (
	// MaxPaginationLimit is the maximum allowed value for limit parameter
	MaxPaginationLimit = 1000
	// MaxPaginationOffset is the maximum allowed value for offset parameter
	MaxPaginationOffset = 1000000 // 1 million - reasonable for web UI pagination
)

// ValidatePaginationParams validates limit and offset parameters
// Returns a RequestError if validation fails, nil otherwise
func ValidatePaginationParams(limit, offset *int) *RequestError {
	if limit != nil {
		if *limit < 0 || *limit > MaxPaginationLimit {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_limit",
				Message: fmt.Sprintf("limit must be between 0 and %d", MaxPaginationLimit),
			}
		}
	}
	if offset != nil {
		if *offset < 0 || *offset > MaxPaginationOffset {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_offset",
				Message: fmt.Sprintf("offset must be between 0 and %d", MaxPaginationOffset),
			}
		}
	}
	return nil
}

// ParsePatchRequest parses JSON Patch operations from the request body
func ParsePatchRequest(c *gin.Context) ([]PatchOperation, error) {
	bodyBytes, err := c.GetRawData()
	if err != nil {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Failed to read request body: " + err.Error(),
		}
	}

	if len(bodyBytes) == 0 {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Request body is empty",
		}
	}

	// Reset the body for later use if needed
	c.Request.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))

	var operations []PatchOperation
	if err := json.Unmarshal(bodyBytes, &operations); err != nil {
		return nil, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid JSON Patch format: " + err.Error(),
		}
	}

	return operations, nil
}

// ParseRequestBody parses JSON request body into the specified type
func ParseRequestBody[T any](c *gin.Context) (T, error) {
	var zero T

	bodyBytes, err := c.GetRawData()
	if err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Failed to read request body: " + err.Error(),
		}
	}

	if len(bodyBytes) == 0 {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
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
			Code:    "invalid_input",
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
			Code:    "invalid_input",
			Message: "Failed to process JSON: " + err.Error(),
		}
	}

	var result T
	if err := json.Unmarshal(cleanedJSON, &result); err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid JSON format: " + err.Error(),
		}
	}

	return result, nil
}

// sanitizeJSONForUUIDs cleans up JSON by converting invalid UUID values to null
func sanitizeJSONForUUIDs(jsonBytes []byte) ([]byte, error) {
	var rawData map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &rawData); err != nil {
		// If it's not an object, return as-is (might be an array)
		return jsonBytes, nil
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
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// checkDuplicateJSONKeys checks for duplicate keys in a JSON object
// RFC 8259 recommends unique keys, and duplicate keys can cause unexpected behavior
func checkDuplicateJSONKeys(jsonBytes []byte) error {
	dec := json.NewDecoder(strings.NewReader(string(jsonBytes)))
	return checkDuplicateKeysInDecoder(dec, "")
}

// checkDuplicateKeysInDecoder recursively checks for duplicate keys in JSON
func checkDuplicateKeysInDecoder(dec *json.Decoder, path string) error {
	// Read opening token
	t, err := dec.Token()
	if err != nil {
		return nil // Let json.Unmarshal handle syntax errors
	}

	switch t {
	case json.Delim('{'):
		// Object - check for duplicate keys
		keys := make(map[string]bool)
		for dec.More() {
			// Read key
			keyToken, err := dec.Token()
			if err != nil {
				return nil // Let json.Unmarshal handle syntax errors
			}

			key, ok := keyToken.(string)
			if !ok {
				continue
			}

			if keys[key] {
				return &RequestError{
					Status:  http.StatusBadRequest,
					Code:    "invalid_input",
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

// ValidateAuthenticatedUser extracts and validates the authenticated user from context
// Returns (email, providerId, role, error)
// The providerId is the OAuth provider's unique user identifier (from JWT "sub" claim)
// The email is the user's email address (from JWT "email" claim)
func ValidateAuthenticatedUser(c *gin.Context) (string, string, Role, error) {
	// Get user email from JWT claim
	userEmailInterface, _ := c.Get("userEmail")
	userEmail, ok := userEmailInterface.(string)
	if !ok || userEmail == "" {
		return "", "", "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	// Get provider user ID (OAuth provider's unique identifier) from JWT "sub" claim
	providerIDInterface, _ := c.Get("userID")
	providerID, ok := providerIDInterface.(string)
	if !ok || providerID == "" {
		return "", "", "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required - missing provider ID",
		}
	}

	// Get user role from context - should be set by middleware
	roleValue, exists := c.Get("userRole")
	if !exists {
		// For some endpoints, role might not be set by middleware
		// In that case, we return empty role and let the caller handle it
		return userEmail, providerID, "", nil
	}

	userRole, ok := roleValue.(Role)
	if !ok {
		return "", "", "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to determine user role",
		}
	}

	return userEmail, providerID, userRole, nil
}

// IsUserAdministrator checks if the authenticated user is an administrator
// Returns (isAdmin bool, error). Returns false if there's any error or if administrator check is not available.
func IsUserAdministrator(c *gin.Context) (bool, error) {
	logger := slogging.Get().WithContext(c)

	// Get user's internal UUID (NOT the provider's user ID)
	var userInternalUUID *uuid.UUID
	if internalUUIDInterface, exists := c.Get("userInternalUUID"); exists {
		if uuidVal, ok := internalUUIDInterface.(uuid.UUID); ok {
			userInternalUUID = &uuidVal
		} else if uuidStr, ok := internalUUIDInterface.(string); ok {
			if parsedID, err := uuid.Parse(uuidStr); err == nil {
				userInternalUUID = &parsedID
			}
		}
	}

	// Get provider from JWT claims
	provider := c.GetString("userProvider")
	if provider == "" {
		logger.Debug("IsUserAdministrator: no provider in context, user is not admin")
		return false, nil
	}

	// Get user groups from JWT claims (may be empty)
	var groupNames []string
	if groupsInterface, exists := c.Get("userGroups"); exists {
		if groupSlice, ok := groupsInterface.([]string); ok {
			groupNames = groupSlice
		}
	}

	// Check if GlobalAdministratorStore is initialized
	if GlobalAdministratorStore == nil {
		logger.Debug("IsUserAdministrator: GlobalAdministratorStore is nil, user is not admin")
		return false, nil
	}

	// Convert group names to group UUIDs
	var groupUUIDs []uuid.UUID
	if dbStore, ok := GlobalAdministratorStore.(*GormAdministratorStore); ok && len(groupNames) > 0 {
		var err error
		groupUUIDs, err = dbStore.GetGroupUUIDsByNames(c.Request.Context(), provider, groupNames)
		if err != nil {
			logger.Error("IsUserAdministrator: failed to lookup group UUIDs: %v", err)
			return false, nil
		}
	}

	isAdmin, err := GlobalAdministratorStore.IsAdmin(c.Request.Context(), userInternalUUID, provider, groupUUIDs)
	if err != nil {
		logger.Error("IsUserAdministrator: failed to check admin status: %v", err)
		return false, nil
	}

	return isAdmin, nil
}

// RequestError represents an error that should be returned as an HTTP response
type RequestError struct {
	Status  int
	Code    string
	Message string
	Details *ErrorDetails
}

// ErrorDetails provides structured context for errors
type ErrorDetails struct {
	Code       *string                `json:"code,omitempty"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Suggestion *string                `json:"suggestion,omitempty"`
}

func (e *RequestError) Error() string {
	return e.Message
}

// HandleRequestError sends an appropriate HTTP error response
func HandleRequestError(c *gin.Context, err error) {
	if reqErr, ok := err.(*RequestError); ok {
		response := Error{
			Error:            reqErr.Code,
			ErrorDescription: reqErr.Message,
		}

		// Add details if provided
		if reqErr.Details != nil {
			response.Details = &struct {
				Code       *string                 `json:"code,omitempty"`
				Context    *map[string]interface{} `json:"context,omitempty"`
				Suggestion *string                 `json:"suggestion,omitempty"`
			}{
				Code: reqErr.Details.Code,
				Context: func() *map[string]interface{} {
					if len(reqErr.Details.Context) > 0 {
						return &reqErr.Details.Context
					}
					return nil
				}(),
				Suggestion: reqErr.Details.Suggestion,
			}
		}

		// Add WWW-Authenticate header for 401 Unauthorized responses
		if reqErr.Status == http.StatusUnauthorized {
			c.Header("WWW-Authenticate", "Bearer")
		}

		c.JSON(reqErr.Status, response)
	} else {
		// SECURITY: Truncate error message before any stack trace markers to prevent
		// information disclosure in HTTP responses (defense against CWE-209).
		// This ensures that any unexpected errors with stack traces are safely handled
		// before being sent to external clients.
		errorMsg := truncateBeforeStackTrace(err.Error())
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Internal server error: " + errorMsg,
		})
	}
}

// InvalidInputError creates a RequestError for validation failures
func InvalidInputError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusBadRequest,
		Code:    "invalid_input",
		Message: message,
	}
}

// InvalidIDError creates a RequestError for invalid ID formats
func InvalidIDError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusBadRequest,
		Code:    "invalid_id",
		Message: message,
	}
}

// NotFoundError creates a RequestError for resource not found
func NotFoundError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusNotFound,
		Code:    "not_found",
		Message: message,
	}
}

// ServerError creates a RequestError for internal server errors
func ServerError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusInternalServerError,
		Code:    "server_error",
		Message: message,
	}
}

// ForbiddenError creates a RequestError for forbidden access
func ForbiddenError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusForbidden,
		Code:    "forbidden",
		Message: message,
	}
}

func UnauthorizedError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusUnauthorized,
		Code:    "unauthorized",
		Message: message,
	}
}

// ConflictError creates a RequestError for resource conflicts
func ConflictError(message string) *RequestError {
	return &RequestError{
		Status:  http.StatusConflict,
		Code:    "conflict",
		Message: message,
	}
}

// NotFoundErrorWithDetails creates a RequestError for resource not found with additional context
func NotFoundErrorWithDetails(message string, code string, context map[string]interface{}, suggestion string) *RequestError {
	return &RequestError{
		Status:  http.StatusNotFound,
		Code:    "not_found",
		Message: message,
		Details: &ErrorDetails{
			Code:       &code,
			Context:    context,
			Suggestion: &suggestion,
		},
	}
}

// ServerErrorWithDetails creates a RequestError for internal server errors with additional context
func ServerErrorWithDetails(message string, code string, context map[string]interface{}, suggestion string) *RequestError {
	return &RequestError{
		Status:  http.StatusInternalServerError,
		Code:    "server_error",
		Message: message,
		Details: &ErrorDetails{
			Code:       &code,
			Context:    context,
			Suggestion: &suggestion,
		},
	}
}

// InvalidInputErrorWithDetails creates a RequestError for validation failures with additional context
func InvalidInputErrorWithDetails(message string, code string, context map[string]interface{}, suggestion string) *RequestError {
	return &RequestError{
		Status:  http.StatusBadRequest,
		Code:    "invalid_input",
		Message: message,
		Details: &ErrorDetails{
			Code:       &code,
			Context:    context,
			Suggestion: &suggestion,
		},
	}
}

// isForeignKeyConstraintError checks if the error is a foreign key constraint violation
func isForeignKeyConstraintError(err error) bool {
	if err == nil {
		return false
	}

	errorMessage := strings.ToLower(err.Error())
	return strings.Contains(errorMessage, "foreign key constraint") ||
		strings.Contains(errorMessage, "violates foreign key constraint") ||
		strings.Contains(errorMessage, "fkey constraint") ||
		strings.Contains(errorMessage, "constraint") && strings.Contains(errorMessage, "owner_email")
}

// extractTokenFromRequest extracts the JWT token from the Authorization header
func extractTokenFromRequest(c *gin.Context) (string, error) {
	// Get the Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing Authorization header")
	}

	// Parse the header format (Bearer <token>)
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", fmt.Errorf("invalid Authorization header format")
	}

	return parts[1], nil
}

// blacklistTokenIfAvailable attempts to blacklist a JWT token using the available token blacklist service
// Note: This function tries to access the blacklist service but gracefully handles when it's not available
func blacklistTokenIfAvailable(c *gin.Context, _ string, userName string) {
	// Since we don't have direct access to the Server type from api package,
	// we'll focus on logging the intent and let the calling code handle the blacklisting
	// This is a defensive approach that ensures the main error handling works even if
	// blacklisting isn't available
	slogging.Get().WithContext(c).Info("Attempting to invalidate JWT token for user %s due to stale session", userName)

	// In a full implementation, this would integrate with the token blacklist service
	// For now, we log the action and continue with the authentication error response
	slogging.Get().WithContext(c).Warn("Token blacklist integration not yet fully implemented - user %s should log out and log back in", userName)
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
		if idx := strings.Index(errMsg, marker); idx != -1 {
			return strings.TrimSpace(errMsg[:idx])
		}
	}

	// No stack trace markers found, return original message
	return errMsg
}

// parsePositiveInt parses a string as a positive integer with validation
func parsePositiveInt(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("empty value")
	}

	var result int
	if _, err := fmt.Sscanf(value, "%d", &result); err != nil {
		return 0, fmt.Errorf("invalid integer format: %w", err)
	}

	if result <= 0 {
		return 0, fmt.Errorf("value must be positive: %d", result)
	}

	return result, nil
}
