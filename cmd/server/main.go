package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/api" // Your module path
	"github.com/ericfitz/tmi/internal/logging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Server holds dependencies for the API server
type Server struct{
	// Configuration
	config Config
	
	// Add other dependencies like database clients, services, etc.
}

// PublicPathsMiddleware identifies paths that don't require authentication
func PublicPathsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get a context-aware logger
		logger := logging.GetContextLogger(c)
		
		// Public paths that don't require authentication
		if c.Request.URL.Path == "/" || c.Request.URL.Path == "/auth/login" || c.Request.URL.Path == "/auth/callback" {
			logger.Debug("Public path identified: %s", c.Request.URL.Path)
			// Mark this request as public in the context for downstream middleware
			c.Set("isPublicPath", true)
		}
		
		// Always continue to next middleware
		c.Next()
	}
}

// JWT Middleware
func JWTMiddleware() gin.HandlerFunc {
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
				Error:   "unauthorized", 
				Message: "Missing Authorization header",
			})
			c.Abort()
			return
		}
		
		// Parse the header format
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			logger.Warn("Authentication failed: Invalid Authorization header format for path: %s", c.Request.URL.Path)
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:   "unauthorized", 
				Message: "Invalid Authorization header format",
			})
			c.Abort()
			return
		}
		
		tokenStr := parts[1]
		
		// Get JWT secret from config (in production, use environment variables)
		// Note: In a real implementation, this would come from the server's config
		jwtSecret := []byte(getEnv("JWT_SECRET", "secret"))
		
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
				Error:   "unauthorized", 
				Message: "Invalid or expired token",
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
							<option value="admin">Admin</option>
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
		`;
		c.Header("Content-Type", "text/html")
		c.String(http.StatusOK, loginHTML)
		return
	}

	// In production, redirect to the actual OAuth provider
	c.Redirect(http.StatusFound, s.config.Auth.OAuthURL)
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
	jwtSecret := []byte(s.config.Auth.JWTSecret)
	tokenStr, err := token.SignedString(jwtSecret)
	
	if err != nil {
		logger.Error("Failed to sign JWT token: %v", err)
		c.JSON(http.StatusInternalServerError, api.Error{
			Error:   "server_error",
			Message: "Failed to generate authentication token",
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
				Error:   "unauthorized",
				Message: "Not authenticated",
			})
			return
		}
		
		userName, ok := userID.(string)
		if !ok || userName == "" {
			c.JSON(http.StatusUnauthorized, api.Error{
				Error:   "unauthorized",
				Message: "Invalid user context",
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
			"user":         userName,
			"role":         role,
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
	_ = c.DefaultQuery("limit", "20")
	_ = c.DefaultQuery("offset", "0")
	id, err := api.ParseUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.Error{Error: "internal_error", Message: "Invalid UUID format"})
		return
	}
	c.JSON(http.StatusOK, []api.ListItem{
		{Name: "System Threat Model", Id: id},
	})
}

func (s *Server) PostThreatModels(c *gin.Context) {
	var threatModel api.ThreatModel
	if err := c.ShouldBindJSON(&threatModel); err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_input", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, threatModel)
}

func (s *Server) GetThreatModelsId(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	c.JSON(http.StatusOK, api.ThreatModel{Id: id})
}

func (s *Server) PutThreatModelsId(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	var threatModel api.ThreatModel
	if err := c.ShouldBindJSON(&threatModel); err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_input", Message: err.Error()})
		return
	}
	threatModel.Id = id // Ensure ID matches
	c.JSON(http.StatusOK, threatModel)
}

func (s *Server) PatchThreatModelsId(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	var operations []interface{}
	if err := c.ShouldBindJSON(&operations); err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_patch", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, api.ThreatModel{Id: id})
}

func (s *Server) DeleteThreatModelsId(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func setupRouter(config Config) (*gin.Engine, *api.Server) {
	// Create a gin router without default middleware
	r := gin.New()
	
	// Configure gin based on log level
	if config.Server.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	
	// Add custom middleware
	r.Use(logging.LoggerMiddleware())  // Use our new logger middleware
	r.Use(logging.Recoverer())         // Use our new recoverer
	r.Use(api.CORS())
	r.Use(api.ContextTimeout(30 * time.Second))
	
	// Serve static files
	r.Static("/static", "./static")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	
	// Security middleware with public path handling
	r.Use(PublicPathsMiddleware())     // Identify public paths first
	r.Use(JWTMiddleware())             // JWT auth with public path skipping
	
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
	
	// Register generated routes with our server implementation
	api.RegisterGinHandlers(r, server)
	
	// Register WebSocket and custom routes
	apiServer.RegisterHandlers(r)
	
	// Add development routes when in dev mode
	if config.Logging.IsDev {
		logger := logging.Get()
		logger.Info("Adding development-only endpoints")
		r.GET("/dev/me", DevUserInfoHandler()) // Endpoint to check current user
	}
	
	return r, apiServer
}

func main() {
	// Get environment file path from command line if provided
	envFile := ""
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "--env=") {
		envFile = strings.TrimPrefix(os.Args[1], "--env=")
	}
	
	// Load environment variables from file (will use .env by default)
	if err := LoadEnvFile(envFile); err != nil {
		log.Printf("Warning: %v", err)
	}
	
	// Load configuration
	config := LoadConfig()
	
	// Initialize logger
	if err := logging.Initialize(logging.Config{
		Level:           config.Logging.Level,
		IsDev:           config.Logging.IsDev,
		LogDir:          config.Logging.LogDir,
		MaxAgeDays:      config.Logging.MaxAgeDays,
		MaxSizeMB:       config.Logging.MaxSizeMB,
		MaxBackups:      config.Logging.MaxBackups,
		AlsoLogToConsole: config.Logging.AlsoLogToConsole,
	}); err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	
	// Get logger instance
	logger := logging.Get()
	defer logger.Close()
	
	// Log startup information
	logger.Info("Starting TMI API server")
	logger.Info("Environment: %s", map[bool]string{true: "development", false: "production"}[config.Logging.IsDev])
	logger.Info("Log level: %s", config.Server.LogLevel)
	
	// Create a context that will be canceled on shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	
	// Setup router with config
	r, apiServer := setupRouter(config)
	
	// Start WebSocket hub with context for cleanup
	apiServer.StartWebSocketHub(ctx)
	
	// Configure server with timeouts from config
	srv := &http.Server{
		Addr:         ":" + config.Server.Port,
		Handler:      r,
		ReadTimeout:  config.Server.ReadTimeout,
		WriteTimeout: config.Server.WriteTimeout,
		IdleTimeout:  config.Server.IdleTimeout,
	}
	
	// Start server in a goroutine
	go func() {
		logger.Info("Server listening on port %s", config.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
}