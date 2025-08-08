package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

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
		"session_id", "cell", "parent", "entity_id",
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

// isValidUUIDString checks if a string is a valid UUID format
func isValidUUIDString(s string) bool {
	if len(s) != 36 {
		return false
	}
	// Check basic UUID format: 8-4-4-4-12 hex digits with hyphens
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

// isHexDigit checks if a rune is a valid hexadecimal digit
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// ValidateAuthenticatedUser extracts and validates the authenticated user from context
func ValidateAuthenticatedUser(c *gin.Context) (string, Role, error) {
	// Get username from JWT claim
	userID, _ := c.Get("userName")
	userName, ok := userID.(string)
	if !ok || userName == "" {
		return "", "", &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	// Get user role from context - should be set by middleware
	roleValue, exists := c.Get("userRole")
	if !exists {
		// For some endpoints, role might not be set by middleware
		// In that case, we return empty role and let the caller handle it
		return userName, "", nil
	}

	userRole, ok := roleValue.(Role)
	if !ok {
		return userName, "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to determine user role",
		}
	}

	return userName, userRole, nil
}

// RequestError represents an error that should be returned as an HTTP response
type RequestError struct {
	Status  int
	Code    string
	Message string
}

func (e *RequestError) Error() string {
	return e.Message
}

// HandleRequestError sends an appropriate HTTP error response
func HandleRequestError(c *gin.Context, err error) {
	if reqErr, ok := err.(*RequestError); ok {
		c.JSON(reqErr.Status, Error{
			Error:            reqErr.Code,
			ErrorDescription: reqErr.Message,
		})
	} else {
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Internal server error: " + err.Error(),
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
