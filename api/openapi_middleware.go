package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gin-gonic/gin"
	middleware "github.com/oapi-codegen/gin-middleware"
)

// OpenAPIErrorHandler converts OpenAPI validation errors to TMI's error format
func OpenAPIErrorHandler(c *gin.Context, message string, statusCode int) {
	var tmiError error

	// Enhanced debug logging for OpenAPI validation failures with request tracing
	requestID := getRequestID(c)
	logger := slogging.GetContextLogger(c)
	logger.Error("OPENAPI_VALIDATION_FAILED [%s] %s %s -> %d: %s",
		requestID, c.Request.Method, c.Request.URL.Path, statusCode, message)

	// Check for "no matching operation" errors (route/method not found in OpenAPI spec)
	messageLower := strings.ToLower(message)
	if strings.Contains(messageLower, "no matching operation") {
		// Determine if this is a 404 (path not found) or 405 (method not allowed)
		// Check if the path exists with any method in the spec
		pathExists := pathExistsInSpec(c.Request.URL.Path)

		if pathExists {
			// Path exists but method doesn't - return 405 Method Not Allowed
			c.Header("Allow", getAllowedMethodsForPath(c.Request.URL.Path))
			tmiError = &RequestError{
				Code:    "method_not_allowed",
				Message: fmt.Sprintf("The HTTP method '%s' is not supported for this endpoint", c.Request.Method),
				Status:  http.StatusMethodNotAllowed,
			}
		} else {
			// Path doesn't exist - return 404 Not Found
			detailedMessage := fmt.Sprintf(
				"The endpoint '%s %s' is not defined in the API specification. "+
					"Check the request method and path (including trailing slashes).",
				c.Request.Method, c.Request.URL.Path)
			tmiError = &RequestError{
				Code:    "not_found",
				Message: detailedMessage,
				Status:  http.StatusNotFound,
			}
		}
	} else {
		// Handle other validation errors
		switch statusCode {
		case http.StatusBadRequest:
			if strings.Contains(messageLower, "required") {
				tmiError = InvalidInputError(message)
			} else if strings.Contains(messageLower, "format") ||
				strings.Contains(messageLower, "pattern") {
				tmiError = InvalidIDError(message)
			} else {
				tmiError = InvalidInputError(message)
			}
		case http.StatusUnprocessableEntity:
			tmiError = InvalidInputError(message)
		default:
			// Treat unexpected validation status codes as client errors, not server errors
			tmiError = InvalidInputError(message)
		}
	}

	// Log the final error being returned to client for debugging
	requestError := tmiError.(*RequestError)
	logger.Error("OPENAPI_ERROR_CONVERTED [%s] Code: %s, Message: %s",
		requestID, requestError.Code, requestError.Message)

	HandleRequestError(c, tmiError)
}

// GinServerErrorHandler converts parameter binding errors to TMI's error format
// This is used by the oapi-codegen generated server wrapper to handle parameter binding errors
func GinServerErrorHandler(c *gin.Context, err error, statusCode int) {
	requestID := getRequestID(c)
	logger := slogging.GetContextLogger(c)

	// Log the parameter binding error
	logger.Error("PARAMETER_BINDING_FAILED [%s] %s %s -> %d: %s",
		requestID, c.Request.Method, c.Request.URL.Path, statusCode, err.Error())

	// Convert to TMI error format
	var tmiError error
	errorMessage := err.Error()
	messageLower := strings.ToLower(errorMessage)

	switch statusCode {
	case http.StatusBadRequest:
		// Check if it's an enum validation error
		if strings.Contains(messageLower, "enum") ||
			strings.Contains(messageLower, "invalid value") {
			tmiError = InvalidInputError(fmt.Sprintf("Invalid parameter value: %s", errorMessage))
		} else if strings.Contains(messageLower, "required") {
			tmiError = InvalidInputError(fmt.Sprintf("Missing required parameter: %s", errorMessage))
		} else if strings.Contains(messageLower, "format") ||
			strings.Contains(messageLower, "pattern") {
			tmiError = InvalidIDError(errorMessage)
		} else {
			tmiError = InvalidInputError(errorMessage)
		}
	case http.StatusUnprocessableEntity:
		tmiError = InvalidInputError(errorMessage)
	default:
		tmiError = ServerError(errorMessage)
	}

	// Log the final error being returned to client
	requestError := tmiError.(*RequestError)
	logger.Error("PARAMETER_ERROR_CONVERTED [%s] Code: %s, Message: %s",
		requestID, requestError.Code, requestError.Message)

	HandleRequestError(c, tmiError)
}

