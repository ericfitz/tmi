package api

import (
	"context"
	"database/sql"
	"errors"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
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

// SecurityHeaders middleware adds security headers to all responses
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prevent MIME type sniffing
		c.Header("X-Content-Type-Options", "nosniff")

		// Prevent clickjacking attacks
		c.Header("X-Frame-Options", "DENY")

		// Enable the browser's built-in XSS filter (for older browsers)
		c.Header("X-XSS-Protection", "1; mode=block")

		// Content Security Policy
		// Check if we're in development mode (can be set via context from config)
		isDev, exists := c.Get("isDev")
		var cspValue string
		if exists && isDev.(bool) {
			// Development CSP - more permissive, allows localhost connections
			cspValue = "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https: http://localhost:*; font-src 'self'; connect-src 'self' http://localhost:* https://localhost:* http://127.0.0.1:* https://127.0.0.1:* wss: ws:;"
		} else {
			// Production CSP - more restrictive
			cspValue = "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' wss: ws:;"
		}
		c.Header("Content-Security-Policy", cspValue)

		// Referrer Policy
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

		// Cache-Control - Prevent caching of sensitive API responses
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate")

		// Permissions Policy (replaces Feature-Policy)
		c.Header("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		c.Next()
	}
}

// HSTSMiddleware adds Strict-Transport-Security header when TLS is enabled
func HSTSMiddleware(tlsEnabled bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tlsEnabled {
			// HSTS with 1 year max-age, includeSubDomains
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
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

// Role represents a user role with permission levels
type Role = AuthorizationRole

const (
	// RoleOwner has full control over the resource
	RoleOwner Role = AuthorizationRoleOwner
	// RoleWriter can edit but not delete or change ownership
	RoleWriter Role = AuthorizationRoleWriter
	// RoleReader can only view the resource
	RoleReader Role = AuthorizationRoleReader
)

// ErrAccessDenied indicates an authorization failure
var ErrAccessDenied = errors.New("access denied")

// GetUserRole determines the role of the user for a given threat model
// This now supports both user and group authorization with IdP scoping
func GetUserRole(userEmail string, userProviderID string, userInternalUUID string, userIdP string, userGroups []string, threatModel ThreatModel) Role {
	// Build authorization data
	authData := AuthorizationData{
		Type:          AuthTypeTMI10,
		Owner:         threatModel.Owner,
		Authorization: threatModel.Authorization,
	}

	// Check access with groups support
	// We'll check each role level from highest to lowest to determine user's actual role
	if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleOwner, authData) {
		return RoleOwner
	}
	if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleWriter, authData) {
		return RoleWriter
	}
	if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, RoleReader, authData) {
		return RoleReader
	}

	// Default to no access
	return ""
}

// CheckThreatModelAccess checks if a user has required access to a threat model
// This now supports both user and group authorization with IdP scoping
func CheckThreatModelAccess(userEmail string, userProviderID string, userInternalUUID string, userIdP string, userGroups []string, threatModel ThreatModel, requiredRole Role) error {
	// Build authorization data
	authData := AuthorizationData{
		Type:          AuthTypeTMI10,
		Owner:         threatModel.Owner,
		Authorization: threatModel.Authorization,
	}

	// Check access with groups support
	if AccessCheckWithGroups(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, requiredRole, authData) {
		return nil
	}

	return ErrAccessDenied
}

