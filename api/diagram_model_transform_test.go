package api

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

// TestFlattenMetadata tests metadata array to map conversion
func TestFlattenMetadata(t *testing.T) {
	t.Run("nil metadata", func(t *testing.T) {
		result := flattenMetadata(nil)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("empty metadata", func(t *testing.T) {
		metadata := []Metadata{}
		result := flattenMetadata(&metadata)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("single metadata item", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "environment", Value: "production"},
		}
		result := flattenMetadata(&metadata)
		assert.Len(t, result, 1)
		assert.Equal(t, "production", result["environment"])
	})

	t.Run("multiple metadata items", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "environment", Value: "production"},
			{Key: "region", Value: "us-west"},
			{Key: "compliance", Value: "PCI-DSS"},
		}
		result := flattenMetadata(&metadata)
		assert.Len(t, result, 3)
		assert.Equal(t, "production", result["environment"])
		assert.Equal(t, "us-west", result["region"])
		assert.Equal(t, "PCI-DSS", result["compliance"])
	})

	t.Run("duplicate keys use last value", func(t *testing.T) {
		metadata := []Metadata{
			{Key: "version", Value: "1.0"},
			{Key: "version", Value: "2.0"},
		}
		result := flattenMetadata(&metadata)
		assert.Len(t, result, 1)
		assert.Equal(t, "2.0", result["version"])
	})
}

// createTestContextWithAccept creates a gin context for testing with optional Accept header
func createTestContextWithAccept(acceptHeader string) *gin.Context {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)
	if acceptHeader != "" {
		c.Request.Header.Set("Accept", acceptHeader)
	}
	return c
}

