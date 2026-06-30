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
	// Content negotiation is now driven solely by the Accept header against the
	// canonical (modernized) media types: application/json (default),
	// application/yaml, application/graphml+xml. The legacy ?format query param
	// and the old synonym media types (application/x-yaml, application/xml,
	// text/yaml, ...) are intentionally no longer accepted.

	t.Run("no Accept header defaults to json", func(t *testing.T) {
		c := createTestContextWithAccept("")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("Accept */* defaults to json", func(t *testing.T) {
		c := createTestContextWithAccept("*/*")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("Accept application/json", func(t *testing.T) {
		c := createTestContextWithAccept("application/json")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("Accept application/yaml", func(t *testing.T) {
		c := createTestContextWithAccept("application/yaml")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("Accept application/graphml+xml", func(t *testing.T) {
		c := createTestContextWithAccept("application/graphml+xml")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "graphml", result)
	})

	t.Run("q-values select the highest acceptable type", func(t *testing.T) {
		c := createTestContextWithAccept("application/json;q=0.5, application/yaml;q=0.9")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("application/* wildcard matches the default (json)", func(t *testing.T) {
		c := createTestContextWithAccept("application/*")
		result, err := negotiateFormat(c)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("legacy application/x-yaml is no longer accepted", func(t *testing.T) {
		c := createTestContextWithAccept("application/x-yaml")
		_, err := negotiateFormat(c)
		assert.Error(t, err)
	})

	t.Run("generic application/xml is no longer accepted", func(t *testing.T) {
		c := createTestContextWithAccept("application/xml")
		_, err := negotiateFormat(c)
		assert.Error(t, err)
	})

	t.Run("unsupported Accept yields an error (406)", func(t *testing.T) {
		c := createTestContextWithAccept("application/pdf")
		_, err := negotiateFormat(c)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no acceptable response media type")
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
