package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log"
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
	"github.com/ericfitz/tmi/internal/logging"
	"github.com/ericfitz/tmi/internal/telemetry"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v4/stdlib"
)

// Server holds dependencies for the API server
type Server struct {
	// Configuration
	config *config.Config

	// Add other dependencies like database clients, services, etc.
}

// HTTPSRedirectMiddleware redirects HTTP requests to HTTPS when TLS is enabled
func HTTPSRedirectMiddleware(tlsEnabled bool, tlsSubjectName string, port string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get logger from context
		logger := logging.GetContextLogger(c)

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
		logger := logging.GetContextLogger(c)

		// Public paths that don't require authentication
		if c.Request.URL.Path == "/" ||
			c.Request.URL.Path == "/version" ||
			c.Request.URL.Path == "/metrics" ||
			c.Request.URL.Path == "/auth/login" ||
			c.Request.URL.Path == "/auth/callback" ||
			c.Request.URL.Path == "/auth/providers" ||
			c.Request.URL.Path == "/auth/token" ||
			c.Request.URL.Path == "/auth/refresh" ||
			strings.HasPrefix(c.Request.URL.Path, "/auth/authorize/") ||
			strings.HasPrefix(c.Request.URL.Path, "/auth/exchange/") ||
			c.Request.URL.Path == "/site.webmanifest" ||
			c.Request.URL.Path == "/favicon.ico" ||
			c.Request.URL.Path == "/favicon.svg" ||
			c.Request.URL.Path == "/web-app-manifest-192x192.png" ||
			c.Request.URL.Path == "/web-app-manifest-512x512.png" {
			logger.Debug("Public path identified: %s", c.Request.URL.Path)
			// Mark this request as public in the context for downstream middleware
			c.Set("isPublicPath", true)
		}

		// Always continue to next middleware
		c.Next()
	}
}

// JWT Middleware factory function that takes config
func JWTMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get a context-aware logger
		logger := logging.GetContextLogger(c)

		// Skip authentication for public paths
		if isPublic, exists := c.Get("isPublicPath"); exists && isPublic.(bool) {
			logger.Debug("Skipping authentication for public path: %s", c.Request.URL.Path)
			// Set a dummy user for context consistency if needed
			c.Set("userName", "anonymous")
			c.Next()
			return
		}

		// Log attempt for debugging
		logger.Debug("Checking authentication for path: %s", c.Request.URL.Path)

		// Get the auth header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			logger.Warn("Authentication failed: Missing Authorization header for path: %s", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Missing Authorization header",
			})
			c.Abort()
			return
		}

		// Parse the header format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			logger.Warn("Authentication failed: Invalid Authorization header format for path: %s", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid Authorization header format",
			})
			c.Abort()
			return
		}

		tokenStr := parts[1]

		// Get JWT secret from config (same source as auth service)
		jwtSecret := []byte(cfg.Auth.JWT.Secret)

		// Validate the token
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			// Verify signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			logger.Warn("Authentication failed: Invalid or expired token - %v", err)
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:            "unauthorized",
				ErrorDescription: "Invalid or expired token",
			})
			c.Abort()
			return
		}

		// Extract claims and add to context
		if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
			// Add user info to context
			if sub, ok := claims["sub"].(string); ok {
				logger.Debug("Authenticated user: %s", sub)
				c.Set("userName", sub)

				// Extract role if present
				if roleValue, hasRole := claims["role"]; hasRole {
					if role, ok := roleValue.(string); ok {
						logger.Debug("User role from token: %s", role)
						c.Set("userTokenRole", role)
					}
				}
			}
		}

		c.Next()
	}
}

func (s *Server) GetApiInfo(c *gin.Context) {
	apiInfoHandler := api.NewApiInfoHandler()
	apiInfoHandler.GetApiInfo(c)
}

