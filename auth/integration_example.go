package auth

import (
	"fmt"
	"os"
)

// printExample is a helper function to print code examples without triggering linter warnings
func printExample(s string) {
	_, err := os.Stdout.WriteString(s + "\n")
	if err != nil {
		fmt.Printf("Error writing to stdout: %v\n", err)
	}
}

// IntegrationExample shows how to integrate the authentication system with the main application
func IntegrationExample() {
	fmt.Println("This is an example of how to integrate the authentication system with the main application.")
	fmt.Println("To integrate the authentication system, make the following changes to cmd/server/main.go:")

	fmt.Println("\n1. Import the required packages:")
	fmt.Print(`
import (
	// ... existing imports
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
)`)

	fmt.Println("\n2. Load unified configuration and initialize auth with it:")
	printExample(`
// Load unified configuration (contains all settings including auth)
config, err := config.Load()
if err != nil {
	logger.Error("Failed to load configuration: %%v", err)
	os.Exit(1)
}

// Initialize authentication system with unified config
// NOTE: Use InitAuthWithConfig, NOT the deprecated InitAuth function
authHandlers, err := auth.InitAuthWithConfig(router, config)
if err != nil {
	logger.Error("Failed to initialize authentication system: %%v", err)
	// Continue anyway for development purposes
}`)

	fmt.Println("\n3. Remove the existing auth handlers from the Server struct:")
	fmt.Print(`
// Remove these methods:
func (s *Server) GetAuthLogin(c *gin.Context) { ... }
func (s *Server) GetAuthCallback(c *gin.Context) { ... }
func (s *Server) PostAuthLogout(c *gin.Context) { ... }`)

	fmt.Println("\n4. Update the setupRouter function to use unified config:")
	printExample(`
func setupRouter(config *config.Config) (*gin.Engine, *api.Server) {
	// ... existing code

	// Initialize authentication system with unified config
	logger := slogging.Get()
	authHandlers, err := auth.InitAuthWithConfig(r, config)
	if err != nil {
		logger.Error("Failed to initialize authentication system: %%v", err)
		// Continue anyway for development purposes
	}

	// ... rest of the function
}`)

	fmt.Println("\n5. Add shutdown code for the auth system in the main function:")
	printExample(`
// In the main function, before srv.Shutdown:
logger.Info("Shutting down authentication system...")
if err := auth.Shutdown(nil); err != nil {
	logger.Error("Error shutting down authentication system: %%v", err)
}`)

	fmt.Println("\nComplete Example:")
	printExample(`
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load unified configuration from environment variables
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	// Create a gin router
	r := gin.New()

	// Configure gin based on log level
	if cfg.Logging.Level == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// Add custom middleware
	r.Use(slogging.LoggerMiddleware())
	r.Use(slogging.Recoverer())
	r.Use(api.CORS())
	r.Use(api.ContextTimeout(30 * time.Second))

	// Serve static files
	r.Static("/static", "./static")

	// Initialize authentication system with unified config
	// NOTE: This loads OAuth and SAML providers from environment variables
	logger := slogging.Get()
	authHandlers, err := auth.InitAuthWithConfig(r, cfg)
	if err != nil {
		logger.Error("Failed to initialize authentication system: %%v", err)
		// Continue anyway for development purposes
	}

	// Create API server
	apiServer := api.NewServer()

	// Apply entity-specific middleware
	r.Use(api.ThreatModelMiddleware())
	r.Use(api.DiagramMiddleware())

	// Register routes
	api.RegisterHandlers(r, apiServer)
	apiServer.RegisterHandlers(r)

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server error: %%v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Shutdown authentication system
	if err := auth.Shutdown(context.Background()); err != nil {
		logger.Error("Error shutting down authentication: %%v", err)
	}

	// Gracefully shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown: %%v", err)
		os.Exit(1)
	}

	logger.Info("Server gracefully stopped")
}`)
}
