package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/api"  // Your module path
	"github.com/ericfitz/tmi/auth" // Import auth package
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// Server holds dependencies for the API server
type Server struct {
	// Configuration
	config *config.Config

	// Token blacklist for logout functionality
	tokenBlacklist *auth.TokenBlacklist

	// Auth handlers for JWT verification
	authHandlers *auth.Handlers

	// API server instance with WebSocket hub
	apiServer *api.Server

	// Add other dependencies like database clients, services, etc.
}

// verifyJWTToken verifies a JWT token using the centralized auth service
func (s *Server) verifyJWTToken(tokenString string) (*jwt.Token, jwt.MapClaims, error) {
	if s.authHandlers == nil {
		return nil, nil, fmt.Errorf("auth handlers not available")
	}

	// Use the auth service's key manager for verification
	claims := jwt.MapClaims{}
	token, err := s.authHandlers.Service().GetKeyManager().VerifyToken(tokenString, claims)
	if err != nil {
		return nil, nil, err
	}

	if !token.Valid {
		return nil, nil, fmt.Errorf("token is not valid")
	}

	return token, claims, nil
}

// HTTPSRedirectMiddleware redirects HTTP requests to HTTPS when TLS is enabled
func HTTPSRedirectMiddleware(tlsEnabled bool, tlsSubjectName string, port string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := slogging.GetContextLogger(c)

		// Only redirect if TLS is enabled and this is not already HTTPS
		// In a real environment, we'd check c.Request.TLS, but in our setup,
		// we need to rely on a header or other mechanism to determine if we're already on HTTPS
		if tlsEnabled && !isHTTPS(c.Request) {
			host := c.Request.Host

			// If we have a specific subject name, use it
			if tlsSubjectName != "" {
				if port != "443" {
					host = fmt.Sprintf("%s:%s", tlsSubjectName, port)
				} else {
					host = tlsSubjectName
				}
			}

			redirectURL := fmt.Sprintf("https://%s%s", host, c.Request.RequestURI)
			logger.Debug("Redirecting to HTTPS: %s", redirectURL)
			c.Redirect(http.StatusPermanentRedirect, redirectURL)
			c.Abort()
			return
		}
		c.Next()
	}
}

// isHTTPS determines if the request is already using HTTPS
func isHTTPS(r *http.Request) bool {
	// Check common headers set by proxies
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}

	// Check if the request was made with TLS
	if r.TLS != nil {
		return true
	}

	// Check if the request came in on the standard HTTPS port
	if r.URL.Scheme == "https" {
		return true
	}

	return false
}

// PublicPathsMiddleware identifies paths that don't require authentication
func PublicPathsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get a context-aware logger
		logger := slogging.GetContextLogger(c)

		// Log entry to middleware
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Processing request: %s %s", c.Request.Method, c.Request.URL.Path)
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Full URL: %s", c.Request.URL.String())
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Query params: %s", c.Request.URL.RawQuery)

		// Public paths that don't require authentication
		isPublic := c.Request.URL.Path == "/" ||
			c.Request.URL.Path == "/api/server-info" ||
			c.Request.URL.Path == "/oauth2/callback" ||
			c.Request.URL.Path == "/oauth2/providers" ||
			c.Request.URL.Path == "/oauth2/refresh" ||
			c.Request.URL.Path == "/oauth2/authorize" ||
			strings.HasPrefix(c.Request.URL.Path, "/oauth2/token") ||
			c.Request.URL.Path == "/oauth2/revoke" ||
			c.Request.URL.Path == "/robots.txt" ||
			c.Request.URL.Path == "/site.webmanifest" ||
			c.Request.URL.Path == "/favicon.ico" ||
			c.Request.URL.Path == "/favicon.svg" ||
			c.Request.URL.Path == "/web-app-manifest-192x192.png" ||
			c.Request.URL.Path == "/web-app-manifest-512x512.png" ||
			c.Request.URL.Path == "/TMI-Logo.svg" ||
			c.Request.URL.Path == "/android-chrome-192x192.png" ||
			c.Request.URL.Path == "/android-chrome-512x512.png" ||
			c.Request.URL.Path == "/apple-touch-icon.png" ||
			c.Request.URL.Path == "/favicon-16x16.png" ||
			c.Request.URL.Path == "/favicon-32x32.png" ||
			c.Request.URL.Path == "/favicon-96x96.png" ||
			c.Request.URL.Path == "/.well-known/openid-configuration" ||
			c.Request.URL.Path == "/.well-known/oauth-authorization-server" ||
			c.Request.URL.Path == "/.well-known/jwks.json" ||
			c.Request.URL.Path == "/saml/metadata" ||
			c.Request.URL.Path == "/saml/acs" ||
			c.Request.URL.Path == "/saml/slo" ||
			c.Request.URL.Path == "/saml/login"

		if isPublic {
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] ✅ Public path identified: %s", c.Request.URL.Path)
			// Mark this request as public in the context for downstream middleware
			c.Set("isPublicPath", true)
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Set isPublicPath=true in context")
		} else {
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] ❌ Private path identified: %s", c.Request.URL.Path)
			logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] isPublicPath not set (defaults to false)")
		}

		// Log exit from middleware
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Continuing to next middleware")

		// Always continue to next middleware
		c.Next()

		// Log completion
		logger.Debug("[PUBLIC_PATHS_MIDDLEWARE] Middleware chain completed for: %s", c.Request.URL.Path)
	}
}

