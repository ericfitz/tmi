package api

import (
	"context"
	"encoding/xml"
	"testing"

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

// TestParseFormat tests format parameter parsing
func TestParseFormat(t *testing.T) {
	t.Run("nil format defaults to json", func(t *testing.T) {
		result, err := parseFormat(nil)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("json format", func(t *testing.T) {
		format := GetDiagramModelParamsFormat("json")
		result, err := parseFormat(&format)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("yaml format", func(t *testing.T) {
		format := GetDiagramModelParamsFormat("yaml")
		result, err := parseFormat(&format)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("graphml format", func(t *testing.T) {
		format := GetDiagramModelParamsFormat("graphml")
		result, err := parseFormat(&format)
		assert.NoError(t, err)
		assert.Equal(t, "graphml", result)
	})

	t.Run("uppercase JSON", func(t *testing.T) {
		format := GetDiagramModelParamsFormat("JSON")
		result, err := parseFormat(&format)
		assert.NoError(t, err)
		assert.Equal(t, "json", result)
	})

	t.Run("mixed case YAML", func(t *testing.T) {
		format := GetDiagramModelParamsFormat("YaML")
		result, err := parseFormat(&format)
		assert.NoError(t, err)
		assert.Equal(t, "yaml", result)
	})

	t.Run("invalid format", func(t *testing.T) {
		format := GetDiagramModelParamsFormat("xml")
		_, err := parseFormat(&format)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid format parameter")
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
		var parsed map[string]interface{}
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
			Description: stringPointer("Payment processing threat model"),
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
