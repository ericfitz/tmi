package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1219: a diagram stored with an edge whose data._metadata holds
// {"key":"port","value":""} (Metadata.value has minLength 1) could never be
// saved again, because PATCH validates cells against the schema and the write
// path that stored them did not. Every cell write path must apply the same
// constraint.

// edgeWithMetadata builds a flow edge carrying the given _metadata entries.
func edgeWithMetadata(t *testing.T, id string, md []Metadata) DfdDiagram_Cells_Item {
	t.Helper()
	item, err := CreateEdge(id, EdgeShapeFlow, uuid.New().String(), uuid.New().String())
	require.NoError(t, err)
	edge, err := item.AsEdge()
	require.NoError(t, err)
	edge.Data = &CellData{UnderscoreMetadata: &md}
	require.NoError(t, SafeFromEdge(&item, edge))
	return item
}

var emptyPortMetadata = []Metadata{
	{Key: "protocol", Value: "internal"},
	{Key: "port", Value: ""},
}

func TestValidateCellSchema(t *testing.T) {
	id := uuid.New().String()

	t.Run("rejects empty metadata value", func(t *testing.T) {
		err := validateCellSchema(edgeWithMetadata(t, id, emptyPortMetadata))
		require.Error(t, err)
	})

	t.Run("accepts valid edge", func(t *testing.T) {
		err := validateCellSchema(edgeWithMetadata(t, id, []Metadata{{Key: "port", Value: "443"}}))
		require.NoError(t, err)
	})

	t.Run("accepts valid node", func(t *testing.T) {
		node, err := CreateNode(id, NodeShapeProcess, 10, 10, 120, 80)
		require.NoError(t, err)
		require.NoError(t, validateCellSchema(node))
	})

	t.Run("rejects unknown shape", func(t *testing.T) {
		var item DfdDiagram_Cells_Item
		require.NoError(t, item.UnmarshalJSON([]byte(`{"id":"`+id+`","shape":"bogus"}`)))
		require.Error(t, validateCellSchema(item))
	})
}

func TestCellOperationProcessor_RejectsSchemaInvalidCell(t *testing.T) {
	existingID := uuid.New().String()
	existing := edgeWithMetadata(t, existingID, []Metadata{{Key: "port", Value: "443"}})

	for _, op := range []string{"add", cellOperationTypeUpdate} {
		t.Run(op, func(t *testing.T) {
			diagramID := NewUUID()
			diagram := &DfdDiagram{Id: &diagramID, Cells: []DfdDiagram_Cells_Item{existing}}
			currentState := map[string]*DfdDiagram_Cells_Item{existingID: &diagram.Cells[0]}

			cellID := existingID
			if op == "add" {
				cellID = uuid.New().String()
			}
			bad := edgeWithMetadata(t, cellID, emptyPortMetadata)

			processor := &CellOperationProcessor{}
			result := processor.validateAndProcessCellOperation(diagram, currentState, CellOperation{ID: cellID, Operation: op, Data: &bad})

			assert.False(t, result.Valid)
			assert.Equal(t, wsReasonCellSchemaInvalid, result.Reason)
			assert.False(t, result.StateChanged)
			require.Len(t, diagram.Cells, 1, "diagram must be unchanged")
			stored, err := diagram.Cells[0].AsEdge()
			require.NoError(t, err)
			assert.Equal(t, "443", (*stored.Data.UnderscoreMetadata)[0].Value)
		})
	}
}

func TestDiagramSession_RejectsSchemaInvalidCell(t *testing.T) {
	existingID := uuid.New().String()
	existing := edgeWithMetadata(t, existingID, []Metadata{{Key: "port", Value: "443"}})

	for _, op := range []string{"add", cellOperationTypeUpdate} {
		t.Run(op, func(t *testing.T) {
			diagramID := NewUUID()
			diagram := &DfdDiagram{Id: &diagramID, Cells: []DfdDiagram_Cells_Item{existing}}
			currentState := map[string]*DfdDiagram_Cells_Item{existingID: &diagram.Cells[0]}

			cellID := existingID
			if op == "add" {
				cellID = uuid.New().String()
			}
			bad := edgeWithMetadata(t, cellID, emptyPortMetadata)

			// No Hub: an invalid cell must be rejected before any save is attempted.
			session := &DiagramSession{}
			result := session.validateAndProcessCellOperation(diagram, currentState, CellOperation{ID: cellID, Operation: op, Data: &bad})

			assert.False(t, result.Valid)
			assert.Equal(t, wsReasonCellSchemaInvalid, result.Reason)
			require.Len(t, diagram.Cells, 1, "diagram must be unchanged")
		})
	}
}

// TestOpenAPIValidation_PutDiagramRejectsEmptyMetadataValue pins the REST PUT
// path, which the OpenAPI request validator already covers.
func TestOpenAPIValidation_PutDiagramRejectsEmptyMetadataValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	validator, err := SetupOpenAPIValidation()
	require.NoError(t, err)
	r := gin.New()
	r.Use(validator)
	r.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

	edge := edgeWithMetadata(t, uuid.New().String(), emptyPortMetadata)
	cellJSON, err := edge.MarshalJSON()
	require.NoError(t, err)
	body := `{"name":"D","type":"DFD-1.0.0","cells":[` + string(cellJSON) + `]}`

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/threat_models/"+testUUID1+"/diagrams/"+uuid.New().String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}
