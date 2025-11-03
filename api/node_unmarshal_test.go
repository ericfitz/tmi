package api

import (
	"encoding/json"
	"testing"
)

// TestNodeUnmarshal_NestedFormat verifies that nodes can be unmarshaled from nested format
func TestNodeUnmarshal_NestedFormat(t *testing.T) {
	jsonData := []byte(`{
		"id": "12345678-1234-1234-1234-123456789012",
		"shape": "process",
		"position": {
			"x": 100,
			"y": 200
		},
		"size": {
			"width": 80,
			"height": 60
		}
	}`)

	var node Node
	err := json.Unmarshal(jsonData, &node)
	if err != nil {
		t.Fatalf("Failed to unmarshal nested format: %v", err)
	}

	// Verify internal representation
	if node.Position.X != 100 {
		t.Errorf("Expected position.x=100, got %f", node.Position.X)
	}
	if node.Position.Y != 200 {
		t.Errorf("Expected position.y=200, got %f", node.Position.Y)
	}
	if node.Size.Width != 80 {
		t.Errorf("Expected size.width=80, got %f", node.Size.Width)
	}
	if node.Size.Height != 60 {
		t.Errorf("Expected size.height=60, got %f", node.Size.Height)
	}
	if node.Shape != "process" {
		t.Errorf("Expected shape=process, got %s", node.Shape)
	}
}

// TestNodeUnmarshal_FlatFormat verifies that nodes can be unmarshaled from flat format
func TestNodeUnmarshal_FlatFormat(t *testing.T) {
	jsonData := []byte(`{
		"id": "12345678-1234-1234-1234-123456789012",
		"shape": "actor",
		"x": 150,
		"y": 250,
		"width": 90,
		"height": 70
	}`)

	var node Node
	err := json.Unmarshal(jsonData, &node)
	if err != nil {
		t.Fatalf("Failed to unmarshal flat format: %v", err)
	}

	// Verify internal representation (should normalize to nested)
	if node.Position.X != 150 {
		t.Errorf("Expected position.x=150, got %f", node.Position.X)
	}
	if node.Position.Y != 250 {
		t.Errorf("Expected position.y=250, got %f", node.Position.Y)
	}
	if node.Size.Width != 90 {
		t.Errorf("Expected size.width=90, got %f", node.Size.Width)
	}
	if node.Size.Height != 70 {
		t.Errorf("Expected size.height=70, got %f", node.Size.Height)
	}
	if node.Shape != "actor" {
		t.Errorf("Expected shape=actor, got %s", node.Shape)
	}
}

// TestNodeMarshal_AlwaysFlat verifies that nodes always marshal to flat format
func TestNodeMarshal_AlwaysFlat(t *testing.T) {
	uuid, _ := ParseUUID("12345678-1234-1234-1234-123456789012")
	node := Node{
		Id:    uuid,
		Shape: "store",
		Position: &struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{
			X: 300,
			Y: 400,
		},
		Size: &struct {
			Height float32 `json:"height"`
			Width  float32 `json:"width"`
		}{
			Width:  100,
			Height: 80,
		},
	}

	jsonData, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("Failed to marshal node: %v", err)
	}

	// Parse the JSON to verify structure
	var result map[string]interface{}
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to parse marshaled JSON: %v", err)
	}

	// Verify flat format fields are present
	if _, ok := result["x"]; !ok {
		t.Error("Expected 'x' field in output")
	}
	if _, ok := result["y"]; !ok {
		t.Error("Expected 'y' field in output")
	}
	if _, ok := result["width"]; !ok {
		t.Error("Expected 'width' field in output")
	}
	if _, ok := result["height"]; !ok {
		t.Error("Expected 'height' field in output")
	}

	// Verify nested format fields are NOT present
	if _, ok := result["position"]; ok {
		t.Error("Did not expect 'position' field in output")
	}
	if _, ok := result["size"]; ok {
		t.Error("Did not expect 'size' field in output")
	}

	// Verify values
	if result["x"].(float64) != 300 {
		t.Errorf("Expected x=300, got %v", result["x"])
	}
	if result["y"].(float64) != 400 {
		t.Errorf("Expected y=400, got %v", result["y"])
	}
	if result["width"].(float64) != 100 {
		t.Errorf("Expected width=100, got %v", result["width"])
	}
	if result["height"].(float64) != 80 {
		t.Errorf("Expected height=80, got %v", result["height"])
	}
}

// TestNodeUnmarshal_BothFormatsPreferFlat verifies that flat format is preferred when both are present
func TestNodeUnmarshal_BothFormatsPreferFlat(t *testing.T) {
	jsonData := []byte(`{
		"id": "12345678-1234-1234-1234-123456789012",
		"shape": "process",
		"x": 500,
		"y": 600,
		"width": 110,
		"height": 90,
		"position": {
			"x": 100,
			"y": 200
		},
		"size": {
			"width": 80,
			"height": 60
		}
	}`)

	var node Node
	err := json.Unmarshal(jsonData, &node)
	if err != nil {
		t.Fatalf("Failed to unmarshal mixed format: %v", err)
	}

	// Should prefer flat format values
	if node.Position.X != 500 {
		t.Errorf("Expected position.x=500 (flat), got %f", node.Position.X)
	}
	if node.Position.Y != 600 {
		t.Errorf("Expected position.y=600 (flat), got %f", node.Position.Y)
	}
	if node.Size.Width != 110 {
		t.Errorf("Expected size.width=110 (flat), got %f", node.Size.Width)
	}
	if node.Size.Height != 90 {
		t.Errorf("Expected size.height=90 (flat), got %f", node.Size.Height)
	}
}

