package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UUID parameter names that should be validated
var uuidParams = []string{
	"id",
	"threat_model_id",
	"diagram_id",
	"document_id",
	"note_id",
	"repository_id",
	"asset_id",
	"threat_id",
	"user_id",
	"invocation_id",
}

// UUIDValidationMiddleware validates UUID path parameters
func UUIDValidationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Get all path parameters
		params := c.Params

		// Check each UUID parameter
		for _, param := range params {
			// Check if this parameter should be a UUID
			if shouldValidateAsUUID(param.Key) {
				// Validate UUID format
				if _, err := uuid.Parse(param.Value); err != nil {
					logger.Warn("Invalid UUID format for parameter %s: %s", param.Key, param.Value)
					HandleRequestError(c, InvalidIDError("Invalid UUID format for parameter: "+param.Key))
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

// shouldValidateAsUUID checks if a parameter name should be validated as UUID
func shouldValidateAsUUID(paramName string) bool {
	for _, uuidParam := range uuidParams {
		if paramName == uuidParam {
			return true
		}
	}
	return false
}

// PathParameterValidationMiddleware validates all path parameters for common issues
func PathParameterValidationMiddleware() gin.HandlerFunc {
	// Regex patterns for validation
	sqlInjectionPattern := regexp.MustCompile(`(?i)(union|select|insert|update|delete|drop|create|alter|exec|execute|script|javascript|<|>)`)
	pathTraversalPattern := regexp.MustCompile(`\.\.`)

	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		// Get all path parameters
		params := c.Params

		for _, param := range params {
			value := param.Value

			// Skip empty values (will be caught by other validation)
			if value == "" {
				logger.Warn("Empty path parameter: %s", param.Key)
				HandleRequestError(c, InvalidIDError("Empty value for parameter: "+param.Key))
				c.Abort()
				return
			}

			// Check for path traversal attempts
			if pathTraversalPattern.MatchString(value) {
				logger.Warn("Path traversal attempt detected in parameter %s: %s", param.Key, value)
				HandleRequestError(c, InvalidIDError("Invalid value for parameter: "+param.Key))
				c.Abort()
				return
			}

			// Check for SQL injection patterns
			if sqlInjectionPattern.MatchString(value) {
				logger.Warn("SQL injection attempt detected in parameter %s: %s", param.Key, value)
				HandleRequestError(c, InvalidIDError("Invalid value for parameter: "+param.Key))
				c.Abort()
				return
			}

			// Check for excessive length (prevent DOS attacks)
			if len(value) > 200 {
				logger.Warn("Excessive parameter length for %s: %d characters", param.Key, len(value))
				HandleRequestError(c, InvalidIDError("Parameter value too long: "+param.Key))
				c.Abort()
				return
			}

			// Check for null bytes
			if strings.Contains(value, "\x00") {
				logger.Warn("Null byte detected in parameter %s", param.Key)
				HandleRequestError(c, InvalidIDError("Invalid value for parameter: "+param.Key))
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

// MethodNotAllowedHandler returns 405 for unsupported HTTP methods
func MethodNotAllowedHandler() gin.HandlerFunc {
	validMethods := map[string]bool{
		http.MethodGet:     true,
		http.MethodPost:    true,
		http.MethodPut:     true,
		http.MethodPatch:   true,
		http.MethodDelete:  true,
		http.MethodOptions: true,
		http.MethodHead:    true,
	}

	return func(c *gin.Context) {
		method := c.Request.Method

		// Check if method is in the valid list
		if !validMethods[method] {
			logger := slogging.Get().WithContext(c)
			logger.Warn("Unsupported HTTP method: %s for path: %s", method, c.Request.URL.Path)

			// Get allowed methods for this path (from router)
			c.Header("Allow", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

			c.JSON(http.StatusMethodNotAllowed, Error{
				Error:            "method_not_allowed",
				ErrorDescription: "The requested HTTP method is not supported for this endpoint",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