// JWT Middleware factory function that takes config, token blacklist, and auth handlers
func JWTMiddleware(cfg *config.Config, tokenBlacklist *auth.TokenBlacklist, authHandlers *auth.Handlers) gin.HandlerFunc {
	// Initialize authentication components
	publicPathChecker := &PublicPathChecker{}
	authenticator := NewJWTAuthenticator(cfg, tokenBlacklist, authHandlers)

	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)

		// Log entry to middleware
		logger.Debug("[JWT_MIDDLEWARE] *** ENTERED MIDDLEWARE FOR: %s", c.Request.URL.Path)
		logger.Debug("[JWT_MIDDLEWARE] Processing request: %s %s", c.Request.Method, c.Request.URL.Path)

		// Check if this is a public path
		if publicPathChecker.IsPublicPath(c) {
			logger.Debug("[JWT_MIDDLEWARE] Continuing to next middleware (public path)")
			c.Next()
			logger.Debug("[JWT_MIDDLEWARE] Returned from middleware chain (public path)")
			return
		}

		// Perform authentication
		if err := authenticator.AuthenticateRequest(c); err != nil {
			if authErr, ok := err.(*AuthError); ok {
				logger.Debug("[JWT_MIDDLEWARE] Authentication failed: %v", err)
				c.JSON(authErr.StatusCode, api.Error{
					Error:            authErr.Code,
					ErrorDescription: authErr.Description,
				})
				c.Abort()
				return
			}

			// Fallback for unexpected errors
			logger.Error("[JWT_MIDDLEWARE] Unexpected authentication error: %v", err)
			c.JSON(http.StatusInternalServerError, api.Error{
				Error:            "server_error",
				ErrorDescription: "Internal authentication error",
			})
			c.Abort()
			return
		}

		logger.Debug("[JWT_MIDDLEWARE] Authentication successful, proceeding to next middleware")
		c.Next()
	}
}

func (s *Server) GetApiInfo(c *gin.Context) {
	// Create API server to provide WebSocket URL building functionality
	// Use minimal logging config since this is just for API info
	wsLoggingConfig := slogging.WebSocketLoggingConfig{
		Enabled:        false, // No WebSocket activity in API info endpoint
		RedactTokens:   true,
		MaxMessageSize: 5 * 1024,
		OnlyDebugLevel: true,
	}
	apiServer := api.NewServer(wsLoggingConfig, 5*time.Minute) // Default timeout for API info
	apiInfoHandler := api.NewApiInfoHandler(apiServer)
	apiInfoHandler.GetApiInfo(c)
}

func (s *Server) GetAuthCallback(c *gin.Context) {
	// Get logger from context
	logger := slogging.GetContextLogger(c)

	// In dev mode, generate a token based on the provided parameters
	username := c.Query("username")
	if username == "" {
		username = "user@example.com"
	}

	role := c.Query("role")
	logger.Debug("Generating dev token for user %s with role %s", username, role)

	// Add additional claims for development
	claims := jwt.MapClaims{
		"sub": username,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	// Add role if specified
	if role != "" {
		claims["role"] = role
	}

	// Sign the token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	jwtSecret := []byte(s.config.Auth.JWT.Secret)
	tokenStr, err := token.SignedString(jwtSecret)

	if err != nil {
		logger.Error("Failed to sign JWT token: %v", err)
		c.JSON(http.StatusInternalServerError, api.Error{
			Error:            "server_error",
			ErrorDescription: "Failed to generate authentication token",
		})
		return
	}

	// Return token response
	c.JSON(http.StatusOK, gin.H{
		"token":      tokenStr,
		"expires_in": 86400, // 24 hours
		"user":       username,
		"role":       role,
	})
}

func (s *Server) PostAuthLogout(c *gin.Context) {
	logger := slogging.GetContextLogger(c)

	// Get the JWT token from the Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		logger.Warn("Logout attempted without Authorization header")
		c.JSON(http.StatusUnauthorized, api.Error{
			Error:            "unauthorized",
			ErrorDescription: "Missing Authorization header",
		})
		return
	}

	// Parse the header format
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		logger.Warn("Logout attempted with invalid Authorization header format")
		c.JSON(http.StatusUnauthorized, api.Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid Authorization header format",
		})
		return
	}

	tokenStr := parts[1]

	// Validate token format and signature before attempting to blacklist
	// Use centralized JWT verification
	_, _, err := s.verifyJWTToken(tokenStr)
	if err != nil {
		logger.Warn("Logout attempted with invalid token: %v", err)
		c.JSON(http.StatusUnauthorized, api.Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid or malformed token",
		})
		return
	}

	// Blacklist the token if blacklist service is available
	if s.tokenBlacklist != nil {
		if err := s.tokenBlacklist.BlacklistToken(c.Request.Context(), tokenStr); err != nil {
			logger.Error("Failed to blacklist token: %v", err)
			c.JSON(http.StatusInternalServerError, api.Error{
				Error:            "server_error",
				ErrorDescription: "Failed to logout",
			})
			return
		}
		logger.Info("Token successfully blacklisted for user logout")
	} else {
		logger.Warn("Token blacklist service not available - logout will not invalidate token")
	}

	c.Status(http.StatusNoContent)
}

// LogoutUser implements the API interface for logout
func (s *Server) LogoutUser(c *gin.Context) {
	// Extract the token from the Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, api.Error{
			Error:            "unauthorized",
			ErrorDescription: "Missing Authorization header",
		})
		return
	}

	// Parse the header format
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		c.JSON(http.StatusUnauthorized, api.Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid Authorization header format",
		})
		return
	}

	tokenStr := parts[1]

	// Validate token format and signature before attempting to blacklist
	// Use centralized JWT verification
	_, _, err := s.verifyJWTToken(tokenStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, api.Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid or malformed token",
		})
		return
	}

	// Blacklist the token if blacklist service is available
	if s.tokenBlacklist != nil {
		if err := s.tokenBlacklist.BlacklistToken(c.Request.Context(), tokenStr); err != nil {
			c.JSON(http.StatusInternalServerError, api.Error{
				Error:            "server_error",
				ErrorDescription: "Failed to logout",
			})
			return
		}
	}

	c.Status(http.StatusNoContent)
}

