package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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

// RequestLogger is a middleware that logs HTTP requests
func RequestLogger(logLevel LogLevel) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()

		// Process request
		c.Next()

		// Calculate request processing time
		latency := time.Since(start)

		// Log request information
		statusCode := c.Writer.Status()
		method := c.Request.Method
		path := c.Request.URL.Path
		clientIP := c.ClientIP()

		// Determine if we should log based on status code and log level
		shouldLog := false
		logPrefix := ""

		switch {
		case statusCode >= 500:
			// Server errors are always logged at error level
			shouldLog = logLevel <= LogLevelError
			logPrefix = "[ERROR]"
		case statusCode >= 400:
			// Client errors are logged at warn level and below
			shouldLog = logLevel <= LogLevelWarn
			logPrefix = "[WARN]"
		case statusCode >= 300:
			// Redirects are logged at info level and below
			shouldLog = logLevel <= LogLevelInfo
			logPrefix = "[INFO]"
		default:
			// Success responses are logged at info level and below
			shouldLog = logLevel <= LogLevelInfo
			logPrefix = "[INFO]"
		}

		// Debug level logs all requests with extra details
		if logLevel == LogLevelDebug {
			shouldLog = true
			logPrefix = "[DEBUG]"
		}

		if shouldLog {
			// Log the request details
			// In production, you would use a structured logging library like zap
			logMsg := fmt.Sprintf(
				"%s %s | %s | %s | %s | %d: %s | %s\n",
				logPrefix,
				time.Now().Format(time.RFC3339),
				method,
				path,
				clientIP,
				statusCode,
				http.StatusText(statusCode),
				latency.String(),
			)
			gin.DefaultWriter.Write([]byte(logMsg))
		}
	}
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

// Recoverer middleware recovers from panics and logs the error
func Recoverer() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Log the error
				gin.DefaultErrorWriter.Write([]byte("[ERROR] Panic recovered: " + err.(string) + "\n"))

				// Return a 500 error
				c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
					Error:   "internal_server_error",
					Message: "An unexpected error occurred",
				})
			}
		}()

		c.Next()
	}
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
		// Debug log the request and path
		fmt.Printf("[DEBUG MIDDLEWARE] Processing request: %s %s\n", c.Request.Method, c.Request.URL.Path)
		
		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userName")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Authentication required",
			})
			return
		}
		
		userName, ok := userID.(string)
		if !ok || userName == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Invalid authentication",
			})
			return
		}
		
		// For POST to collection endpoint (create new threat model), any authenticated user can proceed
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/threat_models" {
			fmt.Printf("[DEBUG MIDDLEWARE] Allowing create operation for authenticated user: %s\n", userName)
			c.Next()
			return
		}
		
		// Skip for list endpoints
		path := c.Request.URL.Path
		if path == "/threat_models" {
			c.Next()
			return
		}
		
		// Skip for non-threat model endpoints
		if !strings.HasPrefix(path, "/threat_models/") {
			c.Next()
			return
		}
		
		// Extract ID from URL
		parts := strings.Split(path, "/")
		if len(parts) < 3 {
			c.Next()
			return
		}
		
		id := parts[2]
		if id == "" {
			c.Next()
			return
		}
		
		// Get the threat model from storage
		threatModel, err := ThreatModelStore.Get(id)
		if err != nil {
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
			fmt.Printf("[DEBUG MIDDLEWARE] GET request requires Reader role\n")
		case http.MethodDelete:
			// Only owner can delete
			requiredRole = RoleOwner
			fmt.Printf("[DEBUG MIDDLEWARE] DELETE request requires Owner role\n")
		case http.MethodPut:
			// PUT for updates requires writing to the object
			// If this is an update to an existing object, it requires Writer role
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			fmt.Printf("[DEBUG MIDDLEWARE] PUT request requires Writer role (handler will check further)\n")
		case http.MethodPatch:
			// PATCH also requires writing to the object
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			fmt.Printf("[DEBUG MIDDLEWARE] PATCH request requires Writer role (handler will check further)\n")
		default:
			// For unknown methods, let the router handle it
			c.Next()
			return
		}
		
		// Check authorization without reading request body
		// This just checks the basic role permission based on resource ownership
		if err := CheckThreatModelAccess(userName, threatModel, requiredRole); err != nil {
			fmt.Printf("[DEBUG MIDDLEWARE] Access denied for user %s with role %s\n", 
				userName, GetUserRole(userName, threatModel))
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:   "forbidden",
				Message: "You don't have sufficient permissions to perform this action",
			})
			return
		}
		
		// Set the role and threatModel in the context for handlers to use
		c.Set("userRole", GetUserRole(userName, threatModel))
		c.Set("threatModel", threatModel)
		
		fmt.Printf("[DEBUG MIDDLEWARE] Access granted for user %s with role %s\n", 
			userName, GetUserRole(userName, threatModel))
		
		c.Next()
	}
}