// TestNodeUnmarshal_MissingFields verifies proper error handling for incomplete data
func TestNodeUnmarshal_MissingFields(t *testing.T) {
	testCases := []struct {
		name     string
		jsonData string
	}{
		{
			name: "Missing position and x/y",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"size": {"width": 80, "height": 60}
			}`,
		},
		{
			name: "Missing size and width/height",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"position": {"x": 100, "y": 200}
			}`,
		},
		{
			name: "Only x and y (missing width/height)",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"x": 100,
				"y": 200
			}`,
		},
		{
			name: "Only width and height (missing x/y)",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"width": 80,
				"height": 60
			}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var node Node
			err := json.Unmarshal([]byte(tc.jsonData), &node)
			if err == nil {
				t.Error("Expected error for incomplete position/size data, got nil")
			}
		})
	}
}

// TestNodeUnmarshal_ValidationErrors verifies dimension validation
func TestNodeUnmarshal_ValidationErrors(t *testing.T) {
	testCases := []struct {
		name     string
		jsonData string
		errMsg   string
	}{
		{
			name: "Width too small (nested format)",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"position": {"x": 100, "y": 200},
				"size": {"width": 30, "height": 60}
			}`,
			errMsg: "width must be at least 40",
		},
		{
			name: "Height too small (nested format)",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"position": {"x": 100, "y": 200},
				"size": {"width": 80, "height": 20}
			}`,
			errMsg: "height must be at least 30",
		},
		{
			name: "Width too small (flat format)",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"x": 100,
				"y": 200,
				"width": 35,
				"height": 60
			}`,
			errMsg: "width must be at least 40",
		},
		{
			name: "Height too small (flat format)",
			jsonData: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"x": 100,
				"y": 200,
				"width": 80,
				"height": 25
			}`,
			errMsg: "height must be at least 30",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var node Node
			err := json.Unmarshal([]byte(tc.jsonData), &node)
			if err == nil {
				t.Errorf("Expected validation error containing '%s', got nil", tc.errMsg)
			} else if err.Error() == "" {
				t.Errorf("Expected error message containing '%s', got empty error", tc.errMsg)
			}
		})
	}
}

// TestNodeRoundTrip verifies that unmarshal -> marshal produces consistent output
func TestNodeRoundTrip(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name: "Nested format input",
			input: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "process",
				"position": {"x": 100, "y": 200},
				"size": {"width": 80, "height": 60}
			}`,
		},
		{
			name: "Flat format input",
			input: `{
				"id": "12345678-1234-1234-1234-123456789012",
				"shape": "actor",
				"x": 150,
				"y": 250,
				"width": 90,
				"height": 70
			}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Unmarshal input
			var node Node
			err := json.Unmarshal([]byte(tc.input), &node)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			// Marshal back
			output, err := json.Marshal(node)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Parse output to verify it's flat format
			var result map[string]interface{}
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("Failed to parse output: %v", err)
			}

			// Verify output is always flat format
			if _, ok := result["x"]; !ok {
				t.Error("Output missing 'x' field")
			}
			if _, ok := result["y"]; !ok {
				t.Error("Output missing 'y' field")
			}
			if _, ok := result["width"]; !ok {
				t.Error("Output missing 'width' field")
			}
			if _, ok := result["height"]; !ok {
				t.Error("Output missing 'height' field")
			}
			if _, ok := result["position"]; ok {
				t.Error("Output should not have 'position' field")
			}
			if _, ok := result["size"]; ok {
				t.Error("Output should not have 'size' field")
			}
		})
	}
}

// TestNodeUnmarshal_WithOptionalFields verifies handling of optional fields
func TestNodeUnmarshal_WithOptionalFields(t *testing.T) {
	jsonData := []byte(`{
		"id": "12345678-1234-1234-1234-123456789012",
		"shape": "process",
		"x": 100,
		"y": 200,
		"width": 80,
		"height": 60,
		"angle": 45,
		"zIndex": 5,
		"visible": true,
		"parent": "87654321-4321-4321-4321-210987654321"
	}`)

	var node Node
	err := json.Unmarshal(jsonData, &node)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify optional fields
	if node.Angle == nil || *node.Angle != 45 {
		t.Error("Expected angle=45")
	}
	if node.ZIndex == nil || *node.ZIndex != 5 {
		t.Error("Expected zIndex=5")
	}
	if node.Visible == nil || *node.Visible != true {
		t.Error("Expected visible=true")
	}
	if node.Parent == nil {
		t.Error("Expected parent to be set")
	}

	// Marshal and verify optional fields are preserved
	output, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if result["angle"].(float64) != 45 {
		t.Error("Optional field 'angle' not preserved")
	}
	if result["zIndex"].(float64) != 5 {
		t.Error("Optional field 'zIndex' not preserved")
	}
	if result["visible"].(bool) != true {
		t.Error("Optional field 'visible' not preserved")
	}
}
