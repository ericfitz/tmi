package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"slices"
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
	case LogLevelDebugStr:
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

		// XSS Protection - set to 0 per modern security guidance
		// Modern browsers have built-in XSS filters that can be exploited, and CSP provides better protection
		// Setting to 0 disables legacy XSS auditors which can introduce vulnerabilities
		// Reference: https://cheatsheetseries.owasp.org/cheatsheets/HTTP_Headers_Cheat_Sheet.html
		c.Header("X-XSS-Protection", "0")

		// Content Security Policy
		// Check if we're in development mode (can be set via context from config)
		isDev, exists := c.Get("isDev")
		var cspValue string
		if devMode, ok := isDev.(bool); exists && ok && devMode {
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

// CORS middleware to handle Cross-Origin Resource Sharing.
// In dev mode, any origin is reflected (permissive but spec-correct).
// In production, only origins in allowedOrigins are permitted.
func CORS(allowedOrigins []string, isDev bool) gin.HandlerFunc {
	// Build a set for O(1) lookup
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[strings.TrimRight(o, "/")] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		// Determine if this origin is allowed
		var allowedOrigin string
		if origin != "" {
			normalizedOrigin := strings.TrimRight(origin, "/")
			if isDev {
				// In dev mode, reflect any origin (permissive but correct — not wildcard)
				allowedOrigin = origin
			} else if originSet[normalizedOrigin] {
				allowedOrigin = origin
			}
		}

		if allowedOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Vary", "Origin")
		}

		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, Authorization, Accept, Origin, Cache-Control, Pragma, X-Requested-With")
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

// GetUserRole determines the role of the user for a given threat model.
// Uses SamePrincipal for identity matching via AccessCheckWithGroups.
func GetUserRole(user ResolvedUser, groups []string, threatModel ThreatModel) Role {
	// Build authorization data
	var authSlice []Authorization
	if threatModel.Authorization != nil {
		authSlice = *threatModel.Authorization
	}
	authData := AuthorizationData{
		Type:          AuthTypeTMI10,
		Owner:         threatModel.Owner,
		Authorization: authSlice,
	}

	// Check access with groups support
	// We'll check each role level from highest to lowest to determine user's actual role
	if AccessCheckWithGroups(user, groups, RoleOwner, authData) {
		return RoleOwner
	}
	if AccessCheckWithGroups(user, groups, RoleWriter, authData) {
		return RoleWriter
	}
	if AccessCheckWithGroups(user, groups, RoleReader, authData) {
		return RoleReader
	}

	// Default to no access
	return ""
}

// CheckThreatModelAccess checks if a user has required access to a threat model.
// Uses SamePrincipal for identity matching via AccessCheckWithGroups.
func CheckThreatModelAccess(user ResolvedUser, groups []string, threatModel ThreatModel, requiredRole Role) error {
	// Build authorization data
	var checkAuthSlice []Authorization
	if threatModel.Authorization != nil {
		checkAuthSlice = *threatModel.Authorization
	}
	authData := AuthorizationData{
		Type:          AuthTypeTMI10,
		Owner:         threatModel.Owner,
		Authorization: checkAuthSlice,
	}

	// Check access with groups support
	if AccessCheckWithGroups(user, groups, requiredRole, authData) {
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
		if isPublicVal, exists := c.Get("isPublicPath"); exists {
			if pub, ok := isPublicVal.(bool); ok && pub {
				logger.Debug("ThreatModelMiddleware skipping for public path: %s", c.Request.URL.Path)
				c.Next()
				return
			}
		}

		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Authentication required but userEmail not found in context for path: %s", c.Request.URL.Path)
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "No authentication token provided")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userEmail, ok := userID.(string)
		if !ok || userEmail == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Get user's provider ID, internal UUID, IdP, and groups from context (set by JWT middleware)
		userProviderID, userInternalUUID, userIdP, userGroups := GetUserAuthFieldsForAccessCheck(c)

		// Build ResolvedUser for SamePrincipal-based access checks
		user := ResolvedUser{
			InternalUUID: userInternalUUID,
			Provider:     userIdP,
			ProviderID:   userProviderID,
			Email:        userEmail,
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

		// Safety check: if ThreatModelStore is not initialized, service is unavailable
		if ThreatModelStore == nil {
			logger.Error("ThreatModelStore is not initialized")
			c.Header("Retry-After", "30")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, Error{
				Error:            "service_unavailable",
				ErrorDescription: "Storage service temporarily unavailable - please retry",
			})
			return
		}

		// Detect restore routes - need to look up including deleted entities
		// Patterns: /threat_models/{id}/restore or /threat_models/{id}/{type}/{sub_id}/restore
		isRestoreRoute := c.Request.Method == http.MethodPost && parts[len(parts)-1] == "restore"

		// Load lightweight authorization data (owner + ACL) instead of full threat model
		logger.Debug("ThreatModelMiddleware attempting to get auth data for threat model: %s", id)
		authorization, owner, err := loadMiddlewareAuthData(c.Request.Context(), id, isRestoreRoute)
		if err != nil {
			logger.Debug("Threat model not found: %s, error: %v", id, err)
			c.AbortWithStatusJSON(http.StatusNotFound, Error{
				Error:            "not_found",
				ErrorDescription: "Threat model not found",
			})
			return
		}
		logger.Debug("ThreatModelMiddleware successfully loaded auth data for threat model: %s", id)

		// Determine required role based on HTTP method
		var requiredRole Role

		switch c.Request.Method {
		case http.MethodGet:
			// Any valid role can read
			requiredRole = RoleReader
			logger.Debug("GET request requires Reader role")
		case http.MethodPost:
			if isRestoreRoute {
				// Restore operations require Owner role (same as delete)
				requiredRole = RoleOwner
				logger.Debug("POST restore request requires Owner role")
			} else {
				// POST to sub-resource paths (e.g., /threat_models/{id}/threats) requires Writer role
				requiredRole = RoleWriter
				logger.Debug("POST request requires Writer role for sub-resource creation")
			}
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

		// Build auth data from lightweight query
		authData := AuthorizationData{
			Type:          AuthTypeTMI10,
			Owner:         owner,
			Authorization: authorization,
		}

		// Determine user role from lightweight auth data
		userRole := getUserRoleFromAuthData(user, userGroups, authData)

		// Check authorization without reading request body
		if !AccessCheckWithGroups(user, userGroups, requiredRole, authData) {
			logger.Warn("Access denied for user %s with role %s, required role: %s",
				userEmail, userRole, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		c.Set("userRole", userRole)
		// Note: "threatModel" is no longer set in context. Handlers that need the full
		// model call ThreatModelStore.Get() directly via getExistingThreatModel().

		logger.Debug("Access granted for user %s with role %s", userEmail, userRole)

		// Record access for embedding idle cleanup (#250)
		if GlobalAccessTracker != nil {
			GlobalAccessTracker.RecordAccess(id)
		}

		c.Next()
	}
}

// loadMiddlewareAuthData loads lightweight authorization data for a threat model,
// checking Redis cache first (for non-restore routes) and falling back to the store.
func loadMiddlewareAuthData(ctx context.Context, id string, isRestoreRoute bool) ([]Authorization, User, error) {
	// Try cache first (non-restore routes only)
	if !isRestoreRoute && GlobalCacheService != nil {
		cached, cacheErr := GlobalCacheService.GetCachedMiddlewareAuth(ctx, id)
		if cacheErr == nil && cached != nil {
			return cached.Authorization, cached.Owner, nil
		}
	}

	// Cache miss or restore route — load from store
	var authorization []Authorization
	var owner User
	var err error
	if isRestoreRoute {
		authorization, owner, err = ThreatModelStore.GetAuthorizationIncludingDeleted(id)
	} else {
		authorization, owner, err = ThreatModelStore.GetAuthorization(id)
	}
	if err != nil {
		return nil, User{}, err
	}

	// Cache on miss (non-restore only)
	if !isRestoreRoute && GlobalCacheService != nil {
		_ = GlobalCacheService.CacheMiddlewareAuth(ctx, id, MiddlewareAuthData{
			Owner:         owner,
			Authorization: authorization,
		})
	}

	return authorization, owner, nil
}

// getUserRoleFromAuthData determines the highest role a user has from lightweight auth data.
// This avoids loading the full ThreatModel just to check role membership.
func getUserRoleFromAuthData(user ResolvedUser, groups []string, authData AuthorizationData) Role {
	switch {
	case AccessCheckWithGroups(user, groups, RoleOwner, authData):
		return RoleOwner
	case AccessCheckWithGroups(user, groups, RoleWriter, authData):
		return RoleWriter
	case AccessCheckWithGroups(user, groups, RoleReader, authData):
		return RoleReader
	default:
		return ""
	}
}

// DiagramMiddleware creates middleware for diagram authorization
func DiagramMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := slogging.GetContextLogger(c)

		logger.Debug("DiagramMiddleware processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Skip for public paths
		if isPublicVal, exists := c.Get("isPublicPath"); exists {
			if pub, ok := isPublicVal.(bool); ok && pub {
				logger.Debug("DiagramMiddleware skipping for public path: %s", c.Request.URL.Path)
				c.Next()
				return
			}
		}

		// Get username from the request context - needed for all operations
		userID, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Authentication required but userEmail not found in context for path: %s", c.Request.URL.Path)
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "No authentication token provided")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userEmail, ok := userID.(string)
		if !ok || userEmail == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Get user's provider ID, internal UUID, IdP, and groups from context (set by JWT middleware)
		userProviderID, userInternalUUID, userIdP, userGroups := GetUserAuthFieldsForAccessCheck(c)

		// Build ResolvedUser for SamePrincipal-based access checks
		user := ResolvedUser{
			InternalUUID: userInternalUUID,
			Provider:     userIdP,
			ProviderID:   userProviderID,
			Email:        userEmail,
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
		if err := CheckDiagramAccess(user, userGroups, diagram, requiredRole); err != nil {
			userRole := GetUserRoleForDiagram(user, userGroups, diagram)
			logger.Warn("Access denied for user %s with role %s, required role: %s",
				userEmail, userRole, requiredRole)
			c.AbortWithStatusJSON(http.StatusForbidden, Error{
				Error:            "forbidden",
				ErrorDescription: "You don't have sufficient permissions to perform this action",
			})
			return
		}

		userRole := GetUserRoleForDiagram(user, userGroups, diagram)
		// Set the role and diagram in the context for handlers to use
		c.Set("userRole", userRole)
		c.Set("diagram", diagram)

		logger.Debug("Access granted for user %s with role %s", userEmail, userRole)

		c.Next()
	}
}

// GetUserRoleForDiagram determines the role of the user for a given diagram.
// Uses SamePrincipal for identity matching via GetUserRole.
func GetUserRoleForDiagram(user ResolvedUser, groups []string, diagram DfdDiagram) Role {
	// Diagrams inherit permissions from their parent threat model
	// For database-backed diagrams, we need to find the parent threat model
	// Try to get the diagram from the store and find its parent threat model
	if diagram.Id != nil {
		diagramID := diagram.Id.String()

		// Get the threat model ID for this diagram
		threatModelID, err := DiagramStore.GetThreatModelID(diagramID)
		if err == nil && threatModelID != "" {
			// Get the threat model from the store
			threatModel, err := ThreatModelStore.Get(threatModelID)
			if err == nil {
				// Use group-based authorization check
				return GetUserRole(user, groups, threatModel)
			}
		}
	}

	// Fallback to TestFixtures for non-database stores
	parentThreatModel := TestFixtures.ThreatModel

	// Use group-based authorization check
	return GetUserRole(user, groups, parentThreatModel)
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
	switch {
	case err != nil:
		logger.Debug("%s - Error reading body: %v", prefix, err)
	case len(bodyBytes) > 0:
		logger.Debug("%s - Body: %s", prefix, html.EscapeString(string(bodyBytes)))
		// Reset the body for later use
		c.Request.Body = NewReadCloser(bodyBytes)
	default:
		logger.Debug("%s - Empty body", prefix)
	}
}

// CheckDiagramAccess checks if a user has required access to a diagram.
// Uses SamePrincipal for identity matching via GetUserRoleForDiagram.
func CheckDiagramAccess(user ResolvedUser, groups []string, diagram DfdDiagram, requiredRole Role) error {
	userRole := GetUserRoleForDiagram(user, groups, diagram)

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
		if isPublicVal, exists := c.Get("isPublicPath"); exists {
			if pub, ok := isPublicVal.(bool); ok && pub {
				logger.Debug("ValidateSubResourceAccess skipping for public path: %s", c.Request.URL.Path)
				c.Next()
				return
			}
		}

		// Get username from the request context
		userID, exists := c.Get("userEmail")
		if !exists {
			logger.Warn("Authentication required but userEmail not found in context for path: %s", c.Request.URL.Path)
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "No authentication token provided")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Authentication required",
			})
			return
		}

		userEmail, ok := userID.(string)
		if !ok || userEmail == "" {
			logger.Warn("Invalid authentication, userName is empty or not a string for path: %s", c.Request.URL.Path)
			SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
			c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid authentication",
			})
			return
		}

		// Get user's provider ID, internal UUID, IdP, and groups from context (set by JWT middleware)
		userProviderID, userInternalUUID, userIdP, userGroups := GetUserAuthFieldsForAccessCheck(c)

		// Build ResolvedUser for SamePrincipal-based access checks
		user := ResolvedUser{
			InternalUUID: userInternalUUID,
			Provider:     userIdP,
			ProviderID:   userProviderID,
			Email:        userEmail,
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
		hasAccess, err := CheckSubResourceAccess(c.Request.Context(), db, cache, user, userGroups, threatModelID, requiredRole)
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

	isValidSubResource := slices.Contains(validSubResources, subResource)

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

// bufferedResponseWriter wraps gin.ResponseWriter to buffer responses
// This allows us to intercept and transform plain text error responses to JSON
type bufferedResponseWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

func (w *bufferedResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

// Status returns the buffered status code so that inner middleware reading
// c.Writer.Status() after c.Next() observe the handler's intended status —
// not the embedded writer's default 200, which holds until JSONErrorHandler
// flushes the buffered response. See issue #289.
func (w *bufferedResponseWriter) Status() int {
	return w.statusCode
}

// WriteHeaderNow forces the buffered status onto the underlying writer.
// Without this override, calls to WriteHeaderNow would fall through to the
// embedded gin.ResponseWriter and commit its default status (200) instead of
// the status the handler asked for via c.Status / c.JSON.
func (w *bufferedResponseWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeader(w.statusCode)
	w.ResponseWriter.WriteHeaderNow()
}

// JSONErrorHandler middleware converts plain text error responses to JSON format
// This catches Gin framework errors that bypass application error handling
// It uses a buffered response writer to intercept responses before they're sent
func JSONErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a buffered response writer
		blw := &bufferedResponseWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBufferString(""),
			statusCode:     http.StatusOK,
		}
		c.Writer = blw

		// Process the request
		c.Next()

		// Get the response details
		statusCode := blw.statusCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		contentType := blw.Header().Get("Content-Type")
		bodyContent := blw.body.String()

		// Check if this is a plain text error response that needs conversion
		if statusCode >= 400 && (contentType == "" || strings.Contains(contentType, "text/plain")) {
			// Convert to JSON format
			blw.Header().Set("Content-Type", "application/json; charset=utf-8")
			blw.Header().Set("Cache-Control", "no-store")

			// Create proper error response
			errorResponse := Error{
				Error:            http.StatusText(statusCode),
				ErrorDescription: "The request could not be processed",
			}

			// If the original body contains useful info, try to include it
			if bodyContent != "" && bodyContent != http.StatusText(statusCode) {
				errorResponse.ErrorDescription = strings.TrimSpace(bodyContent)
			}

			// Marshal the JSON response
			jsonBody, err := json.Marshal(errorResponse)
			if err != nil {
				// Fallback if JSON marshaling fails
				jsonBody = []byte(`{"error":"` + http.StatusText(statusCode) + `","error_description":"The request could not be processed"}`)
			}

			// Write the transformed response
			blw.ResponseWriter.WriteHeader(statusCode)
			_, _ = blw.ResponseWriter.Write(jsonBody)
		} else {
			// Pass through the original response unchanged
			blw.ResponseWriter.WriteHeader(statusCode)
			_, _ = blw.ResponseWriter.Write(blw.body.Bytes())
		}
	}
}

// AcceptHeaderValidation middleware validates that the Accept header is application/json
// Returns 406 Not Acceptable for unsupported media types
func AcceptHeaderValidation() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := slogging.GetContextLogger(c)

		// Skip validation for OPTIONS requests (CORS preflight)
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}

		// Get Accept header
		acceptHeader := c.GetHeader("Accept")

		// If no Accept header, default to */* which we'll treat as acceptable
		if acceptHeader == "" {
			c.Next()
			return
		}

		// Check if Accept header includes a supported media type
		// We're being lenient and accepting quality parameters
		acceptsSupported := strings.Contains(acceptHeader, "application/json") ||
			strings.Contains(acceptHeader, "*/*") ||
			strings.Contains(acceptHeader, "application/*") ||
			strings.Contains(acceptHeader, "text/event-stream")

		if !acceptsSupported {
			logger.Debug("Rejecting request with unsupported Accept header: %s", acceptHeader)
			c.AbortWithStatusJSON(http.StatusNotAcceptable, Error{
				Error:            "not_acceptable",
				ErrorDescription: "The requested Accept header media type is not supported. Supported types: application/json, text/event-stream",
			})
			return
		}

		c.Next()
	}
}
