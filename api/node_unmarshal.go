package api

import (
	"encoding/json"
	"fmt"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// UnmarshalJSON implements custom unmarshaling for Node to support both
// nested format (position/size objects) and flat format (x/y/width/height).
// This allows the API to accept both AntV/X6 formats.
func (n *Node) UnmarshalJSON(data []byte) error {
	// Use a temporary struct that can hold both formats
	type NodeTemp struct {
		// Flat format fields (Format 2)
		X      *float32 `json:"x"`
		Y      *float32 `json:"y"`
		Width  *float32 `json:"width"`
		Height *float32 `json:"height"`

		// Nested format fields (Format 1)
		Position *struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		} `json:"position"`
		Size *struct {
			Width  float32 `json:"width"`
			Height float32 `json:"height"`
		} `json:"size"`

		// All other Node fields
		Angle   *float32            `json:"angle,omitempty"`
		Attrs   *NodeAttrs          `json:"attrs,omitempty"`
		Data    *Node_Data          `json:"data,omitempty"`
		Id      openapi_types.UUID  `json:"id"`
		Markup  *[]MarkupElement    `json:"markup,omitempty"`
		Parent  *openapi_types.UUID `json:"parent"`
		Ports   *PortConfiguration  `json:"ports,omitempty"`
		Shape   NodeShape           `json:"shape"`
		Tools   *[]CellTool         `json:"tools,omitempty"`
		Visible *bool               `json:"visible,omitempty"`
		ZIndex  *float32            `json:"zIndex,omitempty"`
	}

	var temp NodeTemp
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to unmarshal node: %w", err)
	}

	// Determine format and populate position/size
	hasFlat := temp.X != nil && temp.Y != nil && temp.Width != nil && temp.Height != nil
	hasNested := temp.Position != nil && temp.Size != nil

	// Initialize Position and Size pointers
	n.Position = &struct {
		X float32 `json:"x"`
		Y float32 `json:"y"`
	}{}
	n.Size = &struct {
		Height float32 `json:"height"`
		Width  float32 `json:"width"`
	}{}

	if hasFlat && hasNested {
		// Both formats provided - prefer flat format
		n.Position.X = *temp.X
		n.Position.Y = *temp.Y
		n.Size.Width = *temp.Width
		n.Size.Height = *temp.Height
	} else if hasFlat {
		// Flat format only
		n.Position.X = *temp.X
		n.Position.Y = *temp.Y
		n.Size.Width = *temp.Width
		n.Size.Height = *temp.Height
	} else if hasNested {
		// Nested format only
		n.Position.X = temp.Position.X
		n.Position.Y = temp.Position.Y
		n.Size.Width = temp.Size.Width
		n.Size.Height = temp.Size.Height
	} else {
		return fmt.Errorf("node must have either x/y/width/height properties or position/size objects")
	}

	// Validate dimensions
	if n.Size.Width < 40 {
		return fmt.Errorf("node width must be at least 40 pixels, got %.1f", n.Size.Width)
	}
	if n.Size.Height < 30 {
		return fmt.Errorf("node height must be at least 30 pixels, got %.1f", n.Size.Height)
	}

	// Populate all other fields
	n.Angle = temp.Angle
	n.Attrs = temp.Attrs
	n.Data = temp.Data
	n.Id = temp.Id
	n.Markup = temp.Markup
	n.Parent = temp.Parent
	n.Ports = temp.Ports
	n.Shape = temp.Shape
	n.Tools = temp.Tools
	n.Visible = temp.Visible
	n.ZIndex = temp.ZIndex

	return nil
}

// MarshalJSON implements custom marshaling for Node to always output
// flat format (x, y, width, height) as per AntV/X6 Format 2.
func (n Node) MarshalJSON() ([]byte, error) {
	// Create a temporary struct for flat format output
	type NodeFlat struct {
		Angle   *float32            `json:"angle,omitempty"`
		Attrs   *NodeAttrs          `json:"attrs,omitempty"`
		Data    *Node_Data          `json:"data,omitempty"`
		Height  float32             `json:"height"`
		Id      openapi_types.UUID  `json:"id"`
		Markup  *[]MarkupElement    `json:"markup,omitempty"`
		Parent  *openapi_types.UUID `json:"parent"`
		Ports   *PortConfiguration  `json:"ports,omitempty"`
		Shape   NodeShape           `json:"shape"`
		Tools   *[]CellTool         `json:"tools,omitempty"`
		Visible *bool               `json:"visible,omitempty"`
		Width   float32             `json:"width"`
		X       float32             `json:"x"`
		Y       float32             `json:"y"`
		ZIndex  *float32            `json:"zIndex,omitempty"`
	}

	// Handle nil pointers with defaults
	var height, width, x, y float32
	if n.Size != nil {
		height = n.Size.Height
		width = n.Size.Width
	}
	if n.Position != nil {
		x = n.Position.X
		y = n.Position.Y
	}

	flat := NodeFlat{
		Angle:   n.Angle,
		Attrs:   n.Attrs,
		Data:    n.Data,
		Height:  height,
		Id:      n.Id,
		Markup:  n.Markup,
		Parent:  n.Parent,
		Ports:   n.Ports,
		Shape:   n.Shape,
		Tools:   n.Tools,
		Visible: n.Visible,
		Width:   width,
		X:       x,
		Y:       y,
		ZIndex:  n.ZIndex,
	}

	return json.Marshal(flat)
}
