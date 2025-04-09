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
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Server holds dependencies for the API server
type Server struct{
	// Configuration
	config Config
	
	// Add other dependencies like database clients, services, etc.
}

// JWT Middleware
func JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Public paths that don't require authentication
		if c.Request.URL.Path == "/auth/login" || c.Request.URL.Path == "/auth/callback" {
			c.Next()
			return
		}
		
		// Get the auth header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
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
				c.Set("userName", sub)
			}
		}
		
		c.Next()
	}
}

func (s *Server) GetAuthLogin(c *gin.Context) {
	c.Redirect(http.StatusFound, "https://oauth-provider.com/auth")
}

func (s *Server) GetAuthCallback(c *gin.Context) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user@example.com",
		"exp": 3600,
	})
	tokenStr, _ := token.SignedString([]byte("secret"))
	c.JSON(http.StatusOK, gin.H{
		"token":      tokenStr,
		"expires_in": 3600,
	})
}

func (s *Server) PostAuthLogout(c *gin.Context) {
	c.Status(http.StatusNoContent)
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
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.GetDiagramCollaborate(c)
}

func (s *Server) PostDiagramsIdCollaborate(c *gin.Context) {
	diagramHandler := api.NewDiagramHandler()
	diagramHandler.PostDiagramCollaborate(c)
}

func (s *Server) DeleteDiagramsIdCollaborate(c *gin.Context) {
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

func setupRouter(config Config) *gin.Engine {
	// Create a gin router without default middleware
	r := gin.New()
	
	// Configure gin based on log level
	if config.Server.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	
	// Parse log level from config
	logLevel := api.ParseLogLevel(config.Server.LogLevel)
	
	// Add custom middleware
	r.Use(api.RequestLogger(logLevel))
	r.Use(api.Recoverer())
	r.Use(api.CORS())
	r.Use(api.ContextTimeout(30 * time.Second))
	r.Use(JWTMiddleware())
	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())
	
	// Setup server with handlers
	server := &Server{
		config: config,
	}
	
	// Register generated routes with our server implementation
	api.RegisterGinHandlers(r, server)
	
	return r
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
	
	// Setup router with config
	r := setupRouter(config)
	
	// Configure server with timeouts from config
	srv := &http.Server{
		Addr:         ":" + config.Server.Port,
		Handler:      r,
		ReadTimeout:  config.Server.ReadTimeout,
		WriteTimeout: config.Server.WriteTimeout,
		IdleTimeout:  config.Server.IdleTimeout,
	}
	
	// Create a context that will be canceled on shutdown signal
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	
	// Start server in a goroutine
	go func() {
		fmt.Printf("Starting server on port %s\n", config.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %s", err)
		}
	}()
	
	// Wait for interrupt signal
	<-ctx.Done()
	
	// Restore default behavior on the interrupt signal and notify user of shutdown
	stop()
	fmt.Println("Shutting down server...")
	
	// Create a deadline for the shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Gracefully shutdown the server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %s", err)
	}
	
	fmt.Println("Server gracefully stopped")
}
