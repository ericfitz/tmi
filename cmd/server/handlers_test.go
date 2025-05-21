package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Add a fake auth middleware to set user in context
	r.Use(func(c *gin.Context) {
		// Extract token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && authHeader == "Bearer test-token" {
			// Set a test username in the context
			c.Set("userName", "test@example.com")
		}
		c.Next()
	})

	server := &Server{}

	// Initialize test fixtures
	api.InitTestFixtures()

	// Set the owner in test fixtures to match our test user
	api.TestFixtures.Owner = "test@example.com"

	// Create a test diagram with the expected name
	now := time.Now().UTC()
	cells := []api.Cell{}
	metadata := []api.Metadata{}

	diagram := api.Diagram{
		Id:          api.NewUUID(),
		Name:        "Workflow Diagram",
		Description: stringPtr("This is a workflow diagram"),
		CreatedAt:   now,
		ModifiedAt:  now,
		GraphData:   &cells,
		Metadata:    &metadata,
	}

	// Add the diagram to the store using the Create method
	idSetter := func(d api.Diagram, id string) api.Diagram {
		uuid, _ := api.ParseUUID(id)
		d.Id = uuid
		return d
	}
	_, err := api.DiagramStore.Create(diagram, idSetter)
	if err != nil {
		panic("Failed to create test diagram: " + err.Error())
	}

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
	// The first diagram will be the one created by TestPostDiagrams
	assert.Equal(t, "Test Diagram", response[0].Name)
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

	// First, create a new diagram
	diagramReq := api.DiagramRequest{
		Name:        "Test Diagram for GetById",
		Description: stringPtr("This is a test diagram for GetById"),
	}

	body, _ := json.Marshal(diagramReq)
	createReq, _ := http.NewRequest("POST", "/diagrams", bytes.NewBuffer(body))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Add("Authorization", "Bearer test-token")

	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	// Verify creation was successful
	assert.Equal(t, http.StatusCreated, createW.Code)

	// Extract the ID of the created diagram
	var createdDiagram api.Diagram
	if err := json.Unmarshal(createW.Body.Bytes(), &createdDiagram); err != nil {
		t.Fatalf("Failed to unmarshal created diagram: %v", err)
	}
	id := createdDiagram.Id.String()

	// Now test getting the diagram by ID
	getReq, _ := http.NewRequest("GET", "/diagrams/"+id, nil)
	getReq.Header.Add("Authorization", "Bearer test-token")

	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusOK, getW.Code)

	// Check response body
	var response api.Diagram
	unmarshalErr := json.Unmarshal(getW.Body.Bytes(), &response)

	assert.NoError(t, unmarshalErr)
	assert.Equal(t, id, response.Id.String())
	assert.Equal(t, diagramReq.Name, response.Name)
	assert.Equal(t, *diagramReq.Description, *response.Description)
}

// Helper function to create a string pointer
func stringPtr(s string) *string {
	return &s
}
