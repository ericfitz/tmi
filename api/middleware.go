package api

import (
	"context"
	"database/sql"
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
			return auth.Role
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
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
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
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
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
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
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
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
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
func GetUserRoleForDiagram(userName string, diagram DfdDiagram) Role {
	// Diagrams inherit permissions from their parent threat model
	// For database-backed diagrams, we need to find the parent threat model

	// Try to find the parent threat model from the database
	if dbStore, ok := DiagramStore.(*DiagramDatabaseStore); ok {
		// This is a database-backed diagram
		// Query the database directly to get the threat model ID for this diagram
		if diagram.Id != nil {
			diagramID := diagram.Id.String()

			// Query the database to get the threat model ID
			var threatModelID string
			query := `SELECT threat_model_id FROM diagrams WHERE id = $1`
			err := dbStore.db.QueryRow(query, diagramID).Scan(&threatModelID)
			if err != nil {
				// If we can't find the threat model, deny access
				return ""
			}

			// Get the threat model from the store
			if tmStore, ok := ThreatModelStore.(*ThreatModelDatabaseStore); ok {
				threatModel, err := tmStore.Get(threatModelID)
				if err != nil {
					// If we can't get the threat model, deny access
					return ""
				}

				// Check if the user is the owner
				if userName == threatModel.Owner {
					return RoleOwner
				}

				// Check authorization entries
				for _, auth := range threatModel.Authorization {
					if auth.Subject == userName {
						return auth.Role
					}
				}

				// No access found
				return ""
			}
		}
	}

	// Fallback to TestFixtures for non-database stores
	parentThreatModel := TestFixtures.ThreatModel

	// Check if the user is the owner
	if userName == parentThreatModel.Owner {
		return RoleOwner
	}

	// Check authorization entries
	for _, auth := range parentThreatModel.Authorization {
		if auth.Subject == userName {
			return auth.Role
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
func CheckDiagramAccess(userName string, diagram DfdDiagram, requiredRole Role) error {
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

// ValidateSubResourceAccess creates middleware for sub-resource authorization with caching
// This middleware validates access to sub-resources (threats, documents, sources) by inheriting
// permissions from their parent threat model
func ValidateSubResourceAccess(db *sql.DB, cache *CacheService, requiredRole Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := logging.GetContextLogger(c)
		logger.Debug("ValidateSubResourceAccess processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("ValidateSubResourceAccess skipping for public path: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Get username from the request context
		userID, exists := c.Get("userName")
		if !exists {
			logger.Warn("Authentication required but userName not found in context for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Extract threat model ID from the path
		// Sub-resource paths typically follow patterns like:
		// /threat_models/{id}/threats/{threat_id}
		// /threat_models/{id}/documents/{doc_id}
		// /threat_models/{id}/sources/{source_id}
		threatModelID := extractThreatModelIDFromPath(c.Request.URL.Path)
		if threatModelID == "" {
			logger.Debug("No threat model ID found in path, skipping sub-resource auth: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Check sub-resource access using inherited authorization
		hasAccess, err := CheckSubResourceAccess(c.Request.Context(), db, cache, userName, threatModelID, requiredRole)
		if err != nil {
			logger.Error("Failed to check sub-resource access for user %s on threat model %s: %v",
				userName, threatModelID, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Failed to validate permissions",
			})
			return
		}

		if !hasAccess {
			logger.Warn("Access denied for user %s on threat model %s (required role: %s)",
				userName, threatModelID, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		// Set the threat model ID in context for handlers to use
		c.Set("threatModelID", threatModelID)
		c.Set("userRole", requiredRole) // The actual role could be higher

		logger.Debug("Sub-resource access granted for user %s on threat model %s", userName, threatModelID)
		c.Next()
	}
}

// extractThreatModelIDFromPath extracts the threat model ID from sub-resource paths
func extractThreatModelIDFromPath(path string) string {
	// Handle paths like:
	// /threat_models/{threat_model_id}/threats
	// /threat_models/{threat_model_id}/threats/{threat_id}
	// /threat_models/{threat_model_id}/documents
	// /threat_models/{threat_model_id}/documents/{doc_id}
	// /threat_models/{threat_model_id}/sources
	// /threat_models/{threat_model_id}/sources/{source_id}
	// /threat_models/{threat_model_id}/metadata
	// /threat_models/{threat_model_id}/metadata/{key}

	parts := strings.Split(strings.Trim(path, "/"), "/")

	// Must have at least: threat_models, {id}, {sub_resource}
	if len(parts) < 3 {
		return ""
	}

	// Check if it starts with threat_models
	if parts[0] != "threat_models" {
		return ""
	}

	// Second part should be the threat model ID
	threatModelID := parts[1]
	if threatModelID == "" {
		return ""
	}

	// Third part should be a sub-resource type
	if len(parts) < 3 {
		return ""
	}

	subResource := parts[2]
	validSubResources := []string{"threats", "documents", "sources", "metadata", "diagrams"}

	isValidSubResource := false
	for _, valid := range validSubResources {
		if subResource == valid {
			isValidSubResource = true
			break
		}
	}

	if !isValidSubResource {
		return ""
	}

	return threatModelID
}

// ValidateSubResourceAccessReader creates middleware for read-only sub-resource access
func ValidateSubResourceAccessReader(db *sql.DB, cache *CacheService) gin.HandlerFunc {
	return ValidateSubResourceAccess(db, cache, RoleReader)
}

// ValidateSubResourceAccessWriter creates middleware for write sub-resource access
func ValidateSubResourceAccessWriter(db *sql.DB, cache *CacheService) gin.HandlerFunc {
	return ValidateSubResourceAccess(db, cache, RoleWriter)
}

// ValidateSubResourceAccessOwner creates middleware for owner-only sub-resource access
func ValidateSubResourceAccessOwner(db *sql.DB, cache *CacheService) gin.HandlerFunc {
	return ValidateSubResourceAccess(db, cache, RoleOwner)
}
