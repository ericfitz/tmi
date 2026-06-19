package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Addon Methods - Complete ServerInterface Implementation

// CreateAddon creates a new add-on (admin only)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate addon creation to the standalone admin handler (mutates shared state)
func (s *Server) CreateAddon(c *gin.Context) {
	// Delegate to existing standalone handler
	CreateAddon(c)
}

// ListAddons lists all add-ons
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate addon listing to the standalone handler (reads DB)
func (s *Server) ListAddons(c *gin.Context, params ListAddonsParams) {
	// The standalone handler reads query params directly from context
	// which is already set by the OpenAPI middleware
	ListAddons(c)
}

// GetAddon gets a single add-on by ID
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate addon fetch by ID to the standalone handler (reads DB)
func (s *Server) GetAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	GetAddon(c)
}

// DeleteAddon deletes an add-on (admin only)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate addon deletion to the standalone admin handler (mutates shared state)
func (s *Server) DeleteAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	DeleteAddon(c)
}

// InvokeAddon invokes an add-on
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: delegate addon invocation to the standalone handler (mutates shared state)
func (s *Server) InvokeAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	InvokeAddon(c)
}
