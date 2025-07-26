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

	var result T
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return zero, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_input",
			Message: "Invalid JSON format: " + err.Error(),
		}
	}

	return result, nil
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
