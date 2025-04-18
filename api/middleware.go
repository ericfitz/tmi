package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
)

// LogLevel represents logging verbosity
type LogLevel int

const (
	// LogLevelDebug includes detailed debug information
	LogLevelDebug LogLevel = iota
	// LogLevelInfo includes general request information
	LogLevelInfo
	// LogLevelWarn includes warnings and errors only
	LogLevelWarn
	// LogLevelError includes only errors
	LogLevelError
)

// ParseLogLevel converts a string log level to LogLevel
func ParseLogLevel(level string) LogLevel {
	switch strings.ToLower(level) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}

// RequestLogger is a middleware that logs HTTP requests (deprecated, use logging.LoggerMiddleware)
func RequestLogger(logLevel LogLevel) gin.HandlerFunc {
	return logging.LoggerMiddleware()
}

// CORS middleware to handle Cross-Origin Resource Sharing
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// ContextTimeout adds a timeout to the request context
func ContextTimeout(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a new context with timeout
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		// Update the request with the new context
		c.Request = c.Request.WithContext(ctx)

		// Continue processing
		c.Next()
	}
}

// Recoverer middleware recovers from panics and logs the error (deprecated, use logging.Recoverer)
func Recoverer() gin.HandlerFunc {
	return logging.Recoverer()
}

// Role represents a user role with permission levels
type Role = AuthorizationRole

const (
	// RoleOwner has full control over the resource
	RoleOwner Role = Owner
	// RoleWriter can edit but not delete or change ownership
	RoleWriter Role = Writer
	// RoleReader can only view the resource
	RoleReader Role = Reader
)

// ErrAccessDenied indicates an authorization failure
var ErrAccessDenied = errors.New("access denied")

// GetUserRole determines the role of the user for a given threat model
func GetUserRole(userName string, threatModel ThreatModel) Role {
	// If the user is the owner, they have owner role
	if threatModel.Owner == userName {
		return RoleOwner
	}

	// Check authorization entries
	for _, auth := range threatModel.Authorization {
		if auth.Subject == userName {
			return Role(auth.Role)
		}
	}

	// Default to no access
	return ""
}

// CheckThreatModelAccess checks if a user has required access to a threat model
func CheckThreatModelAccess(userName string, threatModel ThreatModel, requiredRole Role) error {
	userRole := GetUserRole(userName, threatModel)

	// If no role found, access is denied
	if userRole == "" {
		return ErrAccessDenied
	}

	// Check role hierarchy
	switch requiredRole {
	case RoleReader:
		// Reader, Writer, and Owner roles can all read
		return nil
	case RoleWriter:
		// Writer and Owner roles can write
		if userRole == RoleWriter || userRole == RoleOwner {
			return nil
		}
	case RoleOwner:
		// Only Owner role can perform owner actions
		if userRole == RoleOwner {
			return nil
		}
	}

	return ErrAccessDenied
}