func (s *Server) GetAuthLogin(c *gin.Context) {
	// In dev mode, show a simple login page instead of redirecting to OAuth
	if s.config.Logging.IsDev {
		loginHTML := `
		<!DOCTYPE html>
		<html>
		<head>
			<title>TMI Dev Login</title>
			<link rel="icon" href="/favicon.ico" type="image/x-icon">
			<link rel="shortcut icon" href="/favicon.ico" type="image/x-icon">
			<style>
				body { font-family: Arial, sans-serif; margin: 40px; line-height: 1.6; }
				.container { max-width: 500px; margin: 0 auto; padding: 20px; border: 1px solid #ddd; border-radius: 5px; }
				h1 { color: #333; }
				input, select { width: 100%; padding: 8px; margin: 8px 0; box-sizing: border-box; }
				button { background-color: #4CAF50; color: white; padding: 10px 15px; border: none; border-radius: 4px; cursor: pointer; }
				button:hover { background-color: #45a049; }
			</style>
		</head>
		<body>
			<div class="container">
				<h1>TMI Development Login</h1>
				<form id="loginForm">
					<div>
						<label for="username">Username:</label>
						<input type="text" id="username" name="username" value="user@example.com" placeholder="Enter username or email" required>
					</div>
					<div>
						<label for="role">Role:</label>
						<select id="role" name="role">
							<option value="owner">Owner</option>
							<option value="reader">Reader</option>
							<option value="writer">Writer</option>
						</select>
					</div>
					<div>
						<button type="submit">Login</button>
					</div>
				</form>
				<div id="result" style="margin-top: 20px;"></div>
			</div>

			<script>
				document.getElementById('loginForm').addEventListener('submit', function(e) {
					e.preventDefault();
					
					const username = document.getElementById('username').value;
					const role = document.getElementById('role').value;
					
					fetch('/auth/callback?username=' + encodeURIComponent(username) + '&role=' + encodeURIComponent(role))
						.then(response => response.json())
						.then(data => {
							document.getElementById('result').innerHTML = 
								'<p>Login successful! Copy this token to use in your Authorization header:</p>' +
								'<pre style="background: #f4f4f4; padding: 10px; overflow-x: auto;">Bearer ' + data.token + '</pre>' +
								'<button onclick="copyToken()">Copy Token</button>' +
								'<button onclick="storeAndRedirect()">Save & Go to App</button>';
							
							window.tokenData = data.token;
						})
						.catch(error => {
							document.getElementById('result').innerHTML = '<p>Error: ' + error.message + '</p>';
						});
				});
				
				function copyToken() {
					navigator.clipboard.writeText('Bearer ' + window.tokenData);
					alert('Token copied to clipboard');
				}
				
				function storeAndRedirect() {
					localStorage.setItem('tmi_auth_token', 'Bearer ' + window.tokenData);
					window.location.href = '/';
				}
			</script>
		</body>
		</html>
		`
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, loginHTML)
		return
	}

	// In production, redirect to the actual OAuth provider
	c.Redirect(http.StatusFound, s.config.Auth.OAuth.CallbackURL)
}

func (s *Server) GetAuthCallback(c *gin.Context) {
	// Get logger from context
	logger := logging.GetContextLogger(c)

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
	c.Status(http.StatusNoContent)
}

// Dev-mode only endpoint to get current user info
func DevUserInfoHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := logging.GetContextLogger(c)
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

func (s *Server) GetDiagrams(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.GetDiagrams(c)
}

func (s *Server) PostDiagrams(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.CreateDiagram(c)
}

func (s *Server) GetDiagramsId(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.GetDiagramByID(c)
}

func (s *Server) PutDiagramsId(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.UpdateDiagram(c)
}

func (s *Server) PatchDiagramsId(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.PatchDiagram(c)
}

func (s *Server) DeleteDiagramsId(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.DeleteDiagram(c)
}

func (s *Server) GetDiagramsIdCollaborate(c *gin.Context) {
	// Ensure API server is in context for WebSocket operations
	if _, exists := c.Get("server"); !exists {
		// Only check since the middleware should have added it
		logging.Get().Error("API server not found in context")
	}

	diagramHandler := api.NewDiagramHandler()
	diagramHandler.GetDiagramCollaborate(c)
}

func (s *Server) PostDiagramsIdCollaborate(c *gin.Context) {
	// Ensure API server is in context for WebSocket operations
	if _, exists := c.Get("server"); !exists {
		// Only check since the middleware should have added it
		logging.Get().Error("API server not found in context")
	}

	diagramHandler := api.NewDiagramHandler()
	diagramHandler.PostDiagramCollaborate(c)
}

