package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/api" // Your module path
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Server struct{}

// JWT Middleware
func JWTMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/auth/login" || c.Request.URL.Path == "/auth/callback" {
			c.Next()
			return
		}
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, api.Error{Error: "unauthorized", Message: "Missing Authorization header"})
			c.Abort()
			return
		}
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, api.Error{Error: "unauthorized", Message: "Invalid Authorization header format"})
			c.Abort()
			return
		}
		tokenStr := parts[1]
		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return []byte("secret"), nil // Replace with real secret
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, api.Error{Error: "unauthorized", Message: "Invalid or expired token"})
			c.Abort()
			return
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
	// Use limit and offset to avoid "declared and not used" error
	_ = c.DefaultQuery("limit", "20")
	_ = c.DefaultQuery("offset", "0")
	id, err := api.ParseUUID("123e4567-e89b-12d3-a456-426614174000")
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.Error{Error: "internal_error", Message: "Invalid UUID format"})
		return
	}
	c.JSON(http.StatusOK, []api.ListItem{
		{Name: "Workflow Diagram", Id: id},
	})
}

func (s *Server) PostDiagrams(c *gin.Context) {
	var diagram api.Diagram
	if err := c.ShouldBindJSON(&diagram); err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_input", Message: err.Error()})
		return
	}
	c.JSON(http.StatusCreated, diagram)
}

func (s *Server) GetDiagramsId(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	c.JSON(http.StatusOK, api.Diagram{Id: id})
}

func (s *Server) PutDiagramsId(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	var diagram api.Diagram
	if err := c.ShouldBindJSON(&diagram); err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_input", Message: err.Error()})
		return
	}
	diagram.Id = id // Ensure ID matches
	c.JSON(http.StatusOK, diagram)
}

func (s *Server) PatchDiagramsId(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	// Use a generic slice for JSON Patch operations; check api/api.go for exact type
	var operations []interface{}
	if err := c.ShouldBindJSON(&operations); err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_patch", Message: err.Error()})
		return
	}
	c.JSON(http.StatusOK, api.Diagram{Id: id})
}

func (s *Server) DeleteDiagramsId(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func (s *Server) GetDiagramsIdCollaborate(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	c.JSON(http.StatusOK, api.CollaborationSession{DiagramId: id})
}

func (s *Server) PostDiagramsIdCollaborate(c *gin.Context) {
	id, err := api.ParseUUID(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.Error{Error: "invalid_uuid", Message: "Invalid UUID format"})
		return
	}
	c.JSON(http.StatusOK, api.CollaborationSession{DiagramId: id})
}

func (s *Server) DeleteDiagramsIdCollaborate(c *gin.Context) {
	c.Status(http.StatusNoContent)
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

func main() {
	r := gin.Default()
	server := &Server{}

	r.Use(JWTMiddleware())
	api.RegisterGinHandlers(r, server)

	fmt.Println("Starting server on :8080")
	if err := r.Run(":8080"); err != nil {
		panic(err)
	}
}