// ThreatModelMiddleware creates middleware for threat model authorization
func ThreatModelMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := slogging.GetContextLogger(c)

		logger.Debug("ThreatModelMiddleware processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("ThreatModelMiddleware skipping for public path: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Authentication required but userEmail not found in context for path: %s", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userEmail, ok := userID.(string)
		if !ok || userEmail == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Get user's provider ID, internal UUID, IdP, and groups from context (set by JWT middleware)
		userProviderID := ""
		if providerID, exists := c.Get("userID"); exists {
			userProviderID, _ = providerID.(string)
		}

		userInternalUUID := ""
		if internalUUID, exists := c.Get("userInternalUUID"); exists {
			userInternalUUID, _ = internalUUID.(string)
		}

		userIdP := ""
		if idp, exists := c.Get("userIdP"); exists {
			userIdP, _ = idp.(string)
		}

		var userGroups []string
		if groups, exists := c.Get("userGroups"); exists {
			userGroups, _ = groups.([]string)
		}

		// For POST to collection endpoint (create new threat model), any authenticated user can proceed
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/threat_models" {
			logger.Debug("Allowing create operation for authenticated user: %s", userEmail)
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

		// Skip for collaboration endpoints, they have their own access control
		// Path pattern: /threat_models/{id}/diagrams/{diagram_id}/collaborate
		if len(parts) >= 5 && parts[3] == "diagrams" && len(parts) >= 6 && parts[5] == "collaborate" {
			logger.Debug("Skipping auth check for collaboration endpoint")
			c.Next()
			return
		}

		// Safety check: if ThreatModelStore is not initialized, skip validation
		if ThreatModelStore == nil {
			logger.Error("ThreatModelStore is not initialized")
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Storage not available",
			})
			return
		}

		// Get the threat model from storage
		logger.Debug("ThreatModelMiddleware attempting to get threat model with ID: %s", id)
		threatModel, err := ThreatModelStore.Get(id)
		if err != nil {
			logger.Debug("Threat model not found: %s, error: %v", id, err)
			// Return 404 instead of letting handler deal with it
			c.AbortWithStatusJSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Threat model not found",
			})
			return
		}
		logger.Debug("ThreatModelMiddleware successfully found threat model: %s", id)

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
		if err := CheckThreatModelAccess(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, threatModel, requiredRole); err != nil {
			userRole := GetUserRole(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, threatModel)
			logger.Warn("Access denied for user %s with role %s, required role: %s",
				userEmail, userRole, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		userRole := GetUserRole(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, threatModel)
		// Set the role and threatModel in the context for handlers to use
		c.Set("userRole", userRole)
		c.Set("threatModel", threatModel)

		logger.Debug("Access granted for user %s with role %s", userEmail, userRole)

		c.Next()
	}
}

// DiagramMiddleware creates middleware for diagram authorization
func DiagramMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := slogging.GetContextLogger(c)

		logger.Debug("DiagramMiddleware processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("DiagramMiddleware skipping for public path: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Authentication required but userEmail not found in context for path: %s", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userEmail, ok := userID.(string)
		if !ok || userEmail == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Get user's provider ID, internal UUID, IdP, and groups from context (set by JWT middleware)
		userProviderID := ""
		if providerID, exists := c.Get("userID"); exists {
			userProviderID, _ = providerID.(string)
		}

		userInternalUUID := ""
		if internalUUID, exists := c.Get("userInternalUUID"); exists {
			userInternalUUID, _ = internalUUID.(string)
		}

		userIdP := ""
		if idp, exists := c.Get("userIdP"); exists {
			userIdP, _ = idp.(string)
		}

		var userGroups []string
		if groups, exists := c.Get("userGroups"); exists {
			userGroups, _ = groups.([]string)
		}

		// For POST to collection endpoint (create new diagram), any authenticated user can proceed
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/diagrams" {
			logger.Debug("Allowing create operation for authenticated user: %s", userEmail)
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
		if err := CheckDiagramAccess(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, diagram, requiredRole); err != nil {
			userRole := GetUserRoleForDiagram(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, diagram)
			logger.Warn("Access denied for user %s with role %s, required role: %s",
				userEmail, userRole, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		userRole := GetUserRoleForDiagram(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, diagram)
		// Set the role and diagram in the context for handlers to use
		c.Set("userRole", userRole)
		c.Set("diagram", diagram)

		logger.Debug("Access granted for user %s with role %s", userEmail, userRole)

		c.Next()
	}
}

// GetUserRoleForDiagram determines the role of the user for a given diagram
// This now supports both user and group authorization with IdP scoping and flexible user matching
func GetUserRoleForDiagram(userEmail string, userProviderID string, userInternalUUID string, userIdP string, userGroups []string, diagram DfdDiagram) Role {
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

				// Use group-based authorization check
				return GetUserRole(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, threatModel)
			}
		}
	}

	// Fallback to TestFixtures for non-database stores
	parentThreatModel := TestFixtures.ThreatModel

	// Use group-based authorization check
	return GetUserRole(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, parentThreatModel)
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
	logger := slogging.GetContextLogger(c)

	logger.Debug("%s - Method: %s, Path: %s", prefix, c.Request.Method, c.Request.URL.Path)

	// Log headers as structured data on same line
	logger.Debug("%s - Headers: %v", prefix, slogging.RedactHeaders(c.Request.Header))

	// Try to log body
	bodyBytes, err := c.GetRawData()
	if err != nil {
		logger.Debug("%s - Error reading body: %v", prefix, err)
	} else if len(bodyBytes) > 0 {
		logger.Debug("%s - Body: %s", prefix, html.EscapeString(string(bodyBytes)))
		// Reset the body for later use
		c.Request.Body = NewReadCloser(bodyBytes)
	} else {
		logger.Debug("%s - Empty body", prefix)
	}
}