// TestNegotiateFormat tests format negotiation with query param and Accept header
func TestNegotiateFormat(t *testing.T) {
	t.Run("nil format and no Accept header defaults to json", func(t *testing.T) {
		c := createTestContextWithAccept("")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("query param json format", func(t *testing.T) {
		c := createTestContextWithAccept("")
		format := GetDiagramModelParamsFormat("json")
		result, err := negotiateFormat(c, &format)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("query param yaml format", func(t *testing.T) {
		c := createTestContextWithAccept("")
		format := GetDiagramModelParamsFormat("yaml")
		result, err := negotiateFormat(c, &format)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("query param graphml format", func(t *testing.T) {
		c := createTestContextWithAccept("")
		format := GetDiagramModelParamsFormat("graphml")
		result, err := negotiateFormat(c, &format)
		assert.NoError(t, err)
		assert.Equal(t, "graphml", result)
	})

	t.Run("query param uppercase JSON", func(t *testing.T) {
		c := createTestContextWithAccept("")
		format := GetDiagramModelParamsFormat("JSON")
		result, err := negotiateFormat(c, &format)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("query param mixed case YAML", func(t *testing.T) {
		c := createTestContextWithAccept("")
		format := GetDiagramModelParamsFormat("YaML")
		result, err := negotiateFormat(c, &format)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("invalid query param format", func(t *testing.T) {
		c := createTestContextWithAccept("")
		format := GetDiagramModelParamsFormat("pdf")
		_, err := negotiateFormat(c, &format)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid format parameter")
	})

	// Accept header tests
	t.Run("Accept header application/json", func(t *testing.T) {
		c := createTestContextWithAccept("application/json")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("Accept header application/x-yaml", func(t *testing.T) {
		c := createTestContextWithAccept("application/x-yaml")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("Accept header application/yaml", func(t *testing.T) {
		c := createTestContextWithAccept("application/yaml")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("Accept header text/yaml", func(t *testing.T) {
		c := createTestContextWithAccept("text/yaml")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("Accept header application/xml", func(t *testing.T) {
		c := createTestContextWithAccept("application/xml")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "graphml", result)
	})

	t.Run("Accept header application/graphml+xml", func(t *testing.T) {
		c := createTestContextWithAccept("application/graphml+xml")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "graphml", result)
	})

	t.Run("Accept header */* defaults to json", func(t *testing.T) {
		c := createTestContextWithAccept("*/*")
		result, err := negotiateFormat(c, nil)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("unsupported Accept header", func(t *testing.T) {
		c := createTestContextWithAccept("application/pdf")
		_, err := negotiateFormat(c, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported Accept header")
	})

	t.Run("query param takes precedence over Accept header", func(t *testing.T) {
		c := createTestContextWithAccept("application/xml") // Would return graphml
		format := GetDiagramModelParamsFormat("yaml")
		result, err := negotiateFormat(c, &format)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result) // Query param wins
	})
}

// TestSerializeAsYAML tests YAML serialization
func TestSerializeAsYAML(t *testing.T) {
	t.Run("minimal model to YAML", func(t *testing.T) {
		tmID := uuid.New()
		model := MinimalDiagramModel{
			Id:          tmID,
			Name:        "Test Threat Model",
			Description: "Test description",
			Metadata:    map[string]string{"env": "test"},
			Cells:       []MinimalCell{},
			Assets:      []Asset{},
		}

		yamlBytes, err := serializeAsYAML(model)
		assert.NoError(t, err)
		assert.NotEmpty(t, yamlBytes)

		// Verify it's valid YAML by parsing it back
		var parsed map[string]any
		err = yaml.Unmarshal(yamlBytes, &parsed)
		assert.NoError(t, err)
		assert.Equal(t, "Test Threat Model", parsed["name"])
		assert.Equal(t, "Test description", parsed["description"])
	})
}

// TestSerializeAsGraphML tests GraphML XML serialization
func TestSerializeAsGraphML(t *testing.T) {
	t.Run("minimal model to GraphML", func(t *testing.T) {
		tmID := uuid.New()
		model := MinimalDiagramModel{
			Id:          tmID,
			Name:        "Test Threat Model",
			Description: "Test description",
			Metadata:    map[string]string{"env": "test"},
			Cells:       []MinimalCell{},
			Assets:      []Asset{},
		}

		graphmlBytes, err := serializeAsGraphML(model)
		assert.NoError(t, err)
		assert.NotEmpty(t, graphmlBytes)

		// Verify it's valid XML
		var parsed GraphML
		err = xml.Unmarshal(graphmlBytes, &parsed)
		assert.NoError(t, err)
		assert.Equal(t, "http://graphml.graphdrawing.org/xmlns", parsed.XMLNS)
	})
}

// TestBuildMinimalDiagramModel tests full transformation pipeline
func TestBuildMinimalDiagramModel(t *testing.T) {
	t.Run("simple transformation", func(t *testing.T) {
		// Create threat model
		tmID := uuid.New()
		tm := ThreatModel{
			Id:          &tmID,
			Name:        "Payment System",
			Description: new("Payment processing threat model"),
			Metadata: &[]Metadata{
				{Key: "environment", Value: "production"},
				{Key: "compliance", Value: "PCI-DSS"},
			},
		}

		// Create simple diagram
		diagramID := uuid.New()
		diagram := DfdDiagram{
			Id:    &diagramID,
			Name:  "Payment Flow",
			Cells: []DfdDiagram_Cells_Item{},
		}

		// Transform (nil asset store - no assets to fetch)
		result := buildMinimalDiagramModel(context.Background(), tm, diagram, nil)

		// Verify threat model fields
		assert.Equal(t, tmID, result.Id)
		assert.Equal(t, "Payment System", result.Name)
		assert.Equal(t, "Payment processing threat model", result.Description)
		assert.Len(t, result.Metadata, 2)
		assert.Equal(t, "production", result.Metadata["environment"])
		assert.Equal(t, "PCI-DSS", result.Metadata["compliance"])

		// Verify cells array is empty (no cells provided)
		assert.Empty(t, result.Cells)

		// Verify assets array is empty (no assets referenced)
		assert.Empty(t, result.Assets)
	})
}