// SetupOpenAPIValidation creates and returns OpenAPI validation middleware
func SetupOpenAPIValidation() (gin.HandlerFunc, error) {
	swagger, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("failed to load OpenAPI spec: %w", err)
	}

	// Clear security requirements for all paths to avoid authentication validation errors
	// Authentication is handled separately by our JWT middleware
	swagger.Components.SecuritySchemes = nil

	// Remove security requirements from all paths since we handle auth with our own JWT middleware
	// Use empty security requirements (non-nil pointer to empty slice) to explicitly disable security
	emptySecurityRequirements := openapi3.NewSecurityRequirements()
	for _, pathItem := range swagger.Paths.Map() {
		if pathItem.Get != nil {
			pathItem.Get.Security = emptySecurityRequirements
		}
		if pathItem.Post != nil {
			pathItem.Post.Security = emptySecurityRequirements
		}
		if pathItem.Put != nil {
			pathItem.Put.Security = emptySecurityRequirements
		}
		if pathItem.Patch != nil {
			pathItem.Patch.Security = emptySecurityRequirements
		}
		if pathItem.Delete != nil {
			pathItem.Delete.Security = emptySecurityRequirements
		}
	}

	// Clear servers to avoid host validation issues in tests
	swagger.Servers = nil

	// Create OpenAPI validator with custom logic to skip WebSocket routes
	// Provide a no-op authentication function to bypass security validation
	validator := middleware.OapiRequestValidatorWithOptions(swagger,
		&middleware.Options{
			ErrorHandler:          OpenAPIErrorHandler,
			SilenceServersWarning: true, // Silence the servers warning for tests
			Options: openapi3filter.Options{
				AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			},
		})

	// Return a wrapper that skips validation for WebSocket routes
	return func(c *gin.Context) {
		requestID := getRequestID(c)
		logger := slogging.GetContextLogger(c)

		// Skip OpenAPI validation for WebSocket endpoints
		// WebSocket endpoints are not REST APIs and shouldn't be validated against OpenAPI spec
		if strings.HasSuffix(c.Request.URL.Path, "/ws") {
			logger.Debug("OPENAPI_VALIDATION_SKIPPED [%s] WebSocket endpoint: %s %s",
				requestID, c.Request.Method, c.Request.URL.Path)
			c.Next()
			return
		}

		// Log that OpenAPI validation is being applied
		logger.Debug("OPENAPI_VALIDATION_STARTING [%s] %s %s",
			requestID, c.Request.Method, c.Request.URL.Path)

		// Apply OpenAPI validation for all routes
		validator(c)

		// Log if validation passed (if we get here, validation succeeded)
		logger.Debug("OPENAPI_VALIDATION_PASSED [%s] %s %s",
			requestID, c.Request.Method, c.Request.URL.Path)
	}, nil
}

// cachedSwagger holds the parsed OpenAPI spec for path lookups
var cachedSwagger *openapi3.T

// initCachedSwagger initializes the cached swagger spec
func initCachedSwagger() {
	if cachedSwagger == nil {
		swagger, err := GetSwagger()
		if err == nil {
			cachedSwagger = swagger
		}
	}
}

// pathExistsInSpec checks if a path exists in the OpenAPI spec (with any method)
// It handles path parameters by normalizing the path
func pathExistsInSpec(requestPath string) bool {
	initCachedSwagger()
	if cachedSwagger == nil {
		return false
	}

	// First, try exact match
	if _, ok := cachedSwagger.Paths.Map()[requestPath]; ok {
		return true
	}

	// Try matching with path parameters
	// Convert request path segments to template patterns for matching
	requestParts := strings.Split(strings.Trim(requestPath, "/"), "/")

	for specPath := range cachedSwagger.Paths.Map() {
		specParts := strings.Split(strings.Trim(specPath, "/"), "/")

		if len(specParts) != len(requestParts) {
			continue
		}

		match := true
		for i, specPart := range specParts {
			// Path parameter segments start with { and end with }
			if strings.HasPrefix(specPart, "{") && strings.HasSuffix(specPart, "}") {
				// This is a path parameter, it matches any value
				continue
			}
			if specPart != requestParts[i] {
				match = false
				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

// getAllowedMethodsForPath returns the allowed HTTP methods for a path
func getAllowedMethodsForPath(requestPath string) string {
	initCachedSwagger()
	if cachedSwagger == nil {
		return "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	}

	// Find the matching path item
	var pathItem *openapi3.PathItem

	// First, try exact match
	if item, ok := cachedSwagger.Paths.Map()[requestPath]; ok {
		pathItem = item
	} else {
		// Try matching with path parameters
		requestParts := strings.Split(strings.Trim(requestPath, "/"), "/")

		for specPath, item := range cachedSwagger.Paths.Map() {
			specParts := strings.Split(strings.Trim(specPath, "/"), "/")

			if len(specParts) != len(requestParts) {
				continue
			}

			match := true
			for i, specPart := range specParts {
				if strings.HasPrefix(specPart, "{") && strings.HasSuffix(specPart, "}") {
					continue
				}
				if specPart != requestParts[i] {
					match = false
					break
				}
			}

			if match {
				pathItem = item
				break
			}
		}
	}

	if pathItem == nil {
		return "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	}

	// Build list of allowed methods
	var methods []string
	if pathItem.Get != nil {
		methods = append(methods, "GET")
	}
	if pathItem.Post != nil {
		methods = append(methods, "POST")
	}
	if pathItem.Put != nil {
		methods = append(methods, "PUT")
	}
	if pathItem.Patch != nil {
		methods = append(methods, "PATCH")
	}
	if pathItem.Delete != nil {
		methods = append(methods, "DELETE")
	}
	// Always allow OPTIONS for CORS preflight
	methods = append(methods, "OPTIONS")

	return strings.Join(methods, ", ")
}