func (s *Server) DeleteDiagramsIdCollaborate(c *gin.Context) {
	// Ensure API server is in context for WebSocket operations
	if _, exists := c.Get("server"); !exists {
		// Only check since the middleware should have added it
		logging.Get().Error("API server not found in context")
	}

	diagramHandler := api.NewDiagramHandler()
	diagramHandler.DeleteDiagramCollaborate(c)
}

func (s *Server) GetThreatModels(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler()
	threatModelHandler.GetThreatModels(c)
}

func (s *Server) PostThreatModels(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler()
	threatModelHandler.CreateThreatModel(c)
}

func (s *Server) GetThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler()
	threatModelHandler.GetThreatModelByID(c)
}

func (s *Server) PutThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler()
	threatModelHandler.UpdateThreatModel(c)
}

func (s *Server) PatchThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler()
	threatModelHandler.PatchThreatModel(c)
}

func (s *Server) DeleteThreatModelsId(c *gin.Context) {
	threatModelHandler := api.NewThreatModelHandler()
	threatModelHandler.DeleteThreatModel(c)
}

// Threat Model Diagrams
func (s *Server) GetThreatModelsThreatModelIdDiagrams(c *gin.Context) {
	threatModelId := c.Param("id")
	handler := api.NewThreatModelDiagramHandler()
	handler.GetDiagrams(c, threatModelId)
}

func (s *Server) PostThreatModelsThreatModelIdDiagrams(c *gin.Context) {
	threatModelId := c.Param("id")
	handler := api.NewThreatModelDiagramHandler()
	handler.CreateDiagram(c, threatModelId)
}

func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.GetDiagramByID(c, threatModelId, diagramId)
}

func (s *Server) PutThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.UpdateDiagram(c, threatModelId, diagramId)
}

func (s *Server) PatchThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.PatchDiagram(c, threatModelId, diagramId)
}

func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramId(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.DeleteDiagram(c, threatModelId, diagramId)
}

// Threat Model Diagram Collaboration
func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.GetDiagramCollaborate(c, threatModelId, diagramId)
}

func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.PostDiagramCollaborate(c, threatModelId, diagramId)
}

func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramIdCollaborate(c *gin.Context) {
	threatModelId := c.Param("id")
	diagramId := c.Param("diagram_id")
	handler := api.NewThreatModelDiagramHandler()
	handler.DeleteDiagramCollaborate(c, threatModelId, diagramId)
}

