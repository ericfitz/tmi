package api

import (
	"github.com/gin-gonic/gin"
)

// GinHandlerFunc converts a Gin handler to an Echo handler
type GinHandlerFunc func(c *gin.Context) error

// GinServerInterface extends ServerInterface for Gin
type GinServerInterface interface {
	// Root API Info
	GetApiInfo(c *gin.Context)
	
	// Authentication
	GetAuthLogin(c *gin.Context)
	GetAuthCallback(c *gin.Context)
	PostAuthLogout(c *gin.Context)
	
	// Diagram Management
	GetDiagrams(c *gin.Context)
	PostDiagrams(c *gin.Context)
	GetDiagramsId(c *gin.Context)
	PutDiagramsId(c *gin.Context)
	PatchDiagramsId(c *gin.Context)
	DeleteDiagramsId(c *gin.Context)
	
	// Diagram Collaboration
	GetDiagramsIdCollaborate(c *gin.Context)
	PostDiagramsIdCollaborate(c *gin.Context)
	DeleteDiagramsIdCollaborate(c *gin.Context)
	
	// Threat Model Management
	GetThreatModels(c *gin.Context)
	PostThreatModels(c *gin.Context)
	GetThreatModelsId(c *gin.Context)
	PutThreatModelsId(c *gin.Context)
	PatchThreatModelsId(c *gin.Context)
	DeleteThreatModelsId(c *gin.Context)
}

// GinRouter is a simplified interface for Gin router
type GinRouter interface {
	GET(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	POST(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	PUT(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	DELETE(path string, handlers ...gin.HandlerFunc) gin.IRoutes
	PATCH(path string, handlers ...gin.HandlerFunc) gin.IRoutes
}

// RegisterHandlers registers the API handlers to a Gin router
func RegisterGinHandlers(r GinRouter, si GinServerInterface) {
	// Root
	r.GET("/", si.GetApiInfo)
	
	// Auth
	r.GET("/auth/login", si.GetAuthLogin)
	r.GET("/auth/callback", si.GetAuthCallback)
	r.POST("/auth/logout", si.PostAuthLogout)
	
	// Diagrams
	r.GET("/diagrams", si.GetDiagrams)
	r.POST("/diagrams", si.PostDiagrams)
	r.GET("/diagrams/:id", si.GetDiagramsId)
	r.PUT("/diagrams/:id", si.PutDiagramsId)
	r.PATCH("/diagrams/:id", si.PatchDiagramsId)
	r.DELETE("/diagrams/:id", si.DeleteDiagramsId)
	
	// Diagram Collaboration
	r.GET("/diagrams/:id/collaborate", si.GetDiagramsIdCollaborate)
	r.POST("/diagrams/:id/collaborate", si.PostDiagramsIdCollaborate)
	r.DELETE("/diagrams/:id/collaborate", si.DeleteDiagramsIdCollaborate)
	
	// Threat Models
	r.GET("/threat_models", si.GetThreatModels)
	r.POST("/threat_models", si.PostThreatModels)
	r.GET("/threat_models/:id", si.GetThreatModelsId)
	r.PUT("/threat_models/:id", si.PutThreatModelsId)
	r.PATCH("/threat_models/:id", si.PatchThreatModelsId)
	r.DELETE("/threat_models/:id", si.DeleteThreatModelsId)
}