// CheckDiagramAccess checks if a user has required access to a diagram
// This now supports both user and group authorization with IdP scoping and flexible user matching
func CheckDiagramAccess(userEmail string, userProviderID string, userInternalUUID string, userIdP string, userGroups []string, diagram DfdDiagram, requiredRole Role) error {
	userRole := GetUserRoleForDiagram(userEmail, userProviderID, userInternalUUID, userIdP, userGroups, diagram)

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
		logger := slogging.GetContextLogger(c)
		logger.Debug("ValidateSubResourceAccess processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("ValidateSubResourceAccess skipping for public path: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Get username from the request context
		userID, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Authentication required but userEmail not found in context for path: %s", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userEmail, ok := userID.(string)
		if !ok || userEmail == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			c.Header("WWW-Authenticate", "Bearer")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Get user's provider ID, internal UUID, IdP, and groups from context (set by JWT middleware)
		userProviderID := ""
		if providerID, exists := c.Get("userID"); exists {
			userProviderID, _ = providerID.(string)
		}

		userInternalUUID := ""
		if internalUUID, exists := c.Get("userInternalUUID"); exists {
			userInternalUUID, _ = internalUUID.(string)
		}

		userIdP := ""
		if idp, exists := c.Get("userIdP"); exists {
			userIdP, _ = idp.(string)
		}

		var userGroups []string
		if groups, exists := c.Get("userGroups"); exists {
			userGroups, _ = groups.([]string)
		}

		// Extract threat model ID from the path
		// Sub-resource paths typically follow patterns like:
		// /threat_models/{threat_model_id}/threats/{threat_id}
		// /threat_models/{threat_model_id}/documents/{doc_id}
		// /threat_models/{threat_model_id}/sources/{source_id}
		threatModelID := extractThreatModelIDFromPath(c.Request.URL.Path)
		if threatModelID == "" {
			logger.Debug("No threat model ID found in path, skipping sub-resource auth: %s", c.Request.URL.Path)
			c.Next()
			return
		}

		// Check sub-resource access using inherited authorization with group support
		hasAccess, err := CheckSubResourceAccess(c.Request.Context(), db, cache, userEmail, userProviderID, userInternalUUID, userIdP, userGroups, threatModelID, requiredRole)
		if err != nil {
			logger.Error("Failed to check sub-resource access for user %s on threat model %s: %v",
				userEmail, threatModelID, err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Failed to validate permissions",
			})
			return
		}

		if !hasAccess {
			logger.Warn("Access denied for user %s on threat model %s (required role: %s)",
				userEmail, threatModelID, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		// Set the threat model ID in context for handlers to use
		c.Set("threatModelID", threatModelID)
		c.Set("userRole", requiredRole) // The actual role could be higher

		logger.Debug("Sub-resource access granted for user %s on threat model %s", userEmail, threatModelID)
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

// JSONErrorHandler middleware converts plain text error responses to JSON format
// This catches Gin framework errors that bypass application error handling
func JSONErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// After handlers execute, check if we have a plain text error response
		status := c.Writer.Status()
		if status >= 400 {
			contentType := c.Writer.Header().Get("Content-Type")

			// If no content type set or it's plain text, ensure JSON format
			if contentType == "" || strings.Contains(contentType, "text/plain") {
				// Set proper headers
				c.Header("Content-Type", "application/json; charset=utf-8")
				c.Header("Cache-Control", "no-store")

				// Use standard error response format
				c.JSON(status, Error{
					Error:            http.StatusText(status),
					ErrorDescription: "The request could not be processed",
				})
			}
		}
	}
}