// Dev-mode only endpoint to get current user info
func DevUserInfoHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)
		logger.Debug("Handling /dev/me request")

		// Get username from context
		userID, exists := c.Get("userName")
		if !exists {
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Not authenticated",
			})
			return
		}

		userName, ok := userID.(string)
		if !ok || userName == "" {
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid user context",
			})
			return
		}

		// Get role from token if available
		role := "unknown"
		if tokenRole, exists := c.Get("userTokenRole"); exists {
			if r, ok := tokenRole.(string); ok {
				role = r
			}
		}

		// Return user info
		c.JSON(http.StatusOK, gin.H{
			"user":          userName,
			"role":          role,
			"authenticated": true,
		})
	}
}

func (s *Server) GetThreatModels(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler(s.apiServer.GetWebSocketHub())
	threatModelHandler.GetThreatModels(c)
}

func (s *Server) PostThreatModels(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler(s.apiServer.GetWebSocketHub())
	threatModelHandler.CreateThreatModel(c)
}

func (s *Server) GetThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler(s.apiServer.GetWebSocketHub())
	threatModelHandler.GetThreatModelByID(c)
}

func (s *Server) PutThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler(s.apiServer.GetWebSocketHub())
	threatModelHandler.UpdateThreatModel(c)
}

func (s *Server) PatchThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler(s.apiServer.GetWebSocketHub())
	threatModelHandler.PatchThreatModel(c)
}

func (s *Server) DeleteThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler(s.apiServer.GetWebSocketHub())
	threatModelHandler.DeleteThreatModel(c)
}

// Threat Model Metadata handlers
func (s *Server) GetThreatModelsIdMetadata(c *gin.Context) {
	handler := api.NewThreatModelMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetThreatModelMetadata(c)
}

func (s *Server) PostThreatModelsIdMetadata(c *gin.Context) {
	handler := api.NewThreatModelMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.CreateThreatModelMetadata(c)
}

func (s *Server) GetThreatModelsIdMetadataKey(c *gin.Context) {
	handler := api.NewThreatModelMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetThreatModelMetadataByKey(c)
}

func (s *Server) PutThreatModelsIdMetadataKey(c *gin.Context) {
	handler := api.NewThreatModelMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.UpdateThreatModelMetadata(c)
}

func (s *Server) DeleteThreatModelsIdMetadataKey(c *gin.Context) {
	handler := api.NewThreatModelMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.DeleteThreatModelMetadata(c)
}

func (s *Server) PostThreatModelsIdMetadataBulk(c *gin.Context) {
	handler := api.NewThreatModelMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.BulkCreateThreatModelMetadata(c)
}

// Threat Model Diagrams
func (s *Server) GetThreatModelsThreatModelIdDiagrams(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.GetDiagrams(c, threatModelId)
}

func (s *Server) PostThreatModelsThreatModelIdDiagrams(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.CreateDiagram(c, threatModelId)
}

func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.GetDiagramByID(c, threatModelId, diagramId)
}

func (s *Server) PutThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.UpdateDiagram(c, threatModelId, diagramId)
}

func (s *Server) PatchThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.PatchDiagram(c, threatModelId, diagramId)
}

func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.DeleteDiagram(c, threatModelId, diagramId)
}

// Threat Model Diagram Collaboration
func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.GetDiagramCollaborate(c, threatModelId, diagramId)
}

func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.CreateDiagramCollaborate(c, threatModelId, diagramId)
}

func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("threat_model_id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler(s.apiServer.GetWebSocketHub())
	handler.DeleteDiagramCollaborate(c, threatModelId, diagramId)
}

// Diagram Metadata handlers