// DiagramMiddleware creates middleware for diagram authorization
func DiagramMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Debug log the request and path
		fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] Processing request: %s %s\n", c.Request.Method, c.Request.URL.Path)
		
		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userName")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Authentication required",
			})
			return
		}
		
		userName, ok := userID.(string)
		if !ok || userName == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:   "unauthorized",
				Message: "Invalid authentication",
			})
			return
		}
		
		// For POST to collection endpoint (create new diagram), any authenticated user can proceed
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/diagrams" {
			fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] Allowing create operation for authenticated user: %s\n", userName)
			c.Next()
			return
		}
		
		// Skip for list endpoints
		path := c.Request.URL.Path
		if path == "/diagrams" {
			c.Next()
			return
		}
		
		// Skip for non-diagram endpoints
		if !strings.HasPrefix(path, "/diagrams/") {
			c.Next()
			return
		}
		
		// Extract ID from URL
		parts := strings.Split(path, "/")
		if len(parts) < 3 {
			c.Next()
			return
		}
		
		id := parts[2]
		if id == "" {
			c.Next()
			return
		}
		
		// Skip for collaboration endpoints, they have their own access control
		if len(parts) > 3 && parts[3] == "collaborate" {
			c.Next()
			return
		}
		
		// Get the diagram from storage
		diagram, err := DiagramStore.Get(id)
		if err != nil {
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
			fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] GET request requires Reader role\n")
		case http.MethodDelete:
			// Only owner can delete
			requiredRole = RoleOwner
			fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] DELETE request requires Owner role\n")
		case http.MethodPut:
			// PUT for updates requires writing to the object
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] PUT request requires Writer role (handler will check further)\n")
		case http.MethodPatch:
			// PATCH requires writing to the object
			// Handler will enforce more specific permissions for owner/auth changes
			requiredRole = RoleWriter
			fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] PATCH request requires Writer role (handler will check further)\n")
		default:
			// For unknown methods, let the router handle it
			c.Next()
			return
		}
		
		// Check authorization without reading request body
		// This just checks the basic role permission based on resource ownership
		if err := CheckDiagramAccess(userName, diagram, requiredRole); err != nil {
			fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] Access denied for user %s with role %s\n", 
				userName, GetUserRoleForDiagram(userName, diagram))
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:   "forbidden",
				Message: "You don't have sufficient permissions to perform this action",
			})
			return
		}
		
		// Set the role and diagram in the context for handlers to use
		c.Set("userRole", GetUserRoleForDiagram(userName, diagram))
		c.Set("diagram", diagram)
		
		fmt.Printf("[DEBUG DIAGRAM MIDDLEWARE] Access granted for user %s with role %s\n", 
			userName, GetUserRoleForDiagram(userName, diagram))
		
		c.Next()
	}
}

// GetUserRoleForDiagram determines the role of the user for a given diagram
func GetUserRoleForDiagram(userName string, diagram Diagram) Role {
	// If the user is the owner, they have owner role
	if diagram.Owner == userName {
		return RoleOwner
	}

	// Check authorization entries
	for _, auth := range diagram.Authorization {
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
	gin.DefaultWriter.Write([]byte(fmt.Sprintf("[DEBUG %s] Method: %s, Path: %s\n", 
		prefix, c.Request.Method, c.Request.URL.Path)))
	
	// Log headers
	gin.DefaultWriter.Write([]byte(fmt.Sprintf("[DEBUG %s] Headers:\n", prefix)))
	for k, v := range c.Request.Header {
		gin.DefaultWriter.Write([]byte(fmt.Sprintf("  %s: %v\n", k, v)))
	}
	
	// Try to log body
	bodyBytes, err := c.GetRawData()
	if err != nil {
		gin.DefaultWriter.Write([]byte(fmt.Sprintf("[DEBUG %s] Error reading body: %v\n", prefix, err)))
	} else if len(bodyBytes) > 0 {
		gin.DefaultWriter.Write([]byte(fmt.Sprintf("[DEBUG %s] Body: %s\n", prefix, string(bodyBytes))))
		// Reset the body for later use
		c.Request.Body = NewReadCloser(bodyBytes)
	} else {
		gin.DefaultWriter.Write([]byte(fmt.Sprintf("[DEBUG %s] Empty body\n", prefix)))
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