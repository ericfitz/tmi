package api

import (
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Addon Methods - Complete ServerInterface Implementation

// CreateAddon creates a new add-on (admin only)
func (s *Server) CreateAddon(c *gin.Context) {
	// Delegate to existing standalone handler
	CreateAddon(c)
}

// ListAddons lists all add-ons
func (s *Server) ListAddons(c *gin.Context, params ListAddonsParams) {
	// The standalone handler reads query params directly from context
	// which is already set by the OpenAPI middleware
	ListAddons(c)
}

// GetAddon gets a single add-on by ID
func (s *Server) GetAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	GetAddon(c)
}

// DeleteAddon deletes an add-on (admin only)
func (s *Server) DeleteAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	DeleteAddon(c)
}

// InvokeAddon invokes an add-on
func (s *Server) InvokeAddon(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	InvokeAddon(c)
}

// ListInvocations lists invocations (user sees own, admin sees all)
func (s *Server) ListInvocations(c *gin.Context, params ListInvocationsParams) {
	// The standalone handler reads query params directly from context
	ListInvocations(c)
}

// GetInvocation gets a single invocation by ID
func (s *Server) GetInvocation(c *gin.Context, id openapi_types.UUID) {
	// Delegate to existing standalone handler
	GetInvocation(c)
}

// UpdateInvocationStatus updates invocation status (webhook callback with HMAC auth)
func (s *Server) UpdateInvocationStatus(c *gin.Context, id openapi_types.UUID, params UpdateInvocationStatusParams) {
	// The standalone handler reads the HMAC signature from headers directly
	UpdateInvocationStatus(c)
}