// Diagram Metadata handlers
func (s *Server) GetDiagramsIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostDiagramsIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetDiagramsIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutDiagramsIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteDiagramsIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostDiagramsIdMetadataBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Diagram Cell Metadata handlers
func (s *Server) GetDiagramsIdCellsCellIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostDiagramsIdCellsCellIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetDiagramsIdCellsCellIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutDiagramsIdCellsCellIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteDiagramsIdCellsCellIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PatchDiagramsIdCellsCellId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostDiagramsIdCellsBatchPatch(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Diagram Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdDiagramsDiagramIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDiagramsDiagramIdMetadataBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Threats handlers
func (s *Server) GetThreatModelsThreatModelIdThreats(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdThreats(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PatchThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdThreatsThreatId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdThreatsBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdThreatsBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Threat Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdThreatsThreatIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdThreatsThreatIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdThreatsThreatIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdThreatsThreatIdMetadataBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Documents handlers
func (s *Server) GetThreatModelsThreatModelIdDocuments(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDocuments(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdDocumentsDocumentId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDocumentsBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Document Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdDocumentsDocumentIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDocumentsDocumentIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdDocumentsDocumentIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdDocumentsDocumentIdMetadataBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Sources handlers
func (s *Server) GetThreatModelsThreatModelIdSources(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdSources(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdSourcesSourceId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdSourcesSourceId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdSourcesSourceId(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdSourcesBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

// Threat Model Source Metadata handlers
func (s *Server) GetThreatModelsThreatModelIdSourcesSourceIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdSourcesSourceIdMetadata(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) GetThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PutThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) DeleteThreatModelsThreatModelIdSourcesSourceIdMetadataKey(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
}

func (s *Server) PostThreatModelsThreatModelIdSourcesSourceIdMetadataBulk(c *gin.Context) {
	c.JSON(501, gin.H{"error": "not implemented"})
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

	// Add custom middleware
	// Initialize telemetry middleware if available
	if telemetryService := telemetry.GetService(); telemetryService != nil {
		httpTracing, err := telemetry.NewHTTPTracing(telemetryService.GetTracer(), telemetryService.GetMeter())
		if err != nil {
			logger := logging.Get()
			logger.Error("Failed to create HTTP tracing middleware: %v", err)
			// Fall back to regular logging middleware
			r.Use(logging.LoggerMiddleware())
		} else {
			// Use enhanced tracing middleware that replaces logging middleware
			r.Use(httpTracing.TracingLoggerMiddleware())
		}
	} else {
		// Fall back to regular logging middleware if telemetry is not available
		r.Use(logging.LoggerMiddleware())
	}

	r.Use(logging.Recoverer()) // Use our recoverer
	r.Use(api.CORS())
	r.Use(api.ContextTimeout(30 * time.Second))

	// Add Prometheus metrics endpoint if telemetry is enabled
	if telemetry.GetService() != nil {
		r.GET("/metrics", func(c *gin.Context) {
			// Use promhttp.Handler() to serve Prometheus metrics
			// This will be automatically populated by the Prometheus exporter
			c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			c.String(200, "# Metrics endpoint active - metrics available via OpenTelemetry Prometheus exporter\n")
		})
	}

	// Serve static files
	r.Static("/static", "./static")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/site.webmanifest", "./static/site.webmanifest")
	r.StaticFile("/web-app-manifest-192x192.png", "./static/web-app-manifest-192x192.png")
	r.StaticFile("/web-app-manifest-512x512.png", "./static/web-app-manifest-512x512.png")
	r.StaticFile("/favicon.svg", "./static/favicon.svg")

	// Security middleware with public path handling
	r.Use(PublicPathsMiddleware()) // Identify public paths first
	r.Use(JWTMiddleware(config))   // JWT auth with public path skipping

	// Create API server with handlers
	apiServer := api.NewServer()

	// Add server middleware to make API server available in context
	r.Use(func(c *gin.Context) {
		c.Set("server", apiServer)
		c.Next()
	})

	// Apply entity-specific middleware
	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())

	// Setup server with handlers
	server := &Server{
		config: config,
	}

	// Register WebSocket and custom routes
	apiServer.RegisterHandlers(r)

	// Initialize auth package with database connections
	// This must be done before registering API routes to avoid conflicts
	logger := logging.Get()
	logger.Info("Initializing authentication system with database connections")
	if err := auth.InitAuthWithConfig(r, config); err != nil {
		logger.Error("Failed to initialize authentication system: %v", err)
		// Continue anyway for development purposes
	}

	// Initialize database stores for API data persistence
	logger.Info("Initializing database stores for threat models and diagrams")
	dbManager := auth.GetDatabaseManager()
	if dbManager != nil && dbManager.Postgres() != nil {
		logger.Info("Using database-backed stores for data persistence")
		api.InitializeDatabaseStores(dbManager.Postgres().GetDB())
	} else {
		logger.Warn("Database not available, falling back to in-memory stores")
		api.InitializeInMemoryStores()
	}

	// Validate database schema after auth initialization
	logger.Info("Validating database schema...")
	if err := validateDatabaseSchema(config); err != nil {
		logger.Error("Database schema validation failed: %v", err)
		// In production, you might want to exit here
		// os.Exit(1)
	}

	// Register API routes except for auth routes which are handled by the auth package
	// Create a custom router that skips auth routes
	customRouter := &customGinRouter{
		router:     r,
		skipRoutes: []string{"/auth/login", "/auth/callback", "/auth/logout"},
	}
	api.RegisterGinHandlers(customRouter, server)

	// Add development routes when in dev mode
	if config.Logging.IsDev {
		logger := logging.Get()
		logger.Info("Adding development-only endpoints")
		r.GET("/dev/me", DevUserInfoHandler()) // Endpoint to check current user
	}

	return r, apiServer
}

// customGinRouter is a wrapper around gin.Engine that skips certain routes
type customGinRouter struct {
	router     *gin.Engine
	skipRoutes []string
}

// GET implements the GinRouter interface
func (r *customGinRouter) GET(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	for _, skipRoute := range r.skipRoutes {
		if path == skipRoute {
			return r.router // Skip this route
		}
	}
	return r.router.GET(path, handlers...)
}

// POST implements the GinRouter interface
func (r *customGinRouter) POST(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	for _, skipRoute := range r.skipRoutes {
		if path == skipRoute {
			return r.router // Skip this route
		}
	}
	return r.router.POST(path, handlers...)
}

// PUT implements the GinRouter interface
func (r *customGinRouter) PUT(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	for _, skipRoute := range r.skipRoutes {
		if path == skipRoute {
			return r.router // Skip this route
		}
	}
	return r.router.PUT(path, handlers...)
}

// DELETE implements the GinRouter interface
func (r *customGinRouter) DELETE(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	for _, skipRoute := range r.skipRoutes {
		if path == skipRoute {
			return r.router // Skip this route
		}
	}
	return r.router.DELETE(path, handlers...)
}

// PATCH implements the GinRouter interface
func (r *customGinRouter) PATCH(path string, handlers ...gin.HandlerFunc) gin.IRoutes {
	for _, skipRoute := range r.skipRoutes {
		if path == skipRoute {
			return r.router // Skip this route
		}
	}
	return r.router.PATCH(path, handlers...)
}

func main() {
	// Parse command line flags
	configFile, generateConfig, err := config.ParseFlags()
	if err != nil {
		log.Fatalf("Error parsing flags: %v", err)
	}

	// Generate example config files if requested
	if generateConfig {
		if err := config.GenerateExampleConfig(); err != nil {
			log.Fatalf("Error generating config: %v", err)
		}
		return
	}

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Initialize logger
	if err := logging.Initialize(logging.Config{
		Level:            cfg.GetLogLevel(),
		IsDev:            cfg.Logging.IsDev,
		LogDir:           cfg.Logging.LogDir,
		MaxAgeDays:       cfg.Logging.MaxAgeDays,
		MaxSizeMB:        cfg.Logging.MaxSizeMB,
		MaxBackups:       cfg.Logging.MaxBackups,
		AlsoLogToConsole: cfg.Logging.AlsoLogToConsole,
	}); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}

	// Get logger instance
	logger := logging.Get()
	defer func() {
		if err := logger.Close(); err != nil {
			log.Printf("Error closing logger: %v", err)
		}
	}()

	// Initialize OpenTelemetry
	logger.Info("Initializing OpenTelemetry...")
	otelConfig, err := telemetry.LoadConfig()
	if err != nil {
		logger.Error("Failed to load telemetry configuration: %v", err)
		// Continue without telemetry in case of configuration issues
	} else {
		if err := telemetry.Initialize(otelConfig); err != nil {
			logger.Error("Failed to initialize telemetry: %v", err)
			// Continue without telemetry in case of initialization issues
		} else {
			logger.Info("OpenTelemetry initialized successfully")
			// Set up graceful shutdown for telemetry
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := telemetry.Shutdown(ctx); err != nil {
					logger.Error("Error shutting down telemetry: %v", err)
				} else {
					logger.Info("Telemetry shutdown completed")
				}
			}()
		}
	}

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

	// Add middleware to provide server configuration to handlers
	r.Use(func(c *gin.Context) {
		c.Set("tlsEnabled", cfg.Server.TLSEnabled)
		c.Set("tlsSubjectName", cfg.Server.TLSSubjectName)
		c.Set("serverPort", cfg.Server.Port)
		c.Set("isDev", cfg.Logging.IsDev)
		c.Next()
	})

	// Start WebSocket hub with context for cleanup
	apiServer.StartWebSocketHub(ctx)

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
			fmt.Printf("Error closing database: %v\n", err)
		}
	}()

	// Test connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Validate schema
	results, err := dbschema.ValidateSchema(db)
	if err != nil {
		return fmt.Errorf("failed to validate schema: %w", err)
	}

	// Log results
	dbschema.LogValidationResults(results)

	// Check if any validation failed
	for _, result := range results {
		if !result.Valid {
			return fmt.Errorf("schema validation failed for table %s", result.TableName)
		}
	}

	return nil
}
