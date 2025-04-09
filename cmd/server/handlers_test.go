package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	server := &Server{}
	api.RegisterGinHandlers(r, server)
	return r
}

func TestGetDiagrams(t *testing.T) {
	// Setup
	r := setupTestRouter()
	
	// Create test request
	req, _ := http.NewRequest("GET", "/diagrams", nil)
	w := httptest.NewRecorder()
	
	// Add test auth token (for testing only)
	req.Header.Add("Authorization", "Bearer test-token")
	
	// Serve the request
	r.ServeHTTP(w, req)
	
	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)
	
	// Check response body contains expected data
	var response []api.ListItem
	err := json.Unmarshal(w.Body.Bytes(), &response)
	
	assert.NoError(t, err)
	assert.NotEmpty(t, response)
	assert.Equal(t, "Workflow Diagram", response[0].Name)
}

func TestPostDiagrams(t *testing.T) {
	// Setup
	r := setupTestRouter()
	
	// Test request body
	diagram := api.DiagramRequest{
		Name:        "Test Diagram",
		Description: stringPtr("Test diagram description"),
	}
	
	body, _ := json.Marshal(diagram)
	req, _ := http.NewRequest("POST", "/diagrams", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer test-token")
	
	w := httptest.NewRecorder()
	
	// Serve the request
	r.ServeHTTP(w, req)
	
	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code)
	
	// Verify the response contains the created diagram
	var response api.Diagram
	err := json.Unmarshal(w.Body.Bytes(), &response)
	
	assert.NoError(t, err)
	assert.Equal(t, diagram.Name, response.Name)
	assert.Equal(t, *diagram.Description, *response.Description)
	assert.NotEmpty(t, response.Id)
}

func TestGetDiagramsId(t *testing.T) {
	// Setup
	r := setupTestRouter()
	
	// Create a test UUID
	id := "123e4567-e89b-12d3-a456-426614174000"
	
	// Create test request
	req, _ := http.NewRequest("GET", "/diagrams/"+id, nil)
	req.Header.Add("Authorization", "Bearer test-token")
	
	w := httptest.NewRecorder()
	
	// Serve the request
	r.ServeHTTP(w, req)
	
	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)
	
	// Check response body
	var response api.Diagram
	err := json.Unmarshal(w.Body.Bytes(), &response)
	
	assert.NoError(t, err)
	assert.Equal(t, id, response.Id.String())
}

// Helper function to create a string pointer
func stringPtr(s string) *string {
	return &s
}