// Threat Model Diagram Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdMetadata(c *gin.Context) {
	// This endpoint is for threat model diagrams - need to implement specific handler
	c.JSON(501, gin.H{"error": "threat model diagram metadata not yet implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdMetadata(c *gin.Context) {
	// This endpoint is for threat model diagrams - need to implement specific handler
	c.JSON(501, gin.H{"error": "threat model diagram metadata not yet implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context) {
	// This endpoint is for threat model diagrams - need to implement specific handler
	c.JSON(501, gin.H{"error": "threat model diagram metadata not yet implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context) {
	// This endpoint is for threat model diagrams - need to implement specific handler
	c.JSON(501, gin.H{"error": "threat model diagram metadata not yet implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context) {
	// This endpoint is for threat model diagrams - need to implement specific handler
	c.JSON(501, gin.H{"error": "threat model diagram metadata not yet implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdMetadataBulk(c *gin.Context) {
	handler := api.NewDiagramMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.BulkCreateThreatModelDiagramMetadata(c)
}

// Threat Model Threats handlers
func (s *Server) GetThreatModelsThreatModelIdThreats(c *gin.Context) {
	// Use the dedicated threat handler with global store
	handler := api.NewThreatSubResourceHandler(
		api.GlobalThreatStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.GetThreats(c)
}

func (s *Server) PostThreatModelsThreatModelIdThreats(c *gin.Context) {
	// Use the dedicated threat handler with global store
	handler := api.NewThreatSubResourceHandler(
		api.GlobalThreatStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.CreateThreat(c)
}

func (s *Server) GetThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	// Use the dedicated threat handler with global store
	handler := api.NewThreatSubResourceHandler(
		api.GlobalThreatStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.GetThreat(c)
}

func (s *Server) PutThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	// Use the dedicated threat handler with global store
	handler := api.NewThreatSubResourceHandler(
		api.GlobalThreatStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.UpdateThreat(c)
}

func (s *Server) PatchThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	// Use the dedicated threat handler with global store
	handler := api.NewThreatSubResourceHandler(
		api.GlobalThreatStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // invalidator - not needed for current implementation
	)
	handler.PatchThreat(c)
}

func (s *Server) DeleteThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	// Use the dedicated threat handler with global store
	handler := api.NewThreatSubResourceHandler(
		api.GlobalThreatStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.DeleteThreat(c)
}

func (s *Server) PostThreatModelsThreatModelIdThreatsBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdThreatsBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Threat Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdThreatsThreatIdMetadata(c *gin.Context) {
	handler := api.NewThreatMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetThreatMetadata(c)
}

func (s *Server) PostThreatModelsThreatModelIdThreatsThreatIdMetadata(c *gin.Context) {
	handler := api.NewThreatMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.CreateThreatMetadata(c)
}

func (s *Server) GetThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context) {
	handler := api.NewThreatMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetThreatMetadataByKey(c)
}

func (s *Server) PutThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context) {
	handler := api.NewThreatMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.UpdateThreatMetadata(c)
}

func (s *Server) DeleteThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context) {
	handler := api.NewThreatMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.DeleteThreatMetadata(c)
}

func (s *Server) PostThreatModelsThreatModelIdThreatsThreatIdMetadataBulk(c *gin.Context) {
	handler := api.NewThreatMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.BulkCreateThreatMetadata(c)
}

// Threat Model Documents handlers
func (s *Server) GetThreatModelsThreatModelIdDocuments(c *gin.Context) {
	// Use the dedicated document handler with global store
	handler := api.NewDocumentSubResourceHandler(
		api.GlobalDocumentStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.GetDocuments(c)
}

func (s *Server) PostThreatModelsThreatModelIdDocuments(c *gin.Context) {
	// Use the dedicated document handler with global store
	handler := api.NewDocumentSubResourceHandler(
		api.GlobalDocumentStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.CreateDocument(c)
}

func (s *Server) GetThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context) {
	// Use the dedicated document handler with global store
	handler := api.NewDocumentSubResourceHandler(
		api.GlobalDocumentStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.GetDocument(c)
}

func (s *Server) PutThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context) {
	// Use the dedicated document handler with global store
	handler := api.NewDocumentSubResourceHandler(
		api.GlobalDocumentStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.UpdateDocument(c)
}

func (s *Server) DeleteThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context) {
	// Use the dedicated document handler with global store
	handler := api.NewDocumentSubResourceHandler(
		api.GlobalDocumentStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.DeleteDocument(c)
}

func (s *Server) PostThreatModelsThreatModelIdDocumentsBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Document Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdDocumentsDocumentIdMetadata(c *gin.Context) {
	handler := api.NewDocumentMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetDocumentMetadata(c)
}

func (s *Server) PostThreatModelsThreatModelIdDocumentsDocumentIdMetadata(c *gin.Context) {
	handler := api.NewDocumentMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.CreateDocumentMetadata(c)
}

func (s *Server) GetThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context) {
	handler := api.NewDocumentMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetDocumentMetadataByKey(c)
}

func (s *Server) PutThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context) {
	handler := api.NewDocumentMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.UpdateDocumentMetadata(c)
}

func (s *Server) DeleteThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context) {
	handler := api.NewDocumentMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.DeleteDocumentMetadata(c)
}

func (s *Server) PostThreatModelsThreatModelIdDocumentsDocumentIdMetadataBulk(c *gin.Context) {
	handler := api.NewDocumentMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.BulkCreateDocumentMetadata(c)
}

// Threat Model Sources handlers
func (s *Server) GetThreatModelsThreatModelIdSources(c *gin.Context) {
	// Use the dedicated source handler with global store
	handler := api.NewRepositorySubResourceHandler(
		api.GlobalRepositoryStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.GetRepositorys(c)
}

func (s *Server) PostThreatModelsThreatModelIdSources(c *gin.Context) {
	// Use the dedicated source handler with global store
	handler := api.NewRepositorySubResourceHandler(
		api.GlobalRepositoryStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.CreateRepository(c)
}

func (s *Server) GetThreatModelsThreatModelIdSourcesSourceId(c *gin.Context) {
	// Use the dedicated source handler with global store
	handler := api.NewRepositorySubResourceHandler(
		api.GlobalRepositoryStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.GetRepository(c)
}

func (s *Server) PutThreatModelsThreatModelIdSourcesSourceId(c *gin.Context) {
	// Use the dedicated source handler with global store
	handler := api.NewRepositorySubResourceHandler(
		api.GlobalRepositoryStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.UpdateRepository(c)
}

func (s *Server) DeleteThreatModelsThreatModelIdSourcesSourceId(c *gin.Context) {
	// Use the dedicated source handler with global store
	handler := api.NewRepositorySubResourceHandler(
		api.GlobalRepositoryStore,
		nil, // db - not needed for current implementation
		nil, // cache - not needed for current implementation
		nil, // cacheInvalidator - not needed for current implementation
	)
	handler.DeleteRepository(c)
}

func (s *Server) PostThreatModelsThreatModelIdSourcesBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Source Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdSourcesSourceIdMetadata(c *gin.Context) {
	handler := api.NewRepositoryMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetRepositoryMetadata(c)
}

func (s *Server) PostThreatModelsThreatModelIdSourcesSourceIdMetadata(c *gin.Context) {
	handler := api.NewRepositoryMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.CreateRepositoryMetadata(c)
}

func (s *Server) GetThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context) {
	handler := api.NewRepositoryMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.GetRepositoryMetadataByKey(c)
}

func (s *Server) PutThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context) {
	handler := api.NewRepositoryMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.UpdateRepositoryMetadata(c)
}

func (s *Server) DeleteThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context) {
	handler := api.NewRepositoryMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.DeleteRepositoryMetadata(c)
}

func (s *Server) PostThreatModelsThreatModelIdSourcesSourceIdMetadataBulk(c *gin.Context) {
	handler := api.NewRepositoryMetadataHandler(api.GlobalMetadataStore, nil, nil, nil)
	handler.BulkCreateRepositoryMetadata(c)
}

// Batch Operations handlers
func (s *Server) PostThreatModelsThreatModelIdThreatsBatchPatch(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdThreatsBatch(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func setupRouter(config *config.Config) (*gin.Engine, *api.Server) {
	// Create a gin router without default middleware
	r := gin.New()

	// Configure gin based on log level
	if config.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Add custom recovery middleware first (must be before other middleware)
	r.Use(api.CustomRecoveryMiddleware())

	// Add custom logging middleware
	r.Use(slogging.LoggerMiddleware())

	// Add enhanced request/response logging middleware if configured
	if config.Logging.LogAPIRequests || config.Logging.LogAPIResponses {
		requestConfig := slogging.RequestResponseLoggingConfig{
			LogRequests:    config.Logging.LogAPIRequests,
			LogResponses:   config.Logging.LogAPIResponses,
			RedactTokens:   config.Logging.RedactAuthTokens,
			MaxBodySize:    10 * 1024, // 10KB
			OnlyDebugLevel: true,
			SkipPaths: []string{
				"/favicon.ico",
			},
		}
		r.Use(slogging.RequestResponseLogger(requestConfig))
	}

	r.Use(slogging.Recoverer())  // Use our recoverer
	r.Use(api.SecurityHeaders()) // Add security headers
	r.Use(api.CORS())
	r.Use(api.HSTSMiddleware(config.Server.TLSEnabled)) // Add HSTS when TLS is enabled
	r.Use(api.ContextTimeout(30 * time.Second))

	// Serve static files
	r.Static("/static", "./static")
	r.StaticFile("/robots.txt", "./static/robots.txt")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/site.webmanifest", "./static/site.webmanifest")
	r.StaticFile("/web-app-manifest-192x192.png", "./static/web-app-manifest-192x192.png")
	r.StaticFile("/web-app-manifest-512x512.png", "./static/web-app-manifest-512x512.png")
	r.StaticFile("/favicon.svg", "./static/favicon.svg")
	r.StaticFile("/TMI-Logo.svg", "./static/TMI-Logo.svg")
	r.StaticFile("/android-chrome-192x192.png", "./static/android-chrome-192x192.png")
	r.StaticFile("/android-chrome-512x512.png", "./static/android-chrome-512x512.png")
	r.StaticFile("/apple-touch-icon.png", "./static/apple-touch-icon.png")
	r.StaticFile("/favicon-16x16.png", "./static/favicon-16x16.png")
	r.StaticFile("/favicon-32x32.png", "./static/favicon-32x32.png")
	r.StaticFile("/favicon-96x96.png", "./static/favicon-96x96.png")

	// Security middleware with public path handling
	r.Use(PublicPathsMiddleware()) // Identify public paths first

	// Create WebSocket logging configuration from main config
	wsLoggingConfig := slogging.WebSocketLoggingConfig{
		Enabled:        config.Logging.LogWebSocketMsg,
		RedactTokens:   config.Logging.RedactAuthTokens,
		MaxMessageSize: 5 * 1024, // 5KB default
		OnlyDebugLevel: true,
	}

	// Note: API server creation moved to after store initialization
	// to ensure global stores are properly initialized first

	// Initialize auth package with database connections
	// This must be done before registering API routes to avoid conflicts
	logger := slogging.Get()
	logger.Info("Initializing authentication system with database connections")
	authHandlers, err := auth.InitAuthWithConfig(r, config)
	if err != nil {
		logger.Error("Failed to initialize authentication system: %v", err)
		// Continue anyway for development purposes
	}

	// Note: Auth service adapter setup moved to after server creation

	// Note: Middleware setup and route registration moved to after server creation

	// Initialize database stores for API data persistence
	logger.Info("Initializing database stores for all data persistence")
	dbManager := auth.GetDatabaseManager()

	// Always require database
	if dbManager == nil || dbManager.Postgres() == nil {
		logger.Error("Database not available - database is required")
		os.Exit(1)
	}

	logger.Info("Using database-backed stores for data persistence")
	api.InitializeDatabaseStores(dbManager.Postgres().GetDB())

	// Test database connection
	if err := dbManager.Postgres().GetDB().Ping(); err != nil {
		logger.Error("Database connection failed: %v", err)
		os.Exit(1)
	}
	logger.Info("Database connection verified successfully")

	// Ensure "everyone" pseudo-group exists
	if err := ensureEveryonePseudoGroup(dbManager.Postgres().GetDB()); err != nil {
		logger.Error("Failed to ensure everyone pseudo-group: %v", err)
		os.Exit(1)
	}

	// Initialize performance monitoring
	logger.Info("Initializing performance monitoring for collaborative editing")
	api.InitializePerformanceMonitoring()

	// Create API server with handlers (after stores are initialized)
	apiServer := api.NewServer(wsLoggingConfig, config.GetWebSocketInactivityTimeout())

	// Setup server with handlers
	server := &Server{
		config:       config,
		authHandlers: authHandlers,
		apiServer:    apiServer,
	}

	// Set up auth service adapter for OpenAPI integration
	if authHandlers != nil {
		authServiceAdapter := api.NewAuthServiceAdapter(authHandlers)
		apiServer.SetAuthService(authServiceAdapter)
		logger.Info("Auth service adapter configured for OpenAPI integration")
	} else {
		logger.Warn("Auth handlers not available - auth endpoints will return errors")
	}

	// Initialize token blacklist service
	// Note: dbManager was already retrieved during store initialization
	if dbManager != nil && dbManager.Redis() != nil {
		logger.Info("Initializing token blacklist service")
		server.tokenBlacklist = auth.NewTokenBlacklist(dbManager.Redis().GetClient(), authHandlers.Service().GetKeyManager())

		// Initialize event emitter for webhook support
		logger.Info("Initializing event emitter for webhook subscriptions")
		api.InitializeEventEmitter(dbManager.Redis().GetClient(), "tmi:events")

		// Initialize rate limiters
		logger.Info("Initializing API rate limiter")
		apiServer.SetAPIRateLimiter(api.NewAPIRateLimiter(dbManager.Redis().GetClient(), api.GlobalUserAPIQuotaStore))

		logger.Info("Initializing webhook rate limiter")
		apiServer.SetWebhookRateLimiter(api.NewWebhookRateLimiter(dbManager.Redis().GetClient()))

		logger.Info("Initializing IP rate limiter")
		apiServer.SetIPRateLimiter(api.NewIPRateLimiter(dbManager.Redis().GetClient()))

		logger.Info("Initializing auth flow rate limiter")
		apiServer.SetAuthFlowRateLimiter(api.NewAuthFlowRateLimiter(dbManager.Redis().GetClient()))

		// Initialize quota cache for dynamic adjustment (60 second TTL)
		logger.Info("Initializing quota cache with 60 second TTL")
		api.InitializeQuotaCache(60 * time.Second)
	} else {
		logger.Warn("Redis not available - token blacklist service disabled")
		logger.Warn("Redis not available - event emitter disabled (webhooks will not emit events)")
		logger.Warn("Redis not available - rate limiting disabled")
		logger.Warn("Redis not available - quota caching disabled")
	}

	// Add comprehensive request tracing middleware first
	r.Use(api.DetailedRequestLoggingMiddleware())
	r.Use(api.RouteMatchingMiddleware())

	// Test debug logging is working
	logger.Debug("[MAIN] Testing debug logging - this should appear in logs!")

	// Add IP-based rate limiting middleware first (for public endpoints)
	r.Use(api.IPRateLimitMiddleware(apiServer))

	// Add auth flow rate limiting middleware (for OAuth/SAML endpoints)
	r.Use(api.AuthFlowRateLimitMiddleware(apiServer))

	// Now add JWT middleware with token blacklist support and auth handlers for user lookup
	r.Use(JWTMiddleware(config, server.tokenBlacklist, authHandlers)) // JWT auth with public path skipping

	// Add user-based rate limiting middleware (after JWT so user_id is available)
	r.Use(api.RateLimitMiddleware(apiServer))

	// Add server middleware to make API server available in context
	r.Use(func(c *gin.Context) {
		c.Set("server", apiServer)
		c.Next()
	})

	// Add middleware to provide server configuration to handlers
	// This must be before routes are registered so config is available to all endpoints
	r.Use(func(c *gin.Context) {
		c.Set("tlsEnabled", config.Server.TLSEnabled)
		c.Set("tlsSubjectName", config.Server.TLSSubjectName)
		c.Set("serverPort", config.Server.Port)
		c.Set("isDev", config.Logging.IsDev)
		c.Set("operatorName", config.Operator.Name)
		c.Set("operatorContact", config.Operator.Contact)
		c.Next()
	})

	// Add OpenAPI validation middleware
	if openAPIValidator, err := api.SetupOpenAPIValidation(); err != nil {
		logger.Error("Failed to setup OpenAPI validation middleware: %v", err)
		os.Exit(1)
	} else {
		r.Use(openAPIValidator)
	}

	// Apply entity-specific middleware
	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())

	// Register WebSocket and custom non-REST routes
	logger.Info("Registering WebSocket and custom routes")
	apiServer.RegisterHandlers(r)

	// Validate database schema after auth initialization
	logger.Info("Validating database schema...")
	if err := validateDatabaseSchema(config); err != nil {
		logger.Error("Database schema validation failed: %v", err)
		// In production, you might want to exit here
		// os.Exit(1)
	}

	// Register API routes except for auth routes which are handled by the auth package
	// Register OpenAPI-generated routes with the API server instance
	logger.Info("[MAIN_MODULE] Starting OpenAPI route registration")
	logger.Info("[MAIN_MODULE] Registering OpenAPI route: GET /auth/me -> GetCurrentUser")
	logger.Info("[MAIN_MODULE] Registering OpenAPI route: GET /auth/providers -> GetAuthProviders")
	logger.Info("[MAIN_MODULE] Registering OpenAPI route: GET /collaboration/sessions -> GetCollaborationSessions")
	api.RegisterHandlers(r, apiServer)
	logger.Info("[MAIN_MODULE] OpenAPI route registration completed")

	// Register admin endpoints (protected by JWT middleware, admin role required)
	adminGroup := r.Group("/admin/quotas")
	{
		// User API quota management
		adminGroup.GET("/users/:user_id", apiServer.GetUserAPIQuota)
		adminGroup.PUT("/users/:user_id", apiServer.UpdateUserAPIQuota)
		adminGroup.DELETE("/users/:user_id", apiServer.DeleteUserAPIQuota)

		// Webhook quota management
		adminGroup.GET("/webhooks/:user_id", apiServer.GetWebhookQuota)
		adminGroup.PUT("/webhooks/:user_id", apiServer.UpdateWebhookQuota)
		adminGroup.DELETE("/webhooks/:user_id", apiServer.DeleteWebhookQuota)
	}
	logger.Info("Admin quota management endpoints registered")

	// Add development routes when in dev mode
	if config.Logging.IsDev {
		logger := slogging.Get()
		logger.Info("Adding development-only endpoints")
		r.GET("/dev/me", DevUserInfoHandler()) // Endpoint to check current user
	}

	return r, apiServer
}

// startWebhookWorkers initializes and starts all webhook workers
func startWebhookWorkers(ctx context.Context) (*api.WebhookEventConsumer, *api.WebhookChallengeWorker, *api.WebhookDeliveryWorker, *api.WebhookCleanupWorker) {
	logger := slogging.Get()

	// Initialize webhook metrics
	api.InitializeWebhookMetrics()

	// Start webhook workers if database and Redis are available
	dbManager := auth.GetDatabaseManager()
	var webhookConsumer *api.WebhookEventConsumer
	var challengeWorker *api.WebhookChallengeWorker
	var deliveryWorker *api.WebhookDeliveryWorker
	var cleanupWorker *api.WebhookCleanupWorker

	if dbManager != nil && dbManager.Postgres() != nil {
		logger.Info("Starting webhook workers...")

		// Start event consumer (requires Redis)
		if dbManager.Redis() != nil {
			webhookConsumer = api.NewWebhookEventConsumer(
				dbManager.Redis().GetClient(),
				"tmi:events",
				"webhook-consumers",
				fmt.Sprintf("consumer-%d", time.Now().Unix()),
			)
			if err := webhookConsumer.Start(ctx); err != nil {
				logger.Error("Failed to start webhook event consumer: %v", err)
			}
		} else {
			logger.Warn("Redis not available, webhook event consumer disabled")
		}

		// Start challenge verification worker
		challengeWorker = api.NewWebhookChallengeWorker()
		if err := challengeWorker.Start(ctx); err != nil {
			logger.Error("Failed to start webhook challenge worker: %v", err)
		}

		// Start delivery worker
		deliveryWorker = api.NewWebhookDeliveryWorker()
		if err := deliveryWorker.Start(ctx); err != nil {
			logger.Error("Failed to start webhook delivery worker: %v", err)
		}

		// Start cleanup worker
		cleanupWorker = api.NewWebhookCleanupWorker()
		if err := cleanupWorker.Start(ctx); err != nil {
			logger.Error("Failed to start webhook cleanup worker: %v", err)
		}

		logger.Info("Webhook workers started successfully")
	} else {
		logger.Warn("Database not available, webhook workers disabled")
	}

	return webhookConsumer, challengeWorker, deliveryWorker, cleanupWorker
}

// ensureEveryonePseudoGroup ensures the "everyone" pseudo-group exists in the database
func ensureEveryonePseudoGroup(db *sql.DB) error {
	logger := slogging.Get()

	query := `
		INSERT INTO groups (internal_uuid, provider, group_name, name, usage_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (provider, group_name) DO NOTHING
	`

	_, err := db.Exec(query,
		api.EveryonePseudoGroupUUID,
		"*",
		"everyone",
		"Everyone (Pseudo-group)",
		0,
	)
	if err != nil {
		return fmt.Errorf("failed to ensure everyone pseudo-group exists: %w", err)
	}

	logger.Info("Everyone pseudo-group verified/created successfully")
	return nil
}

func main() {
	// Parse command line flags
	configFile, generateConfig, err := config.ParseFlags()
	if err != nil {
		slogging.Get().Error("Error parsing flags: %v", err)
		os.Exit(1)
	}

	// Generate example config files if requested
	if generateConfig {
		if err := config.GenerateExampleConfig(); err != nil {
			slogging.Get().Error("Error generating config: %v", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		slogging.Get().Error("Error loading configuration: %v", err)
		os.Exit(1)
	}

	// Initialize logger
	if err := slogging.Initialize(slogging.Config{
		Level:                       cfg.GetLogLevel(),
		IsDev:                       cfg.Logging.IsDev,
		LogDir:                      cfg.Logging.LogDir,
		MaxAgeDays:                  cfg.Logging.MaxAgeDays,
		MaxSizeMB:                   cfg.Logging.MaxSizeMB,
		MaxBackups:                  cfg.Logging.MaxBackups,
		AlsoLogToConsole:            cfg.Logging.AlsoLogToConsole,
		SuppressUnauthenticatedLogs: cfg.Logging.SuppressUnauthenticatedLogs,
	}); err != nil {
		slogging.Get().Error("Failed to initialize logger: %v", err)
		os.Exit(1)
	}

	// Get logger instance
	logger := slogging.Get()
	defer func() {
		if err := logger.Close(); err != nil {
			slogging.Get().Error("Error closing logger: %v", err)
		}
	}()

	// Log startup information
	logger.Info("Starting TMI API server")
	logger.Info("Environment: %s", map[bool]string{true: "development", false: "production"}[cfg.Logging.IsDev])
	logger.Info("Log level: %s", cfg.Logging.Level)

	// Create a context that will be canceled on shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup router with config
	r, apiServer := setupRouter(cfg)

	// Add HTTPS redirect middleware if enabled
	if cfg.Server.TLSEnabled && cfg.Server.HTTPToHTTPSRedirect {
		r.Use(HTTPSRedirectMiddleware(
			cfg.Server.TLSEnabled,
			cfg.Server.TLSSubjectName,
			cfg.Server.Port,
		))
	}

	// Start WebSocket hub with context for cleanup
	apiServer.StartWebSocketHub(ctx)

	// Initialize and start webhook workers
	webhookConsumer, challengeWorker, deliveryWorker, cleanupWorker := startWebhookWorkers(ctx)

	// Prepare address
	addr := fmt.Sprintf("%s:%s", cfg.Server.Interface, cfg.Server.Port)

	// Validate TLS configuration if enabled
	if cfg.Server.TLSEnabled {
		if cfg.Server.TLSCertFile == "" || cfg.Server.TLSKeyFile == "" {
			logger.Error("TLS enabled but certificate or key file not specified")
			os.Exit(1)
		}

		// Check that files exist
		if _, err := os.Stat(cfg.Server.TLSCertFile); os.IsNotExist(err) {
			logger.Error("TLS certificate file not found: %s", cfg.Server.TLSCertFile)
			os.Exit(1)
		}

		if _, err := os.Stat(cfg.Server.TLSKeyFile); os.IsNotExist(err) {
			logger.Error("TLS key file not found: %s", cfg.Server.TLSKeyFile)
			os.Exit(1)
		}

		// Load certificate to verify it's valid
		cert, err := tls.LoadX509KeyPair(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		if err != nil {
			logger.Error("Failed to load TLS certificate and key: %s", err)
			os.Exit(1)
		}

		// Try to parse the first certificate to get more information
		if len(cert.Certificate) > 0 {
			x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
			if err != nil {
				logger.Warn("Failed to parse X509 certificate: %s", err)
			} else {
				logger.Info("TLS certificate subject: %s", x509Cert.Subject.CommonName)
				logger.Info("TLS certificate expires: %s", x509Cert.NotAfter.Format(time.RFC3339))

				// Warn if subject name doesn't match
				if x509Cert.Subject.CommonName != cfg.Server.TLSSubjectName {
					logger.Warn("Certificate subject name (%s) doesn't match configured TLS_SUBJECT_NAME (%s)",
						x509Cert.Subject.CommonName, cfg.Server.TLSSubjectName)
				}

				// Check certificate expiration
				if x509Cert.NotAfter.Before(time.Now().AddDate(0, 1, 0)) {
					if x509Cert.NotAfter.Before(time.Now()) {
						logger.Error("TLS certificate has expired on %s",
							x509Cert.NotAfter.Format(time.RFC3339))
					} else {
						logger.Warn("TLS certificate will expire within 1 month on %s",
							x509Cert.NotAfter.Format(time.RFC3339))
					}
				}
			}
		}
	}

	// Configure TLS if enabled
	var tlsConfig *tls.Config
	if cfg.Server.TLSEnabled {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: cfg.Server.TLSSubjectName,
		}
	}

	// Configure server with timeouts from config
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
		TLSConfig:    tlsConfig,
	}

	// Start server in a goroutine
	go func() {
		var err error

		if cfg.Server.TLSEnabled {
			logger.Info("Server listening on %s with TLS enabled", addr)
			logger.Info("Using certificate: %s, key: %s, subject name: %s",
				cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile, cfg.Server.TLSSubjectName)
			err = srv.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		} else {
			logger.Info("Server listening on %s", addr)
			err = srv.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			logger.Error("Error starting server: %s", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()

	// Restore default behavior on the interrupt signal and notify user of shutdown
	stop()
	logger.Info("Shutting down server...")

	// Create a deadline for the shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Gracefully shutdown the server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server forced to shutdown: %s", err)
		os.Exit(1)
	}

	logger.Info("Server gracefully stopped")

	// Stop webhook workers
	if webhookConsumer != nil {
		logger.Info("Stopping webhook event consumer...")
		webhookConsumer.Stop()
	}
	if challengeWorker != nil {
		logger.Info("Stopping webhook challenge worker...")
		challengeWorker.Stop()
	}
	if deliveryWorker != nil {
		logger.Info("Stopping webhook delivery worker...")
		deliveryWorker.Stop()
	}
	if cleanupWorker != nil {
		logger.Info("Stopping webhook cleanup worker...")
		cleanupWorker.Stop()
	}

	// Shutdown auth system
	if err := auth.Shutdown(context.TODO()); err != nil {
		logger.Error("Error shutting down auth system: %v", err)
	}
}

// validateDatabaseSchema validates the database schema matches expectations
func validateDatabaseSchema(cfg *config.Config) error {
	// Get database configuration from config
	host := cfg.Database.Postgres.Host
	port := cfg.Database.Postgres.Port
	user := cfg.Database.Postgres.User
	password := cfg.Database.Postgres.Password
	dbName := cfg.Database.Postgres.Database
	sslMode := cfg.Database.Postgres.SSLMode

	// Create database connection string
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		user, password, host, port, dbName, sslMode)

	// Open database connection
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			// Log error but don't fail validation
			slogging.Get().Error("Error closing database: %v", err)
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Validate schema
	result, err := dbschema.ValidateSchema(db)
	if err != nil {
		return fmt.Errorf("failed to validate schema: %w", err)
	}

	// Validation results are already logged by the validator

	// Check if validation failed
	if !result.Valid {
		return fmt.Errorf("schema validation failed: %d errors, %d missing migrations",
			len(result.Errors), len(result.MissingMigrations))
	}

	return nil
}
