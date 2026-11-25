package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/getkin/kin-openapi/openapi3"
)

// Cell schema validation for write paths that do not pass through the OpenAPI
// request validator (WebSocket cell add/update). REST PUT is checked by the
// request validator and PATCH by validatePatchedFields; this applies the same
// Node/Edge component schemas, with the same pruning of members the schema does
// not declare, so a cell one path accepts is never rejected by another (#1219).

var (
	cellSchemaOnce sync.Once
	cellNodeSchema *openapi3.Schema
	cellEdgeSchema *openapi3.Schema
	cellSchemaErr  error
)

func loadCellSchemas() error {
	cellSchemaOnce.Do(func() {
		swagger, err := GetSwagger()
		if err != nil {
			cellSchemaErr = fmt.Errorf("embedded OpenAPI spec unavailable for cell validation: %w", err)
			return
		}
		for name, dst := range map[string]**openapi3.Schema{"Node": &cellNodeSchema, "Edge": &cellEdgeSchema} {
			ref, ok := swagger.Components.Schemas[name]
			if !ok || ref.Value == nil {
				cellSchemaErr = fmt.Errorf("schema %q not found for cell validation", name)
				return
			}
			*dst = ref.Value
		}
	})
	return cellSchemaErr
}

// shapeEnumContains reports whether the schema's shape property lists shape.
func shapeEnumContains(s *openapi3.Schema, shape string) bool {
	p := patchSchemaProperty(s, "shape", 0)
	if p == nil || p.Value == nil {
		return false
	}
	return slices.Contains(p.Value.Enum, any(shape))
}

// validateCellSchema checks one diagram cell against the Node or Edge schema
// selected by its shape. An unknown shape is rejected.
func validateCellSchema(cell DfdDiagram_Cells_Item) error {
	if err := loadCellSchemas(); err != nil {
		return err
	}
	shape, err := cell.Discriminator()
	if err != nil {
		return fmt.Errorf("cell has no readable shape: %w", err)
	}
	var schema *openapi3.Schema
	switch {
	case shapeEnumContains(cellEdgeSchema, shape):
		schema = cellEdgeSchema
	case shapeEnumContains(cellNodeSchema, shape):
		schema = cellNodeSchema
	default:
		return fmt.Errorf("unknown cell shape %q", shape)
	}
	raw, err := cell.MarshalJSON()
	if err != nil {
		return fmt.Errorf("cell cannot be serialized: %w", err)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("cell is not a JSON object: %w", err)
	}
	if err := schema.VisitJSON(pruneToSchema(value, schema, 0)); err != nil {
		return fmt.Errorf("cell does not match the %s schema: %s", shape, patchSchemaReason(err))
	}
	return nil
}

// rejectSchemaInvalidCell returns a failed result when an add or update
// operation carries a cell that violates its schema, or nil to proceed.
func rejectSchemaInvalidCell(cellOp CellOperation) *OperationValidationResult {
	if cellOp.Data == nil || (cellOp.Operation != "add" && cellOp.Operation != cellOperationTypeUpdate) {
		return nil
	}
	if err := validateCellSchema(*cellOp.Data); err != nil {
		slogging.Get().Info("Rejected WebSocket cell %s operation for cell %s: %v", cellOp.Operation, cellOp.ID, err)
		return &OperationValidationResult{
			Valid:         false,
			Reason:        wsReasonCellSchemaInvalid,
			CellsModified: []string{cellOp.ID},
		}
	}
	return nil
}
