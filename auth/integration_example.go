package auth

import (
	"fmt"
	"os"
)

// printExample is a helper function to print code examples without triggering linter warnings
func printExample(s string) {
	_, err := os.Stdout.WriteString(s + "\n")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error writing to stdout: %v\n", err)
	}
}

// IntegrationExample shows how to integrate the authentication system with the main application
func IntegrationExample() {
	fmt.Println("This is an example of how to integrate the authentication system with the main application.")
	fmt.Println("To integrate the authentication system, make the following changes to cmd/server/main.go:")

	fmt.Println("\n1. Import the auth package:")
	fmt.Println(`
import (
	// ... existing imports
	"github.com/ericfitz/tmi/oauth2"
)`)

	fmt.Println("\n2. Replace the existing JWT middleware with the new auth middleware:")
	printExample(`
// Replace this:
r.Use(PublicPathsMiddleware())
r.Use(JWTMiddleware())

// With this:
if err := auth.InitAuth(r); err != nil {
	logger.Error("Failed to initialize authentication system: %%v", err)
	os.Exit(1)
}`)

	fmt.Println("\n3. Remove the existing auth handlers from the Server struct:")
	fmt.Println(`
// Remove these methods:
func (s *Server) GetAuthLogin(c *gin.Context) { ... }
func (s *Server) GetAuthCallback(c *gin.Context) { ... }
func (s *Server) PostAuthLogout(c *gin.Context) { ... }`)

	fmt.Println("\n4. Update the setupRouter function to use the new auth middleware:")
	printExample(`
func setupRouter(config Config) (*gin.Engine, *api.Server) {
	// ... existing code

	// Security middleware with public path handling
	// Remove these lines:
	// r.Use(PublicPathsMiddleware())
	// r.Use(JWTMiddleware())

	// Initialize authentication system
	if err := auth.InitAuth(r); err != nil {
		logger.Error("Failed to initialize authentication system: %%v", err)
		os.Exit(1)
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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/oauth2"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ... existing code

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
	r.Use(slogging.LoggerMiddleware())
	r.Use(slogging.Recoverer())
	r.Use(api.CORS())
	r.Use(api.ContextTimeout(30 * time.Second))

	// Serve static files
	r.Static("/static", "./static")
	r.StaticFile("/favicon.ico", "./static/favicon.ico")
	r.StaticFile("/site.webmanifest", "./static/site.webmanifest")
	r.StaticFile("/web-app-manifest-192x192.png", "./static/web-app-manifest-192x192.png")
	r.StaticFile("/web-app-manifest-512x512.png", "./static/web-app-manifest-512x512.png")
	r.StaticFile("/favicon.svg", "./static/favicon.svg")

	// Initialize authentication system
	logger := slogging.Get()
	if err := auth.InitAuth(r); err != nil {
		logger.Error("Failed to initialize authentication system: %%v", err)
		os.Exit(1)
	}

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
	api.RegisterHandlers(r, apiServer)

	// Register WebSocket and custom routes
	apiServer.RegisterHandlers(r)

	// Add development routes when in dev mode
	if config.Logging.IsDev {
		logger.Info("Adding development-only endpoints")
		r.GET("/dev/me", DevUserInfoHandler()) // Endpoint to check current user
	}

	return r, apiServer
}

func main() {
	// ... existing code

	// Wait for interrupt signal
	<-ctx.Done()

	// Restore default behavior on the interrupt signal and notify user of shutdown
	stop()
	logger.Info("Shutting down server...")

	// Shutdown authentication system
	logger.Info("Shutting down authentication system...")
	if err := auth.Shutdown(nil); err != nil {
		logger.Error("Error shutting down authentication system: %%v", err)
	}

	// Create a deadline for the shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Gracefully shutdown the server
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Server forced to shutdown: %%s", err)
		os.Exit(1)
	}

	logger.Info("Server gracefully stopped")
}`)
}
