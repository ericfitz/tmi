package api

// InsertDiagramForTest inserts a diagram with a specific ID directly into the store
// This is only for testing purposes
// SEM@66c07d60dcc389066ea6e3a699b4bd1ca24bfb65: store a diagram with a specific test ID, overriding the generated ID (reads DB)
func InsertDiagramForTest(id string, diagram DfdDiagram) {
	// Always use the regular Create method since we only have database stores
	_, _ = DiagramStore.Create(diagram, func(d DfdDiagram, generatedId string) DfdDiagram {
		// Override the generated ID with the test ID
		parsedId, _ := ParseUUID(id)
		d.Id = &parsedId
		return d
	})
}