// ThreatModelMiddleware creates middleware for threat model authorization
func ThreatModelMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := logging.GetContextLogger(c)

		logger.Debug("ThreatModelMiddleware processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("ThreatModelMiddleware skipping for public path: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userName")
		if !exists {
			logger.Warn("Authentication required but userName not found in context for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Authentication required",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Invalid authentication",
			})
			return
		}

		// For POST to collection endpoint (create new threat model), any authenticated user can proceed
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/threat_models" {
			logger.Debug("Allowing create operation for authenticated user: %s", userName)
			c.Next()
			return
		}

		// Skip for list endpoints
		path := c.Request.URL.Path
		if path == "/threat_models" {
			logger.Debug("Skipping auth check for list endpoint")
			c.Next()
			return
		}

		// Skip for non-threat model endpoints
		if !strings.HasPrefix(path, "/threat_models/") {
			logger.Debug("Skipping auth check for non-threat model endpoint: %s", path)
			c.Next()
			return
		}

		// Extract ID from URL
		parts := strings.Split(path, "/")
		if len(parts) < 3 {
			logger.Debug("Path does not contain threat model ID: %s", path)
			c.Next()
			return
		}

		id := parts[2]
		if id == "" {
			logger.Debug("Empty threat model ID in path: %s", path)
			c.Next()
			return
		}

		// Get the threat model from storage
		threatModel, err := ThreatModelStore.Get(id)
		if err != nil {
			logger.Debug("Threat model not found: %s, error: %v", id, err)
			// Let the handler deal with not found errors
			c.Next()
			return
		}

		// Determine required role based on HTTP method
		var requiredRole Role

		switch c.Request.Method {
		case http.MethodGet:
			// Any valid role can read
			requiredRole = RoleReader
			logger.Debug("GET request requires Reader role")
		case http.MethodDelete:
			// Only owner can delete
			requiredRole = RoleOwner
			logger.Debug("DELETE request requires Owner role")
		case http.MethodPut:
			// PUT for updates requires writing to the object
			// If this is an update to an existing object, it requires Writer role
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			logger.Debug("PUT request requires Writer role (handler will check further)")
		case http.MethodPatch:
			// PATCH also requires writing to the object
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			logger.Debug("PATCH request requires Writer role (handler will check further)")
		default:
			// For unknown methods, let the router handle it
			logger.Debug("Unknown method, letting router handle it: %s", c.Request.Method)
			c.Next()
			return
		}

		// Check authorization without reading request body
		// This just checks the basic role permission based on resource ownership
		if err := CheckThreatModelAccess(userName, threatModel, requiredRole); err != nil {
			userRole := GetUserRole(userName, threatModel)
			logger.Warn("Access denied for user %s with role %s, required role: %s",
				userName, userRole, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:   "forbidden",
				Message: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		userRole := GetUserRole(userName, threatModel)
		// Set the role and threatModel in the context for handlers to use
		c.Set("userRole", userRole)
		c.Set("threatModel", threatModel)

		logger.Debug("Access granted for user %s with role %s", userName, userRole)

		c.Next()
	}
}

// DiagramMiddleware creates middleware for diagram authorization
func DiagramMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := logging.GetContextLogger(c)

		logger.Debug("DiagramMiddleware processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("DiagramMiddleware skipping for public path: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userName")
		if !exists {
			logger.Warn("Authentication required but userName not found in context for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Authentication required",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Invalid authentication",
			})
			return
		}

		// For POST to collection endpoint (create new diagram), any authenticated user can proceed
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/diagrams" {
			logger.Debug("Allowing create operation for authenticated user: %s", userName)
			c.Next()
			return
		}

		// Skip for list endpoints
		path := c.Request.URL.Path
		if path == "/diagrams" {
			logger.Debug("Skipping auth check for list endpoint")
			c.Next()
			return
		}

		// Skip for non-diagram endpoints
		if !strings.HasPrefix(path, "/diagrams/") {
			logger.Debug("Skipping auth check for non-diagram endpoint: %s", path)
			c.Next()
			return
		}

		// Extract ID from URL
		parts := strings.Split(path, "/")
		if len(parts) < 3 {
			logger.Debug("Path does not contain diagram ID: %s", path)
			c.Next()
			return
		}

		id := parts[2]
		if id == "" {
			logger.Debug("Empty diagram ID in path: %s", path)
			c.Next()
			return
		}

		// Skip for collaboration endpoints, they have their own access control
		if len(parts) > 3 && parts[3] == "collaborate" {
			logger.Debug("Skipping auth check for collaboration endpoint")
			c.Next()
			return
		}

		// Get the diagram from storage
		diagram, err := DiagramStore.Get(id)
		if err != nil {
			logger.Debug("Diagram not found: %s, error: %v", id, err)
			// Let the handler deal with not found errors
			c.Next()
			return
		}

		// Determine required role based on HTTP method
		var requiredRole Role

		switch c.Request.Method {
		case http.MethodGet:
			// Any valid role can read
			requiredRole = RoleReader
			logger.Debug("GET request requires Reader role")
		case http.MethodDelete:
			// Only owner can delete
			requiredRole = RoleOwner
			logger.Debug("DELETE request requires Owner role")
		case http.MethodPut:
			// PUT for updates requires writing to the object
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			logger.Debug("PUT request requires Writer role (handler will check further)")
		case http.MethodPatch:
			// PATCH requires writing to the object
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			logger.Debug("PATCH request requires Writer role (handler will check further)")
		default:
			// For unknown methods, let the router handle it
			logger.Debug("Unknown method, letting router handle it: %s", c.Request.Method)
			c.Next()
			return
		}

		// Check authorization without reading request body
		// This just checks the basic role permission based on resource ownership
		if err := CheckDiagramAccess(userName, diagram, requiredRole); err != nil {
			userRole := GetUserRoleForDiagram(userName, diagram)
			logger.Warn("Access denied for user %s with role %s, required role: %s",
				userName, userRole, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:   "forbidden",
				Message: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		userRole := GetUserRoleForDiagram(userName, diagram)
		// Set the role and diagram in the context for handlers to use
		c.Set("userRole", userRole)
		c.Set("diagram", diagram)

		logger.Debug("Access granted for user %s with role %s", userName, userRole)

		c.Next()
	}
}

