package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	cells := []api.DfdDiagram_Cells_Item{}
	metadata := []api.Metadata{}

	uuid := api.NewUUID()
	diagram := api.DfdDiagram{
		Id:          &uuid,
		Name:        "Workflow Diagram",
		Description: stringPtr("This is a workflow diagram"),
		CreatedAt:   now,
		ModifiedAt:  now,
		Cells:       cells,
		Metadata:    &metadata,
	}

	// Add the diagram to the store using the Create method
	idSetter := func(d api.DfdDiagram, id string) api.DfdDiagram {
		uuid, _ := api.ParseUUID(id)
		d.Id = &uuid
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

	// Check that we have at least one diagram and that the "Workflow Diagram" exists
	// (which is created by setupTestRouter)
	found := false
	for _, item := range response {
		if item.Name == "Workflow Diagram" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should contain the 'Workflow Diagram' created by setupTestRouter")
}

func TestPostDiagrams(t *testing.T) {
	// Setup
	r := setupTestRouter()

	// Step 1: Create a threat model first
	threatModel := api.PostThreatModelsJSONBody{
		Name:        "Test Threat Model for Diagram",
		Description: stringPtr("Test threat model for diagram creation"),
	}

	tmBody, _ := json.Marshal(threatModel)
	tmReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmReq.Header.Add("Authorization", "Bearer test-token")

	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)

	require.Equal(t, http.StatusCreated, tmW.Code, "Failed to create threat model: %s", tmW.Body.String())

	var tmResponse api.ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tmResponse)
	require.NoError(t, err)
	threatModelID := tmResponse.Id.String()

	// Step 2: Create diagram through threat model sub-entity endpoint
	diagram := api.PostThreatModelsThreatModelIdDiagramsJSONBody{
		Name:        "Test Diagram",
		Description: stringPtr("Test diagram description"),
	}

	body, _ := json.Marshal(diagram)
	diagramURL := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
	req, _ := http.NewRequest("POST", diagramURL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()

	// Serve the request
	r.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify the response contains the created diagram
	var responseUnion api.Diagram
	err = json.Unmarshal(w.Body.Bytes(), &responseUnion)
	assert.NoError(t, err)

	// Convert union type to DfdDiagram for field access
	response, err := responseUnion.AsDfdDiagram()
	assert.NoError(t, err)

	assert.Equal(t, diagram.Name, response.Name)
	assert.Equal(t, *diagram.Description, *response.Description)
	assert.NotEmpty(t, response.Id)
}

func TestGetDiagramsId(t *testing.T) {
	// Setup
	r := setupTestRouter()

	// Step 1: Create a threat model first
	threatModel := api.PostThreatModelsJSONBody{
		Name:        "Test Threat Model for GetById",
		Description: stringPtr("Test threat model for diagram retrieval"),
	}

	tmBody, _ := json.Marshal(threatModel)
	tmReq, _ := http.NewRequest("POST", "/threat_models", bytes.NewBuffer(tmBody))
	tmReq.Header.Set("Content-Type", "application/json")
	tmReq.Header.Add("Authorization", "Bearer test-token")

	tmW := httptest.NewRecorder()
	r.ServeHTTP(tmW, tmReq)

	require.Equal(t, http.StatusCreated, tmW.Code, "Failed to create threat model: %s", tmW.Body.String())

	var tmResponse api.ThreatModel
	err := json.Unmarshal(tmW.Body.Bytes(), &tmResponse)
	require.NoError(t, err)
	threatModelID := tmResponse.Id.String()

	// Step 2: Create a new diagram through sub-entity endpoint
	diagramReq := api.PostThreatModelsThreatModelIdDiagramsJSONBody{
		Name:        "Test Diagram for GetById",
		Description: stringPtr("This is a test diagram for GetById"),
	}

	body, _ := json.Marshal(diagramReq)
	diagramURL := fmt.Sprintf("/threat_models/%s/diagrams", threatModelID)
	createReq, _ := http.NewRequest("POST", diagramURL, bytes.NewBuffer(body))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Add("Authorization", "Bearer test-token")

	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	// Verify creation was successful
	assert.Equal(t, http.StatusCreated, createW.Code)

	// Extract the ID of the created diagram
	var createdDiagramUnion api.Diagram
	if err := json.Unmarshal(createW.Body.Bytes(), &createdDiagramUnion); err != nil {
		t.Fatalf("Failed to unmarshal created diagram: %v", err)
	}
	createdDiagram, err := createdDiagramUnion.AsDfdDiagram()
	if err != nil {
		t.Fatalf("Failed to convert created diagram: %v", err)
	}
	id := createdDiagram.Id.String()

	// Step 3: Test getting the diagram by ID through sub-entity endpoint
	getDiagramURL := fmt.Sprintf("/threat_models/%s/diagrams/%s", threatModelID, id)
	getReq, _ := http.NewRequest("GET", getDiagramURL, nil)
	getReq.Header.Add("Authorization", "Bearer test-token")

	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)

	// Assert response
	assert.Equal(t, http.StatusOK, getW.Code)

	// Check response body
	var responseUnion api.Diagram
	unmarshalErr := json.Unmarshal(getW.Body.Bytes(), &responseUnion)
	assert.NoError(t, unmarshalErr)

	// Convert union type to DfdDiagram for field access
	response, err := responseUnion.AsDfdDiagram()
	assert.NoError(t, err)

	assert.Equal(t, id, response.Id.String())
	assert.Equal(t, diagramReq.Name, response.Name)
	assert.Equal(t, *diagramReq.Description, *response.Description)
}

// Helper function to create a string pointer
func stringPtr(s string) *string {
	return &s
}