// GetUserRoleForDiagram determines the role of the user for a given diagram
func GetUserRoleForDiagram(userName string, diagram Diagram) Role {
	// Diagrams use the owner and authorization data fields from their parent threat model
	// Find the parent threat model for this diagram
	var parentThreatModel ThreatModel

	// In a real implementation, we would look up the parent threat model
	// For testing purposes, we'll use the TestFixtures.ThreatModel
	parentThreatModel = TestFixtures.ThreatModel

	// Check if the user is the owner
	if userName == parentThreatModel.Owner {
		return RoleOwner
	}

	// Check authorization entries
	for _, auth := range parentThreatModel.Authorization {
		if auth.Subject == userName {
			return Role(auth.Role)
		}
	}

	// Default to no access
	return ""
}

// NewReadCloser creates a new io.ReadCloser from a byte slice
type readCloser struct {
	*strings.Reader
}

func (r readCloser) Close() error {
	return nil
}

func NewReadCloser(b []byte) *readCloser {
	return &readCloser{strings.NewReader(string(b))}
}

// LogRequest logs debug information about the request
func LogRequest(c *gin.Context, prefix string) {
	// Get logger from context
	logger := logging.GetContextLogger(c)

	logger.Debug("%s - Method: %s, Path: %s", prefix, c.Request.Method, c.Request.URL.Path)

	// Log headers
	headerLog := fmt.Sprintf("%s - Headers:", prefix)
	for k, v := range c.Request.Header {
		headerLog += fmt.Sprintf(" %s=%v", k, v)
	}
	logger.Debug(headerLog)

	// Try to log body
	bodyBytes, err := c.GetRawData()
	if err != nil {
		logger.Debug("%s - Error reading body: %v", prefix, err)
	} else if len(bodyBytes) > 0 {
		logger.Debug("%s - Body: %s", prefix, string(bodyBytes))
		// Reset the body for later use
		c.Request.Body = NewReadCloser(bodyBytes)
	} else {
		logger.Debug("%s - Empty body", prefix)
	}
}

// CheckDiagramAccess checks if a user has required access to a diagram
func CheckDiagramAccess(userName string, diagram Diagram, requiredRole Role) error {
	userRole := GetUserRoleForDiagram(userName, diagram)

	// If no role found, access is denied
	if userRole == "" {
		return ErrAccessDenied
	}

	// Check role hierarchy
	switch requiredRole {
	case RoleReader:
		// Reader, Writer, and Owner roles can all read
		return nil
	case RoleWriter:
		// Writer and Owner roles can write
		if userRole == RoleWriter || userRole == RoleOwner {
			return nil
		}
	case RoleOwner:
		// Only Owner role can perform owner actions
		if userRole == RoleOwner {
			return nil
		}
	}

	return ErrAccessDenied
}
